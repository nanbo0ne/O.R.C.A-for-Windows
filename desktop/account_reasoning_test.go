package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/openai"
)

func TestAccountReasoningCurrentCatalogControlsWire(t *testing.T) {
	for _, tc := range []struct {
		name, status, selection, oldDefault, currentDefault, want string
		levels                                                    []string
		disabled, reject                                          bool
	}{
		{name: "auto-current-default", status: "confirmed", oldDefault: "high", currentDefault: "xhigh", levels: []string{"none", "xhigh"}, want: "xhigh"},
		{name: "explicit-xhigh", status: "confirmed", selection: "xhigh", levels: []string{"none", "xhigh"}, want: "xhigh"},
		{name: "explicit-none", status: "confirmed", selection: "none", levels: []string{"none", "high"}, want: "none"},
		{name: "removed-level", status: "confirmed", selection: "high", levels: []string{"none", "xhigh"}, reject: true},
		{name: "unknown-auto", status: "unknown"},
		{name: "unknown-explicit", status: "unknown", selection: "high", reject: true},
		{name: "auxiliary-disables-stale-setting", status: "confirmed", selection: "high", levels: []string{"xhigh"}, disabled: true},
		{name: "confirmed-empty", status: "confirmed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &orcaaccount.ModelCapabilities{ReasoningStatus: tc.status, ReasoningSource: "declared", ReasoningEfforts: tc.levels, DefaultEffort: tc.currentDefault}
			if tc.status == "unknown" {
				c.ReasoningSource = "unknown"
			}
			m := orcaaccount.Model{ID: "Synthetic/Exact-Case", Protocol: "openai", Endpoint: "/chat/completions", Capabilities: c}
			original := provider.Config{Name: "synthetic", Model: m.ID, Extra: map[string]any{"effort_selection": tc.selection, "effort": tc.oldDefault, "reasoning_protocol": "deepseek", "supported_efforts": []string{"high"}}}
			cfg, err := accountReasoningConfig(original, m, tc.disabled)
			if tc.reject {
				if err == nil {
					t.Fatal("unavailable effort accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if original.Extra["reasoning_protocol"] != "deepseek" || original.Extra["effort"] != tc.oldDefault {
				t.Fatal("mutated controller config")
			}
			wire := make(chan map[string]any, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				wire <- body
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			}))
			defer srv.Close()
			cfg.BaseURL, cfg.HTTPClient = srv.URL, srv.Client()
			p, err := openai.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			stream, err := p.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}, DisableThinking: tc.disabled})
			if err != nil {
				t.Fatal(err)
			}
			for chunk := range stream {
				if chunk.Type == provider.ChunkError {
					t.Fatal(chunk.Err)
				}
			}
			select {
			case body := <-wire:
				if body["model"] != m.ID {
					t.Fatal("model identity changed")
				}
				got, exists := body["reasoning_effort"]
				if tc.want == "" && exists || tc.want != "" && got != tc.want {
					t.Fatalf("effort: got %v, want %q", got, tc.want)
				}
				if _, exists := body["thinking"]; exists {
					t.Fatal("inferred DeepSeek protocol from stale controller")
				}
			case <-ctx.Done():
				t.Fatal("no actual request observed")
			}
		})
	}
}

func TestAccountReasoningMenuUsesConfirmedLevels(t *testing.T) {
	m := orcaaccount.Model{Protocol: "openai", Capabilities: &orcaaccount.ModelCapabilities{ReasoningStatus: "confirmed", ReasoningSource: "manual", ReasoningEfforts: []string{"none", "low", "xhigh"}, DefaultEffort: "low"}}
	p := accountProviderEntry("synthetic", m)
	got := config.EffortCapabilityForEntry(&p)
	if !got.Supported || !reflect.DeepEqual(got.Levels, []string{"auto", "none", "low", "xhigh"}) || got.Default != "low" {
		t.Fatal(got)
	}
	p.Effort = "xhigh"
	m.Capabilities.ReasoningStatus, m.Capabilities.ReasoningSource = "unknown", "unknown"
	m.Capabilities.ReasoningEfforts, m.Capabilities.DefaultEffort = nil, ""
	applyAccountReasoning(&p, m)
	if config.EffortCapabilityForEntry(&p).Supported || p.Effort != "xhigh" {
		t.Fatal("unknown enabled guesses or silently erased preference")
	}
}

func TestAccountReasoningRejectsBeforeCredentials(t *testing.T) {
	f := newAccountStreamFixture(t, "openai")
	p := f.registeredProvider().(*accountProvider)
	p.cfg.Extra["effort_selection"] = "high"
	s := f.app.account
	s.mu.Lock()
	m := s.catalog[p.Name()]
	m.Capabilities = &orcaaccount.ModelCapabilities{ReasoningStatus: "confirmed", ReasoningSource: "declared", ReasoningEfforts: []string{"xhigh"}}
	s.catalog[p.Name()] = m
	s.mu.Unlock()
	if _, err := p.Stream(context.Background(), integrationRequest()); err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatal(err)
	}
	if f.credentials.Load() != 0 || f.inferences.Load() != 0 {
		t.Fatal("unsupported choice acquired credentials or charged a request")
	}
}

func TestAccountReasoningRefreshUpdatesCapabilitiesNotPreference(t *testing.T) {
	f := newAccountStreamFixture(t, "openai")
	p := f.registeredProvider()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	old, ok := cfg.Provider(p.Name())
	if !ok {
		t.Fatal("missing fixture provider")
	}
	old.Effort, old.DefaultEffort = "high", "low"
	old.ReasoningProtocol, old.SupportedEfforts = "openai", []string{"low", "high"}
	old.Thinking = "disabled"
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"confirmed", "unknown"} {
		f.setCatalogHook(func(r *http.Request) (*http.Response, error) {
			c := &orcaaccount.ModelCapabilities{ReasoningStatus: status, ReasoningSource: "unknown"}
			if status == "confirmed" {
				c.ReasoningSource, c.DefaultEffort = "declared", "xhigh"
				c.ReasoningEfforts = []string{"none", "xhigh"}
			}
			return integrationJSON(r, orcaaccount.Models{Currency: "CNY", Models: []orcaaccount.Model{{ID: integrationModel, GroupID: 7, Protocol: "openai", Endpoint: "/chat/completions", Capabilities: c}}})
		})
		expireAccountCatalog(f.app.account)
		if _, _, err := f.app.account.readyAccountModel(context.Background(), p.Name(), integrationModel); err != nil {
			t.Fatal(err)
		}
		after := config.LoadForEdit(path)
		entry, ok := after.Provider(p.Name())
		if !ok || entry.Effort != "high" || entry.Thinking != "disabled" {
			t.Fatal("refresh erased explicit preference")
		}
		if status == "confirmed" {
			if entry.DefaultEffort != "xhigh" || entry.ReasoningProtocol != "openai" || !reflect.DeepEqual(entry.SupportedEfforts, []string{"none", "xhigh"}) {
				t.Fatal("old capability list overrode current catalog")
			}
		} else if entry.ReasoningProtocol != "none" || entry.DefaultEffort != "" || len(entry.SupportedEfforts) != 0 {
			t.Fatal("unknown catalog retained stale capability guesses")
		}
	}
	if f.credentials.Load() != 0 || f.inferences.Load() != 0 {
		t.Fatal("catalog refresh created a chargeable request")
	}
}
