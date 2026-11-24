//go:build windows

package webviewabi

import "golang.org/x/sys/windows"

var IIDView13 = windows.GUID{Data1: 0xf75f09a8, Data2: 0x667e, Data3: 0x4983, Data4: [8]byte{0x88, 0xd6, 0xc8, 0x77, 0x3f, 0x31, 0x5e, 0x84}}
var IIDProfile2 = windows.GUID{Data1: 0xfa740d4b, Data2: 0x5eae, Data3: 0x4344, Data4: [8]byte{0xa8, 0xad, 0x74, 0xbe, 0x31, 0x92, 0x53, 0x97}}

type View13 struct{ Vtbl *View13Vtbl }
type View13Vtbl struct {
	ICoreWebView2_4Vtbl
	AddClientCertificateRequested, RemoveClientCertificateRequested                                                                                  ComProc
	OpenTaskManagerWindow, PrintToPdf                                                                                                                ComProc
	AddIsMutedChanged, RemoveIsMutedChanged, GetIsMuted, PutIsMuted                                                                                  ComProc
	AddIsDocumentPlayingAudioChanged, RemoveIsDocumentPlayingAudioChanged, GetIsDocumentPlayingAudio                                                 ComProc
	AddIsDefaultDownloadDialogOpenChanged, RemoveIsDefaultDownloadDialogOpenChanged                                                                  ComProc
	GetIsDefaultDownloadDialogOpen, OpenDefaultDownloadDialog, CloseDefaultDownloadDialog                                                            ComProc
	GetDefaultDownloadDialogCornerAlignment, PutDefaultDownloadDialogCornerAlignment, GetDefaultDownloadDialogMargin, PutDefaultDownloadDialogMargin ComProc
	AddBasicAuthenticationRequested, RemoveBasicAuthenticationRequested                                                                              ComProc
	CallDevToolsProtocolMethodForSession, AddContextMenuRequested, RemoveContextMenuRequested                                                        ComProc
	AddStatusBarTextChanged, RemoveStatusBarTextChanged, GetStatusBarText                                                                            ComProc
	GetProfile                                                                                                                                       ComProc
}
type Profile2 struct{ Vtbl *Profile2Vtbl }
type Profile2Vtbl struct {
	IUnknownVtbl
	GetProfileName, GetIsInPrivateModeEnabled, GetProfilePath                                                    ComProc
	GetDefaultDownloadFolderPath, PutDefaultDownloadFolderPath, GetPreferredColorScheme, PutPreferredColorScheme ComProc
	ClearBrowsingData, ClearBrowsingDataInTimeRange, ClearBrowsingDataAll                                        ComProc
}
