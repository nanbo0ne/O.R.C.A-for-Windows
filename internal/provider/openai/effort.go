package openai

import (
	"fmt"
	"strings"
)

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
