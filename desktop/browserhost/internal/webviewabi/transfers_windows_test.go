//go:build windows

package webviewabi

import (
	"testing"
	"unsafe"
)

func TestTransferVtableOffsets(t *testing.T) {
	var view ICoreWebView2_4Vtbl
	var args ICoreWebView2DownloadStartingEventArgsVtbl
	var op ICoreWebView2DownloadOperationVtbl
	var deferral ICoreWebView2DeferralVtbl
	word := unsafe.Sizeof(ComProc(0))
	for _, test := range []struct {
		name   string
		offset uintptr
		slot   uintptr
	}{
		{"AddDownloadStarting", unsafe.Offsetof(view.AddDownloadStarting), 75},
		{"RemoveDownloadStarting", unsafe.Offsetof(view.RemoveDownloadStarting), 76},
		{"PutCancel", unsafe.Offsetof(args.PutCancel), 5},
		{"PutResultFilePath", unsafe.Offsetof(args.PutResultFilePath), 7},
		{"PutHandled", unsafe.Offsetof(args.PutHandled), 9},
		{"GetDeferral", unsafe.Offsetof(args.GetDeferral), 10},
		{"GetTotalBytesToReceive", unsafe.Offsetof(op.GetTotalBytesToReceive), 12},
		{"GetBytesReceived", unsafe.Offsetof(op.GetBytesReceived), 13},
		{"GetState", unsafe.Offsetof(op.GetState), 16},
		{"Cancel", unsafe.Offsetof(op.Cancel), 18},
		{"Complete", unsafe.Offsetof(deferral.Complete), 3},
	} {
		if test.offset != test.slot*word {
			t.Errorf("%s slot = %d, want %d", test.name, test.offset/word, test.slot)
		}
	}
}
