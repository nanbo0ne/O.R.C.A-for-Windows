package main

import (
	"runtime"
	"testing"
)

// skipOnMacOS skips fixtures that cannot hold on macOS. Browser control and the
// beta profile are Windows-only surfaces, and macOS maps its temporary
// directory /var to /private/var, so containment guards that reject symlinked
// ancestors (correct where Windows junctions are the escape vector) see the
// platform's own temporary root as an escape.
func skipOnMacOS(t *testing.T, reason string) {
	t.Helper()
	if runtime.GOOS == "darwin" {
		t.Skip(reason)
	}
}
