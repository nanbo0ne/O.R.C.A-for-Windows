//go:build windows

package browserhost

import (
	"testing"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// The app window is located by class name. Windows compares class names
// case-insensitively (the system class registers as "Static"), so a lookup must
// not require an exact-case match or the browser host never finds its parent.
func TestOwnWindowMatchesClassCaseInsensitively(t *testing.T) {
	hwnd := win.CreateWindowEx(0, windows.StringToUTF16Ptr("Static"), windows.StringToUTF16Ptr("orca lookup fixture"), win.WS_OVERLAPPEDWINDOW, 0, 0, 320, 240, 0, 0, 0, nil)
	if hwnd == 0 {
		t.Fatal("create fixture window")
	}
	defer win.DestroyWindow(hwnd)
	if got := ownWindow("STATIC"); got != uintptr(hwnd) {
		t.Fatalf("ownWindow(STATIC) = %d, want %d", got, uintptr(hwnd))
	}
	if got := ownWindow(""); got != 0 {
		t.Fatalf("ownWindow(\"\") = %d, want 0: an empty class must never match an arbitrary window", got)
	}
}
