//go:build windows

package browserhost

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	wv "github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost/internal/webviewabi"
	"golang.org/x/sys/windows"
)

func TestDownloadDestinationValidation(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "approved.bin")
	if got, err := validateDownloadDestination(valid); err != nil || got != valid {
		t.Fatalf("valid path: %q %v", got, err)
	}
	for _, path := range []string{
		"", "relative.bin", `C:relative.bin`, `\\server\share\file.bin`, `\\?\C:\file.bin`, `\\.\NUL`,
		filepath.Join(dir, "file.bin") + ":stream", dir + `\..\escape.bin`, dir + `\CON.txt`,
		dir + `\NUL`, dir + `\COM1.bin`, dir + `\LPT9`, dir + `\bad.`, dir + `\bad `,
		dir + `\bad?`, dir + "\\bad\x00.bin", dir + `\missing\file.bin`, dir,
	} {
		if _, err := validateDownloadDestination(path); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	if err := os.WriteFile(valid, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateDownloadDestination(valid); err == nil {
		t.Fatal("accepted existing destination")
	}
	data, err := os.ReadFile(valid)
	if err != nil || string(data) != "existing" {
		t.Fatalf("validation changed file: %s %v", data, err)
	}
}

func TestDownloadCancelledContextDoesNotStartHost(t *testing.T) {
	h := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, op := range []string{"download-list", "download-approve", "download-cancel-pending"} {
		_, err := h.transferCommand(ctx, "tab", op, `{"id":"1","path":"C:\\never-write.bin"}`)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%s: %v", op, err)
		}
	}
	if h.started.Load() {
		t.Fatal("cancelled command started native host")
	}
}

// This suite uses a hidden window, a new profile and loopback fixtures only. The
// second DownloadStarting handler redirects the engine's fallback path into a
// test directory WITHOUT approving it, so a regression cannot write Downloads.
func TestNativeDownloadTransfers(t *testing.T) {
	if os.Getenv("ORCA_BROWSER_NATIVE_TEST") != "1" {
		t.Skip("isolated WebView2 test is opt-in")
	}
	if os.Getenv("ORCA_BROWSER_TRANSFER_TRACE") == "1" {
		logs := t.TempDir()
		logPath := filepath.Join(logs, "engine.log")
		netPath := filepath.Join(logs, "network.json")
		// Test-process-only logging. Do not change any security feature flags.
		t.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", fmt.Sprintf(`--enable-logging --v=1 --log-file="%s" --log-net-log="%s"`, logPath, netPath))
		t.Cleanup(func() {
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Logf("diagnostic log: %v", err)
				return
			}
			for _, line := range strings.Split(string(data), "\n") {
				lower := strings.ToLower(line)
				if strings.Contains(lower, "download") || strings.Contains(lower, "smartscreen") || strings.Contains(lower, "reputation") || strings.Contains(lower, "timeout") {
					t.Logf("engine: %s", line)
				}
			}
			data, err = os.ReadFile(netPath)
			if err != nil {
				t.Logf("network diagnostic: %v", err)
				return
			}
			var netlog struct {
				Events []struct {
					Time   string `json:"time"`
					Type   int    `json:"type"`
					Phase  int    `json:"phase"`
					Source struct {
						ID int `json:"id"`
					} `json:"source"`
					Params map[string]any `json:"params"`
				} `json:"events"`
				Constants struct {
					LogEventTypes map[string]int `json:"logEventTypes"`
				} `json:"constants"`
			}
			if err := json.Unmarshal(data, &netlog); err != nil {
				t.Logf("parse network diagnostic: %v", err)
				return
			}
			names := make(map[int]string)
			for name, id := range netlog.Constants.LogEventTypes {
				names[id] = name
			}
			for _, event := range netlog.Events {
				name := names[event.Type]
				if name == "URL_REQUEST_START_JOB" || name == "URL_REQUEST_DELEGATE" || strings.Contains(name, "DOWNLOAD") {
					t.Logf("net: time=%s source=%d event=%s phase=%d params=%v", event.Time, event.Source.ID, name, event.Phase, event.Params)
				}
			}
		})
	}
	payload := bytes.Repeat([]byte("ORCA isolated transfer fixture\n"), 8192)
	begin := time.Now()
	var traceMu sync.Mutex
	lastTrace := make(map[string]string)
	trace := func(format string, args ...any) {
		t.Logf("+%s %s", time.Since(begin).Round(time.Millisecond), fmt.Sprintf(format, args...))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="fixture.bin"`)
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/slow" {
			w.Header().Set("Content-Length", "67108864")
			for i := 0; i < 1024; i++ {
				if _, err := w.Write(bytes.Repeat([]byte{byte(i)}, 65536)); err != nil {
					return
				}
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
					return
				case <-time.After(50 * time.Millisecond):
				}
			}
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		n, err := w.Write(payload)
		trace("HTTP response written %s: bytes=%d error=%v", r.URL.String(), n, err)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	events := make(chan Download, 1024)
	h := New(func(e Event) {
		if e.Download != nil {
			d := e.Download
			key := fmt.Sprintf("%s/%s %d/%d", d.Status, d.Phase, d.BytesReceived, d.TotalBytes)
			traceMu.Lock()
			if lastTrace[d.ID] != key {
				trace("event %s: %s", d.ID, key)
				lastTrace[d.ID] = key
			}
			traceMu.Unlock()
			select {
			case events <- *e.Download:
			default:
			}
		}
	})
	if os.Getenv("ORCA_BROWSER_TRANSFER_TRACE") == "1" {
		lastState := make(map[string]int32)
		lastSample := make(map[string]time.Time)
		h.transfers.trace = func(id, step string, state int32, elapsed time.Duration) {
			if step == "native-state" {
				previous, seen := lastState[id]
				if seen && previous == state && time.Since(lastSample[id]) < time.Second && elapsed < 100*time.Millisecond {
					return
				}
				lastState[id], lastSample[id] = state, time.Now()
			}
			trace("probe %s %s: state=%d duration=%s", id, step, state, elapsed.Round(time.Microsecond))
		}
	}
	h.SetNavigationGuard(func(id, url string) bool { return strings.HasPrefix(url, server.URL+"/") })
	if err := h.call(ctx, func() error {
		hwnd := win.CreateWindowEx(0, windows.StringToUTF16Ptr("STATIC"), windows.StringToUTF16Ptr("ORCA isolated download test"), win.WS_OVERLAPPEDWINDOW, 0, 0, 640, 480, 0, 0, 0, nil)
		if hwnd == 0 {
			return errors.New("create hidden download test window")
		}
		h.parent = uintptr(hwnd)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = h.Close(cleanupCtx, "download-test")
		_ = h.call(cleanupCtx, func() error { win.DestroyWindow(win.HWND(h.parent)); return nil })
		h.Shutdown()
	}()
	if err := h.Create(ctx, t.TempDir(), "download-test", "about:blank"); err != nil {
		t.Fatal(err)
	}
	fallbackDir := t.TempDir()
	fallbackPath := filepath.Join(fallbackDir, "unapproved.bin")
	if err := h.call(ctx, func() error {
		s := h.transfers.tabs[h.tabs["download-test"]]
		if s == nil {
			return errors.New("configureTransfers integration missing")
		}
		var token int64
		handler := newCOMCallback(false, func(sender, arg uintptr) uintptr {
			a := (*wv.ICoreWebView2DownloadStartingEventArgs)(unsafe.Pointer(arg))
			_ = raw(unsafe.Pointer(a), a.Vtbl.PutResultFilePath, uintptr(unsafe.Pointer(ptr(fallbackPath))))
			return 0
		})
		defer handler.release()
		return raw(unsafe.Pointer(s.view), s.view.Vtbl.AddDownloadStarting, handler.pointer(), uintptr(unsafe.Pointer(&token)))
	}); err != nil {
		t.Fatal(err)
	}
	sequence := 0
	start := func(t *testing.T, route string) Download {
		t.Helper()
		sequence++
		url := fmt.Sprintf("%s%s?sequence=%d", server.URL, route, sequence)
		trace("navigate %s", url)
		if err := h.Navigate(ctx, "download-test", url); err != nil {
			t.Fatal(err)
		}
		for {
			select {
			case d := <-events:
				if d.URL == url && d.Status == "pending" {
					return d
				}
			case <-ctx.Done():
				t.Fatalf("download never requested approval: %v", ctx.Err())
			}
		}
	}
	list := func(t *testing.T) []Download {
		t.Helper()
		body, err := h.Command(ctx, "download-test", "download-list", "")
		if err != nil {
			t.Fatal(err)
		}
		var items []Download
		if err := json.Unmarshal([]byte(body), &items); err != nil {
			t.Fatal(err)
		}
		return items
	}
	wait := func(t *testing.T, id string, predicate func(Download) bool) Download {
		t.Helper()
		lastNative := ""
		lastHeartbeat := time.Time{}
		for {
			if os.Getenv("ORCA_BROWSER_TRANSFER_TRACE") == "1" && time.Since(lastHeartbeat) >= time.Second {
				started := time.Now()
				probeErr := h.call(ctx, func() error {
					s := h.transfers.tabs[h.tabs["download-test"]]
					for _, p := range s.items {
						if p.data.ID == id && p.operation != nil {
							var received int64
							var state int32
							_ = raw(unsafe.Pointer(p.operation), p.operation.Vtbl.GetBytesReceived, uintptr(unsafe.Pointer(&received)))
							_ = raw(unsafe.Pointer(p.operation), p.operation.Vtbl.GetState, uintptr(unsafe.Pointer(&state)))
							files, _ := os.ReadDir(p.stageDir)
							key := fmt.Sprintf("state=%d bytes=%d staging=%v", state, received, files)
							if key != lastNative {
								trace("native %s %s", id, key)
								lastNative = key
							}
						}
					}
					return nil
				})
				trace("STA heartbeat %s: duration=%s error=%v", id, time.Since(started).Round(time.Microsecond), probeErr)
				lastHeartbeat = time.Now()
			}
			for _, d := range list(t) {
				if d.ID == id && predicate(d) {
					return d
				}
			}
			select {
			case <-ctx.Done():
				t.Fatalf("download %s did not reach expected state: %v", id, ctx.Err())
			case <-time.After(25 * time.Millisecond):
			}
		}
	}
	approve := func(t *testing.T, id, path string) error {
		t.Helper()
		data, _ := json.Marshal(map[string]string{"id": id, "path": path})
		trace("approving %s", id)
		_, err := h.Command(ctx, "download-test", "download-approve", string(data))
		trace("approval returned %s: %v", id, err)
		return err
	}
	assertNoFallback := func(t *testing.T) {
		t.Helper()
		files, err := os.ReadDir(fallbackDir)
		if err != nil || len(files) != 0 {
			t.Fatalf("unapproved download wrote fallback files: %v %v", files, err)
		}
	}

	t.Run("blocked-download-no-file", func(t *testing.T) {
		d := start(t, "/file")
		if !d.RequiresApproval || d.Path != "" {
			t.Fatalf("missing independent approval: %+v", d)
		}
		time.Sleep(150 * time.Millisecond)
		assertNoFallback(t)
		if _, err := h.Command(ctx, "download-test", "download-deny", d.ID); err != nil {
			t.Fatal(err)
		}
		wait(t, d.ID, func(d Download) bool { return d.Status == "denied" })
		assertNoFallback(t)
		if err := approve(t, d.ID, filepath.Join(t.TempDir(), "late.bin")); err == nil {
			t.Fatal("late approval accepted")
		}
	})
	t.Run("approved-download-hash", func(t *testing.T) {
		d := start(t, "/file")
		path := filepath.Join(t.TempDir(), "approved.bin")
		if err := approve(t, d.ID, path); err != nil {
			t.Fatal(err)
		}
		d = wait(t, d.ID, func(d Download) bool {
			return d.Phase == "finalizing" || d.Status == "completed" || d.Status == "interrupted"
		})
		if d.Phase == "finalizing" {
			if d.Status != "inProgress" || d.BytesReceived != d.TotalBytes {
				t.Fatalf("incorrect finalization state: %+v", d)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("uncompleted native download was published: %v", err)
			}
		}
		d = wait(t, d.ID, func(d Download) bool { return d.Status == "completed" || d.Status == "interrupted" })
		if d.Status != "completed" || d.Error != "" {
			t.Fatalf("download failed: %+v", d)
		}
		got, err := os.ReadFile(path)
		if err != nil || sha256.Sum256(got) != sha256.Sum256(payload) {
			t.Fatalf("download hash mismatch: %v", err)
		}
		if d.BytesReceived != int64(len(payload)) || d.TotalBytes != int64(len(payload)) {
			t.Fatalf("incorrect progress: %+v", d)
		}
		assertNoFallback(t)
	})
	t.Run("cancel-approved-download", func(t *testing.T) {
		d := start(t, "/slow")
		path := filepath.Join(t.TempDir(), "cancelled.bin")
		if err := approve(t, d.ID, path); err != nil {
			t.Fatal(err)
		}
		wait(t, d.ID, func(d Download) bool { return d.BytesReceived > 0 })
		if _, err := h.Command(ctx, "download-test", "download-cancel", d.ID); err != nil {
			t.Fatal(err)
		}
		wait(t, d.ID, func(d Download) bool { return d.Status == "cancelled" })
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cancel published destination: %v", err)
		}
		assertNoFallback(t)
	})
	t.Run("approval-timeout", func(t *testing.T) {
		if err := h.call(ctx, func() error { h.transfers.approvalTimeout = 350 * time.Millisecond; return nil }); err != nil {
			t.Fatal(err)
		}
		d := start(t, "/file")
		wait(t, d.ID, func(d Download) bool { return d.Status == "timedOut" })
		assertNoFallback(t)
		if err := h.call(ctx, func() error { h.transfers.approvalTimeout = 0; return nil }); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("cancel-pending-and-context", func(t *testing.T) {
		d := start(t, "/file")
		cancelCtx, cancelCommand := context.WithCancel(ctx)
		cancelCommand()
		path := filepath.Join(t.TempDir(), "context-cancelled.bin")
		data, _ := json.Marshal(map[string]string{"id": d.ID, "path": path})
		if _, err := h.Command(cancelCtx, "download-test", "download-approve", string(data)); !errors.Is(err, context.Canceled) {
			t.Fatalf("context error = %v", err)
		}
		if _, err := h.Command(ctx, "download-test", "download-cancel-pending", ""); err != nil {
			t.Fatal(err)
		}
		wait(t, d.ID, func(d Download) bool { return d.Status == "cancelled" })
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("context cancellation published destination: %v", err)
		}
		assertNoFallback(t)
	})
	t.Run("close-cancels-pending", func(t *testing.T) {
		_ = start(t, "/file")
		if err := h.Close(ctx, "download-test"); err != nil {
			t.Fatal(err)
		}
		if err := h.call(ctx, func() error {
			if len(h.transfers.tabs) != 0 {
				return errors.New("closeTransfers integration missing")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		assertNoFallback(t)
	})
}
