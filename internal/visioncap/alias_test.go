package visioncap

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

func TestOfficialFlashAliasOverridePriority(t *testing.T) {
	for _, tt := range []struct {
		name      string
		overrides map[string]string
		want      string
	}{
		{"legacy false", map[string]string{"deepseek-v4-flash": Unsupported}, Unsupported},
		{"vision false", map[string]string{"deepseek-v4-flash-vision-exp": Unsupported}, Unsupported},
		{"legacy true", map[string]string{"deepseek-v4-flash": Supported}, Supported},
		{"vision true", map[string]string{"deepseek-v4-flash-vision-exp": Supported}, Supported},
		{"false beats legacy true", map[string]string{"deepseek-v4-flash": Supported, "deepseek-v4-flash-vision-exp": Unsupported}, Unsupported},
		{"false beats vision true", map[string]string{"deepseek-v4-flash": Unsupported, "deepseek-v4-flash-vision-exp": Supported}, Unsupported},
		{"canonical true wins", map[string]string{"deepseek-flash": Supported, "deepseek-v4-flash": Unsupported}, Supported},
		{"canonical false wins", map[string]string{"deepseek-flash": Unsupported, "deepseek-v4-flash": Supported}, Unsupported},
		{"canonical auto wins", map[string]string{"deepseek-flash": OverrideAuto, "deepseek-v4-flash": Unsupported}, OverrideAuto},
		{"canonical metadata keeps legacy choice", map[string]string{"deepseek-flash": "", "deepseek-v4-flash": Unsupported}, Unsupported},
		{"automatic legacy result is not an override", map[string]string{"deepseek-v4-flash": OverrideAuto}, OverrideAuto},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "vision.json")
			store := Load(path)
			entry := config.ProviderEntry{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com/v1/"}
			for model, override := range tt.overrides {
				entry.Model = model
				if err := store.Put(Capability{Key: Key(&entry), ModelRef: ModelRef(&entry), Status: Unsupported, Source: SourceProbe, ProbeVersion: 1, Override: override}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			store = Load(path)
			original := Load(path)
			entry.Name = "deepseek"
			for _, model := range []string{"deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp"} {
				entry.Model = model
				stored := store.Stored(&entry)
				if stored.Key != Key(&entry) || stored.ModelRef != ModelRef(&entry) || stored.Status != Supported || stored.AutomaticStatus != Supported || stored.Override != tt.want {
					t.Fatalf("%s stored = %+v, want override %q with current identity", model, stored, tt.want)
				}
				wantStatus, wantSource := tt.want, SourceManual
				if tt.want == OverrideAuto {
					wantStatus, wantSource = Supported, SourceMetadata
				}
				if got := store.Get(&entry); got.Status != wantStatus || got.AutomaticStatus != Supported || got.Source != wantSource {
					t.Fatalf("%s effective = %+v", model, got)
				}
			}
			if !reflect.DeepEqual(store.Items, original.Items) {
				t.Fatal("alias lookup mutated cached records")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("alias lookup rewrote persisted records: %v", err)
			}
		})
	}
}

func TestOfficialFlashInheritedOverrideCanBeCleared(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vision.json")
	store := Load(path)
	entry := &config.ProviderEntry{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"}
	legacy := Capability{Key: Key(entry), Status: Unsupported, Override: Unsupported, Source: SourceMetadata}
	if err := store.Put(legacy); err != nil {
		t.Fatal(err)
	}
	entry.Model = "deepseek-flash"
	current := Load(path).Stored(entry)
	if current.Override != Unsupported || current.Key != Key(entry) {
		t.Fatalf("inherited override = %+v", current)
	}
	current.Override = OverrideAuto
	if err := store.Put(current); err != nil {
		t.Fatal(err)
	}
	if got := Load(path).Get(entry); got.Status != Supported || got.Override != OverrideAuto || got.Source != SourceMetadata {
		t.Fatalf("cleared canonical override fell back to legacy: %+v", got)
	}
	if got := Load(path).Items[legacy.Key]; got != legacy {
		t.Fatalf("clearing canonical override changed legacy record: %+v", got)
	}
}

func TestMigratedFlashProviderRefsKeepVisionOverride(t *testing.T) {
	for _, name := range []string{"deepseek", "deepseek-flash", "deepseek-pro"} {
		for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-flash-vision-exp"} {
			t.Run(name+"/"+model, func(t *testing.T) {
				entry := config.ProviderEntry{Name: name, Kind: "openai", BaseURL: "https://api.deepseek.com/v1", Model: model, APIKeyEnv: "PROJECT_KEY_SLOT"}
				store := Load(filepath.Join(t.TempDir(), "vision.json"))
				if err := store.Put(Capability{Key: Key(&entry), ModelRef: ModelRef(&entry), Status: Unsupported, Override: Unsupported}); err != nil {
					t.Fatal(err)
				}
				cfg := &config.Config{Providers: []config.ProviderEntry{entry}}
				ref := cfg.UpgradeDeepSeekV41Ref(name + "/" + model)
				resolved, ok := cfg.ResolveModel(ref)
				if !ok || resolved.Name != name || resolved.Model != "deepseek-flash" || resolved.APIKeyEnv != entry.APIKeyEnv {
					t.Fatalf("migrated ref %q changed provider identity: %+v", ref, resolved)
				}
				if got := store.Get(resolved); got.Status != Unsupported || got.Override != Unsupported || got.Key != Key(resolved) || got.ModelRef != ref {
					t.Fatalf("migrated ref %q lost manual override: %+v", ref, got)
				}
			})
		}
	}
}

func TestFlashAliasOverrideDoesNotCrossOfficialEndpointPaths(t *testing.T) {
	store := Load(filepath.Join(t.TempDir(), "vision.json"))
	entry := &config.ProviderEntry{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"}
	if err := store.Put(Capability{Key: Key(entry), Override: Unsupported}); err != nil {
		t.Fatal(err)
	}
	entry.Model = "deepseek-flash"
	entry.BaseURL = "https://api.deepseek.com"
	if got := store.Get(entry); got.Status != Supported || got.Override != OverrideAuto {
		t.Fatalf("borrowed override from a different endpoint key: %+v", got)
	}
}

func TestFlashAliasOverrideRequiresOfficialIdentity(t *testing.T) {
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
			store := Load(filepath.Join(t.TempDir(), "vision.json"))
			entry := &config.ProviderEntry{Name: tt.name, Kind: tt.kind, BaseURL: tt.endpoint, Model: "deepseek-v4-flash"}
			if err := store.Put(Capability{Key: Key(entry), Status: Unsupported, Override: Unsupported}); err != nil {
				t.Fatal(err)
			}
			entry.Model = tt.model
			if got := store.Get(entry); got.Override != OverrideAuto || got.Source == SourceManual {
				t.Fatalf("unrelated model inherited legacy override: %+v", got)
			}
			if err := store.Put(Capability{Key: Key(entry), Status: Unknown, Override: Supported}); err != nil {
				t.Fatal(err)
			}
			if got := store.Get(entry); got.Status != Supported || got.Override != Supported || got.Source != SourceManual {
				t.Fatalf("exact unrelated override changed: %+v", got)
			}
		})
	}
}
