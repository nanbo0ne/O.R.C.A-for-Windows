package config

import "testing"

func TestDefaultAutoPlanOff(t *testing.T) {
	if got := Default().Agent.AutoPlan; got != "off" {
		t.Fatalf("default auto_plan = %q, want off", got)
	}
}

func TestDefaultCloudProvidersAreDeepSeekOnly(t *testing.T) {
	c := Default()
	if c.DefaultModel != "deepseek/deepseek-flash" {
		t.Fatalf("default_model = %q, want deepseek/deepseek-flash", c.DefaultModel)
	}
	if c.Desktop.ShowReasoning != nil || c.DesktopProcessDisplayMode() != ProcessDisplayCompact {
		t.Fatalf("default show_reasoning = %+v, mode = %q; want hidden/compact", c.Desktop.ShowReasoning, c.DesktopProcessDisplayMode())
	}
	if len(c.Providers) != 3 {
		t.Fatalf("default providers = %d, want canonical DeepSeek and two compatibility identities", len(c.Providers))
	}
	for _, p := range c.Providers {
		if p.Name != "deepseek" && p.Name != "deepseek-flash" && p.Name != "deepseek-pro" {
			t.Fatalf("unexpected default cloud provider %q", p.Name)
		}
		if p.BaseURL != "https://api.deepseek.com" || p.APIKeyEnv != "DEEPSEEK_API_KEY" {
			t.Fatalf("default provider %q = %+v, want DeepSeek endpoint and key", p.Name, p)
		}
	}
}
