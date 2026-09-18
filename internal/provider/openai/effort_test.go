package openai

import (
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func newClient(t *testing.T, baseURL, effort string) *client {
	t.Helper()
	extra := map[string]any{}
	if effort != "" {
		extra["effort"] = effort
	}
	p, err := New(provider.Config{Name: "p", BaseURL: baseURL, Model: "m", APIKey: "k", Extra: extra})
	if err != nil {
		t.Fatalf("New(%q, effort=%q): %v", baseURL, effort, err)
	}
	return p.(*client)
}

func TestEffortNormalization(t *testing.T) {
	const mimo = "https://api.xiaomimimo.com/v1"
	const deepseek = "https://api.deepseek.com/v1"

	tests := []struct {
		base, effort, want string
	}{
		{mimo, "max", "high"}, // DeepSeek-ism clamped to the OpenAI ceiling — MiMo 400s on "max"
		{mimo, "high", "high"},
		{mimo, "medium", "medium"},
		{mimo, "low", "low"},
		{mimo, "MAX", "high"}, // case-insensitive
		{mimo, "auto", ""},    // UI/config auto means omit provider-specific effort
		{mimo, "", ""},        // unset stays omitted
		{deepseek, "max", "max"},
		{deepseek, "high", "high"},
		{deepseek, "low", "low"},
		{deepseek, "minimal", "low"},
		{deepseek, "medium", "high"},
		{deepseek, "xhigh", "high"},
		{deepseek, "ultra", "max"},
		{deepseek, "auto", "high"},
		{deepseek, "off", "high"},
		{deepseek, "", "high"},
	}
	for _, tc := range tests {
		if got := newClient(t, tc.base, tc.effort).effort; got != tc.want {
			t.Errorf("base=%s effort=%q: got %q, want %q", tc.base, tc.effort, got, tc.want)
		}
	}
}

func TestEffortInvalidRejected(t *testing.T) {
	_, err := New(provider.Config{
		Name: "p", BaseURL: "https://api.xiaomimimo.com/v1", Model: "m", APIKey: "k",
		Extra: map[string]any{"effort": "turbo"},
	})
	if err == nil || !strings.Contains(err.Error(), "low, medium, or high") {
		t.Fatalf("expected a low/medium/high validation error, got: %v", err)
	}
}

func TestConfiguredEffortsRespectProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, base, protocol, effort, want string
		levels                             []string
		reject                             bool
	}{
		{name: "declared xhigh", effort: " XHIGH ", levels: []string{" XHIGH "}, want: "xhigh"},
		{name: "declared max", effort: "max", levels: []string{"max"}, want: "max"},
		{name: "custom value", effort: "turbo", levels: []string{"turbo"}, want: "turbo"},
		{name: "undeclared xhigh", effort: "xhigh", reject: true},
		{name: "empty declaration", effort: "xhigh", levels: []string{"", "auto", " "}, reject: true},
		{name: "restricted declaration", effort: "high", levels: []string{"xhigh"}, reject: true},
		{name: "auto omits", effort: "auto", levels: []string{"xhigh"}},
		{name: "none ignores declaration", protocol: "none", effort: "xhigh", levels: []string{"xhigh"}},
		{name: "official deepseek alias", base: "https://api.deepseek.com/v1", effort: "xhigh", levels: []string{"xhigh"}, want: "high"},
		{name: "official deepseek default", base: "https://api.deepseek.com/v1", effort: "auto", levels: []string{"xhigh"}, want: "high"},
		{name: "official deepseek rejects custom", base: "https://api.deepseek.com/v1", effort: "turbo", levels: []string{"turbo"}, reject: true},
		{name: "minimax disabled", base: "https://api.minimaxi.com/v1", effort: "disabled", levels: []string{"xhigh"}, want: "disabled"},
		{name: "minimax rejects xhigh", base: "https://api.minimaxi.com/v1", effort: "xhigh", levels: []string{"xhigh"}, reject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := tc.base
			if base == "" {
				base = "https://custom.example/v1"
			}
			p, err := New(provider.Config{Name: "custom", BaseURL: base, Model: "m", Extra: map[string]any{
				"effort": tc.effort, "supported_efforts": tc.levels, "reasoning_protocol": tc.protocol,
			}})
			if tc.reject {
				if err == nil {
					t.Fatal("expected effort validation error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := p.(*client).effort; got != tc.want {
				t.Fatalf("effort = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReasoningProtocolOverridesEndpointHeuristic(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "deepseek-proxy",
		BaseURL: "https://proxy.example.com/v1",
		Model:   "deepseek-v4-flash",
		APIKey:  "k",
		Extra:   map[string]any{"reasoning_protocol": "deepseek"},
	})
	if err != nil {
		t.Fatalf("New deepseek protocol: %v", err)
	}
	c := p.(*client)
	if !c.deepseek || c.effort != "high" {
		t.Fatalf("deepseek=%v effort=%q, want true/high", c.deepseek, c.effort)
	}

	p, err = New(provider.Config{
		Name:    "deepseek-direct",
		BaseURL: "https://api.deepseek.com/v1",
		Model:   "deepseek-v4-flash",
		APIKey:  "k",
		Extra:   map[string]any{"reasoning_protocol": "none", "effort": "max"},
	})
	if err != nil {
		t.Fatalf("New none protocol: %v", err)
	}
	c = p.(*client)
	if c.deepseek || c.effort != "" {
		t.Fatalf("deepseek=%v effort=%q, want false/empty", c.deepseek, c.effort)
	}
}
