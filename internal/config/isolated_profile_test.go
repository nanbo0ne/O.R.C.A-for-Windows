package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsolatedProfileSkipsLegacyHomeAndMigrations(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "official-home")
	profile := filepath.Join(base, "beta")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", profile)
	t.Setenv("XDG_CONFIG_HOME", profile)
	for path, body := range map[string]string{
		filepath.Join(home, ".env"):                            "ORCA_PROFILE_FIXTURE=must-not-import\n",
		filepath.Join(home, ".deepseek-orca", "config.json"):   `{"lang":"legacy-sentinel"}`,
		filepath.Join(profile, "deepseek-orca", "config.toml"): "language='legacy-sentinel'\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetIsolatedProfileHome(profile); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetIsolatedProfileHome("") })
	if !IsolatedProfileEnabled() {
		t.Fatal("profile not enabled")
	}
	if got, _ := os.UserHomeDir(); got != home {
		t.Fatal("real home changed")
	}
	if got, _ := ProfileHomeDir(); got != profile {
		t.Fatalf("profile home=%q", got)
	}
	if legacyConfigPath() != "" || LegacyUserConfigPath() != "" || LegacyUserCredentialsPath() != "" {
		t.Fatal("legacy discovery enabled")
	}
	if result, err := MigrateLegacyIfNeeded(); result != nil || err != nil {
		t.Fatalf("legacy migration=%+v, %v", result, err)
	}
	if err := EnsureV11StateMigration(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(profile, "orca")); !os.IsNotExist(err) {
		t.Fatalf("legacy tree migrated: %v", err)
	}
	values := loadDotEnvForRoot(filepath.Join(base, "empty-workspace"))
	if _, ok := values["ORCA_PROFILE_FIXTURE"]; ok {
		t.Fatal("official home credentials imported")
	}
	if err := SetIsolatedProfileHome(""); err != nil {
		t.Fatal(err)
	}
	if got, _ := ProfileHomeDir(); got != home || IsolatedProfileEnabled() {
		t.Fatal("normal discovery not restored")
	}
	if err := SetIsolatedProfileHome("relative"); err == nil {
		t.Fatal("relative profile accepted")
	}
}

func TestIsolatedProfileConfigEditsStayInProfile(t *testing.T) {
	base := t.TempDir()
	profile := filepath.Join(base, "beta")
	project := filepath.Join(base, "project")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	official := filepath.Join(project, "orca.toml")
	want := "language='official-sentinel'\n"
	if err := os.WriteFile(official, []byte(want), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", filepath.Join(base, "original-appdata"))
	t.Setenv("HOME", filepath.Join(base, "original-home"))
	t.Setenv("USERPROFILE", filepath.Join(base, "original-home"))
	if err := SetIsolatedProfileHome(profile); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetIsolatedProfileHome("") })
	c := LoadForEdit(official)
	if c.Language == "official-sentinel" {
		t.Fatal("external config read")
	}
	if err := c.SaveTo(official); err == nil {
		t.Fatal("external config save allowed")
	}
	c = Default()
	if err := c.WriteFile(official); err == nil {
		t.Fatal("external raw config write allowed")
	}
	if err := c.SaveForRoot(project); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveTo(ProjectConfigPath(project)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProfileDataPath(ProjectConfigPath(project)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ProjectConfigPath(project)); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(official); err != nil || string(body) != want {
		t.Fatal("official config changed")
	}
	for _, path := range []string{filepath.Join(project, ".orca", "sessions", "a.jsonl"), filepath.Join(project, ".env"), official} {
		if ProfileDiscoveryAllowed(path) {
			t.Fatalf("protected discovery allowed: %s", path)
		}
	}
	if !ProfileDiscoveryAllowed(filepath.Join(project, "AGENTS.md")) {
		t.Fatal("project tooling blocked")
	}
	t.Setenv("BETA_CUSTOM_KEY_SLOT", "inherited")
	if _, ok := c.Env("BETA_CUSTOM_KEY_SLOT"); ok {
		t.Fatal("arbitrary inherited provider key resolved")
	}
	for _, entry := range c.Environment() {
		if strings.HasPrefix(entry, "BETA_CUSTOM_KEY_SLOT=") {
			t.Fatal("arbitrary inherited key leaked to configured child")
		}
	}
}
