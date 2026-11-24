//go:build windows

// Package webbetahost embeds an isolated, unprivileged website WebView2. It
// never exposes Wails bindings or uses desktop-wide input/clipboard APIs.
package webbetahost

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

//go:embed page.js
var pageScript string

//go:embed probe.js
var probeScript string

//go:embed site.js
var siteScript string

var user32 = windows.NewLazySystemDLL("user32.dll")
var createWindow = user32.NewProc("CreateWindowExW")
var destroyWindow = user32.NewProc("DestroyWindow")
var showWindow = user32.NewProc("ShowWindow")
var setWindowPos = user32.NewProc("SetWindowPos")
var postThread = user32.NewProc("PostThreadMessageW")
var enumWindows = user32.NewProc("EnumWindows")
var getPID = user32.NewProc("GetWindowThreadProcessId")
var getMessage = user32.NewProc("GetMessageW")
var translate = user32.NewProc("TranslateMessage")
var dispatch = user32.NewProc("DispatchMessageW")
var getParent = user32.NewProc("GetParent")
var getClass = user32.NewProc("GetClassNameW")

const wakeMessage = 0x8000 + 173

type Bounds struct {
	X       int32 `json:"x"`
	Y       int32 `json:"y"`
	Width   int32 `json:"width"`
	Height  int32 `json:"height"`
	Visible bool  `json:"visible"`
}
type msg struct {
	hwnd           uintptr
	message        uint32
	wparam, lparam uintptr
	time           uint32
	x, y           int32
	private        uint32
}
type Browser struct {
	mu           sync.Mutex
	state        webbeta.BrowserState
	thread       uint32
	hwnd         uintptr
	chrome       *edge.Chromium
	commands     chan func()
	events       chan webbeta.Event
	active       string
	generation   string
	closed       bool
	ready        chan error
	done         chan struct{}
	process      windows.Handle
	closeError   error
	loginOnly    bool
	windowClass  string
	presented    bool
	debugHistory []json.RawMessage
	recheckID    string
	recheck      chan webbeta.BrowserState
	identity     identityLatch
}

func New(profile string) (*Browser, error) {
	return NewWithLoginMode(profile, false)
}

func NewWithLoginMode(profile string, loginOnly bool) (*Browser, error) {
	return newBrowser(profile, loginOnly, "wailsWindow")
}

func NewWithWindowClass(profile, windowClass string) (*Browser, error) {
	if windowClass == "" {
		return nil, errors.New("missing host window class")
	}
	return newBrowser(profile, false, windowClass)
}

// NewLoginOnlyWithWindowClass cannot run the site adapter or automated requests.
func NewLoginOnlyWithWindowClass(profile, windowClass string) (*Browser, error) {
	if windowClass == "" {
		return nil, errors.New("missing host window class")
	}
	return newBrowser(profile, true, windowClass)
}

var ErrLoginOnly = errors.New("login preview does not allow automated requests")

func newBrowser(profile string, loginOnly bool, windowClass string) (*Browser, error) {
	identity, err := loadIdentityLatch(profile)
	if err != nil {
		return nil, err
	}
	b := &Browser{commands: make(chan func(), 64), events: make(chan webbeta.Event, 128), ready: make(chan error, 1), done: make(chan struct{})}
	b.identity = identity
	b.loginOnly = loginOnly
	b.windowClass = windowClass
	go b.loop(profile)
	select {
	case err := <-b.ready:
		return b, err
	case <-time.After(20 * time.Second):
		b.Close()
		return b, errors.New("WebView2 initialization timed out")
	}
}
func ownWindow(windowClass string) uintptr {
	var result uintptr
	cb := syscall.NewCallback(func(h, l uintptr) uintptr {
		var pid uint32
		getPID.Call(h, uintptr(unsafe.Pointer(&pid)))
		parent, _, _ := getParent.Call(h)
		if pid == uint32(os.Getpid()) && parent == 0 {
			var class [128]uint16
			getClass.Call(h, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
			if windows.UTF16ToString(class[:]) == windowClass {
				result = h
				return 0
			}
		}
		return 1
	})
	enumWindows.Call(cb, 0)
	return result
}
func (b *Browser) loop(profile string) {
	defer close(b.done)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ole := windows.NewLazySystemDLL("ole32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 2)
	if int32(hr) < 0 {
		b.ready <- errors.New("COM initialization failed")
		return
	}
	defer ole.NewProc("CoUninitialize").Call()
	parent := ownWindow(b.windowClass)
	if parent == 0 {
		b.ready <- errors.New("O.R.C.A. window is not ready")
		return
	}
	class, _ := windows.UTF16PtrFromString("STATIC")
	hwnd, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(class)), 0, 0x40000000|0x02000000, 0, 0, 1000, 700, parent, 0, 0, 0)
	if hwnd == 0 {
		b.ready <- fmt.Errorf("create browser child: %w", err)
		return
	}
	defer destroyWindow.Call(hwnd)
	c := edge.NewChromium()
	var loginHandlers []*loginBoundaryCallback
	defer func() { runtime.KeepAlive(loginHandlers) }()
	defer func() {
		var processErr error
		b.mu.Lock()
		missingProcess := b.process == 0
		b.mu.Unlock()
		if missingProcess && c.GetController() != nil {
			process, err := ownBrowserProcess(c)
			processErr = err
			b.mu.Lock()
			b.process = process
			b.mu.Unlock()
		}
		b.mu.Lock()
		b.closed = true
		b.generation = ""
		b.state = webbeta.BrowserState{Blocked: "identity_closed"}
		b.mu.Unlock()
		err := errors.Join(processErr, releaseChromium(c))
		b.mu.Lock()
		b.chrome = nil
		b.closeError = err
		b.mu.Unlock()
	}()
	c.DataPath = profile
	c.MessageCallback = func(message string, sender *edge.ICoreWebView2, args *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		source, err := args.GetSource()
		if err != nil || !webbeta.AllowedSource(source) {
			return
		}
		current, err := sender.GetSource()
		if err != nil || source != current {
			return
		}
		var ev webbeta.Event
		if len(message) > 2<<20 || json.Unmarshal([]byte(message), &ev) != nil {
			return
		}
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return
		}
		if b.loginOnly {
			valid := ev.Generation != "" && ev.Generation == b.generation && ev.Kind == "login_health"
			b.mu.Unlock()
			if valid && (ev.Code == "login_controls_present" || ev.Code == "document_loaded") {
				raw, _ := json.Marshal(map[string]any{"stage": ev.Code, "officialOrigin": true, "automaticSending": false})
				_ = os.WriteFile(filepath.Join(filepath.Dir(profile), "login-health.json"), raw, 0600)
			}
			return
		}
		if ev.Kind == "composer_probe" {
			valid := ev.Generation != "" && ev.Generation == b.generation && allowUnverifiedSite && os.Getenv("ORCA_WEB_BETA_PROBE") == "1"
			b.mu.Unlock()
			if valid && len(ev.Text) < 64000 {
				_ = os.WriteFile(filepath.Join(filepath.Dir(profile), "composer-probe.json"), []byte(ev.Text), 0600)
			}
			return
		}
		if (ev.Kind == "state" || ev.Kind == "rechecked") && ev.State != nil {
			if ev.Generation == "" || ev.Generation != b.generation {
				b.mu.Unlock()
				return
			}
			b.state = b.identity.observe(*ev.State)
			if ev.Kind == "rechecked" && ev.ID == b.recheckID && b.recheck != nil {
				select {
				case b.recheck <- b.state:
				default:
				}
			}
			b.mu.Unlock()
			return
		}
		active := b.active
		validGeneration := ev.Generation != "" && ev.Generation == b.generation
		b.mu.Unlock()
		if !validGeneration || active == "" || ev.ID != active {
			return
		}
		if ev.Kind == "owned_debug" {
			if allowUnverifiedSite && len(ev.Text) < 16000 {
				_ = os.WriteFile(filepath.Join(filepath.Dir(profile), "acceptance-debug.json"), []byte(ev.Text), 0600)
				if json.Valid([]byte(ev.Text)) {
					b.mu.Lock()
					if len(b.debugHistory) < 24 {
						b.debugHistory = append(b.debugHistory, json.RawMessage(ev.Text))
					}
					raw, _ := json.Marshal(b.debugHistory)
					b.mu.Unlock()
					_ = os.WriteFile(filepath.Join(filepath.Dir(profile), "acceptance-phases.json"), raw, 0600)
				}
			}
			return
		}
		select {
		case b.events <- ev:
		default:
			b.block("event_backpressure")
		}
	}
	c.NavigationCompletedCallback = func(sender *edge.ICoreWebView2, args *edge.ICoreWebView2NavigationCompletedEventArgs) {
		u, err := sender.GetSource()
		var nonce [16]byte
		if _, e := rand.Read(nonce[:]); e != nil {
			b.block("navigation_identity_failed")
			return
		}
		generation := hex.EncodeToString(nonce[:])
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return
		}
		b.state = webbeta.BrowserState{URL: u}
		b.generation = generation
		b.mu.Unlock()
		if err == nil && webbeta.AllowedSource(u) {
			for _, script := range documentScripts(b.loginOnly, generation, allowUnverifiedSite, os.Getenv("ORCA_WEB_BETA_PROBE") == "1") {
				c.Eval(script)
			}
		} else {
			b.block("external_navigation")
		}
	}
	c.ProcessFailedCallback = func(*edge.ICoreWebView2, *edge.ICoreWebView2ProcessFailedEventArgs) {
		b.block("browser_process_failed")
	}
	if !c.Embed(hwnd) {
		b.ready <- errors.New("WebView2 unavailable")
		return
	}
	if b.loginOnly {
		view, err := c.GetController().GetCoreWebView2()
		if err == nil {
			loginHandlers, err = installLoginBoundary(view)
			view.Release()
		}
		if err != nil {
			b.ready <- err
			return
		}
	}
	settings, err := c.GetSettings()
	if err != nil {
		b.ready <- err
		return
	}
	defer settings.Release()
	if err = settings.PutAreHostObjectsAllowed(false); err != nil {
		b.ready <- err
		return
	}
	if b.loginOnly {
		for _, set := range []func(bool) error{settings.PutAreDefaultScriptDialogsEnabled, settings.PutAreDevToolsEnabled, settings.PutAreDefaultContextMenusEnabled} {
			if err := set(false); err != nil {
				b.ready <- err
				return
			}
		}
	}
	c.SetGlobalPermission(edge.CoreWebView2PermissionStateDeny)
	// A hidden parent keeps input focus untouched; the controller still needs
	// a real viewport and rendering from cold start, before opening the panel.
	c.Resize()
	if err := c.Show(); err != nil {
		b.ready <- err
		return
	}
	process, err := ownBrowserProcess(c)
	if err != nil {
		b.ready <- err
		return
	}
	b.mu.Lock()
	b.process = process
	b.chrome = c
	b.hwnd = hwnd
	b.thread = windows.GetCurrentThreadId()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return
	}
	c.Navigate(webbeta.Origin)
	b.ready <- nil
	var m msg
	for {
		r, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			break
		}
		if m.message == wakeMessage {
			for {
				select {
				case fn := <-b.commands:
					fn()
				default:
					goto drained
				}
			}
		}
	drained:
		b.mu.Lock()
		closed := b.closed
		b.mu.Unlock()
		if closed {
			break
		}
		translate.Call(uintptr(unsafe.Pointer(&m)))
		dispatch.Call(uintptr(unsafe.Pointer(&m)))
	}
}
func (b *Browser) post(fn func()) error {
	b.mu.Lock()
	thread, closed := b.thread, b.closed
	b.mu.Unlock()
	if closed || thread == 0 {
		return errors.New("browser disconnected")
	}
	select {
	case b.commands <- func() {
		b.mu.Lock()
		closed := b.closed
		b.mu.Unlock()
		if !closed {
			fn()
		}
	}:
		postThread.Call(uintptr(thread), wakeMessage, 0, 0)
		return nil
	default:
		return errors.New("browser command queue full")
	}
}
func (b *Browser) State() webbeta.BrowserState { b.mu.Lock(); defer b.mu.Unlock(); return b.state }
func (b *Browser) GuardIdentity(existing bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.identity.known = b.identity.known || existing
	b.state = b.identity.observe(b.state)
}
func (b *Browser) Presented() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.presented }
func (b *Browser) Recheck(ctx context.Context) error {
	if b.loginOnly {
		return ErrLoginOnly
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	id := hex.EncodeToString(nonce[:])
	b.mu.Lock()
	if b.identity.lost {
		b.mu.Unlock()
		return ErrRecheckIdentity
	}
	if b.active != "" || b.recheck != nil {
		b.mu.Unlock()
		return ErrRecheckBusy
	}
	ch := make(chan webbeta.BrowserState, 1)
	b.recheckID, b.recheck = id, ch
	b.mu.Unlock()
	defer func() { b.mu.Lock(); b.recheckID = ""; b.recheck = nil; b.mu.Unlock() }()
	// A fresh view may still be navigating. Do not lose a one-shot recheck by
	// evaluating it before the document adapter has been installed.
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		b.mu.Lock()
		generation, closed, blocked := b.generation, b.closed, b.state.Blocked
		b.mu.Unlock()
		if closed {
			return ErrRecheckUnavailable
		}
		if generation != "" {
			break
		}
		if blocked != "" {
			return recheckStateError(b.State())
		}
		select {
		case <-ctx.Done():
			return errors.Join(ErrRecheckTimeout, ctx.Err())
		case <-tick.C:
		}
	}
	if err := b.post(func() { b.chrome.Eval(`window.__orcaWebBeta?.recheck("` + id + `")`) }); err != nil {
		return ErrRecheckUnavailable
	}
	select {
	case state := <-ch:
		return recheckStateError(state)
	case <-ctx.Done():
		return errors.Join(ErrRecheckTimeout, ctx.Err())
	}
}
func (b *Browser) block(code string) {
	b.mu.Lock()
	b.state.Ready = false
	b.state.Blocked = code
	b.mu.Unlock()
}
func (b *Browser) SetBounds(v Bounds) error {
	return b.post(func() {
		if v.Width < 1 || v.Height < 1 || !v.Visible {
			// Hide the native host without suspending the website controller.
			// A live Agent needs its established viewport while the panel is closed.
			// This changes no page APIs, identity or security settings.
			showWindow.Call(b.hwnd, 0)
			b.mu.Lock()
			b.presented = false
			b.mu.Unlock()
			return
		}
		// The controller was created beneath a hidden host. Showing only the
		// STATIC window does not unhide WebView2's own child HWNDs.
		// HWND_TOP affects siblings only; SWP_NOACTIVATE preserves user focus.
		ok, _, _ := setWindowPos.Call(b.hwnd, 0, uintptr(v.X), uintptr(v.Y), uintptr(v.Width), uintptr(v.Height), 0x0010|0x0040)
		if ok == 0 {
			b.block("browser_bounds_failed")
			return
		}
		b.chrome.Resize()
		if err := b.chrome.Show(); err != nil {
			b.block("browser_show_failed")
			return
		}
		b.mu.Lock()
		b.presented = true
		b.mu.Unlock()
	})
}
func (b *Browser) Close() {
	b.mu.Lock()
	b.closed = true
	b.generation = ""
	b.state = webbeta.BrowserState{Blocked: "identity_closed"}
	thread := b.thread
	b.mu.Unlock()
	if thread != 0 {
		postThread.Call(uintptr(thread), wakeMessage, 0, 0)
	}
}
func (b *Browser) Logout(ctx context.Context) error {
	// Cookie-only deletion is not logout: the runtime must drain and clear the
	// entire owned profile after this release barrier.
	return b.CloseAndWait(ctx)
}
func (b *Browser) Run(ctx context.Context, cmd webbeta.Command, emit func(webbeta.Event) error) (result error) {
	if err := ctx.Err(); err != nil {
		return webbeta.KnownUnsent(err)
	}
	if b.loginOnly {
		return webbeta.KnownUnsent(ErrLoginOnly)
	}
	b.mu.Lock()
	if b.active != "" || !b.state.Ready {
		b.mu.Unlock()
		return webbeta.KnownUnsent(&webbeta.Failure{Code: "page_not_ready"})
	}
	b.active = cmd.ID
	b.debugHistory = nil
	b.mu.Unlock()
	started := time.Now()
	var dispatchCancelled atomic.Bool
	diagnostic := &webbeta.DispatchDiagnosticError{Operation: cmd.Operation, Stage: "navigation"}
	publish := func() {
		diagnostic.ElapsedMS = time.Since(started).Milliseconds()
		provider.RecordCancellationDiagnostic(ctx, *diagnostic)
	}
	publish()
	defer func() {
		if result != nil {
			diagnostic.Cause = result
			diagnostic.ElapsedMS = time.Since(started).Milliseconds()
			result = diagnostic
			publish()
		}
	}()
	stop := func(cause error) error {
		diagnostic.RecordTrigger(cause)
		dispatchCancelled.Store(true)
		err := b.stopWithDiagnostic(cmd.ID, cmd.URL, cause, diagnostic)
		diagnostic.Stop = "confirmed"
		var failure *webbeta.Failure
		if errors.As(err, &failure) && failure.Code == "stop_unconfirmed" {
			diagnostic.Stop = "unconfirmed"
			publish()
			return errors.Join(err, cause)
		}
		publish()
		return err
	}
	defer func() { b.mu.Lock(); b.active = ""; b.mu.Unlock() }()
	for {
		select {
		case <-b.events:
		default:
			goto drained
		}
	}
drained:
	if cmd.URL != "" && !webbeta.ValidChatURL(cmd.URL) {
		return webbeta.KnownUnsent(errors.New("invalid conversation URL"))
	}
	u := cmd.URL
	if u == "" {
		u = webbeta.Origin + "/"
	}
	b.mu.Lock()
	reuse := canReuseDocument(b.state, b.generation, u) && !b.identity.lost
	b.mu.Unlock()
	if !reuse {
		if err := b.navigateForDispatch(ctx, u); err != nil {
			return webbeta.KnownUnsent(err)
		}
	}
	diagnostic.Observe(webbeta.Event{Kind: "phase", Code: "ready"})
	publish()
	b.mu.Lock()
	generation := b.generation
	valid := b.state.Ready && b.state.URL == u && b.state.Blocked == "" && !b.identity.lost
	b.mu.Unlock()
	if !valid || generation == "" {
		return webbeta.KnownUnsent(&webbeta.Failure{Code: "page_not_ready"})
	}
	raw, err := marshalPageCommand(cmd)
	if err != nil {
		return webbeta.KnownUnsent(err)
	}
	if err = b.post(func() {
		if !dispatchCancelled.Load() && ctx.Err() == nil {
			b.chrome.Eval(dispatchScript(raw, generation))
		}
	}); err != nil {
		return webbeta.KnownUnsent(err)
	}
	diagnostic.Observe(webbeta.Event{Kind: "phase", Code: "dispatch_queued"})
	publish()
	return awaitBrowserResult(ctx, cmd.ID, b.events, emit, stop, b.block, diagnostic, 8*time.Second, 10*time.Minute)
}

// Navigation retries cannot replay a message: dispatch has not been queued yet.
func (b *Browser) navigateForDispatch(ctx context.Context, u string) error {
	for attempt := 0; attempt < 2; attempt++ {
		b.mu.Lock()
		b.state.Ready = false
		b.state.URL = ""
		b.generation = ""
		b.mu.Unlock()
		if err := b.post(func() { b.chrome.Navigate(u) }); err != nil {
			return err
		}
		if err := b.waitDispatchDocument(ctx, u); err != nil {
			var failure *webbeta.Failure
			if errors.As(err, &failure) && failure.Code == "navigation_timeout" {
				continue
			}
			return err
		}
		return nil
	}
	return &webbeta.Failure{Code: "navigation_timeout"}
}

func (b *Browser) waitDispatchDocument(ctx context.Context, u string) error {
	navDeadline := time.NewTimer(30 * time.Second)
	defer navDeadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-navDeadline.C:
			return &webbeta.Failure{Code: "navigation_timeout"}
		case <-tick.C:
			st := b.State()
			if webbeta.IsAccountBlocked(&webbeta.Failure{Code: st.Blocked}) {
				return &webbeta.Failure{Code: st.Blocked}
			}
			if st.Ready && !st.Busy && st.URL == u && st.Blocked == "" {
				return nil
			}
		}
	}
}
func (b *Browser) stop(id, url string, cause error) error {
	return b.stopWithDiagnostic(id, url, cause, nil)
}

func (b *Browser) stopWithDiagnostic(id, url string, cause error, diagnostic *webbeta.DispatchDiagnosticError) error {
	b.mu.Lock()
	generation := b.generation
	b.mu.Unlock()
	raw, _ := json.Marshal(id)
	_ = b.post(func() { b.chrome.Eval("window.__orcaWebBeta?.stop(" + string(raw) + ")") })
	t := time.NewTimer(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case ev := <-b.events:
			if diagnostic != nil && ev.Kind == "stop_failed" {
				b.observeStopFailure(id, url, generation, ev, diagnostic)
				continue
			}
			if ev.ID == id && ev.Kind == "stopped" && ev.Generation == generation {
				result, _ := b.confirmStopReceipt(id, url, generation, ev, cause)
				return result
			}
		case <-t.C:
			b.block("stop_unconfirmed")
			_ = b.post(func() { b.chrome.Navigate("about:blank") })
			return &webbeta.Failure{Code: "stop_unconfirmed", Sent: true}
		}
	}
}

func (b *Browser) observeStopFailure(id, url, generation string, ev webbeta.Event, diagnostic *webbeta.DispatchDiagnosticError) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.identity.lost || b.active != id || generation == "" || b.generation != generation || b.state.URL != url || ev.ID != id || ev.Kind != "stop_failed" || ev.Generation != generation {
		return
	}
	diagnostic.Observe(ev)
}

func (b *Browser) confirmStopReceipt(id, url, generation string, ev webbeta.Event, cause error) (error, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Recheck at consumption, not just when the page event entered the queue.
	if b.closed || b.identity.lost || b.active != id || generation == "" || b.generation != generation || b.state.URL != url || b.state.Blocked != "" || ev.ID != id || ev.Kind != "stopped" || ev.Generation != generation {
		return &webbeta.Failure{Code: "stop_unconfirmed", Sent: true}, false
	}
	if ev.Stop == nil {
		return cause, false
	}
	proof := *ev.Stop
	proof.RequestID, proof.URL, proof.Document = id, url, ev.Generation
	return webbeta.ConfirmStopped(cause, proof), true
}
