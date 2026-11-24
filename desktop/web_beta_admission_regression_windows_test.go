//go:build windows

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	agenttest "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent/testutil"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

func TestReviewOrdinaryAPICompletesWhileWebStartupIsPending(t *testing.T) {
	isolatedWebRuntime(t)
	complete := webbeta.BeginEngineStartup()
	defer complete(errors.New("synthetic startup never logged in"))
	webBetaAdmission.blocked = true
	p := agenttest.NewMock("synthetic-api", agenttest.Turn{Text: "synthetic ordinary API completed"})
	a := agent.New(p, tool.NewRegistry(), agent.NewSession("synthetic API"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Runner: a, Executor: a, Sink: event.Discard, ModelRef: "api/synthetic", ProviderKind: "openai"})
	app := webBetaTestApp(ctrl)
	app.tabs["web"].model = "api/synthetic"
	if err := app.SubmitToTab("web", "synthetic question"); err != nil {
		t.Fatal("web admission blocked unrelated API", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ctrl.WaitIdle(ctx); err != nil {
		t.Fatal("ordinary API waited for web startup", err)
	}
	if len(p.Requests()) != 1 || webbeta.CurrentEngine() != nil || betaRuntime.browser != nil {
		t.Fatal("API route entered web startup or failed to generate once")
	}
}

// Preserved review regression; the controller records the actual bound Kind.
func TestReviewWebProviderAliasMustShareAdmissionAndRetirement(t *testing.T) {
	isolatedWebRuntime(t)
	entry := config.ProviderEntry{Name: "synthetic-web-alias", Kind: "deepseekweb-beta", Model: "synthetic", BaseURL: webbeta.Origin}
	if !entry.Configured() {
		t.Fatal("alias not configured")
	}
	cfg := config.Default()
	if err := cfg.UpsertProvider(entry); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	p, err := provider.New(entry.Kind, provider.Config{Name: entry.Name, Model: entry.Model, BaseURL: entry.BaseURL})
	if err != nil {
		t.Fatal("alias provider could not be created", err)
	}
	if _, ok := p.(provider.TaskProvider); !ok {
		t.Fatal("not an actual web task provider")
	}
	ref := entry.Name + "/" + entry.Model
	ctrl := control.New(control.Options{Sink: event.Discard, ModelRef: ref, ProviderKind: entry.Kind})
	app := webBetaTestApp(ctrl)
	app.tabs["web"].model = ref
	app.tabs["web"].runtimeReconfiguring = true
	webBetaAdmission.blocked = true
	if err := app.SubmitToTab("web", "synthetic accepted draft"); err == nil || app.tabs["web"].pendingSubmitCount != 0 {
		t.Errorf("web-kind alias bypassed logout gate: err=%v queued=%d", err, app.tabs["web"].pendingSubmitCount)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	webBetaAdmission.Lock()
	retired, err := app.retireWebBetaControllers(ctx)
	webBetaAdmission.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !ctrl.Retired() || len(retired) != 1 || app.tabs["web"].pendingSubmitCount != 0 {
		t.Errorf("web-kind alias escaped queue retirement: retired=%v tracked=%d queued=%d", ctrl.Retired(), len(retired), app.tabs["web"].pendingSubmitCount)
	}
}

// This distinguishes duplicate text in newly sent deltas from tool-ID replay.
func TestReviewBrowserDeltaMustNotRepeatAcknowledgedActionSummary(t *testing.T) {
	s, native, _ := browserFixture(t)
	autoBrowserConsent(s)
	var requests []provider.Request
	p := browserCompleter(func(provider.Request) <-chan provider.Chunk {
		switch native.actions {
		case 0:
			return browserCall("scroll-once", "browser_action", `{"kind":"scroll","deltaY":80}`)
		case 1:
			return browserCall("wait-once", "browser_action", `{"kind":"wait"}`)
		default:
			return browserCall("finish", "browser_complete", `{"summary":"synthetic done"}`)
		}
	})
	inner := p.stream
	p.stream = func(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		requests = append(requests, r)
		return inner(ctx, r)
	}
	if _, err := s.run(computerParentTestContext(t), "owner", "outer", browserInput(), p, "synthetic/api"); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 5 || native.actions != 2 {
		t.Fatal("unexpected fixture trace")
	}
	current, previous := requests[3], requests[2]
	toolResults := 0
	for _, m := range current.Messages {
		if m.Role == provider.RoleTool && m.ToolCallID == "scroll-once" {
			toolResults++
		}
	}
	if toolResults != 1 {
		t.Fatalf("correlated result duplicated: %d", toolResults)
	}
	for _, m := range current.Messages[len(previous.Messages):] {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, "scroll completed; new observation required") {
			t.Error("already-acknowledged scroll result was reintroduced in the new cumulative user receipt summary; actual tool ID was not replayed")
		}
	}
}
