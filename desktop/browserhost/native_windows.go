//go:build windows

package browserhost

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"
	wv "github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost/internal/webviewabi"
	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

const WindowClass = "OrcaBrowserPreviewWindow"

// All COM objects belong to this actor's STA. No calls use the foreground
// window or a public remote-debugging socket.
type Host struct {
	once        sync.Once
	queue       chan func()
	ready       chan struct{}
	done        chan struct{}
	thread      uint32
	initErr     error
	tabs        map[string]*nativeTab
	envs        map[string]*wv.ICoreWebView2Environment
	emit        func(Event)
	policyMu    sync.RWMutex
	guard       func(string, string) bool
	closing     atomic.Bool
	parent      uintptr
	windowClass string
	started     atomic.Bool
	events      chan Event
	eventSafety nativeEventSafety
	transfers   transferStore
	uploads     uploadStore
}

type nativeTab struct {
	id            string
	view          *wv.ICoreWebView2
	controller    *wv.ICoreWebView2Controller
	hwnd          win.HWND
	generation    uint64
	document      string
	zoom          float64
	loading       bool
	closed        bool
	closeErr      error
	width, height float64
	world         int64
}

func New(emit func(Event)) *Host {
	return NewWithWindowClass(WindowClass, emit)
}

// The application selects its own exact class; lookup also requires our PID.
func NewWithWindowClass(windowClass string, emit func(Event)) *Host {
	return &Host{windowClass: windowClass, queue: make(chan func(), 128), ready: make(chan struct{}), done: make(chan struct{}), tabs: map[string]*nativeTab{}, envs: map[string]*wv.ICoreWebView2Environment{}, emit: emit, events: make(chan Event, 256)}
}

func (h *Host) SetNavigationGuard(guard func(string, string) bool) {
	h.policyMu.Lock()
	h.guard = guard
	h.policyMu.Unlock()
}

func (h *Host) allowed(id, address string) bool {
	if h.eventSafety.check(h.tabs[id]) != nil {
		return false
	}
	if address == "about:blank" {
		return true
	}
	h.policyMu.RLock()
	guard := h.guard
	h.policyMu.RUnlock()
	return guard != nil && guard(id, address)
}

func (h *Host) event(e Event) {
	if h.emit != nil {
		h.eventSafety.enqueue(h.events, h.tabs[e.TabID], e)
	}
}

func (h *Host) loop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(h.done)
	ole := windows.NewLazySystemDLL("ole32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 2)
	if int32(hr) < 0 {
		h.initErr = fmt.Errorf("initialize browser COM: 0x%x", hr)
		close(h.ready)
		return
	}
	defer ole.NewProc("CoUninitialize").Call()
	h.thread = windows.GetCurrentThreadId()
	var msg win.MSG
	win.PeekMessage(&msg, 0, 0, 0, 0)
	close(h.ready)
	for {
		ret := win.GetMessage(&msg, 0, 0, 0)
		if ret <= 0 {
			break
		}
		if msg.Message == win.WM_APP+49 {
			for {
				select {
				case fn := <-h.queue:
					fn()
				default:
					goto drained
				}
			}
		}
	drained:
		win.TranslateMessage(&msg)
		win.DispatchMessage(&msg)
	}
}

func (h *Host) post(ctx context.Context, fn func()) error {
	if h.closing.Load() {
		return errors.New("browser host closed")
	}
	h.once.Do(func() {
		h.started.Store(true)
		go func() {
			for {
				select {
				case e := <-h.events:
					if h.emit != nil {
						h.eventSafety.dispatch(h.emit, e)
					}
				case <-h.done:
					return
				}
			}
		}()
		go h.loop()
	})
	select {
	case <-h.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	if h.initErr != nil {
		return h.initErr
	}
	select {
	case h.queue <- func() {
		if ctx.Err() == nil {
			fn()
		}
	}:
	case <-ctx.Done():
		return ctx.Err()
	case <-h.done:
		return errors.New("browser host stopped")
	}
	windows.NewLazySystemDLL("user32.dll").NewProc("PostThreadMessageW").Call(uintptr(h.thread), win.WM_APP+49, 0, 0)
	return nil
}

func (h *Host) call(ctx context.Context, fn func() error) error {
	result := make(chan error, 1)
	if err := h.post(ctx, func() { result <- fn() }); err != nil {
		return err
	}
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-h.done:
		return errors.New("browser host stopped")
	}
}

// Explicit vtable calls avoid incorrect out-pointer wrappers in generated SDKs.
//
//go:uintptrescapes
func raw(object unsafe.Pointer, method wv.ComProc, args ...uintptr) error {
	values := append([]uintptr{uintptr(object)}, args...)
	hr, _, _ := method.Call(values...)
	runtime.KeepAlive(object)
	if int32(hr) < 0 {
		return fmt.Errorf("WebView2 HRESULT 0x%08x", uint32(hr))
	}
	return nil
}
func readString(object unsafe.Pointer, method wv.ComProc) string {
	var value *uint16
	if raw(object, method, uintptr(unsafe.Pointer(&value))) != nil || value == nil {
		return ""
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(value))
	return windows.UTF16PtrToString(value)
}
func readBool(object unsafe.Pointer, method wv.ComProc) bool {
	var v int32
	_ = raw(object, method, uintptr(unsafe.Pointer(&v)))
	return v != 0
}
func ptr(s string) *uint16 { return windows.StringToUTF16Ptr(s) }

type environmentResult func(webviewloader.HRESULT, *webviewloader.ICoreWebView2Environment) webviewloader.HRESULT

func (fn environmentResult) EnvironmentCompleted(hr webviewloader.HRESULT, e *webviewloader.ICoreWebView2Environment) webviewloader.HRESULT {
	return fn(hr, e)
}

type windowLookup struct {
	class string
	found uintptr
}

// NewCallback trampolines cannot be freed. Reuse one for every enumeration.
// Windows compares window class names case-insensitively (the system class is
// registered as "Static"), so an exact-match lookup can miss the app window.
var enumOwnWindow = syscall.NewCallback(func(hwnd uintptr, lookup *windowLookup) uintptr {
	user32 := windows.NewLazySystemDLL("user32.dll")
	var pid uint32
	user32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid != uint32(os.Getpid()) {
		return 1
	}
	var name [128]uint16
	user32.NewProc("GetClassNameW").Call(hwnd, uintptr(unsafe.Pointer(&name[0])), 128)
	if strings.EqualFold(windows.UTF16ToString(name[:]), lookup.class) {
		lookup.found = hwnd
		return 0
	}
	return 1
})

func ownWindow(windowClass string) uintptr {
	if windowClass == "" {
		return 0
	}
	lookup := &windowLookup{class: windowClass}
	windows.NewLazySystemDLL("user32.dll").NewProc("EnumWindows").Call(enumOwnWindow, uintptr(unsafe.Pointer(lookup)))
	runtime.KeepAlive(lookup)
	return lookup.found
}

func (h *Host) Create(ctx context.Context, profile, id, address string) error {
	if err := ValidateManagedPath(profile); err != nil {
		return err
	}
	result := make(chan error, 1)
	err := h.post(ctx, func() {
		if err := ValidateManagedPath(profile); err != nil {
			result <- err
			return
		}
		if e := h.eventSafety.check(nil); e != nil {
			result <- e
			return
		}
		if h.tabs[id] != nil {
			result <- errors.New("browser tab already exists")
			return
		}
		parent := h.parent
		if parent == 0 {
			parent = ownWindow(h.windowClass)
		}
		if parent == 0 {
			result <- errors.New("O.R.C.A. browser host window not ready")
			return
		}
		create := func(env *wv.ICoreWebView2Environment) {
			if ctx.Err() != nil {
				return
			}
			if err := ValidateManagedPath(profile); err != nil {
				result <- err
				return
			}
			hwnd := win.CreateWindowEx(0, windows.StringToUTF16Ptr("STATIC"), nil, win.WS_CHILD|win.WS_CLIPCHILDREN|win.WS_CLIPSIBLINGS, 0, 0, 640, 480, win.HWND(parent), 0, 0, nil)
			if hwnd == 0 {
				result <- errors.New("cannot create browser child window")
				return
			}
			tab := &nativeTab{id: id, hwnd: hwnd, zoom: 1, generation: 1, document: id + "-1"}
			handler := newCOMCallback(true, func(hr, controller uintptr) uintptr {
				if int32(hr) < 0 || controller == 0 {
					win.DestroyWindow(hwnd)
					result <- fmt.Errorf("create browser controller: 0x%x", hr)
					return 0
				}
				tab.controller = (*wv.ICoreWebView2Controller)(unsafe.Pointer(controller))
				tab.controller.AddRef()
				view, e := tab.controller.GetCoreWebView2()
				tab.view = view
				if e == nil {
					e = h.configure(tab)
				}
				if e != nil || ctx.Err() != nil {
					h.closeTab(tab)
					if e == nil {
						e = ctx.Err()
					}
					result <- e
					return 0
				}
				finish := func(e error) {
					if e == nil {
						e = ctx.Err()
					}
					if e != nil {
						h.closeTab(tab)
						result <- e
						return
					}
					h.tabs[id] = tab
					_ = raw(unsafe.Pointer(tab.controller), tab.controller.Vtbl.PutIsVisible, 0)
					if address != "" {
						e = tab.view.Navigate(address)
					}
					result <- e
				}
				if e = h.configureUploads(tab, finish); e != nil {
					finish(e)
				}
				return 0
			})
			defer handler.release()
			if err := raw(unsafe.Pointer(env), env.Vtbl.CreateCoreWebView2Controller, uintptr(hwnd), handler.pointer()); err != nil {
				win.DestroyWindow(hwnd)
				result <- err
			}
		}
		if env := h.envs[profile]; env != nil {
			create(env)
			return
		}
		err := webviewloader.CreateCoreWebView2EnvironmentWithOptions(environmentResult(func(hr webviewloader.HRESULT, e *webviewloader.ICoreWebView2Environment) webviewloader.HRESULT {
			if hr < 0 || e == nil {
				result <- fmt.Errorf("WebView2 unavailable: 0x%x", uint32(hr))
				return 0
			}
			env := (*wv.ICoreWebView2Environment)(unsafe.Pointer(e))
			env.AddRef()
			h.envs[profile] = env
			create(env)
			return 0
		}), webviewloader.WithUserDataFolder(profile))
		if err != nil {
			result <- err
		}
	})
	if err != nil {
		return err
	}
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Host) closeTab(t *nativeTab) error {
	if t.closed && t.closeErr == nil {
		return nil
	}
	t.closed = true
	h.closeUploads(t)
	h.closeTransfers(t)
	if t.controller != nil {
		_ = raw(unsafe.Pointer(t.controller), t.controller.Vtbl.PutIsVisible, 0)
		win.ShowWindow(t.hwnd, win.SW_HIDE)
		if err := t.controller.Close(); err != nil {
			t.closeErr = err
			return err
		}
		_ = raw(unsafe.Pointer(t.controller), t.controller.Vtbl.Release)
		t.controller = nil
	}
	if t.view != nil {
		_ = raw(unsafe.Pointer(t.view), t.view.Vtbl.Release)
		t.view = nil
	}
	win.DestroyWindow(t.hwnd)
	t.closeErr = nil
	h.eventSafety.forget(t)
	return nil
}
func (h *Host) Close(ctx context.Context, id string) error {
	return h.call(ctx, func() error {
		if t := h.tabs[id]; t != nil {
			if err := h.closeTab(t); err != nil {
				return err
			}
			delete(h.tabs, id)
		}
		return nil
	})
}
func (h *Host) Navigate(ctx context.Context, id, address string) error {
	return h.call(ctx, func() error {
		t, e := h.tab(id)
		if e != nil {
			return e
		}
		if fence, ok := ctx.Value(actionFenceKey{}).(actionFence); ok && (fence.document != t.document || fence.generation != t.generation) {
			return ErrStaleObservation
		}
		if !h.allowed(id, address) {
			return errors.New("browser origin approval required")
		}
		return t.view.Navigate(address)
	})
}
func (h *Host) tab(id string) (*nativeTab, error) {
	t := h.tabs[id]
	if t == nil || t.closed {
		return nil, errors.New("browser tab closed")
	}
	if err := h.eventSafety.check(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (h *Host) Present(ctx context.Context, v Viewport) error {
	return h.call(ctx, func() error {
		for id, t := range h.tabs {
			if t.closed || t.controller == nil {
				continue
			}
			show := v.Visible && id == v.TabID && v.Width > 0 && v.Height > 0
			if show {
				if err := h.eventSafety.check(t); err != nil {
					return err
				}
			}
			var visible uintptr
			if show {
				visible = 1
			}
			_ = raw(unsafe.Pointer(t.controller), t.controller.Vtbl.PutIsVisible, visible)
			if !show {
				win.ShowWindow(t.hwnd, win.SW_HIDE)
				continue
			}
			s := v.Scale
			if s <= 0 || s > 8 {
				return errors.New("invalid browser scale")
			}
			win.SetWindowPos(t.hwnd, win.HWND_TOP, int32(math.Round(v.X*s)), int32(math.Round(v.Y*s)), int32(math.Round(v.Width*s)), int32(math.Round(v.Height*s)), win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW)
			t.width = v.Width
			t.height = v.Height
			_ = t.controller.PutBounds(wv.RECT{Right: int32(math.Round(v.Width * s)), Bottom: int32(math.Round(v.Height * s))})
		}
		return nil
	})
}

func (h *Host) configure(t *nativeTab) error {
	s, e := t.view.GetSettings()
	if e != nil {
		return e
	}
	defer raw(unsafe.Pointer(s), s.Vtbl.Release)
	for _, method := range []wv.ComProc{s.Vtbl.PutAreHostObjectsAllowed, s.Vtbl.PutIsWebMessageEnabled, s.Vtbl.PutAreDevToolsEnabled, s.Vtbl.PutAreDefaultContextMenusEnabled, s.Vtbl.PutAreDefaultScriptDialogsEnabled} {
		if err := raw(unsafe.Pointer(s), method, 0); err != nil {
			return err
		}
	}
	register := func(method wv.ComProc, fn func(uintptr, uintptr) uintptr) error {
		var token int64
		handler := newCOMCallback(false, func(sender, arg uintptr) uintptr {
			if t.closed {
				return 0
			}
			return fn(sender, arg)
		})
		defer handler.release()
		return raw(unsafe.Pointer(t.view), method, handler.pointer(), uintptr(unsafe.Pointer(&token)))
	}
	if err := register(t.view.Vtbl.AddNavigationStarting, func(sender, arg uintptr) uintptr {
		a := (*wv.ICoreWebView2NavigationStartingEventArgs)(unsafe.Pointer(arg))
		address := readString(unsafe.Pointer(a), a.Vtbl.GetUri)
		if !h.allowed(t.id, address) {
			_ = raw(unsafe.Pointer(a), a.Vtbl.PutCancel, 1)
			h.event(Event{TabID: t.id, Kind: "blocked", URL: address})
			return 0
		}
		t.generation++
		t.document = t.id + "-" + strconv.FormatUint(t.generation, 10)
		t.world = 0
		t.loading = true
		h.event(Event{TabID: t.id, Kind: "navigation", URL: address})
		return 0
	}); err != nil {
		return err
	}
	if err := register(t.view.Vtbl.AddNavigationCompleted, func(sender, arg uintptr) uintptr {
		t.loading = false
		e := h.tabEvent(t, "loaded")
		a := (*wv.ICoreWebView2NavigationCompletedEventArgs)(unsafe.Pointer(arg))
		if a != nil && !readBool(unsafe.Pointer(a), a.Vtbl.GetIsSuccess) {
			var status int32
			_ = raw(unsafe.Pointer(a), a.Vtbl.GetWebErrorStatus, uintptr(unsafe.Pointer(&status)))
			// An explicit Stop produces OPERATION_CANCELED, not a broken tab.
			if status != 14 {
				e.Kind = "error"
				e.Error = fmt.Sprintf("Page did not load (WebView2 error %d). Check the address or connection and reload.", status)
			}
		}
		h.event(e)
		return 0
	}); err != nil {
		return err
	}
	for _, entry := range []struct {
		method wv.ComProc
		kind   string
	}{{t.view.Vtbl.AddHistoryChanged, "history"}, {t.view.Vtbl.AddDocumentTitleChanged, "title"}, {t.view.Vtbl.AddSourceChanged, "source"}} {
		kind := entry.kind
		if err := register(entry.method, func(sender, arg uintptr) uintptr {
			if kind == "source" {
				t.generation++
			}
			h.event(h.tabEvent(t, kind))
			return 0
		}); err != nil {
			return err
		}
	}
	if err := register(t.view.Vtbl.AddPermissionRequested, func(sender, arg uintptr) uintptr {
		a := (*wv.ICoreWebView2PermissionRequestedEventArgs)(unsafe.Pointer(arg))
		_ = raw(unsafe.Pointer(a), a.Vtbl.PutState, 2)
		return 0
	}); err != nil {
		return err
	}
	if err := register(t.view.Vtbl.AddNewWindowRequested, func(sender, arg uintptr) uintptr {
		a := (*wv.ICoreWebView2NewWindowRequestedEventArgs)(unsafe.Pointer(arg))
		address := readString(unsafe.Pointer(a), a.Vtbl.GetUri)
		_ = raw(unsafe.Pointer(a), a.Vtbl.PutHandled, 1)
		h.event(Event{TabID: t.id, Kind: "popup", URL: address})
		return 0
	}); err != nil {
		return err
	}
	// JavaScript dialogs must never block the page. The agent cannot see or
	// answer a native modal, so accept it and record it as an event the task
	// log can show. Default dialogs are already disabled, which without a
	// handler leaves the renderer waiting.
	if err := register(t.view.Vtbl.AddScriptDialogOpening, func(sender, arg uintptr) uintptr {
		a := (*wv.ICoreWebView2ScriptDialogOpeningEventArgs)(unsafe.Pointer(arg))
		// get_Kind returns a COREWEBVIEW2_SCRIPT_DIALOG_KIND enum, not a string.
		var code int32
		_ = raw(unsafe.Pointer(a), a.Vtbl.GetKind, uintptr(unsafe.Pointer(&code)))
		kind := map[int32]string{0: "alert", 1: "confirm", 2: "prompt"}[code]
		if kind == "" {
			kind = "dialog"
		}
		message := readString(unsafe.Pointer(a), a.Vtbl.GetMessage)
		// prompt(): the answer the page receives is its own default text, so an
		// empty wide string is what a person dismissing the box would leave.
		if kind == "prompt" {
			empty := windows.StringToUTF16Ptr("")
			_ = raw(unsafe.Pointer(a), a.Vtbl.PutDefaultText, uintptr(unsafe.Pointer(empty)))
			runtime.KeepAlive(empty)
		}
		_ = raw(unsafe.Pointer(a), a.Vtbl.Accept)
		h.event(Event{TabID: t.id, Kind: "dialog", URL: readString(unsafe.Pointer(t.view), t.view.Vtbl.GetSource), Error: kind + ": " + message})
		return 0
	}); err != nil {
		return err
	}
	if err := register(t.view.Vtbl.AddProcessFailed, func(sender, arg uintptr) uintptr {
		t.generation++
		h.event(Event{TabID: t.id, Kind: "error", Error: "Browser renderer stopped. Close this tab and open it again."})
		return 0
	}); err != nil {
		return err
	}
	var focusToken int64
	focusHandler := newCOMCallback(false, func(sender, arg uintptr) uintptr {
		if t.closed {
			return 0
		}
		t.generation++
		h.event(Event{TabID: t.id, Kind: "user-input"})
		return 0
	})
	defer focusHandler.release()
	if err := raw(unsafe.Pointer(t.controller), t.controller.Vtbl.AddGotFocus, focusHandler.pointer(), uintptr(unsafe.Pointer(&focusToken))); err != nil {
		return err
	}
	return h.configureTransfers(t)
}

func (h *Host) tabEvent(t *nativeTab, kind string) Event {
	return Event{TabID: t.id, Kind: kind, URL: readString(unsafe.Pointer(t.view), t.view.Vtbl.GetSource), Title: readString(unsafe.Pointer(t.view), t.view.Vtbl.GetDocumentTitle), CanGoBack: readBool(unsafe.Pointer(t.view), t.view.Vtbl.GetCanGoBack), CanGoForward: readBool(unsafe.Pointer(t.view), t.view.Vtbl.GetCanGoForward)}
}

func (h *Host) cdp(ctx context.Context, id, method string, params any) (json.RawMessage, error) {
	encoded, e := json.Marshal(params)
	if e != nil {
		return nil, e
	}
	type reply struct {
		body json.RawMessage
		err  error
	}
	done := make(chan reply, 1)
	e = h.post(ctx, func() {
		t, err := h.tab(id)
		if err != nil {
			done <- reply{err: err}
			return
		}
		if fence, ok := ctx.Value(actionFenceKey{}).(actionFence); ok && (fence.document != t.document || fence.generation != t.generation) {
			done <- reply{err: ErrStaleObservation}
			return
		}
		handler := newCOMCallback(true, func(hr, result uintptr) uintptr {
			if ctx.Err() != nil {
				return 0
			}
			if int32(hr) < 0 {
				done <- reply{err: fmt.Errorf("browser operation failed: 0x%x", hr)}
			} else {
				done <- reply{body: json.RawMessage(windows.UTF16PtrToString((*uint16)(unsafe.Pointer(result))))}
			}
			return 0
		})
		defer handler.release()
		if err := raw(unsafe.Pointer(t.view), t.view.Vtbl.CallDevToolsProtocolMethod, uintptr(unsafe.Pointer(ptr(method))), uintptr(unsafe.Pointer(ptr(string(encoded)))), handler.pointer()); err != nil {
			done <- reply{err: err}
		}
	})
	if e != nil {
		return nil, e
	}
	select {
	case r := <-done:
		return r.body, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-h.done:
		return nil, errors.New("browser closed")
	}
}

func (h *Host) evaluate(ctx context.Context, id, expression string) (json.RawMessage, error) {
	world, e := h.isolatedWorld(ctx, id)
	if e != nil {
		return nil, e
	}
	data, e := h.cdp(ctx, id, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true, "contextId": world})
	if e != nil {
		return nil, e
	}
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		}
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if e = json.Unmarshal(data, &r); e != nil {
		return nil, e
	}
	if len(r.Exception) > 0 {
		return nil, errors.New("page observation failed")
	}
	return r.Result.Value, nil
}

func (h *Host) Command(ctx context.Context, id, op, value string) (string, error) {
	if strings.HasPrefix(op, "upload-") {
		return h.uploadCommand(ctx, id, op, value)
	}
	if op == "clear" {
		return "", h.clearProfile(ctx, id)
	}
	if strings.HasPrefix(op, "download-") {
		return h.transferCommand(ctx, id, op, value)
	}
	if op == "find" {
		encoded, _ := json.Marshal(value)
		_, e := h.evaluate(ctx, id, "window.find("+string(encoded)+",false,false,true)")
		return "", e
	}
	if op == "screenshot" {
		data, e := h.cdp(ctx, id, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": false})
		return string(data), e
	}
	var result string
	err := h.call(ctx, func() error {
		t, e := h.tab(id)
		if e != nil {
			return e
		}
		switch op {
		case "back":
			return t.view.GoBack()
		case "forward":
			return t.view.GoForward()
		case "reload":
			return t.view.Reload()
		case "stop":
			return t.view.Stop()
		case "zoom":
			z, e := strconv.ParseFloat(value, 64)
			if e != nil || z < 0.5 || z > 2 {
				return errors.New("zoom must be 50-200%")
			}
			t.zoom = z
			return raw(unsafe.Pointer(t.controller), t.controller.Vtbl.PutZoomFactor, uintptr(math.Float64bits(z)))
		case "state":
			b, _ := json.Marshal(map[string]any{"id": id, "title": readString(unsafe.Pointer(t.view), t.view.Vtbl.GetDocumentTitle), "url": readString(unsafe.Pointer(t.view), t.view.Vtbl.GetSource), "canGoBack": readBool(unsafe.Pointer(t.view), t.view.Vtbl.GetCanGoBack), "canGoForward": readBool(unsafe.Pointer(t.view), t.view.Vtbl.GetCanGoForward), "zoom": t.zoom, "loading": t.loading})
			result = string(b)
			return nil
		default:
			return errors.New("unsupported browser command")
		}
	})
	return result, err
}

func (h *Host) Observe(ctx context.Context, id string) (Observation, error) {
	var o Observation
	var visible bool
	err := h.call(ctx, func() error {
		t, e := h.tab(id)
		if e != nil {
			return e
		}
		t.generation++
		o.TabID = id
		o.DocumentID = t.document
		o.Generation = t.generation
		o.URL = readString(unsafe.Pointer(t.view), t.view.Vtbl.GetSource)
		o.Title = readString(unsafe.Pointer(t.view), t.view.Vtbl.GetDocumentTitle)
		visible = readBool(unsafe.Pointer(t.controller), t.controller.Vtbl.GetIsVisible)
		return nil
	})
	if err != nil {
		return o, err
	}
	data, err := h.evaluate(ctx, id, observeScript)
	if err != nil {
		return o, err
	}
	var snapshot struct {
		Text              string    `json:"text"`
		Elements          []Element `json:"elements"`
		ViewportWidth     float64   `json:"viewportWidth"`
		ViewportHeight    float64   `json:"viewportHeight"`
		ScreenshotBlocked string    `json:"screenshotBlocked"`
		Frames            int       `json:"frames"`
		ShadowRoots       int       `json:"shadowRoots"`
	}
	if err = json.Unmarshal(data, &snapshot); err != nil {
		return o, err
	}
	o.Text = snapshot.Text
	o.Elements = snapshot.Elements
	o.Frames = snapshot.Frames
	o.ShadowRoots = snapshot.ShadowRoots
	o.ScreenshotBlocked = snapshot.ScreenshotBlocked
	o.ViewportWidth, o.ViewportHeight = snapshot.ViewportWidth, snapshot.ViewportHeight
	if visible && o.ScreenshotBlocked == "" {
		o.ScreenshotBlocked = h.screenshotBoundary(ctx, id)
	}
	if visible && o.ScreenshotBlocked == "" {
		captureCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		screen, captureErr := h.cdp(captureCtx, id, "Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": 70, "captureBeyondViewport": false})
		cancel()
		if captureErr == nil {
			var img struct{ Data string }
			if json.Unmarshal(screen, &img) == nil {
				o.Screenshot, _ = base64.StdEncoding.DecodeString(img.Data)
				o.MIME = "image/jpeg"
				if cfg, e := jpeg.DecodeConfig(bytes.NewReader(o.Screenshot)); e == nil {
					o.ScreenshotWidth, o.ScreenshotHeight = cfg.Width, cfg.Height
				}
			}
		}
	}
	// A live page mutates constantly (timers, ads, animations). Revision drift is
	// not staleness: only a new document invalidates the observation, and each
	// action re-checks that its own target is still attached.
	err = h.call(ctx, func() error {
		t, e := h.tab(id)
		if e != nil {
			return e
		}
		if t.generation != o.Generation || t.document != o.DocumentID {
			return ErrStaleObservation
		}
		return nil
	})
	return o, err
}

func (h *Host) Execute(ctx context.Context, id string, a Action) error {
	err := h.call(ctx, func() error {
		t, e := h.tab(id)
		if e != nil {
			return e
		}
		if a.Generation != t.generation || a.DocumentID != t.document {
			return ErrStaleObservation
		}
		focus := win.GetFocus()
		if focus != 0 && (focus == t.hwnd || win.IsChild(t.hwnd, focus)) {
			h.event(Event{TabID: id, Kind: "user-input"})
			return errors.New("browser is under manual control")
		}
		t.generation++
		return nil
	})
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, actionFenceKey{}, actionFence{document: a.DocumentID, generation: a.Generation + 1})
	if a.Kind == "navigate" {
		return h.Navigate(ctx, id, a.URL)
	}
	if a.Kind == "wait" {
		select {
		case <-time.After(250 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	x, y := a.X, a.Y
	if a.ElementID != "" {
		key, _ := json.Marshal(a.ElementID)
		// Null means the approved target is gone or no longer matches what was
		// observed; the caller re-observes instead of acting on a guess.
		data, e := h.evaluate(ctx, id, "(()=>{const e=window.__orcaObserved?.get("+string(key)+");if(!e||!e.isConnected||!window.__orcaSig?.has(e)||window.__orcaSig.get(e)!==window.__orcaSigOf(e))return null;const r=e.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()")
		if e != nil {
			return e
		}
		if string(data) == "null" {
			return ErrStaleObservation
		}
		var p struct{ X, Y float64 }
		if e = json.Unmarshal(data, &p); e != nil {
			return e
		}
		x, y = p.X, p.Y
	}
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || x < 0 || y < 0 {
		return errors.New("invalid browser coordinates")
	}
	if a.Kind == "click" || a.Kind == "type" || a.Kind == "select" || a.Kind == "scroll" {
		data, e := h.evaluate(ctx, id, "({width:innerWidth,height:innerHeight})")
		if e != nil {
			return e
		}
		var size struct{ Width, Height float64 }
		if e = json.Unmarshal(data, &size); e != nil {
			return e
		}
		if x >= size.Width || y >= size.Height {
			return errors.New("browser action outside viewport")
		}
		if a.Kind != "scroll" {
			if !h.approvedHit(ctx, id, a.ElementID, x, y) {
				return errors.New("browser target is covered, protected or outside the approved document; observe again or take over")
			}
		}
	}
	switch a.Kind {
	case "waitfor":
		return h.waitFor(ctx, id, a.Text, a.DeltaY)
	case "upload":
		return h.uploadFiles(ctx, id, a.ElementID, a.Text)
	case "click", "dblclick", "hover", "type", "clear", "select":
		if a.Kind == "click" || a.Kind == "dblclick" || a.Kind == "hover" {
			count := 1
			if a.Kind == "dblclick" {
				count = 2
			}
			// Move first so hover-driven menus open the way a person triggers them.
			src := "mouseMoved"
			if a.Kind == "hover" {
				src = "mouseMoved"
			}
			if _, err = h.cdp(ctx, id, "Input.dispatchMouseEvent", map[string]any{"type": src, "x": x, "y": y, "button": "none"}); err != nil {
				return err
			}
			if a.Kind != "hover" {
				if err = h.inputPair(ctx, id, "Input.dispatchMouseEvent", map[string]any{"x": x, "y": y, "button": "left", "clickCount": count}, "mousePressed", "mouseReleased"); err != nil {
					return err
				}
			}
		}
		if a.Kind == "clear" {
			key, _ := json.Marshal(a.ElementID)
			_, err = h.evaluate(ctx, id, `(()=>{const e=window.__orcaObserved?.get(`+string(key)+`);if(!e?.isConnected||e.type==='password'||e.type==='file')throw Error('protected or stale input');if(e instanceof HTMLInputElement||e instanceof HTMLTextAreaElement)Object.getOwnPropertyDescriptor(e.constructor.prototype,'value').set.call(e,'');else if(e.isContentEditable)e.textContent='';else throw Error('not editable');e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));})()`)
		}
		if a.Kind == "type" {
			key, _ := json.Marshal(a.ElementID)
			value, _ := json.Marshal(a.Text)
			// Fill the approved node directly in one isolated-world operation.
			// Page focus handlers cannot redirect a later keyboard insertion.
			_, err = h.evaluate(ctx, id, `(()=>{const e=window.__orcaObserved?.get(`+string(key)+`);if(!e?.isConnected||e.disabled||e.readOnly||e.type==='password'||e.type==='file'||/cc-|one-time-code/.test(e.autocomplete||''))throw Error('protected or stale input');const v=`+string(value)+`;if(e instanceof HTMLInputElement&&['text','search','email','tel','url','number'].includes(e.type))Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(e,v);else if(e instanceof HTMLTextAreaElement)Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,'value').set.call(e,v);else if(e.isContentEditable)e.textContent=v;else throw Error('not editable');e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));})()`)
		}
		if a.Kind == "select" {
			key, _ := json.Marshal(a.ElementID)
			value, _ := json.Marshal(a.Text)
			_, err = h.evaluate(ctx, id, "(()=>{const e=window.__orcaObserved?.get("+string(key)+");const v="+string(value)+";if(!(e instanceof HTMLSelectElement)||!e.isConnected||e.disabled)throw Error('select option unavailable');const o=[...e.options].find(o=>(o.value===v||o.label.trim()===v||o.text.trim()===v)&&!o.disabled);if(!o)throw Error('select option unavailable');e.value=o.value;e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));})()")
		}
	case "scroll":
		_, err = h.cdp(ctx, id, "Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": x, "y": y, "deltaX": 0, "deltaY": a.DeltaY})
	case "key":
		code, ok := browserKeyCode(a.Key)
		if !ok {
			return errors.New("unsupported browser key")
		}
		params := map[string]any{"key": a.Key, "code": browserKeyDomCode(a.Key), "windowsVirtualKeyCode": code}
		if mods := browserKeyModifiers(a.Text); mods != 0 {
			params["modifiers"] = mods
		}
		err = h.inputPair(ctx, id, "Input.dispatchKeyEvent", params, "keyDown", "keyUp")
	default:
		return errors.New("unsupported browser action")
	}
	return err
}

// browserKeyCode maps the keys an agent is allowed to press onto Windows
// virtual-key codes. Anything outside this set is refused rather than guessed.
func browserKeyCode(key string) (int, bool) {
	named := map[string]int{
		"Enter": 13, "Tab": 9, "Escape": 27, "Backspace": 8, "Space": 32, " ": 32,
		"ArrowDown": 40, "ArrowUp": 38, "ArrowLeft": 37, "ArrowRight": 39,
		"Home": 36, "End": 35, "PageUp": 33, "PageDown": 34, "Delete": 46, "Insert": 45,
	}
	if code, ok := named[key]; ok {
		return code, true
	}
	if len(key) == 1 {
		ch := key[0]
		if ch >= 'a' && ch <= 'z' {
			return int(ch - 'a' + 'a'), true
		}
		if ch >= 'A' && ch <= 'Z' {
			return int(ch), true
		}
		if ch >= '0' && ch <= '9' {
			return int(ch), true
		}
	}
	return 0, false
}

func browserKeyDomCode(key string) string {
	switch key {
	case "Enter", "Tab", "Escape", "Backspace", "Space", " ", "Delete", "Insert", "Home", "End", "PageUp", "PageDown",
		"ArrowDown", "ArrowUp", "ArrowLeft", "ArrowRight":
		switch key {
		case " ":
			return "Space"
		case "Space":
			return "Space"
		}
		return key
	}
	if len(key) == 1 {
		if (key[0] >= 'a' && key[0] <= 'z') || (key[0] >= 'A' && key[0] <= 'Z') {
			return "Key" + strings.ToUpper(key)
		}
		if key[0] >= '0' && key[0] <= '9' {
			return "Digit" + key
		}
	}
	return ""
}

// browserKeyModifiers reads a "+"-joined modifier list such as "ctrl+shift".
func browserKeyModifiers(text string) int {
	mods := 0
	for _, part := range strings.Split(strings.ToLower(strings.TrimSpace(text)), "+") {
		switch part {
		case "alt":
			mods |= 1
		case "ctrl", "control":
			mods |= 2
		case "meta", "cmd":
			mods |= 4
		case "shift":
			mods |= 8
		}
	}
	return mods
}

// waitFor polls the page for a selector or a visible text needle. Pages that
// render after a network round trip (dynamic loading, slow resources, infinite
// scroll) need this instead of a fixed sleep.
func (h *Host) waitFor(ctx context.Context, id, needle string, timeoutMs float64) error {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return errors.New("wait needs a selector or text to wait for")
	}
	if timeoutMs <= 0 || timeoutMs > 60000 {
		timeoutMs = 15000
	}
	script := `(async()=>{const sel=__SEL__,text=__TEXT__;const end=Date.now()+__MS__;const hit=()=>{if(sel){try{if(document.querySelector(sel))return true}catch(_){}}const root=document.body;if(!root)return false;if(text&&(root.innerText||'').includes(text))return true;for(const f of document.querySelectorAll('iframe')){try{if(f.contentDocument&&((sel&&f.contentDocument.querySelector(sel))||(text&&(f.contentDocument.body?.innerText||'').includes(text))))return true}catch(_){}}return false};if(hit())return true;while(Date.now()<end){await new Promise(r=>setTimeout(r,150));if(hit())return true}return false})()`
	sel, text := "", needle
	if looksLikeSelector(needle) {
		sel = needle
		text = ""
	}
	sj, _ := json.Marshal(sel)
	tj, _ := json.Marshal(text)
	script = strings.NewReplacer("__SEL__", string(sj), "__TEXT__", string(tj), "__MS__", strconv.FormatFloat(timeoutMs, 'f', -1, 64)).Replace(script)
	data, err := h.evaluate(ctx, id, script)
	if err != nil {
		return err
	}
	if string(data) != "true" {
		return fmt.Errorf("waited %dms but %q never appeared", int(timeoutMs), needle)
	}
	return nil
}

func looksLikeSelector(value string) bool {
	if strings.ContainsAny(value, " ") {
		return false
	}
	for _, prefix := range []string{"#", ".", "[", "a[", "div", "input", "button", "select", "textarea", "*"} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

// uploadFiles answers the file chooser an input[type=file] opens. The chooser
// is intercepted by the host, so the path list is supplied here after the
// click rather than through a native dialog.
func (h *Host) uploadFiles(ctx context.Context, id, elementID, paths string) error {
	var list []string
	if err := json.Unmarshal([]byte(paths), &list); err != nil || len(list) == 0 {
		list = nil
		for _, part := range strings.Split(paths, "\n") {
			if p := strings.TrimSpace(part); p != "" {
				list = append(list, p)
			}
		}
	}
	if len(list) == 0 {
		return errors.New("upload needs at least one absolute file path")
	}
	a := Action{Kind: "click", ElementID: elementID, Generation: h.generationOf(ctx, id)}
	_ = a
	if err := h.Execute(ctx, id, Action{Kind: "click", ElementID: elementID}); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := h.uploadCommand(ctx, id, "upload-list", "")
		if err != nil {
			return err
		}
		var pending []Upload
		_ = json.Unmarshal([]byte(raw), &pending)
		for _, u := range pending {
			if u.Status == "pending" || u.Status == "" {
				body, _ := json.Marshal(map[string]any{"id": u.ID, "paths": list})
				_, err := h.uploadCommand(ctx, id, "upload-approve", string(body))
				return err
			}
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return errors.New("no file chooser appeared after clicking the upload control")
}

func (h *Host) generationOf(ctx context.Context, id string) uint64 {
	var gen uint64
	_ = h.call(ctx, func() error {
		t, err := h.tab(id)
		if err == nil {
			gen = t.generation
		}
		return nil
	})
	return gen
}

func (h *Host) inputPair(ctx context.Context, id, method string, params map[string]any, down, up string) error {
	params["type"] = down
	if _, err := h.cdp(ctx, id, method, params); err != nil {
		return err
	}
	params["type"] = up
	_, err := h.cdp(ctx, id, method, params)
	if err != nil {
		// Release only this embedded target's pressed key/button after cancel;
		// never continue typing, click again or inject into the system desktop.
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = h.cdp(cleanup, id, method, params)
		return uncertainBrowserInput(err)
	}
	return err
}

// Once input was dispatched, even a later stale-fence error cannot authorize a
// replay. Keep this distinct from a stale observation rejected before input.
func uncertainBrowserInput(err error) error {
	return fmt.Errorf("browser input outcome uncertain after dispatch; observe and ask the user before continuing: %v", err)
}

func (h *Host) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = h.ShutdownConfirmed(ctx)
}

func (h *Host) ShutdownConfirmed(ctx context.Context) error {
	if !h.started.Load() {
		return nil
	}
	if h.closing.Load() {
		select {
		case <-h.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	err := h.call(ctx, func() error {
		var closeErrors []error
		for id, t := range h.tabs {
			if err := h.closeTab(t); err != nil {
				closeErrors = append(closeErrors, err)
				continue
			}
			delete(h.tabs, id)
		}
		if len(closeErrors) > 0 {
			return errors.Join(closeErrors...)
		}
		for key, e := range h.envs {
			_ = raw(unsafe.Pointer(e), e.Vtbl.Release)
			delete(h.envs, key)
		}
		h.closing.Store(true)
		win.PostQuitMessage(0)
		return nil
	})
	if err != nil && !h.closing.Load() {
		return err
	}
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

const observeScript = `(()=>{
  const elements=[], refs=new Map(); let i=0, frames=0, shadowRoots=0;
  // Staleness is a property of the approved target, not of the document: a page
  // that runs timers, ads or animations is not stale. A button whose own label
  // or state changed since the observation, however, is no longer what was
  // approved, so its signature is recorded here and re-checked per action.
  window.__orcaSig=new WeakMap();
  window.__orcaSigOf=e=>{const editable=(e instanceof HTMLInputElement)||(e instanceof HTMLTextAreaElement)||e.isContentEditable;return [e.tagName,e.type||'',e.disabled?'1':'0',e.type==='checkbox'||e.type==='radio'?(e.checked?'1':'0'):'',editable?'':(e.innerText||e.textContent||'').trim().slice(0,160)].join('\u0001')};
  const visible=e=>{const r=e.getBoundingClientRect(),s=getComputedStyle(e);return r.width>0&&r.height>0&&r.bottom>0&&r.right>0&&r.top<innerHeight&&r.left<innerWidth&&s.visibility!=='hidden'&&s.display!=='none'};
  let screenshotBlocked='';
  // Walk the document and every open shadow root. Closed roots stay opaque.
  const collect=(root,x0,y0)=>{
    if(!root) return;
    for(const e of root.querySelectorAll('*')){
      if(e.shadowRoot){shadowRoots++;collect(e.shadowRoot,x0,y0)}
      if(e.localName==='iframe'||e.localName==='frame'){frames++;try{const d=e.contentDocument;if(d&&d.body)collect(d,x0+e.getBoundingClientRect().x,y0+e.getBoundingClientRect().y)}catch(_){}}
    }
  };
  try{collect(document,0,0)}catch(_){}
  const all=document.querySelectorAll('*');
  if(all.length>30000)screenshotBlocked='Document is too large to inspect safely for screenshots';
  else for(const e of all){if(!visible(e))continue;if(e.type==='password'||/cc-|one-time-code/i.test(e.autocomplete||'')){screenshotBlocked='Protected input is visible; screenshot sharing is disabled';break}}
  const harvest=(root,x0,y0)=>{
    if(!root) return;
    const candidates=root.querySelectorAll('a,button,input,textarea,select,[role="button"],[role="checkbox"],[role="tab"],[role="menuitem"],[contenteditable="true"],canvas,img,iframe,frame,[onclick],summary');
    for(const e of candidates){
      if(i>=200) return;
      const r=e.getBoundingClientRect(), style=getComputedStyle(e);
      if(r.width<1||r.height<1||r.bottom<0||r.right<0||r.top>innerHeight||r.left>innerWidth||style.visibility==='hidden'||style.display==='none')continue;
      const id='e'+(++i), sensitive=e.type==='password'||e.type==='file'||/cc-|one-time-code/i.test(e.autocomplete||''); refs.set(id,e);
      window.__orcaSig.set(e, window.__orcaSigOf(e));
      elements.push({id,role:e.getAttribute('role')||e.tagName.toLowerCase(),name:(e.getAttribute('aria-label')||e.labels?.[0]?.innerText||e.innerText||e.getAttribute('placeholder')||e.getAttribute('title')||'').slice(0,180),type:e.type||'',value:sensitive?'[redacted]':String(e.value||'').slice(0,500),href:e.tagName==='A'?e.href:'',src:(e.localName==='iframe'||e.localName==='frame')?(e.src||''):'',sensitive,disabled:!!e.disabled,checked:e.type==='checkbox'||e.type==='radio'?!!e.checked:undefined,options:e instanceof HTMLSelectElement?[...e.options].slice(0,100).map(o=>({value:o.value,label:o.label,disabled:o.disabled})):undefined,x:x0+r.x,y:y0+r.y,w:r.width,h:r.height});
    }
    for(const e of root.querySelectorAll('*')) if(e.shadowRoot) harvest(e.shadowRoot,x0,y0);
  };
  harvest(document,0,0);
  window.__orcaObserved=refs;
  window.__orcaMutationObserver?.disconnect();
  window.__orcaObservationRevision=0;
  window.__orcaMutationObserver=new MutationObserver(()=>window.__orcaObservationRevision++);
  window.__orcaMutationObserver.observe(document,{subtree:true,childList:true,attributes:true,characterData:true});
  if(!window.__orcaInputWatched){
    for(const event of ['pointerdown','keydown','input','wheel'])document.addEventListener(event,e=>{if(e.isTrusted)window.__orcaObservationRevision++},true);
    window.__orcaInputWatched=true;
  }
  return {text:(document.body?.innerText||'').slice(0,16000),elements,viewportWidth:innerWidth,viewportHeight:innerHeight,screenshotBlocked,frames,shadowRoots};
})()`
