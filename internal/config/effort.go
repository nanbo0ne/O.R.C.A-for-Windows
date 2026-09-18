package config

import (
	"fmt"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/openai"
)

const (
	ReasoningProtocolAuto     = "auto"
	ReasoningProtocolDeepSeek = "deepseek"
	ReasoningProtocolOpenAI   = "openai"
	ReasoningProtocolNone     = "none"
)

// EffortCapability describes the abstract effort levels a provider/model can set
// through the /effort command.
type EffortCapability struct {
	Supported bool
	Levels    []string
	Default   string
}

type modelReasoningCapability struct {
	Protocol string
	Levels   []string
	Default  string
}

var modelReasoningCapabilities = map[string]modelReasoningCapability{
	"deepseek-flash":               {Protocol: ReasoningProtocolDeepSeek, Levels: []string{"low", "high", "max"}, Default: "high"},
	"deepseek-v4-flash":            {Protocol: ReasoningProtocolDeepSeek, Levels: []string{"low", "high", "max"}, Default: "high"},
	"deepseek-v4-pro":              {Protocol: ReasoningProtocolDeepSeek, Levels: []string{"low", "high", "max"}, Default: "high"},
	"deepseek-v4-flash-vision-exp": {Protocol: ReasoningProtocolDeepSeek, Levels: []string{"low", "high", "max"}, Default: "high"},
}

// IsOfficialDeepSeekEntry reports whether an entry uses DeepSeek's official API.
// Model capability and billing facts must never leak to similarly named relay models.
func IsOfficialDeepSeekEntry(e *ProviderEntry) bool { return isDeepSeekEntry(e) }

// EffortCapabilityForEntry returns the user-facing /effort levels for a resolved
// provider entry. Provider implementations still decide how a stored effort is
// serialized into requests.
func EffortCapabilityForEntry(e *ProviderEntry) EffortCapability {
	if explicitReasoningProtocol(e) == ReasoningProtocolNone {
		return EffortCapability{}
	}
	supported := normalizedSupportedEfforts(e)
	if len(supported) > 0 {
		levels := make([]string, 0, len(supported)+1)
		levels = append(levels, "auto")
		levels = append(levels, supported...)
		def := defaultSupportedEffort(e, supported)
		if def == "" {
			def = "auto"
		}
		return EffortCapability{Supported: true, Levels: levels, Default: def}
	}
	switch explicitReasoningProtocol(e) {
	case ReasoningProtocolDeepSeek:
		return deepSeekEffortCapability()
	case ReasoningProtocolOpenAI:
		return openAIEffortCapability()
	}
	if cap, ok := resolvedModelReasoningCapability(e); ok {
		return effortCapabilityFromModel(cap)
	}
	switch ReasoningProtocolForEntry(e) {
	case ReasoningProtocolDeepSeek:
		return deepSeekEffortCapability()
	case ReasoningProtocolOpenAI:
		return openAIEffortCapability()
	}
	switch {
	case isMiniMaxEntry(e):
		// MiniMax-M3 only exposes a binary thinking knob (adaptive|disabled)
		// on its OpenAI-compatible endpoint, so /effort mirrors the API
		// vocabulary verbatim. Default is "adaptive" because the M3 model
		// runs with thinking on out of the box; "auto" means "don't override
		// the model default" (== adaptive for M3).
		return EffortCapability{Supported: true, Levels: []string{"auto", "adaptive", "disabled"}, Default: "adaptive"}
	case e != nil && e.Kind == "anthropic":
		return EffortCapability{Supported: true, Levels: []string{"auto", "low", "medium", "high", "xhigh", "max"}, Default: "auto"}
	default:
		return EffortCapability{}
	}
}

// NormalizeEffort maps a user-supplied /effort level into the value stored in
// config. Empty means auto/provider default.
func NormalizeEffort(e *ProviderEntry, raw string) (string, error) {
	level := normalizeEffortLevel(raw)
	if level == "" {
		return "", fmt.Errorf("usage: /effort auto|<level>")
	}
	if level == "auto" {
		return "", nil
	}
	if explicitReasoningProtocol(e) == ReasoningProtocolNone {
		return "", effortNotConfigurableError(e)
	}
	if ReasoningProtocolForEntry(e) == ReasoningProtocolDeepSeek && level != "off" {
		var err error
		level, err = openai.NormalizeDeepSeekEffort(level)
		if err != nil {
			return "", fmt.Errorf("usage: /effort auto|low|high|max")
		}
	}
	supported := normalizedSupportedEfforts(e)
	if len(supported) > 0 {
		if containsString(supported, level) {
			return level, nil
		}
		return "", fmt.Errorf("usage: /effort auto|%s", strings.Join(supported, "|"))
	}
	switch ReasoningProtocolForEntry(e) {
	case ReasoningProtocolDeepSeek:
		switch level {
		case "low", "high", "max":
			return level, nil
		default:
			return "", fmt.Errorf("usage: /effort auto|low|high|max")
		}
	case ReasoningProtocolOpenAI:
		switch level {
		case "low", "medium", "high":
			return level, nil
		default:
			return "", fmt.Errorf("usage: /effort auto|low|medium|high")
		}
	}
	switch {
	case isMiniMaxEntry(e):
		// The M3 knob is binary; map Anthropic / OpenAI-style levels onto the
		// nearest valid value so a stale /effort high|low still works. "off"
		// is a retired DeepSeek level meaning "no thinking" — on M3 that maps
		// to "disabled" rather than the model default, since M3 actually
		// supports a "thinking off" mode and "off" is the natural request.
		switch level {
		case "adaptive", "disabled":
			return level, nil
		case "off":
			return "disabled", nil
		case "low", "medium", "high":
			return "adaptive", nil
		case "xhigh", "max":
			return "disabled", nil
		default:
			return "", fmt.Errorf("usage: /effort auto|adaptive|disabled")
		}
	case e != nil && e.Kind == "anthropic":
		switch level {
		case "low", "medium", "high", "xhigh", "max":
			return level, nil
		default:
			return "", fmt.Errorf("usage: /effort auto|low|medium|high|xhigh|max")
		}
	default:
		return "", effortNotConfigurableError(e)
	}
}

// EffortDisplay returns the selected /effort level, using "auto" for provider
// default.
func EffortDisplay(e *ProviderEntry) string {
	if e == nil || strings.TrimSpace(e.Effort) == "" {
		return "auto"
	}
	return normalizeEffortLevel(e.Effort)
}

// EffectiveEffort resolves the provider-visible effort value. Explicit
// ProviderEntry.Effort wins; otherwise a configured SupportedEfforts list makes
// a valid DefaultEffort the runtime default. Empty means
// provider default / omit the provider-specific effort field.
func EffectiveEffort(e *ProviderEntry) string {
	if e == nil || explicitReasoningProtocol(e) == ReasoningProtocolNone {
		return ""
	}
	if effort := normalizeStoredEffort(e.Effort); effort != "" {
		if ReasoningProtocolForEntry(e) == ReasoningProtocolDeepSeek {
			if normalized, err := openai.NormalizeDeepSeekEffort(effort); err == nil {
				return normalized
			}
		}
		return effort
	}
	supported := normalizedSupportedEfforts(e)
	if len(supported) == 0 {
		if ReasoningProtocolForEntry(e) == ReasoningProtocolDeepSeek {
			return "high"
		}
		if cap, ok := resolvedModelReasoningCapability(e); ok {
			def := normalizeEffortLevel(cap.Default)
			if def != "" && def != "auto" && containsString(cap.Levels, def) {
				return def
			}
			if len(cap.Levels) > 0 {
				return cap.Levels[0]
			}
		}
		return ""
	}
	return defaultSupportedEffort(e, supported)
}

func defaultSupportedEffort(e *ProviderEntry, supported []string) string {
	def := normalizeEffortLevel(e.DefaultEffort)
	if ReasoningProtocolForEntry(e) == ReasoningProtocolDeepSeek {
		if normalized, err := openai.NormalizeDeepSeekEffort(def); err == nil {
			def = normalized
		}
		if !containsString(supported, def) && containsString(supported, "high") {
			def = "high"
		}
	}
	if !containsString(supported, def) {
		if ReasoningProtocolForEntry(e) == ReasoningProtocolDeepSeek || isMiniMaxEntry(e) {
			return supported[0]
		}
		return ""
	}
	return def
}

func normalizeEffortConfig(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		normalizeProviderEffortFields(&c.Providers[i])
	}
}

func normalizeProviderEffortFields(e *ProviderEntry) {
	if e == nil {
		return
	}
	e.Effort = normalizeStoredEffort(e.Effort)
	e.ReasoningProtocol = normalizeReasoningProtocol(e.ReasoningProtocol)
	e.DefaultEffort = normalizeEffortLevel(e.DefaultEffort)
	e.SupportedEfforts = normalizedSupportedEfforts(e)
	if ReasoningProtocolForEntry(e) == ReasoningProtocolDeepSeek {
		if effort, err := openai.NormalizeDeepSeekEffort(e.Effort); err == nil {
			e.Effort = effort
		}
	}
}

func normalizeStoredEffort(raw string) string {
	level := normalizeEffortLevel(raw)
	if level == "auto" || level == "off" {
		return ""
	}
	return level
}

// ReasoningProtocolForEntry resolves the provider request shape for reasoning
// controls. Explicit config wins, then the model capability registry, then legacy
// endpoint heuristics.
func ReasoningProtocolForEntry(e *ProviderEntry) string {
	if explicit := explicitReasoningProtocol(e); explicit != "" {
		return explicit
	}
	if cap, ok := resolvedModelReasoningCapability(e); ok {
		return cap.Protocol
	}
	if isDeepSeekEntry(e) {
		return ReasoningProtocolDeepSeek
	}
	return ""
}

func explicitReasoningProtocol(e *ProviderEntry) string {
	if e == nil {
		return ""
	}
	protocol := normalizeReasoningProtocol(e.ReasoningProtocol)
	if protocol == ReasoningProtocolAuto {
		return ""
	}
	return protocol
}

func normalizeReasoningProtocol(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", ReasoningProtocolAuto:
		return ""
	case ReasoningProtocolDeepSeek, ReasoningProtocolOpenAI, ReasoningProtocolNone:
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

// isDeepSeekEntry reports whether the entry points at DeepSeek's API. The
// actual host matching lives in provider/openai so the openai package and
// the config layer stay in lockstep when new gateways are added.
func isDeepSeekEntry(e *ProviderEntry) bool {
	return e != nil && e.Kind == "openai" && openai.IsDeepSeek(e.BaseURL)
}

// isMiniMaxEntry reports whether the entry points at MiniMax's OpenAI-compatible
// endpoint. See openai.IsMiniMax for the host-matching rule; the entry-wrapper
// just gates on the openai kind.
func isMiniMaxEntry(e *ProviderEntry) bool {
	return e != nil && e.Kind == "openai" && openai.IsMiniMax(e.BaseURL)
}

func resolvedModelReasoningCapability(e *ProviderEntry) (modelReasoningCapability, bool) {
	if e == nil || e.Kind != "openai" {
		return modelReasoningCapability{}, false
	}
	cap, ok := modelReasoningCapabilities[strings.ToLower(strings.TrimSpace(e.Model))]
	return cap, ok
}

func effortCapabilityFromModel(cap modelReasoningCapability) EffortCapability {
	levels := make([]string, 0, len(cap.Levels)+1)
	levels = append(levels, "auto")
	levels = append(levels, cap.Levels...)
	def := normalizeEffortLevel(cap.Default)
	if def == "" || !containsString(cap.Levels, def) {
		def = "auto"
	}
	return EffortCapability{Supported: true, Levels: levels, Default: def}
}

func deepSeekEffortCapability() EffortCapability {
	return EffortCapability{Supported: true, Levels: []string{"auto", "low", "high", "max"}, Default: "high"}
}

func openAIEffortCapability() EffortCapability {
	return EffortCapability{Supported: true, Levels: []string{"auto", "low", "medium", "high"}, Default: "auto"}
}

func effortNotConfigurableError(e *ProviderEntry) error {
	name := ""
	if e != nil {
		name = e.Name
	}
	if name == "" {
		name = "this model"
	}
	return fmt.Errorf("effort is not configurable for %s", name)
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func normalizeEffortLevel(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func normalizedSupportedEfforts(e *ProviderEntry) []string {
	if e == nil || len(e.SupportedEfforts) == 0 {
		return nil
	}
	out := make([]string, 0, len(e.SupportedEfforts))
	seen := map[string]bool{}
	for _, raw := range e.SupportedEfforts {
		level := normalizeEffortLevel(raw)
		if ReasoningProtocolForEntry(e) == ReasoningProtocolDeepSeek {
			if normalized, err := openai.NormalizeDeepSeekEffort(level); err == nil {
				level = normalized
			}
		}
		if level == "" || level == "auto" || seen[level] {
			continue
		}
		seen[level] = true
		out = append(out, level)
	}
	return out
}
