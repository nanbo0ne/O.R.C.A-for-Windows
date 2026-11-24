//go:build windows

package webbetahost

import (
	"fmt"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

// Base WebView2 event interfaces, matching the pinned go-webview2 v1.0.23
// vtable. Keep these handlers alive until after the controller is released.
type loginBoundaryCallback struct {
	vtbl *[4]uintptr
	fn   func(unsafe.Pointer) uintptr
	refs atomic.Int32
}

var loginBoundaryVtable = [4]uintptr{
	syscall.NewCallback(func(self *loginBoundaryCallback, iid *windows.GUID, out *unsafe.Pointer) uintptr {
		if iid == nil || out == nil {
			return 0x80004003
		}
		*out = nil
		unknown := windows.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
		if *iid != unknown {
			return 0x80004002
		}
		*out = unsafe.Pointer(self)
		self.refs.Add(1)
		return 0
	}),
	syscall.NewCallback(func(self *loginBoundaryCallback) uintptr { return uintptr(self.refs.Add(1)) }),
	syscall.NewCallback(func(self *loginBoundaryCallback) uintptr {
		return uintptr(self.refs.Add(-1))
	}),
	syscall.NewCallback(func(self *loginBoundaryCallback, sender uintptr, args unsafe.Pointer) uintptr {
		return self.fn(args)
	}),
}

func loginNavigationStarting(args unsafe.Pointer) uintptr {
	table := *(**[10]uintptr)(args)
	var value *uint16
	// ComProc.Call's uintptrescapes annotation keeps output pointers off a Go
	// stack that can move when COM calls back into Go (including test fixtures).
	hr, _, _ := edge.ComProc(table[3]).Call(uintptr(args), uintptr(unsafe.Pointer(&value))) // get_Uri
	allowed := int32(hr) >= 0 && value != nil && webbeta.AllowedSource(windows.UTF16PtrToString(value))
	if value != nil {
		windows.CoTaskMemFree(unsafe.Pointer(value))
	}
	if !allowed {
		hr, _, _ = edge.ComProc(table[8]).Call(uintptr(args), 1) // put_Cancel
		return hr
	}
	return 0
}

func loginNewWindowRequested(args unsafe.Pointer) uintptr {
	table := *(**[7]uintptr)(args)
	hr, _, _ := edge.ComProc(table[6]).Call(uintptr(args), 1) // put_Handled: never open a popup
	return hr
}

func installLoginBoundary(view *edge.ICoreWebView2) ([]*loginBoundaryCallback, error) {
	table := *(**[46]uintptr)(unsafe.Pointer(view))
	var handlers []*loginBoundaryCallback
	for _, entry := range []struct {
		slot int
		fn   func(unsafe.Pointer) uintptr
	}{{7, loginNavigationStarting}, {17, loginNavigationStarting}, {44, loginNewWindowRequested}} {
		handler := &loginBoundaryCallback{vtbl: &loginBoundaryVtable, fn: entry.fn}
		handler.refs.Store(1)
		handlers = append(handlers, handler)
		var token int64
		hr, _, _ := edge.ComProc(table[entry.slot]).Call(uintptr(unsafe.Pointer(view)), uintptr(unsafe.Pointer(handler)), uintptr(unsafe.Pointer(&token)))
		if int32(hr) < 0 {
			return handlers, fmt.Errorf("install login-only navigation guard: 0x%x", hr)
		}
	}
	return handlers, nil
}
