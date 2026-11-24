package main

import (
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func accountReasoningDeclared(m orcaaccount.Model) bool {
	return m.Protocol == "openai" && m.Capabilities != nil && m.Capabilities.ReasoningStatus != ""
}

func applyAccountReasoning(entry *config.ProviderEntry, m orcaaccount.Model) {
	if !accountReasoningDeclared(m) {
		return
	}
	entry.ReasoningProtocol = config.ReasoningProtocolNone
	entry.SupportedEfforts, entry.DefaultEffort = nil, ""
	c := m.Capabilities
	if orcaaccount.ValidReasoningCapabilities(c) && c.ReasoningStatus == "confirmed" && len(c.ReasoningEfforts) > 0 {
		entry.ReasoningProtocol = config.ReasoningProtocolOpenAI
		entry.SupportedEfforts = slices.Clone(c.ReasoningEfforts)
		entry.DefaultEffort = c.DefaultEffort
	}
}

// Recheck the current catalog before credentials or a paid request are created.
// A stale controller cannot reinstate capability metadata removed by the server.
func accountReasoningConfig(cfg provider.Config, m orcaaccount.Model, disabled bool) (provider.Config, error) {
	if !accountReasoningDeclared(m) {
		return cfg, nil
	}
	c := m.Capabilities
	if !orcaaccount.ValidReasoningCapabilities(c) {
		return cfg, orcaaccount.ErrProtocol
	}
	selection, explicit := cfg.Extra["effort_selection"].(string)
	if !explicit {
		selection, _ = cfg.Extra["effort"].(string)
	}
	selection = strings.ToLower(strings.TrimSpace(selection))
	if selection == "auto" {
		selection = ""
	}
	if !disabled && selection != "" && !slices.Contains(c.ReasoningEfforts, selection) {
		return cfg, errors.New("this reasoning level is no longer supported by the ORCA account model; choose Auto or a currently supported level")
	}
	cfg.Extra = maps.Clone(cfg.Extra)
	if cfg.Extra == nil {
		cfg.Extra = map[string]any{}
	}
	cfg.Extra["reasoning_protocol"] = "none"
	cfg.Extra["supported_efforts"] = slices.Clone(c.ReasoningEfforts)
	cfg.Extra["effort"] = ""
	if !disabled && c.ReasoningStatus == "confirmed" && len(c.ReasoningEfforts) > 0 {
		cfg.Extra["reasoning_protocol"] = "openai"
		if selection == "" {
			selection = c.DefaultEffort
		}
		cfg.Extra["effort"] = selection
	}
	return cfg, nil
}
