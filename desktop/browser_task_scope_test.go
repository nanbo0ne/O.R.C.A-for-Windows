package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type browserScopeFixture struct {
	stream               func(context.Context, provider.Request) (<-chan provider.Chunk, error)
	beginErr             error
	conversation, parent string
	cancel               context.CancelFunc
	ended                atomic.Bool
	beginCount           int
}

func (p *browserScopeFixture) Name() string { return "synthetic-scoped-browser" }
func (p *browserScopeFixture) BeginTask(ctx context.Context, conversation, parent string) (context.Context, func(), error) {
	p.beginCount++
	p.conversation, p.parent = conversation, parent
	if p.beginErr != nil {
		return nil, nil, p.beginErr
	}
	ctx, p.cancel = context.WithCancel(ctx)
	return ctx, func() { p.ended.Store(true); p.cancel() }, nil
}
func (p *browserScopeFixture) Stream(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	return p.stream(ctx, r)
}

func scopedBrowserProbe(r provider.Request) (<-chan provider.Chunk, bool) {
	if len(r.Tools) != 1 || r.Tools[0].Name != "browser_capability_check" {
		return nil, false
	}
	for i := len(r.Messages) - 1; i >= 0; i-- {
		text := r.Messages[i].Content
		if !strings.HasPrefix(text, "Synthetic browser capability test") {
			continue
		}
		_, nonce, ok := strings.Cut(text, "nonce ")
		if !ok {
			return nil, false
		}
		args, _ := json.Marshal(map[string]string{"nonce": strings.TrimSuffix(nonce, ". No prose.")})
		return browserCall("synthetic-probe", "browser_capability_check", string(args)), true
	}
	return nil, false
}

func TestBrowserRunHistoryStableScopeAndConfirmedResults(t *testing.T) {
	s, b, _ := browserFixture(t)
	autoBrowserConsent(s)
	ctx := computerParentTestContext(t)
	parent, _ := agent.ParentTurn(ctx)
	var requests []provider.Request
	p := &browserScopeFixture{}
	p.stream = func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		requests = append(requests, r)
		if ch, ok := scopedBrowserProbe(r); ok {
			return ch, nil
		}
		if len(r.Tools) == 0 {
			return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: `{"satisfied":true,"reason":"synthetic fresh evidence"}`}), nil
		}
		if b.actions == 0 {
			return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: "  inspected\n"}, provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "step", Name: "browser_action", Arguments: `{"kind":"wait"}`}}), nil
		}
		return browserCall("finish", "browser_complete", `{"summary":"done"}`), nil
	}
	if _, err := s.run(ctx, "owner", "outer", browserInput(), p, "synthetic/model"); err != nil {
		t.Fatal(err)
	}
	if p.beginCount != 1 || !p.ended.Load() {
		t.Fatalf("scope begin=%d ended=%v", p.beginCount, p.ended.Load())
	}
	if len(requests) != 4 || b.actions != 1 {
		t.Fatalf("requests=%d actions=%d", len(requests), b.actions)
	}
	for i, r := range requests {
		if r.ConversationID == "" || r.ConversationID != p.conversation || r.ConversationID == "owner" || r.ParentTaskID != parent || r.ParentTaskID != p.parent {
			t.Fatalf("lost host identity at step %d: %+v", i, r)
		}
		if i > 0 {
			old, _ := json.Marshal(requests[i-1].Messages)
			prefix, _ := json.Marshal(r.Messages[:min(len(r.Messages), len(requests[i-1].Messages))])
			if string(old) != string(prefix) {
				t.Fatalf("acknowledged prefix changed at step %d", i)
			}
		}
	}
	if requests[0].Messages[0].Role != provider.RoleSystem || !strings.Contains(requests[0].Messages[0].Content, browserInput().Goal) {
		t.Fatal("system/goal not fixed on first request")
	}
	var assistant, probe, action bool
	for _, m := range requests[2].Messages {
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) == 1 && m.ToolCalls[0].ID == "step" {
			assistant = m.Content == "  inspected\n"
		}
		if m.Role == provider.RoleTool && m.ToolCallID == "synthetic-probe" && m.Name == "browser_capability_check" {
			probe = strings.Contains(m.Content, "confirmed")
		}
		if m.Role == provider.RoleTool && m.ToolCallID == "step" && m.Name == "browser_action" {
			action = strings.Contains(m.Content, "native_completed")
		}
	}
	if !assistant || !probe || !action {
		t.Fatalf("lost confirmed history: assistant=%v probe=%v action=%v", assistant, probe, action)
	}
}

func TestBrowserRunScopeCancellationPreventsNativeDispatch(t *testing.T) {
	s, b, _ := browserFixture(t)
	autoBrowserConsent(s)
	p := &browserScopeFixture{}
	p.stream = func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		if ch, ok := scopedBrowserProbe(r); ok {
			return ch, nil
		}
		if p.cancel != nil {
			p.cancel()
		}
		return browserCall("must-not-run", "browser_action", `{"kind":"wait"}`), nil
	}
	_, err := s.run(computerParentTestContext(t), "owner", "outer", browserInput(), p, "synthetic/model")
	if !errors.Is(err, context.Canceled) || b.actions != 0 || !p.ended.Load() {
		t.Fatalf("err=%v actions=%d ended=%v", err, b.actions, p.ended.Load())
	}
}

func TestBrowserRunScopeHeldUntilNativeActuallyExits(t *testing.T) {
	s, b, _ := browserFixture(t)
	autoBrowserConsent(s)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	p := &browserScopeFixture{}
	var cleanupOutsideScope atomic.Bool
	var cleanupCount atomic.Int32
	b.command = func(_, op, _ string) (string, error) {
		if op == "download-cancel-pending" || op == "upload-cancel-pending" {
			cleanupCount.Add(1)
			if p.ended.Load() {
				cleanupOutsideScope.Store(true)
			}
		}
		return "", nil
	}
	b.execute = func(ctx context.Context) error { close(entered); <-release; return ctx.Err() }
	p.stream = func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		if p.cancel == nil {
			return nil, errors.New("task scope was not started")
		}
		if ch, ok := scopedBrowserProbe(r); ok {
			return ch, nil
		}
		return browserCall("wait", "browser_action", `{"kind":"wait"}`), nil
	}
	ctx := computerParentTestContext(t)
	go func() { _, err := s.run(ctx, "owner", "outer", browserInput(), p, "synthetic/model"); done <- err }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("scope not held before native: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("native fixture did not enter")
	}
	p.cancel()
	endedEarly := p.ended.Load()
	busy := s.state("owner").Busy
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || endedEarly || !busy || !p.ended.Load() {
			t.Fatalf("err=%v early=%v busy=%v ended=%v", err, endedEarly, busy, p.ended.Load())
		}
		if cleanupOutsideScope.Load() || cleanupCount.Load() != 2 {
			t.Fatal("scope released before owned transfer cleanup finished")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("native fixture did not exit")
	}
}

func TestBrowserRunScopeFailureDoesNotStartNativeWork(t *testing.T) {
	s, b, _ := browserFixture(t)
	autoBrowserConsent(s)
	p := &browserScopeFixture{beginErr: errors.New("revoked identity")}
	p.stream = func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
		return nil, errors.New("unexpected generation")
	}
	_, err := s.run(computerParentTestContext(t), "owner", "outer", browserInput(), p, "synthetic/model")
	if !errors.Is(err, p.beginErr) || b.actions != 0 || b.observations != 0 {
		t.Fatalf("err=%v native=%d/%d", err, b.actions, b.observations)
	}
}

func TestBrowserUsageForwardsTaskScope(t *testing.T) {
	inner := &browserScopeFixture{}
	p := &browserUsageProvider{Provider: inner, parent: "parent", sink: event.FuncSink(func(event.Event) {})}
	scoped, ok := any(p).(provider.TaskProvider)
	if !ok {
		t.Fatal("usage wrapper erased task scope")
	}
	ctx, end, err := scoped.BeginTask(context.Background(), "browser-owned", "parent")
	if err != nil {
		t.Fatal(err)
	}
	inner.cancel()
	if ctx.Err() == nil || inner.conversation != "browser-owned" || inner.parent != "parent" {
		t.Fatal("scope identity/cancellation lost")
	}
	end()
	if !inner.ended.Load() {
		t.Fatal("scope finish not forwarded")
	}
}

func TestBrowserCollectKeepsExactAssistantContent(t *testing.T) {
	p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
		return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: "  confirmed\n"}), nil
	}}
	body, _, err := collectBrowserResponse(context.Background(), p, nil, nil)
	if err != nil || body != "  confirmed\n" {
		t.Fatalf("content changed: %q, %v", body, err)
	}
}

func TestBrowserUsageScopeRejectsDifferentParentAndPreservesPlainAPI(t *testing.T) {
	inner := &browserScopeFixture{}
	p := &browserUsageProvider{Provider: inner, parent: "parent"}
	if _, _, err := p.BeginTask(context.Background(), "owned", "different-parent"); err == nil || inner.beginCount != 0 {
		t.Fatal("scope and request could be attributed to different parents")
	}
	plain := &browserUsageProvider{Provider: &usageFixtureProvider{}, parent: "parent"}
	ctx, cancel := context.WithCancel(context.Background())
	scoped, finish, err := plain.BeginTask(ctx, "owned", "parent")
	if err != nil || scoped != ctx || finish == nil {
		t.Fatal("non-scoped API wrapper changed context", err)
	}
	finish()
	if ctx.Err() != nil {
		t.Fatal("no-op API scope cancelled its caller")
	}
	cancel()
}

func TestBrowserRunConversationIdentityIndependentOfGoalAndOtherRuns(t *testing.T) {
	s, _, _ := browserFixture(t)
	autoBrowserConsent(s)
	ctx := computerParentTestContext(t)
	parent, _ := agent.ParentTurn(ctx)
	var identities []string
	p := browserCompleter(func(r provider.Request) <-chan provider.Chunk {
		if r.ParentTaskID != parent {
			t.Fatal("plain API lost parent attribution")
		}
		identities = append(identities, r.ConversationID)
		return browserCall("complete", "browser_complete", `{"summary":"done"}`)
	})
	input := browserInput()
	input.Goal = "synthetic conversationId=owner parentTaskId=user-controlled"
	for _, call := range []string{"outer-one", "outer-two"} {
		if _, err := s.run(ctx, "owner", call, input, p, "synthetic/api"); err != nil {
			t.Fatal(err)
		}
	}
	if len(identities) != 2 || identities[0] == identities[1] || identities[0] == "owner" || identities[1] == "user-controlled" || identities[0] == "" || identities[1] == "" {
		t.Fatal("browser run identity was reused or derived from task text", identities)
	}
}
