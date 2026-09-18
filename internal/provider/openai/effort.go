package openai

import (
	"fmt"
	"strings"
)

// Explicit capabilities belong to the endpoint and must not be clamped to the
// legacy OpenAI scale. Empty/auto entries do not declare a wire-level value.
func normalizeOpenAIEffort(effort string, supported []string) (string, error) {
	var levels []string
	for _, raw := range supported {
		level := strings.ToLower(strings.TrimSpace(raw))
		if level == "" || level == "auto" {
			continue
		}
		if level == effort {
			return effort, nil
		}
		levels = append(levels, level)
	}
	if len(levels) > 0 {
		return "", fmt.Errorf("effort must be one of configured supported_efforts: %s", strings.Join(levels, ", "))
	}
	// Preserve compatibility for existing endpoints without explicit capabilities.
	switch effort {
	case "max":
		return "high", nil
	case "low", "medium", "high":
		return effort, nil
	default:
		return "", fmt.Errorf("effort must be low, medium, or high")
	}
}

// NormalizeDeepSeekEffort maps compatibility levels onto DeepSeek's API scale.
// Empty means the provider default (high), not disabled thinking.
func NormalizeDeepSeekEffort(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto", "off":
		return "", nil
	case "minimal", "low":
		return "low", nil
	case "medium", "high", "xhigh":
		return "high", nil
	case "ultra", "max":
		return "max", nil
	default:
		return "", fmt.Errorf("DeepSeek effort must be low, high, or max")
	}
}
