package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type OnboardingState struct {
	Required        bool           `json:"required"`
	Completed       bool           `json:"completed"`
	HasCloudModel   bool           `json:"hasCloudModel"`
	HasLocalRuntime bool           `json:"hasLocalRuntime"`
	Platform        string         `json:"platform"`
	Providers       []ProviderView `json:"providers"`
}

func (a *App) GetOnboardingState() OnboardingState {
	cfg, err := config.Load()
	if err != nil {
		return OnboardingState{Required: true, Platform: a.Platform(), Providers: officialProviderViews(map[string]bool{})}
	}
	hasCloud := false
	for i := range cfg.Providers {
		if cfg.Providers[i].Configured() {
			hasCloud = true
			break
		}
	}
	hasLocal := cfg.LocalAI.Enabled && a.localRuntimeInstalled()
	completed := cfg.Desktop.OnboardingCompleted
	return OnboardingState{
		Required:        !completed && !hasCloud && !hasLocal,
		Completed:       completed,
		HasCloudModel:   hasCloud,
		HasLocalRuntime: hasLocal,
		Platform:        a.Platform(),
		Providers:       officialProviderViews(officialProviderAddedSet(cfg)),
	}
}

func (a *App) NeedsOnboarding() bool { return a.GetOnboardingState().Required }

func (a *App) CompleteOnboarding() error {
	return a.applyConfigOnly(func(c *config.Config) error {
		c.Desktop.OnboardingCompleted = true
		c.ConfigVersion = 11
		return nil
	})
}

// ConnectProviderPreset validates a curated provider without creating a chat
// session, persists its isolated credential, and makes its models available.
func (a *App) ConnectProviderPreset(presetID, apiKey string) ([]string, error) {
	preset, ok := config.ProviderPresetByID(presetID)
	if !ok {
		return nil, fmt.Errorf("unknown provider preset %q", presetID)
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	old, existed := os.LookupEnv(preset.Entry.APIKeyEnv)
	if err := os.Setenv(preset.Entry.APIKeyEnv, apiKey); err != nil {
		return nil, err
	}
	defer func() {
		if existed {
			_ = os.Setenv(preset.Entry.APIKeyEnv, old)
		} else {
			_ = os.Unsetenv(preset.Entry.APIKeyEnv)
		}
	}()

	ctx, cancel := context.WithTimeout(a.reqCtx(), 20*time.Second)
	defer cancel()
	models, fetchErr := preset.Entry.FetchModels(ctx)
	models = chatProviderModels(models)
	models, err := validateProviderPresetModels(ctx, preset.Entry, apiKey, models, fetchErr)
	if err != nil {
		return nil, err
	}
	models = uniqueStrings(append(preset.Entry.ChatModelList(), models...))

	if err := upsertDotEnv(preset.Entry.APIKeyEnv, apiKey); err != nil {
		return nil, fmt.Errorf("save credential: %w", err)
	}
	err = a.applyConfigChange(func(c *config.Config) error {
		entry := preset.Entry
		entry.Model = ""
		entry.Models = append([]string(nil), models...)
		entry.Default = providerDefaultForModels(preset.Entry.DefaultModel(), models)
		if len(models) == 1 {
			entry.Model = models[0]
			entry.Models = nil
			entry.Default = ""
		}
		if err := c.UpsertProvider(entry); err != nil {
			return err
		}
		addProviderAccess(c, entry.Name)
		if strings.TrimSpace(c.DefaultModel) == "" {
			c.DefaultModel = entry.Name + "/" + entry.DefaultModel()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return nonNil(models), nil
}

func validateProviderPresetModels(ctx context.Context, entry config.ProviderEntry, apiKey string, models []string, fetchErr error) ([]string, error) {
	if fetchErr == nil && len(models) != 0 {
		return models, nil
	}
	if betaEnabled() {
		if fetchErr != nil {
			return nil, fmt.Errorf("provider model catalog unavailable; no inference probe was sent: %w", fetchErr)
		}
		return nil, fmt.Errorf("provider model catalog is empty; no inference probe was sent")
	}
	if err := probeProviderCredential(ctx, entry, apiKey); err != nil {
		if fetchErr != nil {
			return nil, fmt.Errorf("validate provider: %w", fetchErr)
		}
		return nil, fmt.Errorf("validate provider: %w", err)
	}
	return append([]string(nil), entry.ChatModelList()...), nil
}

func probeProviderCredential(ctx context.Context, entry config.ProviderEntry, apiKey string) error {
	if err := automaticAuxiliaryAllowed(&entry); err != nil {
		return err
	}
	p, err := provider.New(entry.Kind, provider.Config{
		Name: entry.Name, BaseURL: entry.BaseURL, Model: entry.DefaultModel(), APIKey: apiKey,
		Extra: map[string]any{"api_key_env": entry.APIKeyEnv, "reasoning_protocol": entry.ReasoningProtocol},
	})
	if err != nil {
		return err
	}
	stream, err := p.Stream(ctx, bindAuxiliaryRequest(ctx, auxiliaryProbe, provider.Request{
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "Reply with OK."}},
		MaxTokens: 8,
	}))
	if err != nil {
		return err
	}
	_, err = collectAuxiliaryText(ctx, stream)
	return err
}

// ConnectKey remains as a V2 binding compatibility shim.
func (a *App) ConnectKey(apiKey string) error {
	_, err := a.ConnectProviderPreset("deepseek", apiKey)
	return err
}
