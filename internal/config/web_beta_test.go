package config

import "testing"

func TestWebBetaConfiguredUsesNoReadinessEnvironment(t *testing.T) {
	for _, ready := range []string{"", "0", "1", "other-process"} {
		t.Setenv("ORCA_WEB_BETA_READY", ready)
		web := ProviderEntry{Kind: "deepseekweb-beta"}
		api := ProviderEntry{Kind: "deepseek"}
		if !web.Configured() || api.Configured() {
			t.Fatalf("catalog eligibility depends on readiness or bypasses API auth: %q", ready)
		}
	}
}
