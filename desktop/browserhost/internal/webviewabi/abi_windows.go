//go:build windows

// Package webviewabi contains the small Win32 WebView2 ABI used by the browser.
// BOOL and HWND are passed by value; COM string getters receive an out pointer.
// Kept separate from the dependency's generated package, whose callback init
// registers Go string arguments that cannot be marshalled by syscall.NewCallback.
package webviewabi

import (
	"errors"
	"golang.org/x/sys/windows"
	"runtime"
	"syscall"
	"unsafe"
)

type ComProc uintptr

//go:uintptrescapes
func (p ComProc) Call(a ...uintptr) (uintptr, uintptr, error) {
	return syscall.SyscallN(uintptr(p), a...)
}

type IUnknownVtbl struct{ QueryInterface, AddRef, Release ComProc }
type RECT struct{ Left, Top, Right, Bottom int32 }
type ICoreWebView2Environment struct{ Vtbl *ICoreWebView2EnvironmentVtbl }
type ICoreWebView2EnvironmentVtbl struct {
	IUnknownVtbl
	CreateCoreWebView2Controller, CreateWebResourceResponse, GetBrowserVersionString, AddNewBrowserVersionAvailable, RemoveNewBrowserVersionAvailable ComProc
}

func (i *ICoreWebView2Environment) AddRef() uintptr {
	r, _, _ := i.Vtbl.AddRef.Call(uintptr(unsafe.Pointer(i)))
	return r
}

type ICoreWebView2Controller struct{ Vtbl *ICoreWebView2ControllerVtbl }
type ICoreWebView2ControllerVtbl struct {
	IUnknownVtbl
	GetIsVisible, PutIsVisible, GetBounds, PutBounds, GetZoomFactor, PutZoomFactor              ComProc
	AddZoomFactorChanged, RemoveZoomFactorChanged, SetBoundsAndZoomFactor                       ComProc
	MoveFocus, AddMoveFocusRequested, RemoveMoveFocusRequested, AddGotFocus, RemoveGotFocus     ComProc
	AddLostFocus, RemoveLostFocus, AddAcceleratorKeyPressed, RemoveAcceleratorKeyPressed        ComProc
	GetParentWindow, PutParentWindow, NotifyParentWindowPositionChanged, Close, GetCoreWebView2 ComProc
}

// Out parameters must escape before conversion to uintptr: these wrappers can
// grow the Go stack before reaching SyscallN.
//
//go:uintptrescapes
func call(p unsafe.Pointer, m ComProc, args ...uintptr) error {
	hr, _, _ := m.Call(append([]uintptr{uintptr(p)}, args...)...)
	runtime.KeepAlive(p)
	if int32(hr) < 0 {
		return syscall.Errno(hr)
	}
	return nil
}
func (i *ICoreWebView2Controller) AddRef() uintptr {
	r, _, _ := i.Vtbl.AddRef.Call(uintptr(unsafe.Pointer(i)))
	return r
}
func (i *ICoreWebView2Controller) GetCoreWebView2() (*ICoreWebView2, error) {
	var v *ICoreWebView2
	e := call(unsafe.Pointer(i), i.Vtbl.GetCoreWebView2, uintptr(unsafe.Pointer(&v)))
	if e == nil && v == nil {
		e = errors.New("WebView2 returned an empty browser interface")
	}
	return v, e
}
func (i *ICoreWebView2Controller) Close() error { return call(unsafe.Pointer(i), i.Vtbl.Close) }
func (i *ICoreWebView2Controller) PutBounds(r RECT) error {
	return call(unsafe.Pointer(i), i.Vtbl.PutBounds, uintptr(unsafe.Pointer(&r)))
}

type ICoreWebView2 struct{ Vtbl *ICoreWebView2Vtbl }
type ICoreWebView2Vtbl struct {
	IUnknownVtbl
	GetSettings, GetSource, Navigate, NavigateToString                                                                            ComProc
	AddNavigationStarting, RemoveNavigationStarting, AddContentLoading, RemoveContentLoading                                      ComProc
	AddSourceChanged, RemoveSourceChanged, AddHistoryChanged, RemoveHistoryChanged                                                ComProc
	AddNavigationCompleted, RemoveNavigationCompleted, AddFrameNavigationStarting, RemoveFrameNavigationStarting                  ComProc
	AddFrameNavigationCompleted, RemoveFrameNavigationCompleted, AddScriptDialogOpening, RemoveScriptDialogOpening                ComProc
	AddPermissionRequested, RemovePermissionRequested, AddProcessFailed, RemoveProcessFailed                                      ComProc
	AddScriptToExecuteOnDocumentCreated, RemoveScriptToExecuteOnDocumentCreated, ExecuteScript, CapturePreview                    ComProc
	Reload, PostWebMessageAsJson, PostWebMessageAsString, AddWebMessageReceived, RemoveWebMessageReceived                         ComProc
	CallDevToolsProtocolMethod, GetBrowserProcessId, GetCanGoBack, GetCanGoForward, GoBack, GoForward                             ComProc
	GetDevToolsProtocolEventReceiver, Stop, AddNewWindowRequested, RemoveNewWindowRequested                                       ComProc
	AddDocumentTitleChanged, RemoveDocumentTitleChanged, GetDocumentTitle, AddHostObjectToScript, RemoveHostObjectFromScript      ComProc
	OpenDevToolsWindow, AddContainsFullScreenElementChanged, RemoveContainsFullScreenElementChanged, GetContainsFullScreenElement ComProc
	AddWebResourceRequested, RemoveWebResourceRequested, AddWebResourceRequestedFilter, RemoveWebResourceRequestedFilter          ComProc
	AddWindowCloseRequested, RemoveWindowCloseRequested                                                                           ComProc
}

func (i *ICoreWebView2) GetSettings() (*ICoreWebView2Settings, error) {
	if i == nil || i.Vtbl == nil {
		return nil, errors.New("WebView2 browser interface is unavailable")
	}
	var v *ICoreWebView2Settings
	e := call(unsafe.Pointer(i), i.Vtbl.GetSettings, uintptr(unsafe.Pointer(&v)))
	if e == nil && v == nil {
		e = errors.New("WebView2 returned an empty settings interface")
	}
	return v, e
}
func (i *ICoreWebView2) Navigate(s string) error {
	p, e := windows.UTF16PtrFromString(s)
	if e != nil {
		return e
	}
	return call(unsafe.Pointer(i), i.Vtbl.Navigate, uintptr(unsafe.Pointer(p)))
}
func (i *ICoreWebView2) GoBack() error    { return call(unsafe.Pointer(i), i.Vtbl.GoBack) }
func (i *ICoreWebView2) GoForward() error { return call(unsafe.Pointer(i), i.Vtbl.GoForward) }
func (i *ICoreWebView2) Reload() error    { return call(unsafe.Pointer(i), i.Vtbl.Reload) }
func (i *ICoreWebView2) Stop() error      { return call(unsafe.Pointer(i), i.Vtbl.Stop) }

type ICoreWebView2Settings struct{ Vtbl *ICoreWebView2SettingsVtbl }
type ICoreWebView2SettingsVtbl struct {
	IUnknownVtbl
	GetIsScriptEnabled, PutIsScriptEnabled, GetIsWebMessageEnabled, PutIsWebMessageEnabled                             ComProc
	GetAreDefaultScriptDialogsEnabled, PutAreDefaultScriptDialogsEnabled, GetIsStatusBarEnabled, PutIsStatusBarEnabled ComProc
	GetAreDevToolsEnabled, PutAreDevToolsEnabled, GetAreDefaultContextMenusEnabled, PutAreDefaultContextMenusEnabled   ComProc
	GetAreHostObjectsAllowed, PutAreHostObjectsAllowed, GetIsZoomControlEnabled, PutIsZoomControlEnabled               ComProc
	GetIsBuiltInErrorPageEnabled, PutIsBuiltInErrorPageEnabled                                                         ComProc
}
type ICoreWebView2NavigationStartingEventArgs struct {
	Vtbl *ICoreWebView2NavigationStartingEventArgsVtbl
}
type ICoreWebView2NavigationStartingEventArgsVtbl struct {
	IUnknownVtbl
	GetUri, GetIsUserInitiated, GetIsRedirected, GetRequestHeaders, GetCancel, PutCancel, GetNavigationId ComProc
}
type ICoreWebView2NavigationCompletedEventArgs struct {
	Vtbl *ICoreWebView2NavigationCompletedEventArgsVtbl
}
type ICoreWebView2NavigationCompletedEventArgsVtbl struct {
	IUnknownVtbl
	GetIsSuccess, GetWebErrorStatus, GetNavigationId ComProc
}
type ICoreWebView2PermissionRequestedEventArgs struct {
	Vtbl *ICoreWebView2PermissionRequestedEventArgsVtbl
}
type ICoreWebView2PermissionRequestedEventArgsVtbl struct {
	IUnknownVtbl
	GetUri, GetPermissionKind, GetIsUserInitiated, GetState, PutState, GetDeferral ComProc
}
type ICoreWebView2NewWindowRequestedEventArgs struct {
	Vtbl *ICoreWebView2NewWindowRequestedEventArgsVtbl
}
type ICoreWebView2NewWindowRequestedEventArgsVtbl struct {
	IUnknownVtbl
	GetUri, PutNewWindow, GetNewWindow, PutHandled, GetHandled, GetIsUserInitiated, GetDeferral, GetWindowFeatures ComProc
}
