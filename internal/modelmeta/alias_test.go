package modelmeta

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

func TestOfficialFlashContextAliasOverrides(t *testing.T) {
	for _, tt := range []struct {
		name      string
		overrides map[string]int
		want      int
	}{
		{"canonical", map[string]int{"deepseek-flash": 256000}, 256000},
		{"legacy", map[string]int{"deepseek-v4-flash": 128000}, 128000},
		{"vision", map[string]int{"deepseek-v4-flash-vision-exp": 64000}, 64000},
		{"canonical wins", map[string]int{"deepseek-flash": 256000, "deepseek-v4-flash": 128000, "deepseek-v4-flash-vision-exp": 64000}, 256000},
		{"invalid canonical falls back", map[string]int{"deepseek-flash": 0, "deepseek-v4-flash": 128000}, 128000},
		{"no valid override", map[string]int{"deepseek-flash": 0, "deepseek-v4-flash": -1}, 1_000_000},
		{"no overrides", nil, 1_000_000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			original := make(map[string]int, len(tt.overrides))
			for model, window := range tt.overrides {
				original[model] = window
			}
			store := Load(filepath.Join(t.TempDir(), "metadata.json"))
			for _, model := range []string{"deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp"} {
				entry := &config.ProviderEntry{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: model, ContextWindow: 1_000_000, ModelContextWindows: tt.overrides}
				if err := store.Put(MetadataFromDiscovery(entry, 1_000_000, SourceProviderMetadata, CapabilityUnsupported, CapabilityUnknown, CapabilityUnknown, nil)); err != nil {
					t.Fatal(err)
				}
				got := Resolve(entry, store)
				wantSource := SourceUserOverride
				if tt.want == 1_000_000 {
					wantSource = SourceDeepSeekOfficial
				}
				if got.ContextWindow != tt.want || !got.ContextConfirmed || got.ContextSource != wantSource || got.Vision != CapabilitySupported {
					t.Fatalf("%s resolved metadata = %+v, want context %d (%s)", model, got, tt.want, wantSource)
				}
			}
			if tt.overrides != nil && !reflect.DeepEqual(tt.overrides, original) {
				t.Fatal("resolution rewrote explicit context overrides")
			}
		})
	}
}

func TestFlashContextAliasLookupRequiresOfficialIdentity(t *testing.T) {
	for _, tt := range []struct{ name, kind, endpoint, model string }{
		{"deepseek", "openai", "https://relay.example/v1", "deepseek-flash"},
		{"deepseek", "openai", "https://api.deepseek.com/custom", "deepseek-flash"},
		{"deepseek", "openai", "https://api.deepseek.com?route=custom", "deepseek-flash"},
		{"deepseek", "openai", "http://api.deepseek.com", "deepseek-flash"},
		{"custom", "openai", "https://api.deepseek.com", "deepseek-flash"},
		{"deepseek", "anthropic", "https://api.deepseek.com", "deepseek-flash"},
		{"deepseek", "openai", "https://api.deepseek.com", "deepseek-v4-pro"},
	} {
		t.Run(tt.name+"/"+tt.kind+"/"+tt.endpoint+"/"+tt.model, func(t *testing.T) {
			store := Load(filepath.Join(t.TempDir(), "metadata.json"))
			entry := &config.ProviderEntry{Name: tt.name, Kind: tt.kind, BaseURL: tt.endpoint, Model: tt.model, ContextWindow: 32000, ModelContextWindows: map[string]int{"deepseek-v4-flash": 64000, "deepseek-v4-flash-vision-exp": 128000}}
			got := Resolve(entry, store)
			if got.ContextSource == SourceUserOverride || got.ContextWindow == 64000 || got.ContextWindow == 128000 {
				t.Fatalf("unrelated model inherited context override: %+v", got)
			}
			entry.ModelContextWindows[entry.Model] = 16000
			if got := Resolve(entry, store); got.ContextWindow != 16000 || got.ContextSource != SourceUserOverride || !got.ContextConfirmed {
				t.Fatalf("exact context override lost: %+v", got)
			}
		})
	}
}

func TestMigratedFlashProviderRefsKeepContextOverride(t *testing.T) {
	for _, name := range []string{"deepseek", "deepseek-flash", "deepseek-pro"} {
		for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-flash-vision-exp"} {
			t.Run(name+"/"+model, func(t *testing.T) {
				entry := config.ProviderEntry{Name: name, Kind: "openai", BaseURL: "https://api.deepseek.com/v1", Model: model, APIKeyEnv: "PROJECT_KEY_SLOT", ModelContextWindows: map[string]int{model: 64000}}
				cfg := &config.Config{Providers: []config.ProviderEntry{entry}}
				ref := cfg.UpgradeDeepSeekV41Ref(name + "/" + model)
				resolved, ok := cfg.ResolveModel(ref)
				if !ok || resolved.Name != name || resolved.Model != "deepseek-flash" || resolved.APIKeyEnv != entry.APIKeyEnv {
					t.Fatalf("migrated ref %q changed provider identity: %+v", ref, resolved)
				}
				store := Load(filepath.Join(t.TempDir(), "metadata.json"))
				if got := Resolve(resolved, store); got.ContextWindow != 64000 || got.ContextSource != SourceUserOverride || !got.ContextConfirmed || got.ModelRef != ref {
					t.Fatalf("migrated ref %q lost context override: %+v", ref, got)
				}
			})
		}
	}
}
