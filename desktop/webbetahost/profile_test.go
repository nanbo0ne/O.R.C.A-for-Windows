package webbetahost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOwnedProfileClearPreservesSiblingStores(t *testing.T) {
	skipOnMacOS(t, "the DeepSeek web beta is Windows-only and macOS /var is a system symlink to /private/var")
	root := t.TempDir()
	p, err := OpenProfile(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "browser", "cookies"), filepath.Join(root, "account", "credential"), p.StatePath(), filepath.Join(p.BrowserPath(), "cookies")} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.BrowserPath()); !os.IsNotExist(err) {
		t.Fatal("profile retained", err)
	}
	for _, path := range []string{filepath.Join(root, "browser", "cookies"), filepath.Join(root, "account", "credential"), p.StatePath()} {
		if raw, err := os.ReadFile(path); err != nil || string(raw) != "synthetic" {
			t.Fatal("unrelated store altered", path, err)
		}
	}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(p.BrowserPath())
	if err != nil || len(entries) != 0 {
		t.Fatal("new profile inherited files", err)
	}
}

func TestProfileRefusesUnownedPathsAndCorruptMarker(t *testing.T) {
	skipOnMacOS(t, "the DeepSeek web beta is Windows-only and macOS /var is a system symlink to /private/var")
	root := t.TempDir()
	if _, err := OpenProfile("relative"); err == nil {
		t.Fatal("relative root accepted")
	}
	unowned := filepath.Join(root, "deepseek-web")
	if err := os.Mkdir(unowned, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(unowned, "unrelated")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenProfile(root); err == nil {
		t.Fatal("unowned data adopted")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("unowned data touched", err)
	}
	p, err := OpenProfile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.root, ".orca-profile-owner"), []byte("wrong"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Clear(); err == nil {
		t.Fatal("corrupt ownership allowed deletion")
	}
}

func TestProfileRefusesSymlinkEscape(t *testing.T) {
	skipOnMacOS(t, "the DeepSeek web beta is Windows-only and macOS /var is a system symlink to /private/var")
	p, err := OpenProfile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "keep")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, p.BrowserPath()); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := p.Clear(); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("outside data deleted", err)
	}
}
