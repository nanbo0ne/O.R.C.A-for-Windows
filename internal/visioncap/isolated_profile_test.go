package visioncap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

func TestIsolatedProfileCapabilityStore(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "official-home")
	appdata := filepath.Join(base, "official-appdata")
	profile := filepath.Join(base, "beta")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", appdata)
	t.Setenv("XDG_CONFIG_HOME", appdata)
	official := DefaultPath()
	if err := os.MkdirAll(filepath.Dir(official), 0700); err != nil {
		t.Fatal(err)
	}
	want := `{"items":{"official-sentinel":{"status":"supported","source":"manual"}}}`
	if err := os.WriteFile(official, []byte(want), 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.SetIsolatedProfileHome(profile); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.SetIsolatedProfileHome("") })
	if got := DefaultPath(); got != filepath.Join(profile, "orca", "vision-capabilities.json") {
		t.Fatalf("default capability path=%q", got)
	}
	// The config profile alone must suffice: no APPDATA/HOME redirection here.
	if os.Getenv("APPDATA") != appdata || os.Getenv("USERPROFILE") != home {
		t.Fatal("test unexpectedly redirected real-home fixtures")
	}
	if got := Load(""); len(got.Items) != 0 {
		t.Fatal("default store imported official metadata")
	}
	blocked := Load(official)
	if len(blocked.Items) != 0 {
		t.Fatal("explicit official metadata read allowed")
	}
	entry := Capability{Key: "beta-fixture", Status: Supported, Source: SourceMetadata}
	if err := blocked.Put(entry); err == nil {
		t.Fatal("explicit official metadata write allowed")
	}
	owned := Load("")
	if err := owned.Put(entry); err != nil {
		t.Fatal(err)
	}
	if got := Load("").Items[entry.Key]; got.Status != Supported {
		t.Fatal("beta metadata was not persisted")
	}
	if body, err := os.ReadFile(official); err != nil || string(body) != want {
		t.Fatal("official capability fixture changed")
	}
}
