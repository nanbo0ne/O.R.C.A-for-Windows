package control

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

func TestBetaIsolationProtectsAttachmentReferences(t *testing.T) {
	base := t.TempDir()
	profile := filepath.Join(base, "beta")
	workspace := filepath.Join(base, "project")
	for _, dir := range []string{profile, filepath.Join(workspace, ".orca", "attachments")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, ".orca", "attachments", "old.png"), []byte("official sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "code.txt"), []byte("tooling code"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", filepath.Join(base, "original-appdata"))
	t.Setenv("HOME", filepath.Join(base, "original-home"))
	t.Setenv("USERPROFILE", filepath.Join(base, "original-home"))
	if err := config.SetIsolatedProfileHome(profile); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.SetIsolatedProfileHome("") })
	for _, path := range []string{".orca/attachments/old.png", ".env", "orca.toml"} {
		if _, _, ok := resolveAbsRef(path, workspace); ok {
			t.Fatalf("protected reference accepted: %s", path)
		}
	}
	if _, _, ok := resolveAbsRef("code.txt", workspace); !ok {
		t.Fatal("tooling reference rejected")
	}
	root, err := openTaskImageRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := readTaskImage(root, filepath.Join(".orca", "attachments", "old.png")); err == nil {
		t.Fatal("official image read allowed")
	}
	if _, err := snapshotTaskImage(root, ".png", []byte("beta image")); err == nil {
		t.Fatal("snapshot wrote official project state")
	}
	owned, err := openTaskImageRoot(profile)
	if err != nil {
		t.Fatal(err)
	}
	defer owned.Close()
	path, err := snapshotTaskImage(owned, ".png", []byte("beta image"))
	if err != nil {
		t.Fatal(err)
	}
	if data, err := readTaskImage(owned, path); err != nil || string(data) != "beta image" {
		t.Fatalf("beta image roundtrip: %v", err)
	}
}
