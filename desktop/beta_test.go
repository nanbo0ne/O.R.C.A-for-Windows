package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

func withBetaProfile(t *testing.T) (string, string) {
	t.Helper()
	oldChannel, oldRoot := channel, initializedBetaRoot
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	channel, initializedBetaRoot = betaChannel, ""
	base := t.TempDir()
	for _, key := range []string{"APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "HOME", "USERPROFILE"} {
		t.Setenv(key, filepath.Join(base, key))
	}
	t.Setenv("ORCA_DEV", "false")
	t.Setenv("PATH", os.Getenv("PATH"))
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if ok && (strings.Contains(upper, "KEY") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "CREDENTIAL") || strings.HasSuffix(upper, "_AUTH")) {
			t.Setenv(key, value)
		}
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
		_ = config.SetIsolatedProfileHome("")
		channel, initializedBetaRoot = oldChannel, oldRoot
	})
	root, err := betaDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	return base, root
}

func TestBetaIsolationPrecedesConfigAndPreservesRealHome(t *testing.T) {
	base, root := withBetaProfile(t)
	home, _ := os.UserHomeDir()
	beforeHome, beforeProfile := os.Getenv("HOME"), os.Getenv("USERPROFILE")
	fixtures := map[string]string{
		filepath.Join(os.Getenv("APPDATA"), "orca", "config.toml"):                "language = 'official-sentinel'\n",
		filepath.Join(os.Getenv("APPDATA"), "orca", "sessions", "official.jsonl"): "official chat sentinel",
		filepath.Join(os.Getenv("APPDATA"), "deepseek-orca", "config.toml"):       "language = 'legacy-sentinel'\n",
		filepath.Join(home, ".deepseek-orca", "config.json"):                      `{"lang":"legacy","mcpServers":{"official-mcp":{"command":"official-only"}}}`,
		filepath.Join(home, ".env"):                                               "ORCA_BETA_FIXTURE_SECRET=official-only\n",
	}
	for path, body := range fixtures {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := initializeBeta(); err != nil {
		t.Fatal(err)
	}
	if err := isolateBrowserPreview(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("HOME") != beforeHome || os.Getenv("USERPROFILE") != beforeProfile {
		t.Fatal("beta must not redirect HOME/USERPROFILE")
	}
	if got, _ := os.Getwd(); got != filepath.Join(root, "workspace") {
		t.Fatalf("cwd=%q", got)
	}
	if got, _ := config.ProfileHomeDir(); got != root {
		t.Fatalf("profile home=%q", got)
	}
	if err := initializeBeta(); err != nil {
		t.Fatal(err)
	}
	if got, _ := betaDataRoot(); got != root {
		t.Fatalf("non-idempotent root=%q", got)
	}
	for name, path := range map[string]string{
		"config": config.UserConfigPath(), "credentials": config.UserCredentialsPath(),
		"sessions": config.SessionDir(), "archive": config.ArchiveDir(), "memory": config.MemoryUserDir(),
		"cache": config.CacheDir(), "desktop": desktopConfigDir(), "webview": browserPreviewWebviewDataPath(),
	} {
		rel, err := filepath.Rel(root, path)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			t.Errorf("%s escaped beta: %q", name, path)
		}
	}
	if config.LegacyUserConfigPath() != "" || config.LegacyUserCredentialsPath() != "" {
		t.Fatal("legacy lookup remains enabled")
	}
	if result, err := config.MigrateLegacyIfNeeded(); err != nil || result != nil {
		t.Fatalf("migration=%+v, %v", result, err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.Language, "sentinel") {
		t.Fatal("official config was imported")
	}
	if value, ok := cfg.Env("ORCA_BETA_FIXTURE_SECRET"); ok {
		t.Fatalf("home credential was imported: %q", value)
	}
	for _, plugin := range cfg.Plugins {
		if plugin.Name == "official-mcp" {
			t.Fatal("legacy MCP imported")
		}
	}
	if got := loadTabsFile(); len(got.Tabs) != 0 {
		t.Fatalf("unexpected restored tabs: %+v", got)
	}
	for path, want := range fixtures {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("fixture changed: %s (%v)", strings.TrimPrefix(path, base), err)
		}
	}
}

func TestBetaIdentityAndUpdateIsolation(t *testing.T) {
	withBetaProfile(t)
	if !betaEnabled() || !browserPreviewEnabled() || desktopTitle("windows") != betaName || browserWindowClass() != betaWindowClass {
		t.Fatal("beta functionality or identity missing")
	}
	lock := singleInstanceLock(&App{})
	if lock == nil || lock.UniqueId != singleInstanceID+".beta" {
		t.Fatalf("beta lock=%+v", lock)
	}
	a := &App{}
	info, err := a.CheckUpdate()
	if err != nil || info.Available || !strings.Contains(info.Err, "ORCA beta") {
		t.Fatalf("update=%+v, %v", info, err)
	}
	for _, call := range []func() error{func() error { return a.SetUpdateSource(updateSourceGitHub) }, a.DownloadUpdate, a.CancelUpdateDownload, a.ApplyUpdate, a.OpenDownloadedUpdate} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "ORCA beta") {
			t.Fatalf("beta updater call=%v", err)
		}
	}
	a.OpenDownloadPage()
	channel = browserPreviewChannel
	if lock := singleInstanceLock(&App{}); lock.UniqueId != singleInstanceID+".browser-preview" {
		t.Fatal("preview IPC changed")
	}
	channel = "stable"
	if betaEnabled() || browserPreviewEnabled() || disabledDesktopUpdateReason() != "" {
		t.Fatal("stable behavior changed")
	}
	if err := initializeBeta(); err != nil {
		t.Fatal(err)
	}
}

func TestBetaRejectsRelativeConfigRoot(t *testing.T) {
	withBetaProfile(t)
	t.Setenv("APPDATA", "relative")
	t.Setenv("XDG_CONFIG_HOME", "relative")
	if _, err := betaDataRoot(); err == nil {
		t.Fatal("relative root accepted")
	}
}

func TestBetaMainInitializesBeforeConfigAndWindowPreferences(t *testing.T) {
	body, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	init := strings.Index(source, "if err := initializeBeta();")
	for _, call := range []string{"app := NewApp()", "desktopStyle := config.LoadForEdit", "if saved, ok := loadWindowState()"} {
		if at := strings.Index(source, call); init < 0 || at < init {
			t.Fatalf("beta init must precede %s", call)
		}
	}
}
