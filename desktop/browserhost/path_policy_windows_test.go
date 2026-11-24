//go:build windows

package browserhost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

func browserPolicyFixture(t *testing.T) (string, string, string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("APPDATA", filepath.Join(base, "official-appdata"))
	t.Setenv("USERPROFILE", filepath.Join(base, "synthetic-home"))
	home := filepath.Join(base, "beta")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := config.SetIsolatedProfileHome(home); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.SetIsolatedProfileHome("") })
	return base, home, filepath.Join(os.Getenv("APPDATA"), "orca")
}

func TestBetaManagedProfileRejectsJunctionBeforeHostStarts(t *testing.T) {
	_, home, official := browserPolicyFixture(t)
	if err := os.MkdirAll(official, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "browser-profiles")
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "New-Item -ErrorAction Stop -ItemType Junction -Path $env:ORCA_TEST_JUNCTION -Target $env:ORCA_TEST_TARGET | Out-Null")
	cmd.Env = append(os.Environ(), "ORCA_TEST_JUNCTION="+link, "ORCA_TEST_TARGET="+official)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create synthetic junction: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	if err := ValidateManagedPath(filepath.Join(link, "new-profile")); err == nil {
		resolved, resolveErr := filepath.EvalSymlinks(link)
		info, statErr := os.Lstat(link)
		t.Fatalf("junction preflight failed: enabled=%v link=%q target=%q resolved=%q info=%v stat=%v resolve=%v", config.IsolatedProfileEnabled(), link, official, resolved, info, statErr, resolveErr)
	}
	h := New(nil)
	if err := h.Create(context.Background(), filepath.Join(link, "new-profile"), "never-native", "about:blank"); err == nil {
		t.Fatal("profile junction accepted")
	}
	if h.started.Load() {
		t.Fatal("rejected path started native host")
	}
	entries, err := os.ReadDir(official)
	if err != nil || len(entries) != 0 {
		t.Fatal("native profile created external state", err)
	}
	if err := ValidateManagedPath(filepath.Join(home, "new-managed", "profile")); err != nil {
		t.Fatal("ordinary owned profile rejected", err)
	}
	if err := ValidateManagedPath(filepath.Join(official, "new-profile")); err == nil {
		t.Fatal("direct external profile accepted")
	}
	_ = config.SetIsolatedProfileHome("")
	if err := ValidateManagedPath(filepath.Join(link, "new-profile")); err != nil {
		t.Fatal("non-isolated managed-path behavior changed", err)
	}
}

func TestBetaNativeTransferValidatorsRejectOfficialData(t *testing.T) {
	base, _, official := browserPolicyFixture(t)
	protected := filepath.Join(official, "sessions", "sentinel.jsonl")
	if err := os.MkdirAll(filepath.Dir(protected), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(protected, []byte("official sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateUploadSnapshots(context.Background(), []string{protected}, false); err == nil {
		t.Fatal("native upload bypassed official-data exclusion")
	}
	destination := filepath.Join(filepath.Dir(protected), "new-file.txt")
	if _, err := validateDownloadDestination(destination); err == nil {
		t.Fatal("native download bypassed official-data exclusion")
	}
	external := filepath.Join(base, "ordinary.txt")
	if err := os.WriteFile(external, []byte("selected external bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateUploadSnapshots(context.Background(), []string{external}, false); err != nil {
		t.Fatal("ordinary external upload rejected", err)
	}
	if _, err := validateDownloadDestination(filepath.Join(base, "download.txt")); err != nil {
		t.Fatal("ordinary external download rejected", err)
	}
	if data, err := os.ReadFile(protected); err != nil || string(data) != "official sentinel" {
		t.Fatal("official sentinel changed", err)
	}
	_ = config.SetIsolatedProfileHome("")
	if _, err := validateUploadSnapshots(context.Background(), []string{protected}, false); err != nil {
		t.Fatal("non-isolated upload behavior changed", err)
	}
	if _, err := validateDownloadDestination(destination); err != nil {
		t.Fatal("non-isolated download behavior changed", err)
	}
}
