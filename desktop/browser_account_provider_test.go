package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func browserAccountFixture(t *testing.T, role, stored bool) (*accountStreamFixture, string) {
	t.Helper()
	f := newAccountStreamFixture(t, "openai")
	p := f.registeredProvider().(*accountProvider)
	ref := p.cfg.Name + "/" + p.cfg.Model
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Providers = []config.ProviderEntry{{Name: "synthetic", Kind: "openai", BaseURL: "https://synthetic.invalid/v1", Model: "other"}}
	cfg.DefaultModel = "synthetic/other"
	if stored {
		cfg.Providers = append(cfg.Providers, config.ProviderEntry{
			Name: p.cfg.Name, Kind: accountProviderKind, Model: p.cfg.Model, BaseURL: orcaaccount.Router + "/v1",
			Thinking: "adaptive", Effort: "high", ReasoningProtocol: "openai", SupportedEfforts: []string{"low", "high"}, DefaultEffort: "low",
			ContextWindow: 65536, ModelContextWindows: map[string]int{p.cfg.Model: 32768},
		})
	}
	current := ref
	if role {
		cfg.Agent.SubagentModels = map[string]string{"browser": ref}
		current = "synthetic/other"
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	f.app.tabs = map[string]*WorkspaceTab{"owner": {ID: "owner", WorkspaceRoot: t.TempDir(), model: current}}
	return f, ref
}

func TestBrowserAccountModelMissingFromDisk(t *testing.T) {
	for _, role := range []bool{false, true} {
		name := "current"
		if role {
			name = "explicit-role"
		}
		t.Run(name, func(t *testing.T) {
			f, ref := browserAccountFixture(t, role, false)
			before, err := os.ReadFile(config.UserConfigPath())
			if err != nil {
				t.Fatal(err)
			}
			p, identity, err := f.app.browserProvider(context.Background(), "owner")
			if err != nil {
				t.Fatal(err)
			}
			ap, ok := p.(*accountProvider)
			if !ok || ap.cfg.Name+"/"+ap.cfg.Model != ref || identity != ref+" at "+orcaaccount.Router+"/v1" {
				t.Fatalf("wrong account provider: %T identity=%q", p, identity)
			}
			after, err := os.ReadFile(config.UserConfigPath())
			if err != nil || string(before) != string(after) {
				t.Fatal("runtime resolution rewrote config", err)
			}
			if f.credentials.Load() != 0 || f.inferences.Load() != 0 {
				t.Fatal("model selection acquired credentials or sent a probe")
			}
		})
	}
}

func TestBrowserAccountModelPreservesSettingsAndCapabilityDenials(t *testing.T) {
	f, ref := browserAccountFixture(t, false, true)
	s := f.app.account
	name, _, _ := strings.Cut(ref, "/")
	no := false
	s.mu.Lock()
	m := s.catalog[name]
	m.Capabilities = &orcaaccount.ModelCapabilities{Vision: &no, Tools: &no}
	s.catalog[name] = m
	s.mu.Unlock()
	p, _, err := f.app.browserProvider(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	ap, ok := p.(*accountProvider)
	if !ok {
		t.Fatalf("wrong provider %T", p)
	}
	if ap.cfg.Extra["thinking"] != "adaptive" || ap.cfg.Extra["effort"] != "high" || ap.cfg.Extra["reasoning_protocol"] != "openai" {
		t.Fatalf("reasoning settings lost: %#v", ap.cfg.Extra)
	}
	if levels, ok := ap.cfg.Extra["supported_efforts"].([]string); !ok || strings.Join(levels, ",") != "low,high" {
		t.Fatal("effort capability lost", ap.cfg.Extra)
	}
	cfg, err := config.LoadForRoot(f.app.tabs["owner"].WorkspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := f.app.browserProviderEntry(context.Background(), cfg, ref)
	if err != nil || entry.ContextWindow != 65536 || entry.ModelContextWindows[integrationModel] != 32768 || entry.DefaultEffort != "low" {
		t.Fatalf("context/effort metadata lost: %+v %v", entry, err)
	}
	for _, req := range []provider.Request{
		{Tools: []provider.ToolSchema{{Name: "synthetic"}}},
		{Messages: []provider.Message{{Images: []provider.ImageContent{{MediaType: "image/png", Data: "synthetic"}}}}},
	} {
		if _, err := p.Stream(context.Background(), req); err == nil {
			t.Fatal("explicit server capability denial ignored")
		}
	}
	if f.credentials.Load() != 0 || f.inferences.Load() != 0 {
		t.Fatal("denied capability acquired credentials or sent a probe")
	}
}

func TestBrowserAccountEffortUsesCurrentTabNotAnExplicitRole(t *testing.T) {
	for _, role := range []bool{false, true} {
		name := "current"
		if role {
			name = "explicit-role"
		}
		t.Run(name, func(t *testing.T) {
			f, _ := browserAccountFixture(t, role, true)
			low := "low"
			f.app.tabs["owner"].effort = &low
			p, _, err := f.app.browserProvider(context.Background(), "owner")
			if err != nil {
				t.Fatal(err)
			}
			want := "low"
			if role {
				want = "high"
			}
			if got := p.(*accountProvider).cfg.Extra["effort"]; got != want {
				t.Fatalf("effort=%v want %s", got, want)
			}
		})
	}
}

func TestBrowserAccountExpiredCatalogRefreshResolvesExactSelection(t *testing.T) {
	f, ref := browserAccountFixture(t, true, false)
	f.app.account.mu.Lock()
	f.app.account.catalogAt = time.Time{}
	f.app.account.mu.Unlock()
	p, identity, err := f.app.browserProvider(context.Background(), "owner")
	if err != nil || p == nil || identity != ref+" at "+orcaaccount.Router+"/v1" {
		t.Fatalf("exact refreshed role did not resolve: %T %q %v", p, identity, err)
	}
	if f.catalogs.Load() != 2 || f.credentials.Load() != 0 || f.inferences.Load() != 0 {
		t.Fatalf("refresh/probe counts: catalogs=%d credentials=%d inferences=%d", f.catalogs.Load(), f.credentials.Load(), f.inferences.Load())
	}
}

func TestBrowserAccountSelectionRejectsStaleAndRemovedWithoutFallback(t *testing.T) {
	for _, mode := range []string{"removed", "expired", "canceled", "signed-out", "formal"} {
		t.Run(mode, func(t *testing.T) {
			f, ref := browserAccountFixture(t, true, true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := f.app.account
			s.mu.Lock()
			switch mode {
			case "removed":
				name, _, _ := strings.Cut(ref, "/")
				delete(s.catalog, name)
			case "expired":
				s.catalogAt = time.Time{}
				s.refreshModels = nil
			case "signed-out":
				s.view.Authenticated = false
			case "canceled":
				cancel()
			case "formal":
				channel = "release"
			}
			s.mu.Unlock()
			p, _, err := f.app.browserProvider(ctx, "owner")
			if err == nil || p != nil {
				t.Fatalf("unavailable role fell back or was admitted: %T, %v", p, err)
			}
			if f.credentials.Load() != 0 || f.inferences.Load() != 0 {
				t.Fatal("failed resolution acquired credentials or sent a probe")
			}
		})
	}
}
