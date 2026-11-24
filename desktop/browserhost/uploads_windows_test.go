//go:build windows

package browserhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

func TestUploadSnapshotValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.txt")
	if err := os.WriteFile(path, []byte("synthetic only"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := validateUploadSnapshots(ctx, []string{path}, false); err != nil {
		t.Fatal(err)
	}
	for _, paths := range [][]string{nil, {dir}, {"relative.txt"}, {`\\server\share\file`}, {path + ":stream"}, {path, path}, {filepath.Join(dir, "missing.txt")}} {
		if _, err := validateUploadSnapshots(ctx, paths, true); err == nil {
			t.Errorf("accepted %v", paths)
		}
	}
	if _, err := validateUploadSnapshots(ctx, []string{path, path}, false); err == nil {
		t.Fatal("single picker accepted multiple paths")
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := validateUploadSnapshots(cancelCtx, []string{path}, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("context: %v", err)
	}
	h := New(nil)
	if _, err := h.uploadCommand(cancelCtx, "tab", "upload-approve", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("command context: %v", err)
	}
	if h.started.Load() {
		t.Fatal("cancelled command started host")
	}
}

func TestNativeUploadInterception(t *testing.T) {
	if os.Getenv("ORCA_BROWSER_NATIVE_TEST") != "1" {
		t.Skip("isolated WebView2 test is opt-in")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><title>Isolated upload fixture</title><input id="file" type="file" accept=".txt"><input id="multi" type="file" multiple><p id="result"></p>`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	events := make(chan Upload, 256)
	h := New(func(e Event) {
		if e.Upload != nil {
			select {
			case events <- *e.Upload:
			default:
			}
		}
	})
	h.SetNavigationGuard(func(id, url string) bool { return strings.HasPrefix(url, server.URL+"/") })
	if err := h.call(ctx, func() error {
		hwnd := win.CreateWindowEx(0, windows.StringToUTF16Ptr("STATIC"), windows.StringToUTF16Ptr("ORCA isolated upload test"), win.WS_OVERLAPPEDWINDOW, 0, 0, 640, 480, 0, 0, 0, nil)
		if hwnd == 0 {
			return errors.New("create hidden upload fixture window")
		}
		h.parent = uintptr(hwnd)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = h.Close(cleanup, "upload")
		_ = h.Close(cleanup, "other")
		_ = h.call(cleanup, func() error { win.DestroyWindow(win.HWND(h.parent)); return nil })
		h.Shutdown()
	}()
	profile := t.TempDir()
	if err := h.Create(ctx, profile, "upload", server.URL+"/"); err != nil {
		t.Fatal(err)
	}
	if err := h.Create(ctx, profile, "other", "about:blank"); err != nil {
		t.Fatal(err)
	}
	if err := h.call(ctx, func() error {
		if s := h.uploads.tabs[h.tabs["upload"]]; s == nil || !s.ready {
			return errors.New("upload interception readiness integration missing")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for {
		data, err := h.evaluate(ctx, "upload", "!!document.getElementById('file')")
		if err == nil && string(data) == "true" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("fixture did not load")
		case <-time.After(25 * time.Millisecond):
		}
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot.txt")
	if err := os.WriteFile(snapshot, []byte("user-approved synthetic content"), 0600); err != nil {
		t.Fatal(err)
	}
	open := func(t *testing.T, input string) Upload {
		t.Helper()
		var previous uint64
		if err := h.call(ctx, func() error { previous = h.uploads.next; return nil }); err != nil {
			t.Fatal(err)
		}
		world, err := h.isolatedWorld(ctx, "upload")
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(input)
		// Synthetic activation inside the test WebView. No desktop input and no
		// OS picker interaction; Create has already acknowledged interception.
		_, err = h.cdp(ctx, "upload", "Runtime.evaluate", map[string]any{"expression": "document.getElementById(" + string(encoded) + ").click()", "contextId": world, "userGesture": true, "returnByValue": true})
		if err != nil {
			t.Fatal(err)
		}
		for {
			select {
			case d := <-events:
				sequence, _ := strconv.ParseUint(strings.TrimPrefix(d.ID, "upload-"), 10, 64)
				if sequence <= previous {
					continue
				}
				if d.Status == "pending" {
					if !d.RequiresApproval || d.BackendNodeID <= 0 || d.FrameID == "" || d.DocumentID == "" || d.URL != server.URL+"/" || d.Origin != server.URL {
						t.Fatalf("invalid request: %+v", d)
					}
					return d
				}
				if d.Status == "failed" || d.Status == "stale" {
					t.Fatalf("prepare request: %+v", d)
				}
			case <-ctx.Done():
				t.Fatalf("file chooser was not intercepted: %v", ctx.Err())
			}
		}
	}
	files := func(t *testing.T, input string) int {
		t.Helper()
		encoded, _ := json.Marshal(input)
		data, err := h.evaluate(ctx, "upload", "document.getElementById("+string(encoded)+").files.length")
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err = json.Unmarshal(data, &n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	approve := func(t *testing.T, tab string, d Upload, paths ...string) error {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"id": d.ID, "paths": paths})
		_, err := h.Command(ctx, tab, "upload-approve", string(body))
		return err
	}
	t.Run("unapproved-input-has-no-files", func(t *testing.T) {
		d := open(t, "file")
		if n := files(t, "file"); n != 0 {
			t.Fatalf("unapproved input has %d files", n)
		}
		if _, err := h.Command(ctx, "upload", "upload-deny", d.ID); err != nil {
			t.Fatal(err)
		}
		if n := files(t, "file"); n != 0 {
			t.Fatalf("denial selected %d files", n)
		}
		if err := approve(t, "upload", d, snapshot); err == nil {
			t.Fatal("denied request replay succeeded")
		}
	})
	t.Run("owned-tab-and-approved-snapshot", func(t *testing.T) {
		d := open(t, "file")
		if err := approve(t, "other", d, snapshot); err == nil {
			t.Fatal("cross-tab approval succeeded")
		}
		if n := files(t, "file"); n != 0 {
			t.Fatal("cross-tab selected a file")
		}
		if err := approve(t, "upload", d, snapshot); err != nil {
			t.Fatal(err)
		}
		if n := files(t, "file"); n != 1 {
			t.Fatalf("approved files: %d", n)
		}
		data, err := h.evaluate(ctx, "upload", "document.getElementById('file').files[0].text()")
		if err != nil || string(data) != `"user-approved synthetic content"` {
			t.Fatalf("snapshot content: %s %v", data, err)
		}
		if err := approve(t, "upload", d, snapshot); err == nil {
			t.Fatal("approved request replay succeeded")
		}
		_, _ = h.evaluate(ctx, "upload", "document.getElementById('file').value=''")
	})
	t.Run("mutation-revokes-approval", func(t *testing.T) {
		d := open(t, "file")
		if _, err := h.evaluate(ctx, "upload", "document.getElementById('file').setAttribute('data-mutated','1')"); err != nil {
			t.Fatal(err)
		}
		if err := approve(t, "upload", d, snapshot); !errors.Is(err, ErrStaleObservation) {
			t.Fatalf("mutation approval: %v", err)
		}
		if n := files(t, "file"); n != 0 {
			t.Fatal("stale input acquired files")
		}
	})
	t.Run("observation-revokes-approval", func(t *testing.T) {
		d := open(t, "file")
		if err := h.call(ctx, func() error { h.tabs["upload"].generation++; return nil }); err != nil {
			t.Fatal(err)
		}
		if err := approve(t, "upload", d, snapshot); !errors.Is(err, ErrStaleObservation) {
			t.Fatalf("generation approval: %v", err)
		}
		if n := files(t, "file"); n != 0 {
			t.Fatal("stale observation acquired files")
		}
	})
	t.Run("multiple-selection", func(t *testing.T) {
		second := filepath.Join(t.TempDir(), "second.txt")
		if err := os.WriteFile(second, []byte("second"), 0600); err != nil {
			t.Fatal(err)
		}
		d := open(t, "multi")
		if !d.Multiple {
			t.Fatal("multiple mode not captured")
		}
		if err := approve(t, "upload", d, snapshot, second); err != nil {
			t.Fatal(err)
		}
		if n := files(t, "multi"); n != 2 {
			t.Fatalf("multiple files: %d", n)
		}
	})
	t.Run("context-and-batch-cancel", func(t *testing.T) {
		d := open(t, "file")
		cancelled, stop := context.WithCancel(ctx)
		stop()
		body, _ := json.Marshal(map[string]any{"id": d.ID, "paths": []string{snapshot}})
		if _, err := h.Command(cancelled, "upload", "upload-approve", string(body)); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled command: %v", err)
		}
		if _, err := h.Command(ctx, "upload", "upload-cancel-pending", ""); err != nil {
			t.Fatal(err)
		}
		if n := files(t, "file"); n != 0 {
			t.Fatal("cancelled command selected file")
		}
	})
	t.Run("cross-process-frame-picker-denied", func(t *testing.T) {
		frameURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/frame"
		encoded, _ := json.Marshal(frameURL)
		if _, err := h.evaluate(ctx, "upload", "(()=>{const f=document.createElement('iframe');f.src="+string(encoded)+";document.body.append(f)})()"); err != nil {
			t.Fatal(err)
		}
		frameCtx, frameCancel := context.WithTimeout(ctx, 8*time.Second)
		defer frameCancel()
		var child *uploadChild
		for {
			if err := h.call(frameCtx, func() error {
				tab, err := h.tab("upload")
				if err != nil {
					return err
				}
				for _, candidate := range h.uploads.tabs[tab].children {
					if candidate.ready {
						child = candidate
						break
					}
				}
				return nil
			}); err != nil {
				t.Fatalf("child interception was not ready; never trigger a picker: %v", err)
			}
			if child != nil {
				break
			}
			select {
			case <-frameCtx.Done():
				t.Fatal("no isolated iframe target was intercepted")
			case <-time.After(25 * time.Millisecond):
			}
		}
		var previous uint64
		if err := h.call(ctx, func() error { previous = h.uploads.next; return nil }); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		if err := h.call(ctx, func() error {
			tab := h.tabs["upload"]
			h.sendUploadChild(tab, h.uploads.tabs[tab], child.path, "Runtime.evaluate", map[string]any{"expression": "document.getElementById('file').click()", "userGesture": true}, func(err error) { done <- err })
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-frameCtx.Done():
			t.Fatal(frameCtx.Err())
		}
		for {
			select {
			case d := <-events:
				sequence, _ := strconv.ParseUint(strings.TrimPrefix(d.ID, "upload-"), 10, 64)
				if sequence <= previous {
					continue
				}
				if d.Status != "denied" || d.RequiresApproval || !strings.Contains(d.Error, "cross-process") {
					t.Fatalf("iframe picker was not denied: %+v", d)
				}
				return
			case <-frameCtx.Done():
				t.Fatal("no intercepted iframe denial event")
			}
		}
	})
	t.Run("close-revokes-requests", func(t *testing.T) {
		_ = open(t, "file")
		if err := h.Close(ctx, "upload"); err != nil {
			t.Fatal(err)
		}
		if err := h.call(ctx, func() error {
			for tab := range h.uploads.tabs {
				if tab.id == "upload" {
					return errors.New("closeUploads integration missing")
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}
