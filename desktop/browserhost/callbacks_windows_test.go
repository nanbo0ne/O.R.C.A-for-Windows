//go:build windows

package browserhost

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	wv "github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost/internal/webviewabi"
	"golang.org/x/sys/windows"
)

func callbackRootCount() int {
	n := 0
	callbackRoots.Range(func(_, _ any) bool { n++; return true })
	return n
}

func TestCOMCallbackNativeLifetime(t *testing.T) {
	for _, once := range []bool{false, true} {
		calls := 0
		c := newCOMCallback(once, func(uintptr, uintptr) uintptr { calls++; return 0 })
		p := c.pointer()
		syscall.SyscallN(callbackVtbl[1], p)
		if n := c.release(); n != 1 {
			t.Fatalf("creator release: %d", n)
		}
		runtime.GC()
		if _, ok := callbackRoots.Load(p); !ok {
			t.Fatal("native reference lost its GC root")
		}
		syscall.SyscallN(callbackVtbl[3], p, 0, 0)
		syscall.SyscallN(callbackVtbl[3], p, 0, 0)
		want := 2
		if once {
			want = 1
		}
		if calls != want {
			t.Fatalf("calls = %d, want %d", calls, want)
		}
		syscall.SyscallN(callbackVtbl[2], p)
		if _, ok := callbackRoots.Load(p); ok {
			t.Fatal("released callback remains rooted")
		}
		if c.fn != nil {
			t.Fatal("released callback retains closure")
		}
	}
}

func TestCOMCallbackQueryInterface(t *testing.T) {
	c := newCOMCallback(false, func(uintptr, uintptr) uintptr { return 0 })
	defer c.release()
	iid := windows.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	var out unsafe.Pointer
	hr, _, _ := syscall.SyscallN(callbackVtbl[0], c.pointer(), uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&out)))
	if hr != 0 || out != unsafe.Pointer(c) || c.refs.Load() != 2 {
		t.Fatalf("QI hr=%x out=%p refs=%d", hr, out, c.refs.Load())
	}
	c.release()
	iid.Data1 = 1
	hr, _, _ = syscall.SyscallN(callbackVtbl[0], c.pointer(), uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&out)))
	if hr != 0x80004002 || out != nil || c.refs.Load() != 1 {
		t.Fatalf("unknown IID hr=%x out=%p refs=%d", hr, out, c.refs.Load())
	}
}

// A synthetic COM object exercises the real cdp dispatch/cancellation path,
// without creating a native window, browser, profile, or external request.
type delayedCDPView struct {
	vtbl    *wv.ICoreWebView2Vtbl
	pending chan uintptr
	sync    bool
	fail    bool
}

var delayedCDPMethod = wv.ComProc(syscall.NewCallback(func(v *delayedCDPView, method, params, handler uintptr) uintptr {
	if v.fail {
		return 0x80004005
	}
	syscall.SyscallN(callbackVtbl[1], handler)
	if v.sync {
		body, err := nativeFixtureUTF16(`{"fixture":true}`)
		if err != nil {
			syscall.SyscallN(callbackVtbl[2], handler)
			return 0x8007000e
		}
		defer windows.LocalFree(windows.Handle(body))
		syscall.SyscallN(callbackVtbl[3], handler, 0, body)
		syscall.SyscallN(callbackVtbl[2], handler)
	} else {
		v.pending <- handler
	}
	return 0
}))

// Real WebView2 callback strings belong to native memory, not the Go heap.
func nativeFixtureUTF16(value string) (uintptr, error) {
	encoded, err := windows.UTF16FromString(value)
	if err != nil {
		return 0, err
	}
	buffer, err := windows.LocalAlloc(0, uint32(len(encoded)*2))
	if err != nil {
		return 0, err
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(buffer)), len(encoded)), encoded)
	return buffer, nil
}

func syntheticCDPHost(t *testing.T, view *delayedCDPView) *Host {
	t.Helper()
	view.vtbl = &wv.ICoreWebView2Vtbl{CallDevToolsProtocolMethod: delayedCDPMethod}
	h := New(nil)
	h.tabs["fixture"] = &nativeTab{id: "fixture", view: (*wv.ICoreWebView2)(unsafe.Pointer(view))}
	h.once.Do(func() { close(h.ready) })
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case fn := <-h.queue:
				fn()
			case <-h.done:
				return
			}
		}
	}()
	t.Cleanup(func() { close(h.done); <-stopped; runtime.KeepAlive(view) })
	return h
}

func TestCDPDelayedCanceledResponseReleasesCallback(t *testing.T) {
	baseline := callbackRootCount()
	v := &delayedCDPView{pending: make(chan uintptr, 1)}
	h := syntheticCDPHost(t, v)
	limit, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	body, err := nativeFixtureUTF16(`{"fixture":"` + strings.Repeat("x", 256<<10) + `"}`)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.LocalFree(windows.Handle(body))
	for i := 0; i < 32; i++ {
		ctx, cancel := context.WithCancel(limit)
		done := make(chan error, 1)
		go func() { _, err := h.cdp(ctx, "fixture", "Runtime.evaluate", map[string]any{}); done <- err }()
		var p uintptr
		select {
		case p = <-v.pending:
		case <-limit.Done():
			t.Fatal("fixture dispatch timed out")
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel: %v", err)
			}
		case <-limit.Done():
			t.Fatal("cancel blocked on delayed response")
		}
		if err := h.call(limit, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		root, ok := callbackRoots.Load(p)
		if !ok {
			t.Fatal("cancel released native callback prematurely")
		}
		c := root.(*comCallback)
		if c.refs.Load() != 1 {
			t.Fatalf("native refcount: %d", c.refs.Load())
		}
		runtime.GC()
		syscall.SyscallN(callbackVtbl[3], p, 0, body)
		if c.fn != nil {
			t.Fatal("late completion retained response closure")
		}
		if _, ok := callbackRoots.Load(p); !ok {
			t.Fatal("completion released native root before Release")
		}
		syscall.SyscallN(callbackVtbl[2], p)
		if callbackRootCount() != baseline {
			t.Fatal("late response leaked a callback root")
		}
	}
}

func TestCDPCallbackSynchronousCompletionAndDispatchFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		baseline := callbackRootCount()
		v := &delayedCDPView{sync: true, fail: fail}
		h := syntheticCDPHost(t, v)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		body, err := h.cdp(ctx, "fixture", "Runtime.evaluate", nil)
		if fail && err == nil || !fail && (err != nil || string(body) != `{"fixture":true}`) {
			t.Fatalf("fail=%v body=%s error=%v", fail, body, err)
		}
		if err := h.call(ctx, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		cancel()
		if callbackRootCount() != baseline {
			t.Fatal("dispatch retained callback")
		}
	}
}

func TestNativeCDPCallbackLifecycle(t *testing.T) {
	if os.Getenv("ORCA_BROWSER_NATIVE_TEST") != "1" {
		t.Skip("isolated WebView2 test is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	preexisting := make(map[uintptr]*comCallback)
	callbackRoots.Range(func(key, value any) bool { preexisting[key.(uintptr)] = value.(*comCallback); return true })
	profile := t.TempDir()
	h := New(nil)
	if err := h.call(ctx, func() error {
		h.parent = uintptr(win.CreateWindowEx(0, windows.StringToUTF16Ptr("STATIC"), nil, win.WS_OVERLAPPEDWINDOW, 0, 0, 640, 480, 0, 0, 0, nil))
		if h.parent == 0 {
			return errors.New("create hidden callback fixture window")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := h.Close(cleanup, "callback"); err != nil {
			t.Error(err)
		}
		_ = h.call(cleanup, func() error { win.DestroyWindow(win.HWND(h.parent)); return nil })
		if err := h.ShutdownConfirmed(cleanup); err != nil {
			t.Error(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			remaining := 0
			callbackRoots.Range(func(key, _ any) bool {
				if preexisting[key.(uintptr)] == nil {
					remaining++
				}
				return true
			})
			if remaining == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Errorf("closed native fixture retains %d callbacks", remaining)
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	if err := h.Create(ctx, profile, "callback", "about:blank"); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := h.evaluate(ctx, "callback", "1"); err == nil {
			break
		} else if !errors.Is(err, ErrStaleObservation) || ctx.Err() != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	initial := make(map[uintptr]*comCallback)
	callbackRoots.Range(func(key, value any) bool { initial[key.(uintptr)] = value.(*comCallback); return true })
	for i := 0; i < 16; i++ {
		request, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() {
			_, err := h.evaluate(request, "callback", "new Promise(resolve=>{globalThis.__orcaCallbackResolve=resolve})")
			done <- err
		}()
		deadline := time.NewTimer(2 * time.Second)
		// Wait for a new rooted completion handler before cancelling, so this
		// exercises late native completion rather than pre-dispatch cancellation.
		for {
			pending := false
			callbackRoots.Range(func(key, _ any) bool { pending = pending || initial[key.(uintptr)] == nil; return true })
			if pending {
				break
			}
			select {
			case err := <-done:
				stop()
				deadline.Stop()
				t.Fatalf("request completed before cancel: %v", err)
			case <-deadline.C:
				stop()
				t.Fatal("no native callback dispatched")
			case <-time.After(time.Millisecond):
			}
		}
		deadline.Stop()
		// Drive the synthetic promise explicitly: hidden renderers throttle JS
		// timers, which would make a cancellation/lifetime test timing-dependent.
		if ready, err := h.evaluate(ctx, "callback", "typeof globalThis.__orcaCallbackResolve==='function'"); err != nil || string(ready) != "true" {
			stop()
			t.Fatalf("native fixture promise not installed: %s %v", ready, err)
		}
		stop()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("native cancellation: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("native caller did not cancel promptly")
		}
		if _, err := h.evaluate(ctx, "callback", "globalThis.__orcaCallbackResolve('x'.repeat(262144));delete globalThis.__orcaCallbackResolve;true"); err != nil {
			t.Fatal(err)
		}
		deadline = time.NewTimer(2 * time.Second)
		for {
			pending := false
			callbackRoots.Range(func(key, _ any) bool { pending = pending || initial[key.(uintptr)] == nil; return true })
			if !pending {
				break
			}
			select {
			case <-deadline.C:
				t.Fatal("late native response retained a completion callback")
			case <-time.After(10 * time.Millisecond):
			}
		}
		deadline.Stop()
	}
}
