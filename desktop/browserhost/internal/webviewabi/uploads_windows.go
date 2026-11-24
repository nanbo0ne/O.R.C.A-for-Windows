//go:build windows

package webviewabi

type ICoreWebView2DevToolsProtocolEventReceiver struct {
	Vtbl *ICoreWebView2DevToolsProtocolEventReceiverVtbl
}
type ICoreWebView2DevToolsProtocolEventReceiverVtbl struct {
	IUnknownVtbl
	AddDevToolsProtocolEventReceived, RemoveDevToolsProtocolEventReceived ComProc
}

type ICoreWebView2DevToolsProtocolEventReceivedEventArgs struct {
	Vtbl *ICoreWebView2DevToolsProtocolEventReceivedEventArgsVtbl
}
type ICoreWebView2DevToolsProtocolEventReceivedEventArgsVtbl struct {
	IUnknownVtbl
	GetParameterObjectAsJson ComProc
}
