package modelmeta

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func testEntry() *config.ProviderEntry {
	return &config.ProviderEntry{
		Name: "custom", Kind: "openai", BaseURL: "https://example.com/v1", Model: "model-a",
		ContextWindow: 32000, ModelContextWindows: map[string]int{"model-a": 64000},
	}
}

func TestResolveContextPriority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.json")
	store := Load(path)
	entry := testEntry()
	if got := Resolve(entry, store); got.ContextWindow != 64000 || !got.ContextConfirmed || got.ContextSource != SourceUserOverride {
		t.Fatalf("user override = %+v", got)
	}
	if err := store.Put(Metadata{Key: Key(entry), ModelRef: ModelRef(entry), ContextWindow: 128000, ContextConfirmed: true, ContextSource: SourceProviderMetadata}); err != nil {
		t.Fatal(err)
	}
	if got := Resolve(entry, Load(path)); got.ContextWindow != 128000 || got.ContextSource != SourceProviderMetadata {
		t.Fatalf("discovered context = %+v", got)
	}
	local := *entry
	local.Name = "orca-local"
	local.ContextWindow = 8192
	if got := Resolve(&local, store); got.ContextWindow != 8192 || got.ContextSource != SourceLocalRuntime {
		t.Fatalf("local runtime context = %+v", got)
	}
}

func TestSparseRefreshPreservesConfirmedMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.json")
	store := Load(path)
	entry := testEntry()
	first := MetadataFromDiscovery(entry, 128000, "context_length", CapabilitySupported, CapabilitySupported, CapabilitySupported, &provider.Pricing{Input: 2, Currency: "$"})
	if err := store.Put(first); err != nil {
		t.Fatal(err)
	}
	if err := Load(path).Put(MetadataFromDiscovery(entry, 0, "", CapabilityUnknown, CapabilityUnknown, CapabilityUnknown, nil)); err != nil {
		t.Fatal(err)
	}
	got := Load(path).Get(entry)
	if got.ContextWindow != 128000 || got.Vision != CapabilitySupported || got.ToolUse != CapabilitySupported || got.Pricing == nil {
		t.Fatalf("sparse refresh erased metadata: %+v", got)
	}
}

func TestParallelStoresMerge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.json")
	a := Load(path)
	b := Load(path)
	if err := a.Put(Metadata{Key: "a", ContextWindow: 1}); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(Metadata{Key: "b", ContextWindow: 2}); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); len(got.Items) != 2 {
		t.Fatalf("items = %+v", got.Items)
	}
}

func TestOfficialDeepSeekCapabilities(t *testing.T) {
	store := Load(filepath.Join(t.TempDir(), "metadata.json"))
	base := config.ProviderEntry{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com"}
	tests := []struct {
		model, vision string
	}{
		{"deepseek-flash", CapabilitySupported},
		{"deepseek-v4-flash", CapabilitySupported},
		{"deepseek-v4-pro", CapabilityUnsupported},
		{"deepseek-v4-flash-vision-exp", CapabilitySupported},
		{" DEEPSEEK-FLASH ", CapabilitySupported},
	}
	for _, tt := range tests {
		entry := base
		entry.Model = tt.model
		got := Resolve(&entry, store)
		if got.Vision != tt.vision || got.ToolUse != CapabilitySupported || got.StructuredOutput != CapabilitySupported {
			t.Fatalf("%s capabilities = %+v", tt.model, got)
		}
		if got.ContextWindow != 1_000_000 || !got.ContextConfirmed || got.ContextSource != SourceDeepSeekOfficial || got.MetadataSource != SourceDeepSeekOfficial {
			t.Fatalf("%s official context = %+v", tt.model, got)
		}
		if !got.PricingAvailable || got.Currency != "¥" || got.Pricing.Schedule == nil {
			t.Fatalf("%s official pricing unavailable: %+v", tt.model, got)
		}
	}
	relay := base
	relay.Name, relay.BaseURL, relay.Model = "relay", "https://relay.example/v1", "deepseek-v4-flash-vision-exp"
	if got := Resolve(&relay, store); got.Vision != CapabilityUnknown {
		t.Fatalf("relay inherited official capability: %+v", got)
	}
}

func TestOfficialDeepSeekRefreshesCachedMetadataWithoutMutatingHistory(t *testing.T) {
	for _, model := range []string{"deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp", "deepseek-v4-pro"} {
		for _, priceSource := range []string{"cache", "entry"} {
			t.Run(model+"/"+priceSource, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "metadata.json")
				store := Load(path)
				entry := &config.ProviderEntry{
					Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com/v1/", Model: model,
					ContextWindow: 64000,
				}
				oldPrice := &provider.Pricing{CacheHit: 0.05, Input: 1.5, Output: 4.5, Currency: "¥"}
				oldSnapshot := *oldPrice
				if priceSource == "entry" {
					entry.Price = oldPrice
				}
				staleVision, wantVision := CapabilityUnsupported, CapabilitySupported
				if model == "deepseek-v4-pro" {
					staleVision, wantVision = CapabilitySupported, CapabilityUnsupported
				}
				cached := MetadataFromDiscovery(entry, 128000, "context_length", staleVision, CapabilityUnsupported, CapabilityUnsupported, oldPrice)
				if err := store.Put(cached); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				loaded := Load(path)
				got := Resolve(entry, loaded)
				if got.Vision != wantVision || got.ToolUse != CapabilitySupported || got.StructuredOutput != CapabilitySupported || got.ContextWindow != 1_000_000 || !got.ContextConfirmed {
					t.Fatalf("stale metadata survived official refresh: %+v", got)
				}
				want := provider.PricingRates{CacheHit: 0.02, Input: 1, Output: 4}
				if model == "deepseek-v4-pro" {
					want = provider.PricingRates{CacheHit: 0.15, Input: 4.5, Output: 13.5}
				}
				if got.Pricing == nil || got.Pricing.Schedule == nil || got.Pricing.Schedule.OffPeak != want || got.Pricing.CacheHit != want.CacheHit || got.Pricing.Input != want.Input || got.Pricing.Output != want.Output {
					t.Fatalf("stale pricing survived official refresh: %+v", got.Pricing)
				}
				if *oldPrice != oldSnapshot || got.Pricing == oldPrice || entry.ContextWindow != 64000 {
					t.Fatalf("refresh mutated original entry or recorded price: entry=%+v price=%+v", entry, oldPrice)
				}
				if frozen := oldPrice.SnapshotAt(time.Now()); *frozen != oldSnapshot {
					t.Fatalf("historical snapshot was repriced: %+v", frozen)
				}
				if saved := loaded.Get(entry); !reflect.DeepEqual(saved, cached) {
					t.Fatalf("refresh mutated cached record: %+v", saved)
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("resolution rewrote cached metadata: err=%v", err)
				}
			})
		}
	}
}

func TestOfficialDeepSeekMetadataPreservesCustomProviders(t *testing.T) {
	for _, tt := range []struct{ name, kind, endpoint, model string }{
		{"custom", "openai", "https://api.deepseek.com", "deepseek-flash"},
		{"deepseek", "anthropic", "https://api.deepseek.com", "deepseek-flash"},
		{"deepseek", "", "https://api.deepseek.com", "deepseek-flash"},
		{"deepseek", "openai", "https://relay.example/v1", "deepseek-flash"},
		{"deepseek", "openai", "http://api.deepseek.com", "deepseek-flash"},
		{"deepseek", "openai", "https://api.deepseek.com:443", "deepseek-flash"},
		{"deepseek", "openai", "https://api.deepseek.com/custom", "deepseek-flash"},
		{"deepseek", "openai", "https://api.deepseek.com?route=custom", "deepseek-flash"},
		{"deepseek", "openai", "https://user@api.deepseek.com", "deepseek-flash"},
		{"deepseek", "openai", "https://api.deepseek.com", "deepseek-flash-custom"},
		{"custom", "openai", "https://api.deepseek.com", "deepseek-v4-flash"},
		{"custom", "openai", "https://api.deepseek.com", "deepseek-v4-flash-vision-exp"},
		{"custom", "openai", "https://api.deepseek.com", "deepseek-v4-pro"},
	} {
		t.Run(tt.name+"/"+tt.kind+"/"+tt.endpoint+"/"+tt.model, func(t *testing.T) {
			entry := &config.ProviderEntry{Name: tt.name, Kind: tt.kind, BaseURL: tt.endpoint, Model: tt.model, ContextWindow: 64000}
			path := filepath.Join(t.TempDir(), "metadata.json")
			store := Load(path)
			if got := Resolve(entry, store); got.Vision != CapabilityUnknown || got.ToolUse != CapabilityUnknown || got.StructuredOutput != CapabilityUnknown || got.ContextWindow != 64000 || got.ContextConfirmed || got.PricingAvailable {
				t.Fatalf("custom provider inherited official defaults: %+v", got)
			}
			// Even rates matching an old official tariff belong to this custom provider.
			cachedPrice := &provider.Pricing{CacheHit: 0.05, Input: 1.5, Output: 4.5, Currency: "¥"}
			cached := MetadataFromDiscovery(entry, 128000, "context_length", CapabilityUnsupported, CapabilityUnsupported, CapabilityUnsupported, cachedPrice)
			if err := store.Put(cached); err != nil {
				t.Fatal(err)
			}
			got := Resolve(entry, Load(path))
			if got.Vision != CapabilityUnsupported || got.ToolUse != CapabilityUnsupported || got.StructuredOutput != CapabilityUnsupported || got.ContextWindow != 128000 || got.MetadataSource != SourceProviderMetadata || !reflect.DeepEqual(got.Pricing, cachedPrice) {
				t.Fatalf("custom cached metadata changed: %+v", got)
			}
			entry.Price = &provider.Pricing{Input: 7, Output: 11, Currency: "$"}
			if got := Resolve(entry, store); got.Pricing != entry.Price || got.Currency != "$" {
				t.Fatalf("custom configured pricing changed: %+v", got)
			}
		})
	}
}
