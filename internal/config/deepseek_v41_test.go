package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDeepSeekV41SourceMigrationOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orca.toml")
	raw := `config_version = 11
default_model = "deepseek/deepseek-v4-pro"
custom_setting = "keep me"
[desktop]
vision_mode = "off"
ui_style = "classic"
[agent]
planner_model = "deepseek/deepseek-v4-pro"
subagent_models = {vision = "deepseek/deepseek-v4-flash-vision-exp", review = "relay/deepseek-v4-pro"}
[permissions]
auto_review_model = "deepseek/deepseek-v4-flash"
[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"]
default = "deepseek-v4-pro"
api_key_env = "PRIVATE_KEY_SLOT"
[[providers]]
name = "relay"
kind = "openai"
base_url = "https://relay.example/v1"
models = ["deepseek-v4-pro"]
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := migrateDeepSeekV41File(path, Default()); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigVersion != 12 || cfg.DefaultModel != "deepseek/deepseek-flash" || cfg.Agent.PlannerModel != cfg.DefaultModel || cfg.Permissions.AutoReviewModel != cfg.DefaultModel {
		t.Fatalf("migration: %+v", cfg)
	}
	if cfg.Agent.SubagentModels["vision"] != cfg.DefaultModel || cfg.Agent.SubagentModels["review"] != "relay/deepseek-v4-pro" {
		t.Fatal(cfg.Agent.SubagentModels)
	}
	if cfg.Desktop.VisionMode != "off" || cfg.Desktop.UIStyle != "classic" || cfg.Providers[0].APIKeyEnv != "PRIVATE_KEY_SLOT" {
		t.Fatal("unrelated settings changed")
	}
	backup, err := os.ReadFile(path + ".pre-v41.bak")
	if err != nil || string(backup) != raw {
		t.Fatalf("backup: %v", err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "keep me") {
		t.Fatal("unknown setting lost")
	}
	// A deliberate selection after migration must not get upgraded again.
	next := strings.Replace(string(body), `default_model = "deepseek/deepseek-flash"`, `default_model = "deepseek/deepseek-v4-pro"`, 1)
	if err := os.WriteFile(path, []byte(next), 0600); err != nil {
		t.Fatal(err)
	}
	if err := migrateDeepSeekV41File(path, Default()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != next {
		t.Fatal("migration repeated")
	}
}

func TestDeepSeekV41MigrationRejectsLookalikeProvider(t *testing.T) {
	for _, endpoint := range []string{"https://api.deepseek.com.attacker.example", "http://api.deepseek.com", "https://api.deepseek.com/proxy", "https://relay.example"} {
		t.Run(endpoint, func(t *testing.T) {
			cfg := &Config{Providers: []ProviderEntry{{Name: "deepseek", Kind: "openai", BaseURL: endpoint, Model: "deepseek-v4-pro"}}}
			ref := "deepseek/deepseek-v4-pro"
			if got := cfg.UpgradeDeepSeekV41Ref(ref); got != ref {
				t.Fatalf("changed custom provider to %q", got)
			}
		})
	}
}

func TestDeepSeekV41AliasAndProResolution(t *testing.T) {
	cfg := Default()
	for _, model := range []string{"deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp"} {
		p, ok := cfg.ResolveModel("deepseek/" + model)
		if !ok || p.Model != "deepseek-flash" {
			t.Fatalf("%s => %+v, %v", model, p, ok)
		}
	}
	p, ok := cfg.ResolveModel("deepseek/deepseek-v4-pro")
	if !ok || p.Model != "deepseek-v4-pro" {
		t.Fatal("manual Pro unavailable")
	}
	if got := cfg.ResolveVisionModelRef(); got != "deepseek/deepseek-flash" {
		t.Fatal(got)
	}
}

func TestDeepSeekV41LegacySourceRetainsCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orca.toml")
	raw := `config_version = 11
default_model = "deepseek-pro"
[[providers]]
name = "deepseek-pro"
kind = "openai"
base_url = "https://api.deepseek.com/v1"
model = "deepseek-v4-pro"
api_key_env = "SEPARATE_PRO_KEY"
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadForEdit(path)
	p, ok := cfg.ResolveModel(cfg.DefaultModel)
	if !ok || p.Model != "deepseek-flash" || p.APIKeyEnv != "SEPARATE_PRO_KEY" || p.BaseURL != "https://api.deepseek.com/v1" {
		t.Fatalf("lost provider identity: %s %+v %v", cfg.DefaultModel, p, ok)
	}
	if err := cfg.SetDefaultModel("deepseek/deepseek-v4-pro"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	if got := LoadForEdit(path).DefaultModel; got != "deepseek/deepseek-v4-pro" {
		t.Fatal(got)
	}
}

func TestDeepSeekV41ProjectMigrationDoesNotCopyGlobalSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orca.toml")
	raw := "config_version = 11\n[agent]\nsubagent_model = \"deepseek/deepseek-v4-pro\"\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := migrateDeepSeekV41File(path, Default()); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if _, err := toml.DecodeFile(path, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc) != 2 || doc["providers"] != nil || doc["desktop"] != nil {
		t.Fatal("inherited settings leaked into project")
	}
	if doc["agent"].(map[string]any)["subagent_model"] != "deepseek/deepseek-flash" {
		t.Fatal(doc)
	}
}

func TestDeepSeekV41MissingBackupCannotOverwriteConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orca.toml")
	raw := "config_version = 11\ndefault_model = \"deepseek/deepseek-v4-pro\"\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	// A directory cannot be used as a trusted recovery backup.
	if err := os.Mkdir(path+".pre-v41.bak", 0700); err != nil {
		t.Fatal(err)
	}
	if err := migrateDeepSeekV41File(path, Default()); err == nil {
		t.Fatal("accepted invalid backup")
	}
	after, _ := os.ReadFile(path)
	if string(after) != raw {
		t.Fatal("source changed without backup")
	}
	cfg := LoadForEdit(path)
	if cfg.SaveTo(path) == nil || cfg.WriteFile(path) == nil {
		t.Fatal("failed migration allowed settings to overwrite the source")
	}
	after, _ = os.ReadFile(path)
	if string(after) != raw {
		t.Fatal("settings fallback replaced source")
	}
}

func TestDeepSeekV41NoOpMigrationIsRecorded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orca.toml")
	writeLegacy(t, path, "config_version = 11\ndefault_model = \"deepseek/deepseek-flash\"\n")
	if err := migrateDeepSeekV41File(path, Default()); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(body), "config_version = 12") {
		t.Fatalf("inspection not recorded: %v", err)
	}
	body = []byte(strings.Replace(string(body), "deepseek/deepseek-flash", "deepseek/deepseek-v4-pro", 1))
	if err = os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	// Even an unusable lock path must not turn an ordinary V12 read into a write.
	if err = os.Remove(path + ".v41.lock"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path+".v41.lock", 0700); err != nil {
		t.Fatal(err)
	}
	if err = migrateDeepSeekV41File(path, Default()); err != nil {
		t.Fatal(err)
	}
	if cfg := LoadForEdit(path); cfg.DefaultModel != "deepseek/deepseek-v4-pro" || cfg.loadErr != nil {
		t.Fatalf("manual Pro lost: %s, %v", cfg.DefaultModel, cfg.loadErr)
	}
}

func TestDeepSeekV41UnreadableSourceCannotSaveDefaults(t *testing.T) {
	cfg := LoadForEdit(filepath.Join(t.TempDir(), "invalid\x00.toml"))
	path := filepath.Join(t.TempDir(), "existing.toml")
	const original = "config_version = 12\nlanguage = \"zh\"\n"
	writeLegacy(t, path, original)
	if err := cfg.SaveTo(path); err == nil {
		t.Fatal("unreadable source allowed fallback settings save")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != original {
		t.Fatalf("original changed: %v", err)
	}
}

func TestDeepSeekV41SourceIdentityAndProjectOverride(t *testing.T) {
	for _, endpoint := range []string{"https://api.deepseek.com", "https://relay.example/v1"} {
		t.Run(endpoint, func(t *testing.T) {
			_, globalPath, _ := legacyHome(t)
			global := `config_version = 11
default_model = "deepseek-pro/deepseek-v4-pro"
[[providers]]
name = "deepseek-pro"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-pro"
api_key_env = "DEEPSEEK_API_KEY"
`
			writeLegacy(t, globalPath, global)
			projectRoot := t.TempDir()
			projectPath := filepath.Join(projectRoot, "orca.toml")
			project := "config_version = 11\n"
			if endpoint != "https://api.deepseek.com" {
				project += "default_model = \"deepseek-pro/deepseek-v4-pro\"\n"
			}
			project += "[[providers]]\nname = \"deepseek-pro\"\nkind = \"openai\"\nbase_url = \"" + endpoint + "\"\nmodel = \"deepseek-v4-pro\"\napi_key_env = \"PROJECT_KEY_SLOT\"\n"
			writeLegacy(t, projectPath, project)
			cfg, err := LoadForRoot(projectRoot)
			if err != nil {
				t.Fatal(err)
			}
			resolved, ok := cfg.ResolveModel(cfg.DefaultModel)
			if !ok || resolved.APIKeyEnv != "PROJECT_KEY_SLOT" || resolved.BaseURL != endpoint {
				t.Fatalf("project identity bypassed: %q, %+v, %v", cfg.DefaultModel, resolved, ok)
			}
			var persisted Config
			if _, err = toml.DecodeFile(globalPath, &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted.DefaultModel != "deepseek-pro/deepseek-flash" || len(persisted.Providers) != 1 {
				t.Fatalf("source identity changed: %q", persisted.DefaultModel)
			}
			if endpoint != "https://api.deepseek.com" {
				if _, err = toml.DecodeFile(projectPath, &persisted); err != nil {
					t.Fatal(err)
				}
				if persisted.Providers[0].Model != "deepseek-v4-pro" {
					t.Fatal("relay provider model migrated")
				}
			}
		})
	}
}
