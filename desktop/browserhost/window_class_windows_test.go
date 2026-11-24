//go:build windows

package browserhost

import "testing"

func TestHostWindowClassIsExplicit(t *testing.T) {
	if New(nil).windowClass != WindowClass {
		t.Fatal("legacy preview class changed")
	}
	if got := NewWithWindowClass("OrcaBetaWindow", nil).windowClass; got != "OrcaBetaWindow" {
		t.Fatalf("configured class lost: %q", got)
	}
	if ownWindow("") != 0 || ownWindow("OrcaNonexistentTestWindow") != 0 {
		t.Fatal("unknown class must not fall back to another window")
	}
}
