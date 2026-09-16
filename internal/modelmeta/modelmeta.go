// Package modelmeta persists model-list metadata that is useful across app
// restarts but does not belong in the user's provider configuration.
package modelmeta

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/billing"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/product"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

const (
	CapabilitySupported   = "supported"
	CapabilityUnsupported = "unsupported"
	CapabilityUnknown     = "unknown"

	SourceProviderMetadata = "provider_metadata"
	SourceUserOverride     = "user_model_override"
	SourceProviderDefault  = "provider_default"
	SourceLocalRuntime     = "local_runtime"
	SourceDeepSeekOfficial = "deepseek_official"
)

type Metadata struct {
	ModelRef         string            `json:"modelRef"`
	Key              string            `json:"key"`
	ContextWindow    int               `json:"contextWindow,omitempty"`
	ContextConfirmed bool              `json:"contextConfirmed,omitempty"`
	ContextSource    string            `json:"contextSource,omitempty"`
	Vision           string            `json:"vision,omitempty"`
	ToolUse          string            `json:"toolUse,omitempty"`
	StructuredOutput string            `json:"structuredOutput,omitempty"`
	Pricing          *provider.Pricing `json:"pricing,omitempty"`
	PricingSource    string            `json:"pricingSource,omitempty"`
	MetadataSource   string            `json:"metadataSource,omitempty"`
	CheckedAt        int64             `json:"checkedAt,omitempty"`
}

type Resolved struct {
	ModelRef         string
	ContextWindow    int
	ContextConfirmed bool
	ContextSource    string
	Vision           string
	ToolUse          string
	StructuredOutput string
	Pricing          *provider.Pricing
	PricingAvailable bool
	Currency         string
	MetadataSource   string
}

type Store struct {
	mu    sync.Mutex
	path  string
	Items map[string]Metadata `json:"items"`
}

var storeFileMu sync.Mutex

func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "."+product.ConfigDirName)
	} else {
		dir = filepath.Join(dir, product.ConfigDirName)
	}
	return filepath.Join(dir, "model-metadata.json")
}

func Load(path string) *Store {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	store := &Store{path: path, Items: map[string]Metadata{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, store)
		store.path = path
	}
	if store.Items == nil {
		store.Items = map[string]Metadata{}
	}
	return store
}

func Key(entry *config.ProviderEntry) string {
	if entry == nil {
		return ""
	}
	base := strings.TrimRight(strings.ToLower(strings.TrimSpace(entry.BaseURL)), "/")
	return strings.ToLower(strings.TrimSpace(entry.Kind)) + "|" + base + "|" + strings.TrimSpace(entry.Model)
}

func ModelRef(entry *config.ProviderEntry) string {
	if entry == nil {
		return ""
	}
	return strings.TrimSpace(entry.Name) + "/" + strings.TrimSpace(entry.Model)
}

func (s *Store) Get(entry *config.ProviderEntry) Metadata {
	key := Key(entry)
	s.mu.Lock()
	defer s.mu.Unlock()
	if item, ok := s.Items[key]; ok {
		item.Key = key
		item.ModelRef = ModelRef(entry)
		return item
	}
	return Metadata{Key: key, ModelRef: ModelRef(entry), Vision: CapabilityUnknown, ToolUse: CapabilityUnknown, StructuredOutput: CapabilityUnknown}
}

// Put merges non-empty fields with the latest disk state. A sparse /models
// response therefore cannot erase previously confirmed metadata.
func (s *Store) Put(item Metadata) error {
	storeFileMu.Lock()
	defer storeFileMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Items == nil {
		s.Items = map[string]Metadata{}
	}
	if data, err := os.ReadFile(s.path); err == nil {
		var disk struct {
			Items map[string]Metadata `json:"items"`
		}
		if json.Unmarshal(data, &disk) == nil {
			for key, value := range disk.Items {
				s.Items[key] = value
			}
		}
	}
	item = merge(s.Items[item.Key], item)
	s.Items[item.Key] = item
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Items map[string]Metadata `json:"items"`
	}{s.Items}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func merge(current, next Metadata) Metadata {
	if next.ModelRef == "" {
		next.ModelRef = current.ModelRef
	}
	if next.ContextWindow <= 0 {
		next.ContextWindow = current.ContextWindow
		next.ContextConfirmed = current.ContextConfirmed
		next.ContextSource = current.ContextSource
	}
	if next.Vision == "" || next.Vision == CapabilityUnknown {
		next.Vision = current.Vision
	}
	if next.ToolUse == "" || next.ToolUse == CapabilityUnknown {
		next.ToolUse = current.ToolUse
	}
	if next.StructuredOutput == "" || next.StructuredOutput == CapabilityUnknown {
		next.StructuredOutput = current.StructuredOutput
	}
	if next.Pricing == nil {
		next.Pricing = current.Pricing
		next.PricingSource = current.PricingSource
	}
	if next.MetadataSource == "" {
		next.MetadataSource = current.MetadataSource
	}
	if next.CheckedAt == 0 {
		next.CheckedAt = current.CheckedAt
	}
	return next
}

func Resolve(entry *config.ProviderEntry, store *Store) Resolved {
	if entry == nil {
		return Resolved{}
	}
	resolved := Resolved{ModelRef: ModelRef(entry), Vision: CapabilityUnknown, ToolUse: CapabilityUnknown, StructuredOutput: CapabilityUnknown}
	if store == nil {
		store = Load("")
	}
	stored := store.Get(entry)
	resolved.Vision = nonEmpty(stored.Vision, CapabilityUnknown)
	resolved.ToolUse = nonEmpty(stored.ToolUse, CapabilityUnknown)
	resolved.StructuredOutput = nonEmpty(stored.StructuredOutput, CapabilityUnknown)
	resolved.MetadataSource = stored.MetadataSource
	officialModel := ""
	if strings.EqualFold(strings.TrimSpace(entry.Kind), "openai") {
		officialModel = billing.OfficialDeepSeekModel(entry.Name, entry.BaseURL, entry.Model)
	}
	if officialModel != "" {
		resolved.Vision = CapabilitySupported
		if officialModel == "deepseek-v4-pro" {
			resolved.Vision = CapabilityUnsupported
		}
		resolved.ToolUse = CapabilitySupported
		resolved.StructuredOutput = CapabilitySupported
		resolved.MetadataSource = SourceDeepSeekOfficial
	}

	if strings.EqualFold(strings.TrimSpace(entry.Name), "orca-local") && entry.ContextWindow > 0 {
		resolved.ContextWindow = entry.ContextWindow
		resolved.ContextConfirmed = true
		resolved.ContextSource = SourceLocalRuntime
	} else if officialModel != "" {
		resolved.ContextWindow = 1_000_000
		resolved.ContextConfirmed = true
		resolved.ContextSource = SourceDeepSeekOfficial
		if override := officialContextOverride(entry, officialModel); override > 0 {
			resolved.ContextWindow = override
			resolved.ContextSource = SourceUserOverride
		}
	} else if stored.ContextWindow > 0 {
		resolved.ContextWindow = stored.ContextWindow
		resolved.ContextConfirmed = stored.ContextConfirmed
		resolved.ContextSource = nonEmpty(stored.ContextSource, SourceProviderMetadata)
	} else if entry.ModelContextWindows != nil && entry.ModelContextWindows[entry.Model] > 0 {
		resolved.ContextWindow = entry.ModelContextWindows[entry.Model]
		resolved.ContextConfirmed = true
		resolved.ContextSource = SourceUserOverride
	} else if entry.ContextWindow > 0 {
		resolved.ContextWindow = entry.ContextWindow
		resolved.ContextConfirmed = false
		resolved.ContextSource = SourceProviderDefault
	}

	resolved.Pricing = entry.Price
	if resolved.Pricing == nil && stored.Pricing != nil {
		resolved.Pricing = stored.Pricing
	}
	if officialModel != "" {
		// Refresh model metadata for future requests without mutating cached prices
		// or the snapshots already attached to usage records and in-flight requests.
		resolved.Pricing = billing.OfficialDeepSeekPricing(entry.Name, entry.BaseURL, entry.Model)
	}
	if resolved.Pricing != nil {
		resolved.PricingAvailable = true
		resolved.Currency = resolved.Pricing.Symbol()
	}
	return resolved
}

func officialContextOverride(entry *config.ProviderEntry, model string) int {
	models := []string{model, entry.Model}
	if model == "deepseek-flash" {
		models = append(models, "deepseek-v4-flash", "deepseek-v4-flash-vision-exp")
	}
	for _, id := range models {
		if window := entry.ModelContextWindows[id]; window > 0 {
			return window
		}
	}
	return 0
}

func MetadataFromDiscovery(entry *config.ProviderEntry, contextWindow int, contextSource string, vision, toolUse, structuredOutput string, pricing *provider.Pricing) Metadata {
	return Metadata{
		ModelRef: ModelRef(entry), Key: Key(entry), ContextWindow: contextWindow, ContextConfirmed: contextWindow > 0,
		ContextSource: contextSource, Vision: nonEmpty(vision, CapabilityUnknown), ToolUse: nonEmpty(toolUse, CapabilityUnknown),
		StructuredOutput: nonEmpty(structuredOutput, CapabilityUnknown), Pricing: pricing, PricingSource: SourceProviderMetadata,
		MetadataSource: SourceProviderMetadata, CheckedAt: time.Now().UnixMilli(),
	}
}

func Status(value *bool) string {
	if value == nil {
		return CapabilityUnknown
	}
	if *value {
		return CapabilitySupported
	}
	return CapabilityUnsupported
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
