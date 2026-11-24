package boot

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// These are standard-profile boot tests, including legacy migration. Redirect
// only the test process's OS profile; enabling beta isolation would invalidate
// those contracts and hide fixture configuration instead of testing it.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "orca-boot-profile-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create boot test profile:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(root)
	for key, path := range map[string]string{
		"HOME": root, "USERPROFILE": root,
		"APPDATA":         filepath.Join(root, "AppData", "Roaming"),
		"LOCALAPPDATA":    filepath.Join(root, "AppData", "Local"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_CACHE_HOME":  filepath.Join(root, "cache"),
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
	} {
		if err := os.MkdirAll(path, 0700); err != nil {
			panic(fmt.Errorf("create boot fixture %s: %w", key, err))
		}
		if err := os.Setenv(key, path); err != nil {
			panic(fmt.Errorf("set boot fixture %s: %w", key, err))
		}
	}
	m.Run()
}
