package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func browserCall(id, name, args string) <-chan provider.Chunk {
	return controlChunks(provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: args}})
}
func browserProbeReply(r provider.Request) (<-chan provider.Chunk, bool) {
	if len(r.Tools) != 1 || r.Tools[0].Name != "browser_capability_check" {
		return nil, false
	}
	text := r.Messages[len(r.Messages)-1].Content
	i := strings.Index(text, "nonce ")
	nonce := strings.TrimSuffix(text[i+6:], ". No prose.")
	args, _ := json.Marshal(map[string]string{"nonce": nonce})
	return browserCall("probe", "browser_capability_check", string(args)), true
}
func browserCompleter(decide func(provider.Request) <-chan provider.Chunk) *controlTestProvider {
	return &controlTestProvider{stream: func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		if ch, ok := browserProbeReply(r); ok {
			return ch, nil
		}
		if len(r.Tools) == 0 {
			return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: `{"satisfied":true,"reason":"fresh synthetic evidence"}`}), nil
		}
		return decide(r), nil
	}}
}
func browserInput() browserTaskInput {
	return browserTaskInput{Goal: "Synthetic task", Criteria: "Synthetic page evidence"}
}

func TestBrowserDOMFirstAndFreshCompletion(t *testing.T) {
	s, b, _ := browserFixture(t)
	autoBrowserConsent(s)
	decisionEvidence, verificationEvidence := "", ""
	calls := 0
	p := &controlTestProvider{stream: func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		if ch, ok := browserProbeReply(r); ok {
			return ch, nil
		}
		calls++
		for _, m := range r.Messages {
			if len(m.Images) > 0 {
				t.Fatal("screenshot sent despite usable DOM")
			}
			if strings.Contains(m.Content, "secret-fixture") {
				t.Fatal("password value exposed")
			}
		}
		if len(r.Tools) == 0 {
			verificationEvidence = r.Messages[len(r.Messages)-1].Content
			return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: `{"satisfied":true,"reason":"fresh synthetic evidence"}`}), nil
		}
		decisionEvidence = r.Messages[len(r.Messages)-1].Content
		return browserCall("finish", "browser_complete", `{"summary":"synthetic done"}`), nil
	}}
	result, err := s.run(computerParentTestContext(t), "owner", "outer", browserInput(), p, "test/model")
	if err != nil || result != "synthetic done" || calls != 2 || decisionEvidence == verificationEvidence || b.observations != 2 {
		t.Fatalf("result=%s calls=%d obs=%d err=%v", result, calls, b.observations, err)
	}
	if len(s.state("owner").Consents) != 1 {
		t.Fatal("origin/model consent not recorded")
	}
}
func TestBrowserParentTurnBudgetCumulative(t *testing.T) {
	s, b, _ := browserFixture(t)
	autoBrowserConsent(s)
	ctx := computerParentTestContext(t)
	limit := 20
	p := browserCompleter(func(r provider.Request) <-chan provider.Chunk {
		if b.actions == limit {
			return browserCall(fmt.Sprintf("complete-%d", limit), "browser_complete", `{"summary":"done"}`)
		}
		return browserCall(fmt.Sprintf("step-%d", b.actions), "browser_action", `{"kind":"wait"}`)
	})
	if _, err := s.run(ctx, "owner", "first", browserInput(), p, "fake/model"); err != nil {
		t.Fatal(err)
	}
	limit = 30
	if _, err := s.run(ctx, "owner", "second", browserInput(), p, "fake/model"); err != nil {
		t.Fatal(err)
	}
	if b.actions != 30 {
		t.Fatal(b.actions)
	}
	if _, err := s.run(ctx, "owner", "third", browserInput(), p, "fake/model"); !errors.Is(err, errBrowserStopped) {
		t.Fatal(err)
	}
	turn, _ := agent.ParentTurn(ctx)
	s.mu.Lock()
	used := s.budgets[turn].actions
	s.mu.Unlock()
	if used != 30 {
		t.Fatal(used)
	}
}
func TestBrowserNoToolActionReplay(t *testing.T) {
	s, b, _ := browserFixture(t)
	autoBrowserConsent(s)
	ctx := computerParentTestContext(t)
	p := browserCompleter(func(provider.Request) <-chan provider.Chunk {
		return browserCall("same-id", "browser_action", `{"kind":"wait"}`)
	})
	if _, err := s.run(ctx, "owner", "outer", browserInput(), p, "fake/model"); !errors.Is(err, errBrowserStopped) {
		t.Fatal(err)
	}
	if b.actions != 1 {
		t.Fatalf("replayed input: %d", b.actions)
	}
	if _, err := s.run(ctx, "owner", "renamed-outer", browserInput(), p, "fake/model"); !errors.Is(err, errBrowserStopped) {
		t.Fatal(err)
	}
}
func TestBrowserNoParentCallReplayAfterSuccess(t *testing.T) {
	s, _, _ := browserFixture(t)
	autoBrowserConsent(s)
	ctx := computerParentTestContext(t)
	p := browserCompleter(func(provider.Request) <-chan provider.Chunk {
		return browserCall("done", "browser_complete", `{"summary":"done"}`)
	})
	if _, err := s.run(ctx, "owner", "outer", browserInput(), p, "fake/model"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.run(ctx, "owner", "outer", browserInput(), p, "fake/model"); !errors.Is(err, errBrowserStopped) {
		t.Fatal(err)
	}
}
func TestBrowserCumulativeDeadlineNotReset(t *testing.T) {
	s, _, id := browserFixture(t)
	ctx := computerParentTestContext(t)
	task, end, err := s.beginTask(ctx, "owner", "first", id, "fake/model")
	if err != nil {
		t.Fatal(err)
	}
	end(nil)
	s.mu.Lock()
	task.budget.deadline = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if _, _, err := s.beginTask(ctx, "owner", "second", id, "fake/model"); !errors.Is(err, errBrowserStopped) {
		t.Fatal(err)
	}
}

func TestBrowserApprovalExcludesWaitFromParentAndChildClock(t *testing.T) {
	s, _, id := browserFixture(t)
	var paused, resumed atomic.Int32
	ctx := provider.WithTaskHumanWait(computerParentTestContext(t), func() func() {
		paused.Add(1)
		return func() { resumed.Add(1) }
	})
	task, end, err := s.beginTask(ctx, "owner", "approval-clock", id, "fake/model")
	if err != nil {
		t.Fatal(err)
	}
	defer end(nil)
	ready := make(chan string, 1)
	s.emit = func(_ string, st BrowserState) {
		if st.Approval != nil {
			ready <- st.Approval.ID
		}
	}
	done := make(chan error, 1)
	go func() { done <- s.consent(task, "https://example.test", "Synthetic clock consent") }()
	approval := <-ready
	if paused.Load() != 1 {
		t.Fatal("parent model clock was not paused")
	}
	// Simulate ten minutes in the approval screen without a slow wall-clock test.
	s.mu.Lock()
	task.waitAt = time.Now().Add(-10 * time.Minute)
	task.budget.deadline = time.Now().Add(-9 * time.Minute)
	s.mu.Unlock()
	s.expireTask(task)
	if task.ctx.Err() != nil {
		t.Fatal("approval consumed browser execution time")
	}
	if err := s.consent(task, "https://example.test", "Concurrent approval"); !errors.Is(err, errBrowserBusy) {
		t.Fatalf("overwrote pending approval: %v", err)
	}
	s.approve("owner", BrowserCommand{ApprovalID: approval, Allow: true})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if resumed.Load() != 1 {
		t.Fatal("parent clock was not resumed exactly once")
	}
	s.mu.Lock()
	left := time.Until(task.budget.deadline)
	s.mu.Unlock()
	if left < 59*time.Second || left > 61*time.Second || s.state("owner").TaskState != "running" {
		t.Fatalf("execution budget was reset or not extended: %s", left)
	}
	// An active expiry still stops the task and is not mistaken for a user cancel.
	s.mu.Lock()
	task.budget.deadline = time.Now().Add(-time.Second)
	s.mu.Unlock()
	s.expireTask(task)
	if !errors.Is(task.ctx.Err(), context.DeadlineExceeded) {
		t.Fatal(task.ctx.Err())
	}
}

func TestBrowserApprovalDoesNotSuppressCancellationOrParentDeadline(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline", "deny"} {
		t.Run(mode, func(t *testing.T) {
			s, _, id := browserFixture(t)
			ctx, cancel := context.WithCancelCause(computerParentTestContext(t))
			defer cancel(context.Canceled)
			var resumes atomic.Int32
			ctx = provider.WithTaskHumanWait(ctx, func() func() {
				return func() { resumes.Add(1) }
			})
			task, end, err := s.beginTask(ctx, "owner", "wait-"+mode, id, "fake/model")
			if err != nil {
				t.Fatal(err)
			}
			ready := make(chan string, 1)
			s.emit = func(_ string, st BrowserState) {
				if st.Approval != nil {
					ready <- st.Approval.ID
				}
			}
			done := make(chan error, 1)
			go func() { done <- s.consent(task, "https://example.test", "Synthetic wait") }()
			approval := <-ready
			switch mode {
			case "cancel":
				cancel(context.Canceled)
			case "deadline":
				cancel(context.DeadlineExceeded)
			case "deny":
				s.approve("owner", BrowserCommand{ApprovalID: approval, Allow: false})
			}
			select {
			case err = <-done:
				if err == nil || resumes.Load() != 1 {
					t.Fatalf("err=%v resumes=%d", err, resumes.Load())
				}
				if mode == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("approval ignored termination")
			}
			end(err)
			if s.state("owner").Busy || s.state("owner").Approval != nil {
				t.Fatal("finished task retained approval or lease")
			}
		})
	}
}
func TestBrowserCancellationKeepsLeaseUntilInputReturns(t *testing.T) {
	s, b, id := browserFixture(t)
	autoBrowserConsent(s)
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	b.execute = func(ctx context.Context) error { close(entered); <-release; return ctx.Err() }
	ctx := computerParentTestContext(t)
	p := browserCompleter(func(provider.Request) <-chan provider.Chunk {
		return browserCall("wait", "browser_action", `{"kind":"wait"}`)
	})
	go func() { _, err := s.run(ctx, "owner", "outer", browserInput(), p, "fake/model"); finished <- err }()
	<-entered
	s.command(ctx, "owner", BrowserCommand{Op: "cancelTask"})
	if !s.state("owner").Busy {
		t.Fatal("lease released while input outstanding")
	}
	close(release)
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation stuck")
	}
	if s.state("owner").Busy {
		t.Fatal("lease leaked after input completion")
	}
	if _, _, err := s.beginTask(ctx, "owner", "retry", id, "fake/model"); !errors.Is(err, errBrowserStopped) {
		t.Fatal(err)
	}
}
func TestBrowserInputApprovalIndependentOfRiskLabel(t *testing.T) {
	for _, kind := range []string{"click", "type", "select", "key"} {
		t.Run(kind, func(t *testing.T) {
			s, b, _ := browserFixture(t)
			approvals := 0
			s.emit = func(owner string, state BrowserState) {
				if state.Approval != nil {
					approvals++
					s.approve(owner, BrowserCommand{ApprovalID: state.Approval.ID, Allow: approvals == 1})
				}
			}
			args := fmt.Sprintf(`{"kind":%q,"elementId":"input","text":"synthetic","key":"Escape"}`, kind)
			p := browserCompleter(func(provider.Request) <-chan provider.Chunk { return browserCall("action", "browser_action", args) })
			if _, err := s.run(computerParentTestContext(t), "owner", "outer", browserInput(), p, "fake/model"); err == nil {
				t.Fatal("denied approval succeeded")
			}
			if approvals != 2 || b.actions != 0 {
				t.Fatalf("approvals=%d actions=%d", approvals, b.actions)
			}
		})
	}
}
func TestBrowserActionValidation(t *testing.T) {
	o := browserObserved{Observation: browserhost.Observation{TabID: "t", Generation: 3, DocumentID: "d", Elements: []browserhost.Element{{ID: "s", Type: "password"}, {ID: "f", Type: "file"}, {ID: "ok", Type: "text"}}}}
	for _, raw := range []string{`{"kind":"evaluate","text":"alert(1)"}`, `{"kind":"click","generation":3}`, `{"kind":"click","elementId":"missing"}`, `{"kind":"type","elementId":"s"}`, `{"kind":"click","elementId":"f"}`, `{"kind":"click","x":1,"y":1}`, `{"kind":"navigate","url":"file:///secret"}`, `{"kind":"key","key":"a"}`, `{"kind":"key"}`, `{"kind":"waitfor"} {}`} {
		if _, err := browserAction(raw, o, false); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`{"kind":"key","key":"Enter"}`, `{"kind":"key","key":"Tab"}`, `{"kind":"key","key":"ArrowDown"}`, `{"kind":"waitfor","text":"#done"}`, `{"kind":"hover","elementId":"ok"}`, `{"kind":"dblclick","elementId":"ok"}`} {
		if a, err := browserAction(raw, o, false); err != nil || a.Generation != 3 {
			t.Errorf("rejected %s: %v", raw, err)
		}
	}
	if a, err := browserAction(`{"kind":"type","elementId":"ok","text":"fixture"}`, o, false); err != nil || a.Generation != 3 || a.DocumentID != "d" {
		t.Fatal(a, err)
	}
}
func TestBrowserStaleObservationStopsBeforeInput(t *testing.T) {
	s, b, id := browserFixture(t)
	autoBrowserConsent(s)
	replans := 0
	p := browserCompleter(func(provider.Request) <-chan provider.Chunk {
		replans++
		s.onEvent(browserhost.Event{TabID: id, Kind: "loaded", URL: "https://example.test/changed"})
		return browserCall(fmt.Sprintf("stale-%d", replans), "browser_action", `{"kind":"wait"}`)
	})
	if _, err := s.run(computerParentTestContext(t), "owner", "outer", browserInput(), p, "fake/model"); !errors.Is(err, browserhost.ErrStaleObservation) {
		t.Fatal(err)
	}
	if b.actions != 0 {
		t.Fatal("stale input executed")
	}
}
func TestBrowserCompletionInvalidatedDuringVerification(t *testing.T) {
	s, _, id := browserFixture(t)
	autoBrowserConsent(s)
	p := browserCompleter(func(provider.Request) <-chan provider.Chunk {
		return browserCall("done", "browser_complete", `{"summary":"done"}`)
	})
	original := p.stream
	p.stream = func(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		if len(r.Tools) == 0 {
			s.onEvent(browserhost.Event{TabID: id, Kind: "loaded", URL: "https://example.test/changed"})
		}
		return original(ctx, r)
	}
	if _, err := s.run(computerParentTestContext(t), "owner", "outer", browserInput(), p, "fake/model"); !errors.Is(err, browserhost.ErrStaleObservation) {
		t.Fatal(err)
	}
}
func TestBrowserUnknownVisionDoesNotLeakScreenshot(t *testing.T) {
	s, b, _ := browserFixture(t)
	autoBrowserConsent(s)
	b.observe = func(o *browserhost.Observation) { o.Text = ""; o.Elements = nil }
	data, _, _, err := computerQualificationImage()
	if err != nil {
		t.Fatal(err)
	}
	screen, _ := base64.StdEncoding.DecodeString(data)
	b.observe = func(o *browserhost.Observation) {
		o.Text = ""
		o.Elements = nil
		o.Screenshot = screen
		o.ViewportWidth = 160
		o.ViewportHeight = 80
	}
	images := 0
	p := &controlTestProvider{stream: func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		if ch, ok := browserProbeReply(r); ok {
			return ch, nil
		}
		for _, m := range r.Messages {
			for _, image := range m.Images {
				images++
				if image.Name != "qualification.png" {
					t.Fatal("page screenshot exposed before image qualification")
				}
			}
		}
		return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: "I cannot see images"}), nil
	}}
	if _, err := s.run(computerParentTestContext(t), "owner", "outer", browserInput(), p, "fake/model"); err == nil {
		t.Fatal("unknown vision accepted")
	}
	if images != 1 || b.actions != 0 {
		t.Fatal(images, b.actions)
	}
}
func TestBrowserAutoTabOriginConsentBeforeNetworkOrObservation(t *testing.T) {
	s, b, id := browserFixture(t)
	s.command(context.Background(), "owner", BrowserCommand{Op: "close", TabID: id})
	approvals := 0
	s.emit = func(owner string, state BrowserState) {
		if state.Approval != nil {
			approvals++
			if b.observations != 0 {
				t.Fatal("page observed before consent")
			}
			s.approve(owner, BrowserCommand{ApprovalID: state.Approval.ID, Allow: false})
		}
	}
	p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
		t.Fatal("provider called without consent")
		return nil, nil
	}}
	input := browserInput()
	input.URL = "https://new.test/page"
	if _, err := s.run(computerParentTestContext(t), "owner", "outer", input, p, "fake/model"); err == nil {
		t.Fatal("denied task succeeded")
	}
	if approvals != 1 || len(b.tabs) != 1 {
		t.Fatal(approvals, len(b.tabs))
	}
	for _, tab := range b.tabs {
		if tab.URL != "about:blank" {
			t.Fatal("network navigation before consent")
		}
	}
}
func TestBrowserAutoURLAndCapabilityProbe(t *testing.T) {
	s, b, id := browserFixture(t)
	s.command(context.Background(), "owner", BrowserCommand{Op: "close", TabID: id})
	autoBrowserConsent(s)
	p := browserCompleter(func(provider.Request) <-chan provider.Chunk {
		return browserCall("done", "browser_complete", `{"summary":"page opened"}`)
	})
	input := browserInput()
	input.URL = "https://new.test/page"
	if _, err := s.run(computerParentTestContext(t), "owner", "outer", input, p, "fake/model"); err != nil {
		t.Fatal(err)
	}
	for _, tab := range b.tabs {
		if tab.URL != input.URL {
			t.Fatal(tab.URL)
		}
	}
}
func TestBrowserModelSelectionNoFallback(t *testing.T) {
	cfg := &config.Config{DefaultModel: "bad/missing", Providers: []config.ProviderEntry{{Name: "fake", Kind: "openai", Models: []string{"main", "browser"}}}}
	entry, err := resolveBrowserModel(cfg, "fake/main")
	if err != nil || entry.Model != "main" {
		t.Fatal(entry, err)
	}
	cfg.Agent.SubagentModels = map[string]string{"browser": "fake/browser"}
	entry, err = resolveBrowserModel(cfg, "fake/main")
	if err != nil || entry.Model != "browser" {
		t.Fatal(entry, err)
	}
	cfg.Agent.SubagentModels["browser"] = "missing/model"
	if _, err = resolveBrowserModel(cfg, "fake/main"); err == nil {
		t.Fatal("silent model fallback")
	}
}
func TestBrowserToolProbeRejectsWrongOrProseResponse(t *testing.T) {
	for _, mode := range []string{"prose", "nonce", "multiple", "EOF"} {
		t.Run(mode, func(t *testing.T) {
			p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
				switch mode {
				case "prose":
					return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: "ok"}), nil
				case "nonce":
					return browserCall("p", "browser_capability_check", `{"nonce":"wrong"}`), nil
				case "multiple":
					return controlChunks(provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "1"}}, provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "2"}}), nil
				default:
					ch := make(chan provider.Chunk)
					close(ch)
					return ch, nil
				}
			}}
			if probeBrowserTools(context.Background(), p) == nil {
				t.Fatal("unqualified provider accepted")
			}
		})
	}
}
