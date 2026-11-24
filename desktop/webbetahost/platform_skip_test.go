package webbetahost

import (
	"runtime"
	"testing"
)

// skipOnMacOS skips profile fixtures that place their root under the OS
// temporary directory. The DeepSeek web beta is a Windows-only surface, and
// macOS maps /var to /private/var, so the profile guard that rejects symlinked
// ancestors sees the platform's own temporary root as an escape.
func skipOnMacOS(t *testing.T, reason string) {
	t.Helper()
	if runtime.GOOS == "darwin" {
		t.Skip(reason)
	}
}
