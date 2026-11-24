//go:build windows && production && browserui

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	stdruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
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

const (
	browserWailsUIOptInEnv = "ORCA_BROWSER_WAILS_UI_TEST"
	browserWailsUIChildEnv = "ORCA_BROWSER_WAILS_UI_TEST_CHILD"
	browserWailsUIRootEnv  = "ORCA_BROWSER_WAILS_UI_TEST_ROOT"
	browserWailsCaptureEnv = "ORCA_BROWSER_WAILS_CAPTURE_HELPER"
)

var browserWailsUIRequests atomic.Int32

type browserWailsUIDOMReport struct {
	Stage               string  `json:"stage"`
	Phase               string  `json:"phase,omitempty"`
	Error               string  `json:"error,omitempty"`
	OnboardingVisible   bool    `json:"onboardingVisible"`
	OnboardingSkipLabel string  `json:"onboardingSkipLabel,omitempty"`
	TabCount            int     `json:"tabCount"`
	SelectedTabs        int     `json:"selectedTabs"`
	NativeVisible       string  `json:"nativeVisible,omitempty"`
	Owner               string  `json:"owner,omitempty"`
	Address             string  `json:"address,omitempty"`
	Title               string  `json:"title,omitempty"`
	PanelWidth          float64 `json:"panelWidth,omitempty"`
	PanelHeight         float64 `json:"panelHeight,omitempty"`
	ViewportX           float64 `json:"viewportX,omitempty"`
	ViewportY           float64 `json:"viewportY,omitempty"`
	ViewportWidth       float64 `json:"viewportWidth,omitempty"`
	ViewportH           float64 `json:"viewportHeight,omitempty"`
}

// BrowserWailsUIFixture is test-only Wails binding used to report geometry from
// the real embedded frontend without exposing a production API for test data.
type BrowserWailsUIFixture struct {
	mu          sync.Mutex
	reports     []browserWailsUIDOMReport
	wake        chan struct{}
	captureGate chan struct{}
	captureOnce sync.Once
}

func (f *BrowserWailsUIFixture) ReportBrowserUI(payload string) error {
	var report browserWailsUIDOMReport
	if err := json.Unmarshal([]byte(payload), &report); err != nil {
		return fmt.Errorf("decode browser UI DOM report: %w", err)
	}
	f.mu.Lock()
	f.reports = append(f.reports, report)
	f.mu.Unlock()
	select {
	case f.wake <- struct{}{}:
	default:
	}
	return nil
}

func (f *BrowserWailsUIFixture) WaitForBrowserUICapture() {
	<-f.captureGate
}

func (f *BrowserWailsUIFixture) allowOverlayCheck() {
	f.captureOnce.Do(func() { close(f.captureGate) })
}

func (f *BrowserWailsUIFixture) next(ctx context.Context) (browserWailsUIDOMReport, error) {
	for {
		f.mu.Lock()
		if len(f.reports) > 0 {
			report := f.reports[0]
			f.reports = f.reports[1:]
			f.mu.Unlock()
			return report, nil
		}
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return browserWailsUIDOMReport{}, ctx.Err()
		case <-f.wake:
		}
	}
}

func TestBrowserPreviewInProductionUI(t *testing.T) {
	if os.Getenv(browserWailsCaptureEnv) == "1" {
		captureBrowserWailsWindowHelper(t)
		return
	}
	if os.Getenv(browserWailsUIOptInEnv) != "1" {
		t.Skip("set ORCA_BROWSER_WAILS_UI_TEST=1 to run the production-assets Wails browser UI acceptance test")
	}
	if os.Getenv(browserWailsUIChildEnv) == "1" {
		runBrowserWailsUIChild(t)
		return
	}

	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestBrowserPreviewInProductionUI$", "-test.timeout=100s", "-test.v")
	cmd.Env = append(os.Environ(), browserWailsUIOptInEnv+"=1", browserWailsUIChildEnv+"=1", browserWailsUIRootEnv+"="+root)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start isolated production UI Wails process: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		cleanupErr := releaseBrowserWailsUIRoot(root)
		if err != nil {
			t.Fatalf("production UI Wails test failed: %v (isolated profile cleanup: %v)\nstdout:\n%s\nstderr:\n%s", err, cleanupErr, stdout.String(), stderr.String())
		}
		if cleanupErr != nil {
			t.Fatalf("production UI Wails test passed, but isolated profile was not released within bound: %v", cleanupErr)
		}
		t.Logf("isolated Wails UI child output:\n%s", stdout.String())
	case <-time.After(95 * time.Second):
		killErr := cmd.Process.Kill()
		select {
		case waitErr := <-done:
			cleanupErr := releaseBrowserWailsUIRoot(root)
			t.Fatalf("production UI Wails test exceeded 95 seconds; child was terminated (kill: %v, wait: %v, profile cleanup: %v)\nstdout:\n%s\nstderr:\n%s", killErr, waitErr, cleanupErr, stdout.String(), stderr.String())
		case <-time.After(8 * time.Second):
			t.Fatalf("production UI Wails test exceeded 95 seconds and child did not exit within cleanup bound (kill: %v); isolated profile was retained at %s\nstdout:\n%s\nstderr:\n%s", killErr, root, stdout.String(), stderr.String())
		}
	}
}

func releaseBrowserWailsUIRoot(root string) error {
	deadline := time.Now().Add(8 * time.Second)
	for {
		if err := os.RemoveAll(root); err == nil {
			return nil
		} else if time.Now().After(deadline) {
			return fmt.Errorf("remove isolated profile %s: %w", root, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func runBrowserWailsUIChild(t *testing.T) {
	t.Helper()
	root := strings.TrimSpace(os.Getenv(browserWailsUIRootEnv))
	if root == "" || !filepath.IsAbs(root) {
		t.Fatalf("isolated UI test root must be absolute, got %q", root)
	}
	channel = browserPreviewChannel
	if err := os.Setenv(browserPreviewHomeEnv, filepath.Join(root, "browser-preview-home")); err != nil {
		t.Fatal(err)
	}
	if err := isolateBrowserPreview(); err != nil {
		t.Fatalf("isolate browser preview before App construction: %v", err)
	}

	browserWailsUIRequests.Store(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		browserWailsUIRequests.Add(1)
		_, _ = fmt.Fprint(w, `<!doctype html><html><head><meta charset="utf-8"><title>ORCA Browser UI fixture</title></head><body><main><h1>Local browser preview acceptance</h1><p>Only this httptest server is navigated.</p></main></body></html>`)
	}))
	defer server.Close()

	app := NewApp()
	app.mu.Lock()
	ownerID := "browser-ui-fixture-owner"
	app.tabs[ownerID] = &WorkspaceTab{ID: ownerID, Scope: "global", TopicTitle: "Browser UI acceptance", Label: "fixture", Ready: true, promptMode: promptModeCoding}
	app.tabOrder = []string{ownerID}
	app.activeTabID = ownerID
	app.mu.Unlock()
	if app.browser == nil || !app.BrowserAvailable() {
		t.Fatal("browser preview service was not initialized for the isolated Windows fixture")
	}
	if err := app.SetDesktopCheckUpdates(false); err != nil {
		t.Fatalf("disable frontend update polling in isolated config: %v", err)
	}

	dom := &BrowserWailsUIFixture{wake: make(chan struct{}, 1), captureGate: make(chan struct{})}
	evidenceDir := browserWailsUIEvidenceDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	result := make(chan error, 1)
	quit := make(chan context.Context, 1)
	go func() {
		stdruntime.LockOSThread()
		defer stdruntime.UnlockOSThread()
		result <- wails.Run(&options.App{
			Title: browserPreviewTitle, Width: 1240, Height: 820, MinWidth: 900, MinHeight: 620,
			StartHidden: true,
			AssetServer: &assetserver.Options{Assets: assets, Middleware: app.workspaceMediaMiddleware()},
			Bind:        []any{app, dom},
			OnStartup:   func(wctx context.Context) { app.ctx = wctx },
			OnDomReady: func(wctx context.Context) {
				wruntime.WindowShow(wctx)
				quit <- wctx
				go func() {
					err := exerciseBrowserWailsProductionUI(t, ctx, wctx, app, dom, ownerID, server.URL+"/", evidenceDir)
					if err != nil {
						t.Errorf("production UI browser preview: %v", err)
					}
					wruntime.Quit(wctx)
				}()
			},
			OnShutdown: app.shutdown,
			Windows: &winopt.Options{
				WindowClassName:     browserPreviewWindowClass,
				WebviewUserDataPath: browserPreviewWebviewDataPath(),
			},
		})
	}()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Wails production UI run: %v", err)
		}
	case <-ctx.Done():
		select {
		case wctx := <-quit:
			wruntime.Quit(wctx)
		default:
		}
		select {
		case err := <-result:
			if err != nil {
				t.Errorf("Wails returned during timeout cleanup: %v", err)
			}
		case <-time.After(8 * time.Second):
			t.Error("Wails did not finish shutdown after runtime.Quit within 8 seconds")
		}
		t.Fatalf("Wails production UI did not finish within bound: %v", ctx.Err())
	}
}

func browserWailsUIEvidenceDir(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := stdruntime.Caller(0)
	if !ok {
		t.Fatal("locate production UI test source for evidence root")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(sourceFile), "..", ".tmp"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "browser-preview-evidence", "wails-ui")
	if nominated := strings.TrimSpace(os.Getenv("ORCA_BROWSER_UI_EVIDENCE_DIR")); nominated != "" {
		if !filepath.IsAbs(nominated) {
			t.Fatal("ORCA_BROWSER_UI_EVIDENCE_DIR must be absolute")
		}
		dir, err = filepath.Abs(nominated)
		if err != nil {
			t.Fatal(err)
		}
		rel, relErr := filepath.Rel(root, dir)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			t.Fatalf("ORCA_BROWSER_UI_EVIDENCE_DIR must remain inside repository .tmp: %s", dir)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create isolated UI evidence directory: %v", err)
	}
	return dir
}

func captureWailsParentWindow(ctx context.Context, evidenceDir string) (string, error) {
	return captureWailsParentWindowClass(ctx, evidenceDir, browserhost.WindowClass)
}

func captureWailsParentWindowClass(ctx context.Context, evidenceDir, windowClass string) (string, error) {
	if windowClass != browserhost.WindowClass && windowClass != betaWindowClass {
		return "", fmt.Errorf("unsupported test window class %q", windowClass)
	}
	hwnd := findWailsWindowClass(windowClass)
	if hwnd == 0 {
		return "", fmt.Errorf("test-owned Wails parent HWND with class %q was not found", windowClass)
	}
	path, err := os.CreateTemp(evidenceDir, "browser-preview-window-*.png")
	if err != nil {
		return "", fmt.Errorf("reserve parent-window evidence path: %w", err)
	}
	evidencePath := path.Name()
	if err := path.Close(); err != nil {
		_ = os.Remove(evidencePath)
		return "", err
	}
	if err := os.Remove(evidencePath); err != nil {
		return "", fmt.Errorf("prepare parent-window evidence path: %w", err)
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBrowserPreviewInProductionUI$", "-test.timeout=8s")
	cmd.Env = append(os.Environ(),
		browserWailsCaptureEnv+"=1",
		"ORCA_BROWSER_WAILS_CAPTURE_HWND="+strconv.FormatUint(uint64(hwnd), 10),
		"ORCA_BROWSER_WAILS_CAPTURE_PID="+strconv.Itoa(os.Getpid()),
		"ORCA_BROWSER_WAILS_CAPTURE_PATH="+evidencePath,
		"ORCA_BROWSER_WAILS_CAPTURE_CLASS="+windowClass,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(evidencePath)
		return "", fmt.Errorf("bounded PrintWindow helper failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
	data, err := os.ReadFile(evidencePath)
	if err != nil {
		return "", fmt.Errorf("read PrintWindow helper evidence: %w", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 100 || config.Height < 100 {
		_ = os.Remove(evidencePath)
		return "", fmt.Errorf("PrintWindow helper did not produce a valid full-window PNG (%dx%d): %v", config.Width, config.Height, err)
	}
	return evidencePath, nil
}

func captureBrowserWailsWindowHelper(t *testing.T) {
	t.Helper()
	hwnd, err := strconv.ParseUint(os.Getenv("ORCA_BROWSER_WAILS_CAPTURE_HWND"), 10, 64)
	if err != nil || hwnd == 0 {
		t.Fatalf("invalid test-owned Wails HWND: %v", err)
	}
	expectedPID, err := strconv.ParseUint(os.Getenv("ORCA_BROWSER_WAILS_CAPTURE_PID"), 10, 32)
	if err != nil || expectedPID == 0 {
		t.Fatalf("invalid Wails fixture process id: %v", err)
	}
	path := os.Getenv("ORCA_BROWSER_WAILS_CAPTURE_PATH")
	if !filepath.IsAbs(path) || filepath.Ext(path) != ".png" {
		t.Fatalf("invalid parent-window PNG output path %q", path)
	}
	user32 := windows.NewLazySystemDLL("user32.dll")
	if err := user32.Load(); err != nil {
		t.Fatalf("load user32 for bounded PrintWindow helper: %v", err)
	}
	// Match PrintWindow's physical pixels instead of allocating a DPI-virtualized
	// bitmap, which silently crops the right/bottom edges at 125% or 150% scale.
	stdruntime.LockOSThread()
	defer stdruntime.UnlockOSThread()
	dpi := user32.NewProc("SetThreadDpiAwarenessContext")
	previousDPI, _, dpiErr := dpi.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
	if previousDPI == 0 {
		t.Fatalf("set physical-pixel capture context: %v", dpiErr)
	}
	defer dpi.Call(previousDPI)
	procIsWindow := user32.NewProc("IsWindow")
	procGetWindowThreadProcessID := user32.NewProc("GetWindowThreadProcessId")
	procGetClassName := user32.NewProc("GetClassNameW")
	procPrintWindow := user32.NewProc("PrintWindow")
	if ok, _, _ := procIsWindow.Call(uintptr(hwnd)); ok == 0 {
		t.Fatal("requested Wails HWND is not a live window")
	}
	var actualPID uint32
	procGetWindowThreadProcessID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&actualPID)))
	if actualPID != uint32(expectedPID) {
		t.Fatalf("refusing to capture HWND from pid %d; expected isolated Wails fixture pid %d", actualPID, expectedPID)
	}
	var class [128]uint16
	expectedClass := os.Getenv("ORCA_BROWSER_WAILS_CAPTURE_CLASS")
	if expectedClass == "" {
		expectedClass = browserhost.WindowClass
	}
	if expectedClass != browserhost.WindowClass && expectedClass != betaWindowClass {
		t.Fatalf("unsupported capture class %q", expectedClass)
	}
	length, _, _ := procGetClassName.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
	if length == 0 || windows.UTF16ToString(class[:length]) != expectedClass {
		t.Fatalf("refusing to capture unexpected HWND class %q; expected %q", windows.UTF16ToString(class[:]), expectedClass)
	}
	var rect win.RECT
	if !win.GetWindowRect(win.HWND(hwnd), &rect) {
		t.Fatal("GetWindowRect failed for the identified Wails fixture window")
	}
	width, height := int(rect.Right-rect.Left), int(rect.Bottom-rect.Top)
	if width < 100 || height < 100 || width > 8192 || height > 8192 || width*height > 32*1024*1024 {
		t.Fatalf("refusing implausible Wails fixture window dimensions %dx%d", width, height)
	}
	dc := win.CreateCompatibleDC(0)
	if dc == 0 {
		t.Fatal("CreateCompatibleDC failed")
	}
	defer win.DeleteDC(dc)
	header := win.BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})), BiWidth: int32(width), BiHeight: -int32(height), BiPlanes: 1, BiBitCount: 32}
	var bits unsafe.Pointer
	bitmap := win.CreateDIBSection(dc, &header, 0, &bits, 0, 0)
	if bitmap == 0 || bits == nil {
		t.Fatal("CreateDIBSection failed for PrintWindow target")
	}
	defer win.DeleteObject(win.HGDIOBJ(bitmap))
	previous := win.SelectObject(dc, win.HGDIOBJ(bitmap))
	if previous == 0 {
		t.Fatal("SelectObject failed for PrintWindow target")
	}
	defer win.SelectObject(dc, previous)
	if ok, _, callErr := procPrintWindow.Call(uintptr(hwnd), uintptr(dc), 2); ok == 0 {
		t.Fatalf("PrintWindow(PW_RENDERFULLCONTENT=2) returned false: %v", callErr)
	}
	pixels := unsafe.Slice((*byte)(bits), width*height*4)
	colors := make(map[uint32]struct{}, 8)
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < len(pixels); i += 4 {
		b, g, r := pixels[i], pixels[i+1], pixels[i+2]
		if len(colors) < 8 {
			colors[uint32(r)<<16|uint32(g)<<8|uint32(b)] = struct{}{}
		}
		rgba.Pix[i], rgba.Pix[i+1], rgba.Pix[i+2], rgba.Pix[i+3] = r, g, b, 255
	}
	if len(colors) < 2 {
		t.Fatalf("PrintWindow returned a uniform/blank image (%d distinct sampled colors); no evidence saved", len(colors))
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create parent-window PNG: %v", err)
	}
	if err := png.Encode(file, rgba); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		t.Fatalf("encode parent-window PNG: %v", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		t.Fatalf("close parent-window PNG: %v", err)
	}
}

func exerciseBrowserWailsProductionUI(t *testing.T, ctx, wctx context.Context, app *App, fixture *BrowserWailsUIFixture, ownerID, address, evidenceDir string) error {
	defer fixture.allowOverlayCheck()
	wruntime.WindowExecJS(wctx, browserWailsUIFlow(address))
	onboardingVisible, err := fixture.next(ctx)
	if err != nil {
		return fmt.Errorf("wait for visible first-run onboarding evidence: %w", err)
	}
	if onboardingVisible.Stage != "onboarding-visible" || onboardingVisible.Error != "" || !onboardingVisible.OnboardingVisible || !isOnboardingSkipLabel(onboardingVisible.OnboardingSkipLabel) {
		return fmt.Errorf("first-run onboarding/Skip control was not visibly available: %+v", onboardingVisible)
	}
	t.Logf("first-run onboarding was visible; the real Skip control passed viewport/hit-test checks (label=%q)", onboardingVisible.OnboardingSkipLabel)
	onboardingDismissed, err := fixture.next(ctx)
	if err != nil {
		return fmt.Errorf("wait for onboarding Skip completion: %w", err)
	}
	if onboardingDismissed.Stage != "onboarding-dismissed" || onboardingDismissed.Error != "" || onboardingDismissed.OnboardingVisible || !isOnboardingSkipLabel(onboardingDismissed.OnboardingSkipLabel) {
		return fmt.Errorf("real onboarding Skip did not dismiss the overlay: %+v", onboardingDismissed)
	}
	if app.NeedsOnboarding() {
		return fmt.Errorf("real onboarding Skip dismissed its UI but did not persist completion in the isolated test config")
	}
	t.Logf("real onboarding Skip completed and the modal is absent before Globe activation (label=%q)", onboardingDismissed.OnboardingSkipLabel)

	navigated, err := fixture.next(ctx)
	if err != nil {
		return fmt.Errorf("wait for actual embedded UI navigation report: %w", err)
	}
	if navigated.Stage != "navigated" {
		state := app.GetBrowserState(ownerID)
		app.browser.mu.Lock()
		owner := app.browser.owners[ownerID]
		active, viewportVisible := "", false
		if owner != nil {
			active, viewportVisible = owner.active, owner.viewport.Visible
		}
		createdTabs := 0
		createdURLs := make([]string, 0, len(app.browser.tabs))
		for _, tab := range app.browser.tabs {
			if tab.created {
				createdTabs++
				createdURLs = append(createdURLs, tab.URL)
			}
		}
		app.browser.mu.Unlock()
		return fmt.Errorf("UI phase=%s stage=%s error=%s; DOM tabs=%d selected=%d nativeVisible=%s owner=%s address=%s title=%q viewport=%.0fx%.0f; App supported=%v active=%s tabs=%d hostCreated=%d hostURLs=%v ownerActive=%s viewportVisible=%v error=%q",
			navigated.Phase, navigated.Stage, navigated.Error, navigated.TabCount, navigated.SelectedTabs, navigated.NativeVisible, navigated.Owner, navigated.Address, navigated.Title, navigated.ViewportWidth, navigated.ViewportH,
			state.Supported, state.ActiveTabID, len(state.Tabs), createdTabs, createdURLs, active, viewportVisible, state.Error)
	}
	if navigated.Error != "" || navigated.Address != address {
		return fmt.Errorf("local navigation DOM evidence mismatch: %+v", navigated)
	}
	if navigated.ViewportWidth < 200 || navigated.ViewportH < 100 {
		return fmt.Errorf("browser panel viewport has implausible DOM geometry: %+v", navigated)
	}

	state := app.GetBrowserState(ownerID)
	if !state.Supported || state.Error != "" || state.ActiveTabID == "" || len(state.Tabs) == 0 {
		return fmt.Errorf("App binding did not create browserhost tab: %+v", state)
	}
	activeURL := ""
	for _, browserTab := range state.Tabs {
		if browserTab.ID == state.ActiveTabID {
			activeURL = browserTab.URL
		}
	}
	if activeURL != address || browserWailsUIRequests.Load() == 0 {
		return fmt.Errorf("native browser did not complete local httptest navigation: activeURL=%q requests=%d", activeURL, browserWailsUIRequests.Load())
	}
	app.browser.mu.Lock()
	owner := app.browser.owners[ownerID]
	if owner == nil || owner.active != state.ActiveTabID || !owner.viewport.Visible {
		app.browser.mu.Unlock()
		return fmt.Errorf("browserService owner/visible viewport not established: owner=%+v", owner)
	}
	viewport := owner.viewport
	tab := app.browser.tabs[state.ActiveTabID]
	created := tab != nil && tab.created
	app.browser.mu.Unlock()
	if !created {
		return fmt.Errorf("browserService tab %q was not created by the real UI", state.ActiveTabID)
	}
	physicalViewport := viewport
	physicalViewport.X *= viewport.Scale
	physicalViewport.Y *= viewport.Scale
	physicalViewport.Width *= viewport.Scale
	physicalViewport.Height *= viewport.Scale
	physicalViewport.Scale = 1
	if err := verifyWailsChildBounds(ctx, physicalViewport); err != nil {
		return fmt.Errorf("native browserhost viewport does not match production DOM viewport: %w (DOM %+v)", err, navigated)
	}

	screenshotCtx, cancelScreenshot := context.WithTimeout(ctx, 6*time.Second)
	pngJSON, err := app.browser.host.Command(screenshotCtx, state.ActiveTabID, "screenshot", "")
	cancelScreenshot()
	var captureErr error
	var pagePNG []byte
	if err != nil {
		observeCtx, cancelObserve := context.WithTimeout(ctx, 4*time.Second)
		observation, observeErr := app.browser.host.Observe(observeCtx, state.ActiveTabID)
		cancelObserve()
		if observeErr == nil && observation.MIME == "image/jpeg" && len(observation.Screenshot) > 0 {
			img, decodeErr := jpeg.Decode(bytes.NewReader(observation.Screenshot))
			if decodeErr == nil {
				var encoded bytes.Buffer
				if encodeErr := png.Encode(&encoded, img); encodeErr == nil {
					pagePNG = encoded.Bytes()
				}
			}
		}
		if len(pagePNG) == 0 {
			diagnosticCtx, cancelDiagnostic := context.WithTimeout(ctx, 3*time.Second)
			stateJSON, stateErr := app.browser.host.Command(diagnosticCtx, state.ActiveTabID, "state", "")
			cancelDiagnostic()
			captureErr = fmt.Errorf("capture browserhost page PNG: %w; native Observe=%v mime=%q bytes=%d blocked=%q; immediate native state=%s (state command: %v)", err, observeErr, observation.MIME, len(observation.Screenshot), observation.ScreenshotBlocked, stateJSON, stateErr)
		}
	} else {
		var screenshot struct {
			Data string `json:"data"`
		}
		if err = json.Unmarshal([]byte(pngJSON), &screenshot); err != nil {
			captureErr = fmt.Errorf("decode browserhost PNG response: %w", err)
		} else {
			pagePNG, err = base64.StdEncoding.DecodeString(screenshot.Data)
			if err != nil || len(pagePNG) < 100 || string(pagePNG[:8]) != "\x89PNG\r\n\x1a\n" {
				captureErr = fmt.Errorf("browserhost returned invalid page PNG (%d bytes): %v", len(pagePNG), err)
			}
		}
	}
	if captureErr == nil {
		if len(pagePNG) < 100 || string(pagePNG[:8]) != "\x89PNG\r\n\x1a\n" {
			captureErr = fmt.Errorf("browserhost capture did not produce a valid PNG (%d bytes)", len(pagePNG))
		} else if evidence, createErr := os.CreateTemp(evidenceDir, "browser-preview-ui-*.png"); createErr != nil {
			captureErr = fmt.Errorf("create browser page evidence file: %w", createErr)
		} else {
			if _, writeErr := evidence.Write(pagePNG); writeErr != nil {
				captureErr = fmt.Errorf("write browser page evidence: %w", writeErr)
			}
			if closeErr := evidence.Close(); closeErr != nil && captureErr == nil {
				captureErr = closeErr
			}
		}
	}
	windowPNG, windowCaptureErr := captureWailsParentWindow(ctx, evidenceDir)
	if windowCaptureErr != nil {
		t.Logf("optional PrintWindow(PW_RENDERFULLCONTENT=2) parent capture unavailable: %v; no full-window evidence saved", windowCaptureErr)
	} else {
		t.Logf("PrintWindow(PW_RENDERFULLCONTENT=2) captured only test-owned Wails HWND class=%q pid=%d: %s", browserhost.WindowClass, os.Getpid(), windowPNG)
	}
	fixture.allowOverlayCheck()

	overlay, err := fixture.next(ctx)
	if err != nil {
		return fmt.Errorf("wait for Settings overlay UI report: %w", err)
	}
	if overlay.Stage != "overlay" || overlay.Error != "" {
		return fmt.Errorf("Settings overlay report failed: %+v", overlay)
	}
	hideDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(hideDeadline) {
		app.browser.mu.Lock()
		owner = app.browser.owners[ownerID]
		hidden := owner != nil && !owner.viewport.Visible && app.browser.shownOwner != ownerID
		app.browser.mu.Unlock()
		if hidden && verifyWailsChildHidden(ctx) == nil {
			if captureErr != nil {
				return fmt.Errorf("Settings overlay hid the native browser child, but page screenshot evidence is unavailable: %w", captureErr)
			}
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("browser child remained visible behind Settings overlay")
}

func isOnboardingSkipLabel(label string) bool {
	label = strings.Join(strings.Fields(label), " ")
	return label == "暂不配置，进入应用" || label == "Skip setup and open the app"
}

func browserWailsUIFlow(address string) string {
	encoded, _ := json.Marshal(address)
	script := `(async()=>{
const target=__TARGET__,failures=[];let phase="locate-globe",onboardingSkipLabel="";
window.addEventListener("error",e=>failures.push("error:"+e.message));
window.addEventListener("unhandledrejection",e=>failures.push("promise:"+String(e.reason)));
const report=(stage,error="")=>{
 const panel=document.querySelector(".browser-panel"),view=document.querySelector(".browser-panel__viewport"),input=document.querySelector(".browser-panel__address input");
 const onboarding=document.querySelector(".onboarding [role=dialog][aria-modal=true]"),skip=onboarding?.querySelector("button.onboarding__skip");
 const tabs=[...document.querySelectorAll(".browser-panel__tab [role=tab]")],tab=tabs.find(x=>x.getAttribute("aria-selected")==="true")||tabs[0],p=view?.getBoundingClientRect(),r=panel?.getBoundingClientRect();
 return window.go.main.BrowserWailsUIFixture.ReportBrowserUI(JSON.stringify({stage,phase,error,onboardingVisible:!!onboarding,onboardingSkipLabel:skip?.textContent?.trim()||onboardingSkipLabel,tabCount:tabs.length,selectedTabs:tabs.filter(x=>x.getAttribute("aria-selected")==="true").length,nativeVisible:panel?.dataset.nativeVisible||"",owner:panel?.dataset.ownerTabId||"",address:input?.value||"",title:tab?.textContent||"",panelWidth:r?.width||0,panelHeight:r?.height||0,viewportX:p?.x||0,viewportY:p?.y||0,viewportWidth:p?.width||0,viewportHeight:p?.height||0}));
};
const wait=async(fn,label)=>{const end=Date.now()+12000;while(Date.now()<end){const value=fn();if(value)return value;await new Promise(r=>setTimeout(r,100));}const alerts=[...document.querySelectorAll("[role=alert]")].map(e=>e.innerText).join(" | ");throw new Error("timeout: "+label+"; alerts="+alerts+"; failures="+failures.join(" | "));};
try{
 phase="locate-onboarding-skip";const onboarding=await wait(()=>document.querySelector(".onboarding [role=dialog][aria-modal=true]"),"first-run onboarding dialog");const skip=onboarding.querySelector("button.onboarding__skip");if(!skip)throw new Error("onboarding Skip button not found");const skipLabel=skip.textContent.trim();if(!/^(暂不配置，进入应用|Skip setup and open the app)$/.test(skipLabel))throw new Error("unexpected onboarding Skip label: "+skipLabel);if(skip.disabled)throw new Error("onboarding Skip button is disabled");skip.scrollIntoView({block:"center"});await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));const sr=skip.getBoundingClientRect(),hit=document.elementFromPoint(sr.left+sr.width/2,sr.top+sr.height/2);if(sr.width<1||sr.height<1||sr.left<0||sr.top<0||sr.right>innerWidth||sr.bottom>innerHeight||!(hit===skip||skip.contains(hit)))throw new Error("onboarding Skip is not an unobstructed visible hit target");onboardingSkipLabel=skipLabel;phase="verify-onboarding-visible";await report("onboarding-visible");
 phase="click-real-onboarding-skip";skip.click();await wait(()=>!document.querySelector(".onboarding [role=dialog][aria-modal=true]"),"onboarding overlay dismissal after Skip action");phase="verify-onboarding-dismissed";await report("onboarding-dismissed");
 phase="click-globe";const globe=await wait(()=>document.querySelector(".app-chrome__browser-toggle,.modern-chrome__workspace-actions button:has(svg.lucide-globe)"),"Globe button");const gr=globe.getBoundingClientRect(),ghit=document.elementFromPoint(gr.left+gr.width/2,gr.top+gr.height/2);if(gr.width<1||gr.height<1||gr.left<0||gr.top<0||gr.right>innerWidth||gr.bottom>innerHeight||!(ghit===globe||globe.contains(ghit)))throw new Error("Globe is not an unobstructed visible hit target after onboarding dismissal");globe.click();
 phase="open-browser-dock";const dock=await wait(()=>[...document.querySelectorAll(".workbench-dock__tab")].find(b=>/browser|浏览器/i.test(b.textContent||"")),"browser dock tab");dock.click();
 phase="wait-browser-panel";await wait(()=>document.querySelector(".browser-panel__address input"),"real BrowserPanel");
 phase="click-new-tab";const add=await wait(()=>document.querySelector(".browser-panel__tabstrip > button.browser-panel__icon"),"new browser tab control");add.click();
 phase="wait-selected-tab";await wait(()=>document.querySelector(".browser-panel__tab [role=tab][aria-selected=true]"),"browserhost tab created through UI");
 phase="submit-local-address";const input=document.querySelector(".browser-panel__address input"),setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,"value").set;setter.call(input,target);input.dispatchEvent(new InputEvent("input",{bubbles:true,inputType:"insertText",data:target}));input.closest("form").dispatchEvent(new SubmitEvent("submit",{bubbles:true,cancelable:true,submitter:input.closest("form").querySelector("button[type=submit]")}));
 phase="wait-local-navigation";await wait(()=>{const tab=document.querySelector(".browser-panel__tab [role=tab][aria-selected=true]");return input.value===target&&tab&&tab.textContent.includes("ORCA Browser UI fixture")?tab:null;},"httptest page title and address");
 phase="verify-viewport-dom";await wait(()=>{const r=document.querySelector(".browser-panel__viewport").getBoundingClientRect();return r.width>200&&r.height>100?r:null;},"nonzero native viewport geometry");await report("navigated");
 phase="wait-native-capture";await window.go.main.BrowserWailsUIFixture.WaitForBrowserUICapture();
 phase="open-settings-overlay";const settings=await wait(()=>document.querySelector("button.sidebar__navitem svg.lucide-settings"),"Settings navigation button");settings.closest("button").click();await wait(()=>document.querySelector("[role=dialog][aria-modal=true]"),"Settings overlay");await new Promise(r=>setTimeout(r,500));await report("overlay");
}catch(e){try{await report("error",String(e&&e.stack||e));}catch(_){}}
})();`
	return strings.Replace(script, "__TARGET__", string(encoded), 1)
}
