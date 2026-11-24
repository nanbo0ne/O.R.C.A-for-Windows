package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Tests may override individual profile variables, but their fallback must
// never read or migrate a developer's real configuration or credentials.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "orca-config-profile-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create config test profile:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(root)
	for key, path := range map[string]string{
		"HOME": root, "USERPROFILE": root,
		"APPDATA":         filepath.Join(root, "AppData", "Roaming"),
		"LOCALAPPDATA":    filepath.Join(root, "AppData", "Local"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_CACHE_HOME":  filepath.Join(root, "cache"),
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
	} {
		if err := os.MkdirAll(path, 0700); err != nil {
			panic(fmt.Errorf("create config fixture %s: %w", key, err))
		}
		if err := os.Setenv(key, path); err != nil {
			panic(fmt.Errorf("set config fixture %s: %w", key, err))
		}
	}
	m.Run()
}

func configureTestMimo(t *testing.T, c *Config) {
	t.Helper()
	if err := c.UpsertProvider(ProviderEntry{
		Name:          "mimo-pro",
		Kind:          "openai",
		BaseURL:       "https://token-plan-cn.xiaomimimo.com/v1",
		Models:        []string{"mimo-v2.5", "mimo-v2.5-pro"},
		Default:       "mimo-v2.5-pro",
		APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
		ContextWindow: 1_048_576,
		NoProxy:       true,
	}); err != nil {
		t.Fatalf("configure test MiMo: %v", err)
	}
}
