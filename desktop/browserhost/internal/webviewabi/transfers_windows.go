//go:build windows

package webviewabi

import "golang.org/x/sys/windows"

var IIDICoreWebView2_4 = windows.GUID{Data1: 0x20d02d59, Data2: 0x6df2, Data3: 0x42dc, Data4: [8]byte{0xbd, 0x06, 0xf9, 0x8a, 0x69, 0x4b, 0x13, 0x02}}

type ICoreWebView2_4 struct{ Vtbl *ICoreWebView2_4Vtbl }

// These interfaces inherit all prior methods. The generated dependency's _4
// layout incorrectly starts at IUnknown; do not use it as a vtable prefix.
type ICoreWebView2_4Vtbl struct {
	ICoreWebView2Vtbl
	AddWebResourceResponseReceived, RemoveWebResourceResponseReceived                ComProc
	NavigateWithWebResourceRequest, AddDOMContentLoaded, RemoveDOMContentLoaded      ComProc
	GetCookieManager, GetEnvironment                                                 ComProc
	TrySuspend, Resume, GetIsSuspended                                               ComProc
	SetVirtualHostNameToFolderMapping, ClearVirtualHostNameToFolderMapping           ComProc
	AddFrameCreated, RemoveFrameCreated, AddDownloadStarting, RemoveDownloadStarting ComProc
}

type ICoreWebView2DownloadStartingEventArgs struct {
	Vtbl *ICoreWebView2DownloadStartingEventArgsVtbl
}
type ICoreWebView2DownloadStartingEventArgsVtbl struct {
	IUnknownVtbl
	GetDownloadOperation, GetCancel, PutCancel                                ComProc
	GetResultFilePath, PutResultFilePath, GetHandled, PutHandled, GetDeferral ComProc
}

type ICoreWebView2DownloadOperation struct {
	Vtbl *ICoreWebView2DownloadOperationVtbl
}
type ICoreWebView2DownloadOperationVtbl struct {
	IUnknownVtbl
	AddBytesReceivedChanged, RemoveBytesReceivedChanged                                    ComProc
	AddEstimatedEndTimeChanged, RemoveEstimatedEndTimeChanged                              ComProc
	AddStateChanged, RemoveStateChanged                                                    ComProc
	GetUri, GetContentDisposition, GetMimeType, GetTotalBytesToReceive                     ComProc
	GetBytesReceived, GetEstimatedEndTime, GetResultFilePath, GetState, GetInterruptReason ComProc
	Cancel, Pause, Resume, GetCanResume                                                    ComProc
}

type ICoreWebView2Deferral struct{ Vtbl *ICoreWebView2DeferralVtbl }
type ICoreWebView2DeferralVtbl struct {
	IUnknownVtbl
	Complete ComProc
}

const (
	DownloadStateInProgress int32 = iota
	DownloadStateInterrupted
	DownloadStateCompleted
)
