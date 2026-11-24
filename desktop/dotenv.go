package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/fileutil"
)

const (
	mimoAPIKeyEnv       = "MIMO_API_KEY"
	mimoTokenPlanKeyEnv = "MIMO_TOKEN_PLAN_API_KEY"
)

var configVersionLineRE = regexp.MustCompile(`(?m)^\s*config_version\s*=\s*([0-9]+)\s*(?:#.*)?$`)

// migrateMimoCredentialsV10 performs the one-time split of the credential slot
// that V9 accidentally shared between the two official MiMo endpoints.
func migrateMimoCredentialsV10(cfg *config.Config, tabs desktopTabsFile) bool {
	if betaEnabled() || config.IsolatedProfileEnabled() {
		return false
	}
	if cfg == nil || !userConfigPredatesV10() {
		return false
	}
	path := credentialsPath()
	marker := filepath.Join(desktopConfigDir(), "mimo-credentials-v10.migrated")
	if _, err := os.Stat(marker); err == nil {
		return false
	}
	keys := envFileKeys(path)
	if keys[mimoTokenPlanKeyEnv] || !keys[mimoAPIKeyEnv] {
		_ = writeMimoMigrationMarker(marker)
		return false
	}
	access := map[string]bool{}
	for _, raw := range cfg.Desktop.ProviderAccess {
		access[config.CanonicalDesktopOfficialProviderName(raw)] = true
	}
	if !access["mimo-token-plan"] {
		_ = writeMimoMigrationMarker(marker)
		return false
	}
	source := "mimo-token-plan"
	if access["mimo-api"] {
		source = mimoCurrentSource(cfg, tabs)
	}
	if source == "mimo-token-plan" {
		if err := moveMimoCredentialToTokenPlan(path); err != nil {
			return false
		}
	}
	_ = writeMimoMigrationMarker(marker)
	return source == "mimo-token-plan"
}

func userConfigPredatesV10() bool {
	b, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		return false
	}
	m := configVersionLineRE.FindSubmatch(b)
	if len(m) != 2 {
		return true
	}
	return string(m[1]) != "10"
}

func mimoCurrentSource(cfg *config.Config, tabs desktopTabsFile) string {
	refs := make([]string, 0, 4+len(tabs.Tabs))
	for _, tab := range tabs.Tabs {
		if tab.ID == tabs.ActiveTab {
			refs = append(refs, tab.Model)
			break
		}
	}
	refs = append(refs, tabs.RecentConversationPrefs.Model, cfg.DefaultModel, cfg.Bot.Model)
	for _, ref := range refs {
		provider, _, ok := strings.Cut(strings.TrimSpace(ref), "/")
		if ok && (provider == "mimo-api" || provider == "mimo-token-plan") {
			return provider
		}
	}
	return "mimo-api"
}

func currentMimoCredentialSource() string {
	apiValue, apiSet := os.LookupEnv(mimoAPIKeyEnv)
	planValue, planSet := os.LookupEnv(mimoTokenPlanKeyEnv)
	apiSet = apiSet && strings.TrimSpace(apiValue) != ""
	planSet = planSet && strings.TrimSpace(planValue) != ""
	if planSet && !apiSet {
		return "mimo-token-plan"
	}
	return "mimo-api"
}

func normalizeLegacyMimoDesktopState(cfg *config.Config, tabs *desktopTabsFile, source string) {
	if source != "mimo-token-plan" {
		source = "mimo-api"
	}
	normalize := func(ref string) string {
		trimmed := strings.TrimSpace(ref)
		provider, model, hasModel := strings.Cut(trimmed, "/")
		if hasModel && (provider == "mimo-flash" || provider == "mimo-pro") {
			return source + "/" + model
		}
		if !hasModel && (trimmed == "mimo-v2.5" || trimmed == "mimo-v2.5-pro") {
			return source + "/" + trimmed
		}
		return ref
	}
	if cfg != nil {
		cfg.DefaultModel = normalize(cfg.DefaultModel)
		cfg.Bot.Model = normalize(cfg.Bot.Model)
		cfg.Agent.PlannerModel = normalize(cfg.Agent.PlannerModel)
		cfg.Agent.SubagentModel = normalize(cfg.Agent.SubagentModel)
		for name, ref := range cfg.Agent.SubagentModels {
			cfg.Agent.SubagentModels[name] = normalize(ref)
		}
	}
	if tabs != nil {
		for i := range tabs.Tabs {
			tabs.Tabs[i].Model = normalize(tabs.Tabs[i].Model)
		}
		tabs.RecentConversationPrefs.Model = normalize(tabs.RecentConversationPrefs.Model)
	}
}

func moveMimoCredentialToTokenPlan(path string) error {
	if err := config.ValidateProfileDataPath(path); err != nil {
		return err
	}
	if err := config.ValidateProfileDataPath(path + ".v9-mimo.bak"); err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	backup := path + ".v9-mimo.bak"
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		if err := os.WriteFile(backup, b, 0o600); err != nil {
			return err
		}
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		key, value, ok := strings.Cut(trimmed, "=")
		if ok && strings.TrimSpace(key) == mimoAPIKeyEnv {
			lines[i] = mimoTokenPlanKeyEnv + "=" + value
			if err := os.Setenv(mimoTokenPlanKeyEnv, value); err != nil {
				return err
			}
			found = true
		}
	}
	if !found {
		return fmt.Errorf("legacy MiMo credential not found")
	}
	if err := atomicWriteEnvFile(path, strings.Join(lines, "\n")+"\n"); err != nil {
		return err
	}
	return os.Unsetenv(mimoAPIKeyEnv)
}

func atomicWriteEnvFile(path, contents string) error {
	if err := config.ValidateProfileDataPath(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "credentials.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(contents); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmpPath, path)
}

func writeMimoMigrationMarker(path string) error {
	if err := config.ValidateProfileDataPath(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("10\n"), 0o600)
}

// credentialsPath is the orca-owned global secrets file the settings panel
// writes API keys to — the same file `orca setup` writes and config.loadDotEnv
// reads, so a key set in the desktop app resolves for the CLI from any directory.
// Never a project .env: keys stay out of the user's project tree. Falls back to
// ~/.env only when the user config dir can't be resolved.
func credentialsPath() string {
	if p := config.UserCredentialsPath(); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".env")
	}
	return ".env"
}

// upsertDotEnv sets KEY=value in the global credentials file (replacing an
// existing KEY line, else appending), and applies it to the running process so a
// rebuild picks it up without a restart.
func upsertDotEnv(key, value string) error {
	return upsertEnvFile(credentialsPath(), key, value)
}

func removeDotEnv(key string) error {
	return removeEnvFile(credentialsPath(), key)
}

// upsertEnvFile merges KEY=value into a KEY=value file at path, preserving
// comments and unrelated lines, writing atomically via a sibling temp + rename.
func upsertEnvFile(path, key, value string) error {
	if err := config.ValidateProfileDataPath(path); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}
	replaced := false
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if k, _, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == key {
			lines[i] = key + "=" + value
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, key+"="+value)
	}
	out := strings.Join(lines, "\n") + "\n"

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, "credentials.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(out); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Setenv(key, value)
}

func removeEnvFile(path, key string) error {
	if err := config.ValidateProfileDataPath(path); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return os.Unsetenv(key)
		}
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	outLines := make([]string, 0, len(lines))
	for _, ln := range lines {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ln), "export "))
		if t == "" || strings.HasPrefix(t, "#") {
			outLines = append(outLines, ln)
			continue
		}
		if k, _, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == key {
			continue
		}
		outLines = append(outLines, ln)
	}
	out := ""
	if len(outLines) > 0 {
		out = strings.Join(outLines, "\n") + "\n"
	}

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, "credentials.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(out); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Unsetenv(key)
}

// envFileKeys returns the set of KEY names assigned in a KEY=value file, empty
// when the file is absent.
func envFileKeys(path string) map[string]bool {
	keys := map[string]bool{}
	if config.ValidateProfileDataPath(path) != nil {
		return keys
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return keys
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimPrefix(strings.TrimSpace(raw), "export ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, _, ok := strings.Cut(line, "="); ok {
			keys[strings.TrimSpace(k)] = true
		}
	}
	return keys
}

// promoteProviderKeysToCredentials copies any configured provider api_key_env that
// currently resolves (from a project .env, ~/.env, or the OS env) into the global
// credentials file when it isn't there yet, so a key set for one workspace follows
// the user across every project. Promoted keys are then stripped from ~/.env so the
// credentials file is the single source of truth; a project's own .env is
// user-owned and left untouched.
func promoteProviderKeysToCredentials(cfg *config.Config) {
	if betaEnabled() || config.IsolatedProfileEnabled() {
		return
	}
	credPath := credentialsPath()
	have := envFileKeys(credPath)
	for _, p := range cfg.Providers {
		env := strings.TrimSpace(p.APIKeyEnv)
		if env == "" || have[env] {
			continue
		}
		val := os.Getenv(env)
		if val == "" {
			continue
		}
		if err := upsertEnvFile(credPath, env, val); err != nil {
			continue
		}
		have[env] = true
		removeHomeEnvKey(env)
	}
}

// removeHomeEnvKey deletes a single KEY=value assignment from ~/.env (the legacy
// fallback the old migration wrote to), leaving every other line intact. No-op when
// ~/.env is absent or the credentials store resolves to ~/.env itself.
func removeHomeEnvKey(key string) {
	if betaEnabled() || config.IsolatedProfileEnabled() {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(home, ".env")
	if sameConfigPath(path, credentialsPath()) {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var kept []string
	removed := false
	for _, raw := range strings.Split(string(data), "\n") {
		check := strings.TrimPrefix(strings.TrimSpace(raw), "export ")
		if k, _, ok := strings.Cut(check, "="); ok && strings.TrimSpace(k) == key {
			removed = true
			continue
		}
		kept = append(kept, raw)
	}
	if !removed {
		return
	}
	_ = os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o600)
}
