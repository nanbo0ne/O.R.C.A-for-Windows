//go:build windows

package browserhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

// Run the existing native assertions unchanged, but with the beta filesystem
// policy active. Every suite creates a new profile within one fresh temp root.
func TestNativeBetaHostRevalidation(t *testing.T) {
	if os.Getenv("ORCA_BROWSER_NATIVE_BETA_TEST") != "1" {
		t.Skip("explicit native queue approval and ORCA_BROWSER_NATIVE_BETA_TEST=1 required")
	}
	if os.Getenv("ORCA_BROWSER_NATIVE_TEST") != "1" {
		t.Fatal("native beta revalidation requires ORCA_BROWSER_NATIVE_TEST=1 too")
	}
	if config.IsolatedProfileEnabled() {
		t.Fatal("native beta fixture must not reuse an existing isolated profile")
	}
	root := os.Getenv("ORCA_BROWSER_NATIVE_BETA_ROOT")
	if !filepath.IsAbs(root) || !strings.EqualFold(filepath.Clean(root), filepath.Clean(os.TempDir())) {
		t.Fatal("native beta root must be the fresh TEMP directory")
	}
	if err := validateUnredirectedPath(root); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("native beta fixture refuses a reused or inaccessible root")
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "WEBVIEW2_") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("ORCA_BROWSER_TRANSFER_TRACE", "")
	// This Go version uses GOTMPDIR for testing.T.TempDir, while the existing
	// adversarial fixture uses os.MkdirTemp and TEMP. Confine both allocators.
	t.Setenv("GOTMPDIR", root)
	for _, key := range []string{"APPDATA", "LOCALAPPDATA", "HOME", "USERPROFILE"} {
		dir := filepath.Join(root, key)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, dir)
	}
	if err := config.SetIsolatedProfileHome(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.SetIsolatedProfileHome("") })
	for _, test := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"adversarial", TestNativeAdversarialActionBoundaries},
		{"upload", TestNativeUploadInterception},
		{"download", TestNativeDownloadTransfers},
		{"callbacks", TestNativeCDPCallbackLifecycle},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := t.TempDir()
			if err := ValidateManagedPath(probe); err != nil {
				t.Fatalf("fresh fixture path %q outside root %q (TEMP=%q): %v", probe, root, os.TempDir(), err)
			}
			test.run(t)
		})
		if t.Failed() {
			return
		}
	}
}
