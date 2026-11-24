package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/installipc"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/hook"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/memory"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/skill"
)

func writeBetaFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("create fixture parent %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write fixture %q: %v", path, err)
	}
}

func TestBetaInitializationReportsFilesystemStage(t *testing.T) {
	_, root := withBetaProfile(t)
	t.Logf("fixture TEMP=%q TMP=%q beta=%q", os.Getenv("TEMP"), os.Getenv("TMP"), root)
	writeBetaFixture(t, filepath.Join(root, "Local"), "not a directory")
	err := initializeBeta()
	if err == nil || !strings.Contains(err.Error(), "create beta directory") || !strings.Contains(err.Error(), filepath.Join(root, "Local")) {
		t.Fatalf("missing failing stage/path: %v", err)
	}
	if initializedBetaRoot != "" || config.IsolatedProfileEnabled() {
		t.Fatal("failed initialization published a profile")
	}
}

func TestBetaRejectsLinkedParentBeforeCreatingProfile(t *testing.T) {
	base, _ := withBetaProfile(t)
	target := filepath.Join(base, "official-profile")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "linked-appdata")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("fixture symlink unavailable without elevation: %v", err)
	}
	t.Setenv("APPDATA", link)
	t.Setenv("XDG_CONFIG_HOME", link)
	err := initializeBeta()
	if err == nil || !strings.Contains(err.Error(), "redirected prefix") {
		t.Fatalf("linked ancestor accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, betaName)); !os.IsNotExist(err) {
		t.Fatalf("wrote through linked ancestor: %v", err)
	}
}

func TestBetaOpenedProjectDoesNotImportCredentialsOrState(t *testing.T) {
	base, root := withBetaProfile(t)
	workspace := filepath.Join(base, "opened-project")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]string{
		filepath.Join(home, ".env"):                                        "BETA_FIXTURE_API_KEY=official-home\n",
		filepath.Join(workspace, ".env"):                                   "BETA_FIXTURE_API_KEY=official-project\n",
		filepath.Join(workspace, "orca.toml"):                              "language='official-sentinel'\n",
		filepath.Join(workspace, ".mcp.json"):                              `{"mcpServers":{"official-only":{"command":"must-not-run"}}}`,
		filepath.Join(workspace, ".orca", topicTitlesFile):                 `{"old":"official-title"}`,
		filepath.Join(workspace, "ORCA.md"):                                "official-private-memory",
		filepath.Join(workspace, "AGENTS.md"):                              "allowed-tooling-code\n@.env\n@.orca/private.md\n",
		filepath.Join(workspace, ".orca", "private.md"):                    "official-private-import",
		filepath.Join(home, ".orca", "skills", "home-secret", "SKILL.md"):  "---\nname: home-secret\ndescription: sentinel\n---\nprivate",
		filepath.Join(home, "explicit-tools", "external-code", "SKILL.md"): "---\nname: external-code\ndescription: explicit tooling\n---\nallowed",
		filepath.Join(workspace, ".orca", "settings.json"):                 `{"hooks":{"SessionStart":[{"command":"must-not-run"}]}}`,
	}
	for path, body := range fixtures {
		writeBetaFixture(t, path, body)
	}
	developerPath := filepath.Join(base, "developer-tools")
	officialPath := filepath.Join(base, "official-runtime")
	writeBetaFixture(t, filepath.Join(developerPath, "tool.exe"), "fixture")
	writeBetaFixture(t, filepath.Join(officialPath, "Orca.exe"), "fixture")
	t.Setenv("PATH", strings.Join([]string{officialPath, developerPath, "."}, string(os.PathListSeparator)))
	t.Setenv("BETA_FIXTURE_API_KEY", "inherited-secret")
	t.Setenv("BETA_ARBITRARY_CREDENTIAL_NAME", "inherited-custom-secret")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "explicit.gitconfig"))
	if err := initializeBeta(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("PATH") != developerPath {
		t.Fatalf("unexpected beta PATH=%q", os.Getenv("PATH"))
	}
	if os.Getenv("BETA_FIXTURE_API_KEY") != "" {
		t.Fatal("inherited credential survived")
	}
	if os.Getenv("GIT_CONFIG_GLOBAL") != filepath.Join(home, "explicit.gitconfig") {
		t.Fatal("explicit external config pointer changed")
	}
	cfg, err := config.LoadForRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language == "official-sentinel" {
		t.Fatal("official project config loaded")
	}
	for _, key := range []string{"BETA_FIXTURE_API_KEY", "BETA_ARBITRARY_CREDENTIAL_NAME"} {
		if _, ok := cfg.Env(key); ok {
			t.Fatalf("inherited credential resolved: %s", key)
		}
	}
	for _, p := range cfg.Plugins {
		if p.Name == "official-only" {
			t.Fatal("official project MCP loaded")
		}
	}
	removeHomeEnvKey("BETA_FIXTURE_API_KEY")
	promoteProviderKeysToCredentials(cfg)
	if err := removeEnvFile(filepath.Join(home, ".env"), "BETA_FIXTURE_API_KEY"); err == nil {
		t.Fatal("external dotenv mutation accepted")
	}
	if err := upsertDotEnv("BETA_FIXTURE_API_KEY", "beta-only"); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.LoadForRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := cfg.Env("BETA_FIXTURE_API_KEY"); !ok || value != "beta-only" {
		t.Fatal("beta credential did not resolve")
	}
	if len(loadTopicTitles(workspace)) != 0 {
		t.Fatal("official project titles imported")
	}
	if err := saveTopicTitles(workspace, map[string]string{"new": "beta-title"}); err != nil {
		t.Fatal(err)
	}
	if config.ValidateProfileDataPath(topicTitlesPath(workspace)) != nil {
		t.Fatal("titles outside beta root")
	}
	mem := memory.Load(memory.Options{CWD: workspace, UserDir: home})
	block := mem.Block()
	if !strings.Contains(block, "allowed-tooling-code") || strings.Contains(block, "official-private") || strings.Contains(block, "official-project") {
		t.Fatalf("unsafe memory discovery: %q", block)
	}
	if err := memory.AppendDoc(mem.DocPath(memory.ScopeProject), "beta note"); err != nil {
		t.Fatal(err)
	}
	if err := memory.AppendDoc(filepath.Join(workspace, "ORCA.md"), "must-not-write"); err == nil {
		t.Fatal("official memory write allowed")
	}
	if hooks := hook.Load(hook.LoadOptions{ProjectRoot: workspace, Trusted: true}); len(hooks) != 0 {
		t.Fatal("official hooks imported")
	}
	store := skill.New(skill.Options{ProjectRoot: workspace, CustomPaths: []string{"~/explicit-tools"}, DisableBuiltins: true})
	got := store.List()
	if len(got) != 1 || got[0].Name != "external-code" {
		t.Fatalf("explicit tooling not preserved: %+v", got)
	}
	if _, err := store.Create("beta-skill", skill.ScopeProject); err != nil {
		t.Fatal(err)
	}
	if _, err := control.SaveAttachmentDataURLAt(workspace, "note.txt", "data:text/plain;base64,YmV0YQ=="); err == nil {
		t.Fatal("attachment wrote official project state")
	}
	if _, err := control.SaveAttachmentDataURLAt(filepath.Join(root, "workspace"), "note.txt", "data:text/plain;base64,YmV0YQ=="); err != nil {
		t.Fatalf("beta-owned attachment failed: %v", err)
	}
	if _, err := control.SaveAttachmentFileAt(filepath.Join(home, ".env"), filepath.Join(root, "workspace")); err == nil {
		t.Fatal("official credential source accepted as attachment")
	}
	for path, want := range fixtures {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("official fixture changed: %s (%v)", path, err)
		}
	}
}

func TestBetaSessionAndInstallerBoundaries(t *testing.T) {
	base, root := withBetaProfile(t)
	if err := initializeBeta(); err != nil {
		t.Fatal(err)
	}
	official := filepath.Join(base, "official", "sessions", "old.jsonl")
	writeBetaFixture(t, official, "official-transcript")
	if _, err := agent.LoadSession(official); err == nil {
		t.Fatal("official transcript read allowed")
	}
	if err := agent.NewSession("beta").Save(official); err == nil {
		t.Fatal("official transcript write allowed")
	}
	if _, _, err := agent.LoadBranchMeta(official); err == nil {
		t.Fatal("official branch read allowed")
	}
	if _, err := agent.ListSessions(filepath.Dir(official)); err == nil {
		t.Fatal("official sessions listing allowed")
	}
	if _, ok := validatePinnedSessionPath(config.SessionDir(), official); ok {
		t.Fatal("official pinned session accepted")
	}
	betaSession := filepath.Join(root, "orca", "sessions", "new.jsonl")
	if err := agent.NewSession("beta").Save(betaSession); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.LoadSession(betaSession); err != nil {
		t.Fatal(err)
	}
	if _, ok := validatePinnedSessionPath(filepath.Dir(betaSession), "new.jsonl"); !ok {
		t.Fatal("beta-relative pinned session rejected")
	}
	a := &App{}
	installDir := filepath.Join(base, "co-located-install-fixture")
	if err := os.MkdirAll(installDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.listenForInstallerShutdown(context.Background(), installDir, func() { t.Error("legacy IPC quit beta") }); err != nil {
		t.Fatal(err)
	}
	if a.installerShutdown.stop != nil || a.installerShutdown.cancel != nil {
		t.Fatal("beta registered legacy installer listener")
	}
	if requested, err := installipc.RequestShutdown(installDir); err != nil || requested {
		t.Fatalf("unexpected installer endpoint: %v %v", requested, err)
	}
}

func TestBetaAccountMetadataRequiresIsolatedProfile(t *testing.T) {
	base, root := withBetaProfile(t)
	official := filepath.Join(os.Getenv("APPDATA"), "orca", "config.toml")
	writeBetaFixture(t, official, "language='official-sentinel'\n")
	a := &App{}
	catalog := accountCatalog("fixture-subject", []orcaaccount.Model{{ID: "fixture-model", Protocol: "openai", GroupID: 1}})
	if err := a.persistAccountCatalog(catalog); err == nil {
		t.Fatal("account catalog persisted before profile initialization")
	}
	if err := initializeBeta(); err != nil {
		t.Fatal(err)
	}
	if err := a.persistAccountCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	for name := range catalog {
		p, ok := cfg.Provider(name)
		if !ok || p.Kind != accountProviderKind || p.APIKeyEnv != "" {
			t.Fatal("beta account metadata missing or acquired fake credentials")
		}
	}
	if body, err := os.ReadFile(official); err != nil || string(body) != "language='official-sentinel'\n" {
		t.Fatal("account metadata changed official config")
	}
	if _, err := orcaaccount.NewSystemStore(filepath.Join(base, "official", "account.dpapi")); err == nil {
		t.Fatal("account credential store accepted external path")
	}
	if _, err := orcaaccount.NewSystemStore(filepath.Join(root, "account", "session.dpapi")); err != nil {
		t.Fatalf("beta account store rejected: %v", err)
	}
}
