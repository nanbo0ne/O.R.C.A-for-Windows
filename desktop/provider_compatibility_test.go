package main

import (
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

func TestCompatibleTabEffort(t *testing.T) {
	tests := []struct {
		name  string
		entry config.ProviderEntry
		value *string
		want  *string
	}{
		{name: "unset", entry: config.ProviderEntry{Kind: "openai"}},
		{name: "local rejects old anthropic level", entry: config.ProviderEntry{Kind: "openai", BaseURL: "http://localhost:1234/v1", Model: "local"}, value: ptrEffort("xhigh")},
		{name: "explicit auto remains auto", entry: config.ProviderEntry{Kind: "openai"}, value: ptrEffort(""), want: ptrEffort("")},
		{name: "anthropic unchanged", entry: config.ProviderEntry{Kind: "anthropic"}, value: ptrEffort("xhigh"), want: ptrEffort("xhigh")},
		{name: "explicit compatible level preserved", entry: config.ProviderEntry{Kind: "openai", ReasoningProtocol: "openai"}, value: ptrEffort("low"), want: ptrEffort("low")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compatibleTabEffort(&tt.entry, tt.value)
			if (got == nil) != (tt.want == nil) || got != nil && *got != *tt.want {
				t.Fatalf("unexpected normalized override: got %v want %v", got, tt.want)
			}
			if got != nil && got == tt.value {
				t.Fatal("must not alias saved pointer")
			}
		})
	}
}

func ptrEffort(value string) *string { return &value }

func TestSaveProviderProtocolChangeDropsOnlyOldProtocolKnobs(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			isolateDesktopUserDirs(t)
			cfg := config.Default()
			entry := config.ProviderEntry{Name: "local-fixture", Kind: "anthropic", BaseURL: "http://localhost:1234/v1", Model: "local", APIKeyEnv: "FIXTURE_KEY", Effort: "xhigh", Thinking: "adaptive", ContextWindow: 65536}
			if err := cfg.UpsertProvider(entry); err != nil {
				t.Fatal(err)
			}
			if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
				t.Fatal(err)
			}
			app := NewApp()
			if err := app.SaveProvider(ProviderView{Name: entry.Name, Kind: kind, BaseURL: entry.BaseURL, Models: []string{entry.Model}, Default: entry.Model, APIKeyEnv: entry.APIKeyEnv, ContextWindow: entry.ContextWindow}); err != nil {
				t.Fatal(err)
			}
			saved, ok := config.LoadForEdit(config.UserConfigPath()).Provider(entry.Name)
			if !ok {
				t.Fatal("provider missing")
			}
			if saved.Kind != kind || saved.APIKeyEnv != entry.APIKeyEnv || saved.BaseURL != entry.BaseURL || saved.ContextWindow != 65536 {
				t.Fatal("provider identity or settings changed")
			}
			if kind == "openai" && (saved.Effort != "" || saved.Thinking != "") {
				t.Fatal("old protocol knobs survived explicit protocol change")
			}
			if kind == "anthropic" && (saved.Effort != "xhigh" || saved.Thinking != "adaptive") {
				t.Fatal("unchanged Anthropic settings lost")
			}
		})
	}
}
