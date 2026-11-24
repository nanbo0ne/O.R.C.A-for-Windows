//go:build windows && production

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	winopt "github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

const browserWailsOptInEnv = "ORCA_BROWSER_WAILS_TEST"
const browserWailsChildEnv = "ORCA_BROWSER_WAILS_TEST_CHILD"
const browserWailsRootEnv = "ORCA_BROWSER_WAILS_TEST_ROOT"

func TestBrowserPreviewInWailsWindow(t *testing.T) {
	if os.Getenv(browserWailsOptInEnv) != "1" {
		t.Skip("set ORCA_BROWSER_WAILS_TEST=1 to run the isolated Wails/WebView2 acceptance test")
	}
	if os.Getenv(browserWailsChildEnv) == "1" {
		runBrowserWailsAcceptance(t)
		return
	}

	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestBrowserPreviewInWailsWindow$", "-test.timeout=80s")
	cmd.Env = append(os.Environ(), browserWailsOptInEnv+"=1", browserWailsChildEnv+"=1", browserWailsRootEnv+"="+root)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start isolated Wails test process: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("isolated Wails test failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
	case <-time.After(75 * time.Second):
		_ = cmd.Process.Kill()
		waitErr := <-done
		t.Fatalf("isolated Wails test exceeded 75 seconds and its child process was terminated (wait: %v)\nstdout:\n%s\nstderr:\n%s", waitErr, stdout.String(), stderr.String())
	}
}

func runBrowserWailsAcceptance(t *testing.T) {
	t.Helper()
	root := strings.TrimSpace(os.Getenv(browserWailsRootEnv))
	if root == "" || !filepath.IsAbs(root) {
		t.Fatalf("isolated test root must be an absolute path, got %q", root)
	}
	tmpRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".tmp"))
	if err != nil {
		t.Fatal(err)
	}
	evidenceDir := filepath.Join(tmpRoot, "browser-preview-evidence", "wails-host")
	if nominated := strings.TrimSpace(os.Getenv("ORCA_BROWSER_EVIDENCE_DIR")); nominated != "" {
		if !filepath.IsAbs(nominated) {
			t.Fatal("ORCA_BROWSER_EVIDENCE_DIR must be an absolute path")
		}
		absEvidence, err := filepath.Abs(nominated)
		if err != nil {
			t.Fatal(err)
		}
		evidenceDir = absEvidence
		rel, err := filepath.Rel(tmpRoot, evidenceDir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			t.Fatalf("ORCA_BROWSER_EVIDENCE_DIR must be inside the repository .tmp directory: %s", evidenceDir)
		}
	}
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><title>ORCA Wails browser fixture</title><main><label>姓名<input id="name" placeholder="请输入姓名"></label><button id="submit" onclick="document.querySelector('#result').textContent=document.querySelector('#name').value">提交</button><output id="result"></output></main>`)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result := make(chan error, 1)
	runDone := make(chan error, 1)
	lifecycle := make(chan context.Context, 1)
	assets := fstestMapFS()
	wailsData := filepath.Join(root, "wails-webview2-profile")

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		runDone <- wails.Run(&options.App{
			Title: "ORCA browser acceptance fixture", Width: 900, Height: 640,
			StartHidden: true,
			AssetServer: &assetserver.Options{Assets: assets},
			Windows:     &winopt.Options{WindowClassName: browserhost.WindowClass, WebviewUserDataPath: wailsData},
			OnDomReady: func(appCtx context.Context) {
				lifecycle <- appCtx
				go func() {
					err := runBrowserWailsFixture(ctx, root, evidenceDir, server.URL)
					result <- err
					wruntime.Quit(appCtx)
				}()
			},
		})
	}()

	select {
	case err := <-result:
		if err != nil {
			t.Error(err)
		}
		select {
		case err := <-runDone:
			if err != nil {
				t.Errorf("Wails run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Wails did not shut down within 5 seconds after runtime.Quit")
		}
	case <-ctx.Done():
		t.Errorf("Wails acceptance timed out: %v", ctx.Err())
		select {
		case appCtx := <-lifecycle:
			wruntime.Quit(appCtx)
		default:
			t.Error("Wails did not reach OnDomReady; cannot request lifecycle shutdown")
		}
		select {
		case <-runDone:
		case <-time.After(5 * time.Second):
			t.Error("Wails fixture did not exit after bounded timeout")
		}
	}
}

func fstestMapFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(`<!doctype html><html><head><meta charset="utf-8"><title>Wails acceptance fixture</title><style>body{margin:0;font:15px "Segoe UI",sans-serif;color:#202124;background:#f7f8fa}.app-header{height:54px;display:flex;align-items:center;gap:16px;padding:0 20px;background:#fff;border-bottom:1px solid #d9dde3}.brand{font-weight:650}.address-slot{height:30px;flex:1;max-width:520px;padding:7px 12px;border:1px solid #d9dde3;border-radius:4px;color:#60646b}.content{padding:24px}</style></head><body><header class="app-header"><strong class="brand">O.R.C.A.</strong><div class="address-slot" aria-label="地址栏">浏览器预览</div></header><main class="content"><h1>Wails acceptance fixture</h1><p>Browser preview host surface</p></main></body></html>`)},
	}
}

func runBrowserWailsFixture(ctx context.Context, root, evidenceDir, address string) (retErr error) {
	host := browserhost.New(nil)
	defer host.Shutdown()
	if !host.Supported() {
		return fmt.Errorf("browserhost reports unsupported on Windows")
	}
	guardPrefix := address
	host.SetNavigationGuard(func(_ string, target string) bool { return strings.HasPrefix(target, guardPrefix) })
	profile := filepath.Join(root, "browserhost-profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		return err
	}
	if err := host.Create(ctx, profile, "wails-acceptance", address); err != nil {
		return fmt.Errorf("create browser child under Wails window: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := host.Close(closeCtx, "wails-acceptance"); retErr == nil && err != nil {
			retErr = fmt.Errorf("close owned browser child: %w", err)
		}
	}()

	viewport := browserhost.Viewport{TabID: "wails-acceptance", X: 22, Y: 105, Width: 720, Height: 390, Scale: 1, Visible: true}
	if err := host.Present(ctx, viewport); err != nil {
		return fmt.Errorf("present browser child: %w", err)
	}
	var observation browserhost.Observation
	for {
		var err error
		observation, err = host.Observe(ctx, "wails-acceptance")
		if err == nil && strings.Contains(observation.Text, "提交") {
			break
		}
		if ctx.Err() != nil {
			return fmt.Errorf("wait for fixture DOM: %w", ctx.Err())
		}
		time.Sleep(100 * time.Millisecond)
	}

	inputID, submitID := "", ""
	for _, element := range observation.Elements {
		if element.Role == "input" && element.Name != "" {
			inputID = element.ID
		}
		if element.Name == "提交" {
			submitID = element.ID
		}
	}
	if inputID == "" || submitID == "" {
		return fmt.Errorf("observed form controls missing: input=%q submit=%q", inputID, submitID)
	}
	if err := host.Execute(ctx, "wails-acceptance", browserhost.Action{Kind: "click", ElementID: inputID, Generation: observation.Generation, DocumentID: observation.DocumentID}); err != nil {
		return fmt.Errorf("focus form input through browserhost: %w", err)
	}
	observation, err := host.Observe(ctx, "wails-acceptance")
	if err != nil {
		return err
	}
	inputID, submitID = "", ""
	for _, element := range observation.Elements {
		if element.Role == "input" && element.Name != "" {
			inputID = element.ID
		}
		if element.Name == "提交" {
			submitID = element.ID
		}
	}
	if err := host.Execute(ctx, "wails-acceptance", browserhost.Action{Kind: "type", ElementID: inputID, Text: "Wails访客", Generation: observation.Generation, DocumentID: observation.DocumentID}); err != nil {
		return fmt.Errorf("type form value through browserhost: %w", err)
	}
	observation, err = host.Observe(ctx, "wails-acceptance")
	if err != nil {
		return err
	}
	for _, element := range observation.Elements {
		if element.Name == "提交" {
			submitID = element.ID
		}
	}
	if err := host.Execute(ctx, "wails-acceptance", browserhost.Action{Kind: "click", ElementID: submitID, Generation: observation.Generation, DocumentID: observation.DocumentID}); err != nil {
		return fmt.Errorf("submit fixture using native browser input: %w", err)
	}
	observation, err = host.Observe(ctx, "wails-acceptance")
	if err != nil {
		return fmt.Errorf("observe submitted form result: %w", err)
	}
	if !strings.Contains(observation.Text, "Wails访客") {
		return fmt.Errorf("fixture form result missing from native browser observation: %q", observation.Text)
	}
	value, err := host.Command(ctx, "wails-acceptance", "screenshot", "")
	if err != nil {
		return fmt.Errorf("capture browserhost PNG: %w", err)
	}
	var screenshot struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal([]byte(value), &screenshot); err != nil {
		return fmt.Errorf("decode screenshot response: %w", err)
	}
	png, err := base64.StdEncoding.DecodeString(screenshot.Data)
	if err != nil || len(png) < 100 || string(png[:8]) != "\x89PNG\r\n\x1a\n" {
		return fmt.Errorf("browserhost returned invalid PNG (%d bytes): %v", len(png), err)
	}
	evidence, err := os.CreateTemp(evidenceDir, "browser-preview-*.png")
	if err != nil {
		return err
	}
	if _, err := evidence.Write(png); err != nil {
		_ = evidence.Close()
		return err
	}
	if err := evidence.Close(); err != nil {
		return err
	}

	updated := browserhost.Viewport{TabID: "wails-acceptance", X: 34, Y: 120, Width: 540, Height: 300, Scale: 1, Visible: true}
	if err := host.Present(ctx, updated); err != nil {
		return fmt.Errorf("resize browser child: %w", err)
	}
	if err := verifyWailsChildBounds(ctx, updated); err != nil {
		return err
	}
	if err := host.Present(ctx, browserhost.Viewport{Scale: 1, Visible: false}); err != nil {
		return fmt.Errorf("hide browser child: %w", err)
	}
	if err := verifyWailsChildHidden(ctx); err != nil {
		return err
	}
	return nil
}

func verifyWailsChildBounds(ctx context.Context, viewport browserhost.Viewport) error {
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	lastActual, lastParent := "none", "none"
	for deadline.Err() == nil {
		parent := findWailsWindow()
		if parent != 0 {
			var parentRect, clientRect win.RECT
			if win.GetWindowRect(win.HWND(parent), &parentRect) && win.GetClientRect(win.HWND(parent), &clientRect) {
				lastParent = fmt.Sprintf("screen=%d,%d-%d,%d client=%dx%d", parentRect.Left, parentRect.Top, parentRect.Right, parentRect.Bottom, clientRect.Right-clientRect.Left, clientRect.Bottom-clientRect.Top)
			}
			found := false
			win.EnumChildWindows(win.HWND(parent), syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
				var class [64]uint16
				win.GetClassName(win.HWND(hwnd), &class[0], len(class))
				if windows.UTF16ToString(class[:]) != "Static" {
					return 1
				}
				var rect win.RECT
				if !win.GetWindowRect(win.HWND(hwnd), &rect) {
					return 1
				}
				var origin win.POINT
				origin.X, origin.Y = rect.Left, rect.Top
				win.ScreenToClient(win.HWND(parent), &origin)
				lastActual = fmt.Sprintf("class=Static client=%d,%d size=%dx%d screen=%d,%d-%d,%d visible=%v", origin.X, origin.Y, rect.Right-rect.Left, rect.Bottom-rect.Top, rect.Left, rect.Top, rect.Right, rect.Bottom, win.IsWindowVisible(win.HWND(hwnd)))
				if int(origin.X) == int(math.Round(viewport.X)) && int(origin.Y) == int(math.Round(viewport.Y)) && int(rect.Right-rect.Left) == int(math.Round(viewport.Width)) && int(rect.Bottom-rect.Top) == int(math.Round(viewport.Height)) {
					found = true
					return 0
				}
				return 1
			}), 0)
			if found {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("browser child did not reach requested bounds %+v; actual={%s}; parent={%s}", viewport, lastActual, lastParent)
}

func verifyWailsChildHidden(ctx context.Context) error {
	parent := findWailsWindow()
	if parent == 0 {
		return fmt.Errorf("Wails parent window disappeared before hide check")
	}
	var visible bool
	win.EnumChildWindows(win.HWND(parent), syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var class [64]uint16
		win.GetClassName(win.HWND(hwnd), &class[0], len(class))
		if windows.UTF16ToString(class[:]) == "Static" && win.IsWindowVisible(win.HWND(hwnd)) {
			visible = true
			return 0
		}
		return 1
	}), 0)
	if visible {
		return fmt.Errorf("browser child remained visible after Present(Visible=false)")
	}
	return nil
}

func findWailsWindow() uintptr {
	return findWailsWindowClass(browserhost.WindowClass)
}

func findWailsWindowClass(windowClass string) uintptr {
	var found uintptr
	user32 := windows.NewLazySystemDLL("user32.dll")
	callback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var pid uint32
		user32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid != uint32(os.Getpid()) {
			return 1
		}
		var class [128]uint16
		user32.NewProc("GetClassNameW").Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
		if windows.UTF16ToString(class[:]) == windowClass {
			found = hwnd
			return 0
		}
		return 1
	})
	user32.NewProc("EnumWindows").Call(callback, 0)
	return found
}
