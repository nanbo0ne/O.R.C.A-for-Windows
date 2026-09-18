package boot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestNewProviderEffortWire(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, effort, def, want, thinking string
		levels                                      []string
		reject                                      bool
	}{
		{name: "declared xhigh", effort: "xhigh", levels: []string{"high", "xhigh"}, want: "xhigh"},
		{name: "explicit openai xhigh", protocol: "openai", effort: "xhigh", levels: []string{" XHIGH "}, want: "xhigh"},
		{name: "declared max unchanged", effort: "max", levels: []string{"high", "max"}, want: "max"},
		{name: "custom value unchanged", effort: "turbo", levels: []string{"turbo"}, want: "turbo"},
		{name: "undeclared xhigh", effort: "xhigh", reject: true},
		{name: "xhigh outside declaration", effort: "xhigh", levels: []string{"high"}, reject: true},
		{name: "standard outside declaration", effort: "low", levels: []string{"xhigh"}, reject: true},
		{name: "auto valid default", effort: "auto", def: " XHIGH ", levels: []string{"high", "xhigh"}, want: "xhigh"},
		{name: "unset valid default", def: "xhigh", levels: []string{"xhigh"}, want: "xhigh"},
		{name: "explicit overrides default", effort: "high", def: "xhigh", levels: []string{"high", "xhigh"}, want: "high"},
		{name: "auto no default", effort: "auto", levels: []string{"high", "xhigh"}},
		{name: "auto invalid default", effort: "auto", def: "low", levels: []string{"high", "xhigh"}},
		{name: "auto default auto", effort: "auto", def: "auto", levels: []string{"auto", "xhigh"}},
		{name: "none overrides effort", protocol: "none", effort: "xhigh", levels: []string{"xhigh"}},
		{name: "none overrides default", protocol: "none", def: "xhigh", levels: []string{"xhigh"}},
		{name: "deepseek alias", protocol: "deepseek", effort: "xhigh", levels: []string{"xhigh"}, want: "high", thinking: "enabled"},
		{name: "deepseek auto", protocol: "deepseek", effort: "auto", want: "high", thinking: "enabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan map[string]any, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode request: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				requests <- body
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
			}))
			defer srv.Close()
			e := &config.ProviderEntry{
				Name: "custom", Kind: "openai", BaseURL: srv.URL, Model: "m",
				ReasoningProtocol: tc.protocol, Effort: tc.effort,
				SupportedEfforts: tc.levels, DefaultEffort: tc.def,
			}
			p, err := NewProvider(e)
			if tc.reject {
				if err == nil {
					t.Fatal("NewProvider accepted undeclared effort")
				}
				if _, err := config.NormalizeEffort(e, tc.effort); err == nil {
					t.Fatal("NormalizeEffort accepted undeclared effort")
				}
				return
			}
			if err != nil {
				t.Fatalf("NewProvider: %v", err)
			}
			ch, err := p.Stream(context.Background(), provider.Request{
				Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
			})
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			for chunk := range ch {
				if chunk.Type == provider.ChunkError {
					t.Fatalf("stream: %v", chunk.Err)
				}
			}
			select {
			case body := <-requests:
				if got, exists := body["reasoning_effort"]; (tc.want == "" && exists) || (tc.want != "" && got != tc.want) {
					t.Errorf("reasoning_effort = %#v (present=%v), want %q", got, exists, tc.want)
				}
				if tc.thinking == "" {
					if _, exists := body["thinking"]; exists {
						t.Errorf("unexpected thinking: %#v", body["thinking"])
					}
				} else if thinking, ok := body["thinking"].(map[string]any); !ok || thinking["type"] != tc.thinking {
					t.Errorf("thinking = %#v, want %q", body["thinking"], tc.thinking)
				}
			default:
				t.Fatal("no HTTP request received")
			}
		})
	}
}
