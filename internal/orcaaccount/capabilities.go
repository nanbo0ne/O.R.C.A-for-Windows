package orcaaccount

import "slices"

// Empty status is the legacy catalog contract. Unknown is an explicit server
// statement and must not be replaced by guesses based on a model name.
func ValidReasoningCapabilities(c *ModelCapabilities) bool {
	if c == nil || c.ReasoningStatus == "" {
		return true
	}
	if c.ReasoningRevision < 0 || len(c.ReasoningEfforts) > 7 {
		return false
	}
	if c.ReasoningStatus == "unknown" {
		return len(c.ReasoningEfforts) == 0 && c.DefaultEffort == "" && c.ReasoningSource == "unknown"
	}
	if c.ReasoningStatus != "confirmed" || c.ReasoningSource != "declared" && c.ReasoningSource != "manual" {
		return false
	}
	seen := map[string]bool{}
	for _, effort := range c.ReasoningEfforts {
		if !slices.Contains([]string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}, effort) || seen[effort] {
			return false
		}
		seen[effort] = true
	}
	return c.DefaultEffort == "" || seen[c.DefaultEffort]
}
