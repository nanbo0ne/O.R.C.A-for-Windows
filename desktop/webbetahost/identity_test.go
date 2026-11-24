package webbetahost

import (
	"os"
	"path/filepath"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
	"testing"
)

func TestIdleLogoutCannotResumeOldIdentity(t *testing.T) {
	var l identityLatch
	if got := l.observe(webbeta.BrowserState{Blocked: "signed_out"}); got.Blocked != "signed_out" {
		t.Fatal("first login blocked")
	}
	if got := l.observe(webbeta.BrowserState{Ready: true}); !got.Ready {
		t.Fatal("initial login rejected")
	}
	l.observe(webbeta.BrowserState{Blocked: "signed_out"})
	if got := l.observe(webbeta.BrowserState{Ready: true}); got.Ready || got.Blocked != "identity_changed" {
		t.Fatal("old account bindings could resume")
	}
}

func TestLostIdentitySurvivesRestartWithoutReadingLoginData(t *testing.T) {
	skipOnMacOS(t, "the DeepSeek web beta is Windows-only and macOS /var is a system symlink to /private/var")
	profile := t.TempDir()
	l, err := loadIdentityLatch(profile)
	if err != nil {
		t.Fatal(err)
	}
	l.observe(webbeta.BrowserState{Ready: true})
	l.observe(webbeta.BrowserState{Blocked: "signed_out"})
	restored, err := loadIdentityLatch(profile)
	if err != nil {
		t.Fatal(err)
	}
	if got := restored.observe(webbeta.BrowserState{Ready: true}); got.Ready || got.Blocked != "identity_changed" {
		t.Fatal("restart erased the old account boundary")
	}
	data, err := os.ReadFile(filepath.Join(profile, identityMarker))
	if err != nil || len(data) != 0 {
		t.Fatal("marker must contain no identity or website data", err)
	}
}

func TestLoginAdoptionOnlyRemovesOwnedIdentityMarker(t *testing.T) {
	skipOnMacOS(t, "the DeepSeek web beta is Windows-only and macOS /var is a system symlink to /private/var")
	p, err := OpenProfile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	login := filepath.Join(p.BrowserPath(), "synthetic-login")
	if err := os.WriteFile(login, []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	l, err := loadIdentityLatch(p.BrowserPath())
	if err != nil {
		t.Fatal(err)
	}
	l.known = true
	l.observe(webbeta.BrowserState{Blocked: "signed_out"})
	if err := p.PrepareLoginAdoption(); err != nil {
		t.Fatal(err)
	}
	fresh, err := loadIdentityLatch(p.BrowserPath())
	if err != nil || fresh.lost {
		t.Fatal("explicit adoption retained the old latch", err)
	}
	data, err := os.ReadFile(login)
	if err != nil || string(data) != "retain" {
		t.Fatal("adoption cleared website data", err)
	}
}

func TestRestoredIdentityRejectsLoginBoundary(t *testing.T) {
	l := identityLatch{known: true}
	if got := l.observe(webbeta.BrowserState{Blocked: "signed_out"}); got.Blocked != "identity_changed" {
		t.Fatal("restored identity silently changed")
	}
}
