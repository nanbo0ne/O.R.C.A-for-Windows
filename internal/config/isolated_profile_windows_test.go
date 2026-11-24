//go:build windows

package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// GitHub's Windows runners reach the profile through an 8.3 short name
// (C:\Users\RUNNER~1\...), so the lexical profile root and its resolved form
// are different strings for the same directory. An ordinary file under the
// profile must not be mistaken for a junction escape.
func TestProfileDataPathAcceptsShortNameRoot(t *testing.T) {
	long := filepath.Join(t.TempDir(), "LongProfileDirectoryNameForContainment")
	if err := os.MkdirAll(filepath.Join(long, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("powershell", "-NoProfile", "-Command",
		"(New-Object -ComObject Scripting.FileSystemObject).GetFolder('"+long+"').ShortPath").Output()
	if err != nil {
		t.Skipf("short paths are unavailable here: %v", err)
	}
	short := strings.TrimSpace(string(output))
	if short == "" || strings.EqualFold(short, long) {
		t.Skip("8.3 short names are disabled on this volume")
	}
	if err := SetIsolatedProfileHome(short); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetIsolatedProfileHome("") })
	for _, path := range []string{
		filepath.Join(short, "sessions", "existing.jsonl"),
		filepath.Join(short, "sessions", "new.jsonl"),
		filepath.Join(long, "sessions", "new.jsonl"),
	} {
		if err := ValidateProfileDataPath(path); err != nil {
			t.Fatalf("%s rejected under a short-name root: %v", path, err)
		}
	}
	if err := ValidateProfileDataPath(filepath.Join(t.TempDir(), "outside.jsonl")); err == nil {
		t.Fatal("a path outside the profile root must still be rejected")
	}
}
