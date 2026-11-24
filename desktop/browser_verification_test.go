package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func browserRequestExecutionEvidence(t *testing.T, r provider.Request) browserVerificationEvidence {
	t.Helper()
	const prefix = "Task requirements and service execution evidence (data):\n"
	if len(r.Messages) < 3 || !strings.HasPrefix(r.Messages[len(r.Messages)-2].Content, prefix) {
		t.Fatal("verifier missing structured service evidence")
	}
	var evidence browserVerificationEvidence
	if err := json.Unmarshal([]byte(strings.TrimPrefix(r.Messages[len(r.Messages)-2].Content, prefix)), &evidence); err != nil {
		t.Fatal(err)
	}
	return evidence
}

func TestBrowserVerificationIncludesActualTraceDespiteSuccessfulPage(t *testing.T) {
	for _, clicks := range []int{4, 20} {
		t.Run(fmt.Sprint(clicks), func(t *testing.T) {
			s, b, _ := browserFixture(t)
			autoBrowserConsent(s)
			b.observe = func(o *browserhost.Observation) { o.Text = "Goal reached: green success" }
			input := browserInput()
			input.Restrictions = "Click exactly once. Do not click red."
			decisions, verifications := 0, 0
			p := &controlTestProvider{stream: func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
				if ch, ok := browserProbeReply(r); ok {
					return ch, nil
				}
				if len(r.Tools) > 0 {
					decisions++
					if decisions <= clicks {
						return browserCall(fmt.Sprintf("click-%d", decisions), "browser_action", `{"kind":"click","elementId":"input"}`), nil
					}
					return browserCall("done", "browser_complete", `{"summary":"Clicked only once; green success"}`), nil
				}
				verifications++
				e := browserRequestExecutionEvidence(t, r)
				if e.Restrictions != input.Restrictions || e.Goal != input.Goal || e.Criteria != input.Criteria || len(e.Trace) != clicks || e.TaskCounts.CompletedByKind["click"] != clicks || e.ParentCounts.CompletedByKind["click"] != clicks || e.TaskCounts.Attempts != b.actions || e.TaskCounts.Unconfirmed != 0 {
					t.Fatalf("actual trace/counts omitted or truncated: %+v", e)
				}
				for i, receipt := range e.Trace {
					if receipt.Sequence != i+1 || receipt.CallID != fmt.Sprintf("click-%d", i+1) || receipt.Outcome != "native_completed" || receipt.Action.Kind != "click" || receipt.Action.ElementID != "input" || receipt.Action.Generation == 0 || receipt.TaskID != e.TaskID {
						t.Fatalf("incorrect native receipt: %+v", receipt)
					}
				}
				if !strings.Contains(r.Messages[len(r.Messages)-3].Content, "cannot be confirmed") || !strings.Contains(r.Messages[len(r.Messages)-1].Content, "green success") || strings.Contains(r.Messages[len(r.Messages)-2].Content, "Clicked only once") {
					t.Fatal("fresh success must not replace restrictions or service trace")
				}
				return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: `{"satisfied":false,"reason":"Recorded click count violates the restriction despite eventual success."}`}), nil
			}}
			result, err := s.run(computerParentTestContext(t), "owner", "outer", input, p, "fake/model")
			var rejected *browserVerificationError
			if !errors.As(err, &rejected) || rejected.Kind != "negative_evidence" || result != "" || b.actions != clicks || verifications != 1 {
				t.Fatal("restriction violation accepted or verification retried", result, err, b.actions, verifications)
			}
		})
	}
}

func TestBrowserVerificationSeparatesStaleAndPriorTaskCounts(t *testing.T) {
	s, b, _ := browserFixture(t)
	h := &browserPredispatchFake{browserFake: b, staleAttempts: 1}
	s.host = h
	autoBrowserConsent(s)
	ctx := computerParentTestContext(t)
	decisions, verifies := 0, 0
	p := &controlTestProvider{stream: func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		if ch, ok := browserProbeReply(r); ok {
			return ch, nil
		}
		if len(r.Tools) > 0 {
			decisions++
			if decisions <= 2 {
				return browserCall(fmt.Sprintf("wait-%d", decisions), "browser_action", `{"kind":"wait"}`), nil
			}
			return browserCall(fmt.Sprintf("done-%d", decisions), "browser_complete", `{"summary":"done"}`), nil
		}
		verifies++
		e := browserRequestExecutionEvidence(t, r)
		if e.ParentCounts.Attempts != 2 || e.ParentCounts.Completed != 1 || e.ParentCounts.StaleNoInput != 1 || e.ParentCounts.CompletedByKind["wait"] != 1 {
			t.Fatalf("stale counted as dispatched: %+v", e)
		}
		if verifies == 1 {
			if len(e.Trace) != 2 || e.Trace[0].Outcome != "stale_no_input" || e.Trace[1].Outcome != "native_completed" || e.TaskCounts.Completed != 1 {
				t.Fatalf("missing safe-stale receipt: %+v", e)
			}
		} else if len(e.Trace) != 0 || e.TaskCounts.Attempts != 0 {
			t.Fatal("prior task payload exposed as current task evidence")
		}
		return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: `{"satisfied":true,"reason":"synthetic evidence"}`}), nil
	}}
	for _, id := range []string{"first", "second"} {
		if _, err := s.run(ctx, "owner", id, browserInput(), p, "fake/model"); err != nil {
			t.Fatal(err)
		}
	}
	if verifies != 2 || h.attempts != 2 || b.actions != 1 {
		t.Fatal(verifies, h.attempts, b.actions)
	}
}

func TestBrowserVerificationRejectsIncompleteExecutionEvidence(t *testing.T) {
	for _, missing := range []bool{true, false} {
		s, _, id := browserFixture(t)
		task, end, err := s.beginTask(computerParentTestContext(t), "owner", "outer", id, "fake/model")
		if err != nil {
			t.Fatal(err)
		}
		s.mu.Lock()
		task.budget.actions = 1
		if !missing {
			s.startExecutionLocked(task, "uncertain", "https://example.test", browserhost.Action{Kind: "click"})
		}
		s.mu.Unlock()
		_, err = s.verificationMessages(task, browserInput(), provider.Message{Content: "success"})
		var rejected *browserVerificationError
		if !errors.As(err, &rejected) || rejected.Kind != "execution_unconfirmed" {
			t.Fatal("incomplete trace accepted", err)
		}
		end(err)
	}
}

func TestBrowserVerificationFormatsAndSanitizedFailures(t *testing.T) {
	for _, tc := range []struct{ name, body, kind string }{
		{"json", `{"satisfied":true,"reason":"fresh evidence"}`, ""},
		{"fenced", "```json\n{\"satisfied\":true,\"reason\":\"fresh evidence\"}\n```", ""},
		{"fenced-crlf", "```json\r\n{\"satisfied\":true,\"reason\":\"fresh evidence\"}\r\n```", ""},
		{"negative", `{"satisfied":false,"reason":"secret page text"}`, "negative_evidence"},
		{"empty", "", "empty_response"},
		{"thinking", `<think>secret page text</think>{"satisfied":true,"reason":"x"}`, "non_json_response"},
		{"prose", `Done: {"satisfied":true,"reason":"secret page text"}`, "non_json_response"},
		{"truncated", `{"satisfied":true,"reason":"secret page text`, "invalid_json"},
		{"missing", `{"satisfied":true}`, "missing_fields"},
		{"null", `{"satisfied":null,"reason":"x"}`, "missing_fields"},
		{"duplicate", `{"satisfied":false,"satisfied":true,"reason":"x"}`, "duplicate_field"},
		{"unknown", `{"satisfied":true,"reason":"x","extra":true}`, "unknown_field"},
		{"trailing", `{"satisfied":true,"reason":"x"} false`, "trailing_content"},
		{"fenced-with-prose", "```json\n{\"satisfied\":true,\"reason\":\"x\"}\n```\nActually false", "invalid_fence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := browserVerifyVerdict(tc.body, nil)
			if tc.kind == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var diagnostic *browserVerificationError
			if !errors.As(err, &diagnostic) || diagnostic.Kind != tc.kind || strings.Contains(err.Error(), "secret page text") {
				t.Fatalf("wrong or unsanitized failure: %v", err)
			}
		})
	}
	if err := browserVerifyVerdict(`{"satisfied":true,"reason":"x"}`, []provider.ToolCall{{Name: "browser_action"}}); err == nil {
		t.Fatal("verification tool call accepted")
	}
}

func TestBrowserResponseTruncationCannotConfirmOrDispatch(t *testing.T) {
	for _, finish := range []string{"length", "content_filter", "repetition_truncation", "unknown"} {
		p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
			return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: `{"satisfied":true,"reason":"looks complete"}`}, provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: finish}}), nil
		}}
		body, calls, err := collectBrowserResponse(context.Background(), p, nil, nil)
		var diagnostic *browserResponseError
		if !errors.As(err, &diagnostic) || body != "" || len(calls) != 0 {
			t.Fatalf("abnormal %s output survived: %q %v", finish, body, err)
		}
	}
}

type browserPredispatchFake struct {
	*browserFake
	attempts, staleAttempts int
}

func (b *browserPredispatchFake) Execute(ctx context.Context, id string, a browserhost.Action) error {
	b.attempts++
	if b.attempts <= b.staleAttempts {
		return browserhost.ErrStaleObservation
	}
	return b.browserFake.Execute(ctx, id, a)
}

func TestBrowserPredispatchStaleReplansWithNewObservation(t *testing.T) {
	s, b, _ := browserFixture(t)
	h := &browserPredispatchFake{browserFake: b, staleAttempts: 1}
	s.host = h
	autoBrowserConsent(s)
	decisions := 0
	var firstEvidence string
	p := browserCompleter(func(r provider.Request) <-chan provider.Chunk {
		decisions++
		evidence := r.Messages[len(r.Messages)-1].Content
		if decisions == 1 {
			firstEvidence = evidence
			return browserCall("old-action", "browser_action", `{"kind":"wait"}`)
		}
		if decisions == 2 {
			staleResults := 0
			for _, message := range r.Messages {
				if message.Role == provider.RoleTool && message.ToolCallID == "old-action" {
					if message.Name != "browser_action" || message.Content != browserStaleReceipt {
						t.Fatal("stale response lost its correlated execution result")
					}
					staleResults++
				}
			}
			if evidence == firstEvidence || staleResults != 1 {
				t.Fatal("stale response was replayed without fresh evidence/receipt")
			}
			return browserCall("new-action", "browser_action", `{"kind":"wait"}`)
		}
		return browserCall("done", "browser_complete", `{"summary":"done"}`)
	})
	if _, err := s.run(computerParentTestContext(t), "owner", "outer", browserInput(), p, "fake/model"); err != nil {
		t.Fatal(err)
	}
	if h.attempts != 2 || b.actions != 1 || decisions != 3 {
		t.Fatalf("attempts=%d actions=%d decisions=%d", h.attempts, b.actions, decisions)
	}
}

func TestBrowserStaleReplanCannotReuseCallOrResetBudget(t *testing.T) {
	for _, repeatID := range []bool{true, false} {
		t.Run(fmt.Sprint(repeatID), func(t *testing.T) {
			s, b, _ := browserFixture(t)
			h := &browserPredispatchFake{browserFake: b, staleAttempts: 20}
			s.host = h
			autoBrowserConsent(s)
			decisions := 0
			p := browserCompleter(func(provider.Request) <-chan provider.Chunk {
				decisions++
				id := "same-action"
				if !repeatID {
					id = fmt.Sprintf("action-%d", decisions)
				}
				return browserCall(id, "browser_action", `{"kind":"wait"}`)
			})
			ctx := computerParentTestContext(t)
			if _, err := s.run(ctx, "owner", "outer", browserInput(), p, "fake/model"); err == nil {
				t.Fatal("unbounded stale recovery")
			}
			want := browserMaxStaleReplans + 1
			if repeatID {
				want = 1
			}
			if h.attempts != want || b.actions != 0 {
				t.Fatal(h.attempts, b.actions)
			}
			before := decisions
			if _, err := s.run(ctx, "owner", "new-parent-call", browserInput(), p, "fake/model"); !errors.Is(err, errBrowserStopped) || decisions != before {
				t.Fatal("new call reopened stopped parent budget", err)
			}
		})
	}
}

func TestBrowserUncertainInputNeverReplans(t *testing.T) {
	s, b, _ := browserFixture(t)
	autoBrowserConsent(s)
	b.execute = func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > browserNativeTimeout {
			t.Error("input lacks per-action deadline")
		}
		return errors.New("input may have happened; stale observation after dispatch")
	}
	decisions := 0
	p := browserCompleter(func(provider.Request) <-chan provider.Chunk {
		decisions++
		return browserCall("uncertain", "browser_action", `{"kind":"wait"}`)
	})
	ctx := computerParentTestContext(t)
	if _, err := s.run(ctx, "owner", "outer", browserInput(), p, "fake/model"); err == nil {
		t.Fatal("uncertain outcome succeeded")
	}
	if decisions != 1 || b.actions != 1 {
		t.Fatal("uncertain action repeated")
	}
	if _, err := s.run(ctx, "owner", "renamed-parent", browserInput(), p, "fake/model"); !errors.Is(err, errBrowserStopped) || decisions != 1 {
		t.Fatal("parent retry reached model/native", err)
	}
}

func TestBrowserBlockedScreenshotRequiresTakeoverWithoutProbe(t *testing.T) {
	s, b, _ := browserFixture(t)
	autoBrowserConsent(s)
	b.observe = func(o *browserhost.Observation) {
		o.Elements = nil
		o.Screenshot = nil
		o.ScreenshotBlocked = "visible frame cannot be safely inspected"
	}
	p := browserCompleter(func(provider.Request) <-chan provider.Chunk { t.Fatal("blocked image reached provider"); return nil })
	_, err := s.run(computerParentTestContext(t), "owner", "outer", browserInput(), p, "fake/model")
	if err == nil || !strings.Contains(err.Error(), "manual takeover") || strings.Contains(err.Error(), "expand") {
		t.Fatal(err)
	}
}

func TestBrowserScreenshotDenialDoesNotExposeImage(t *testing.T) {
	s, _, id := browserFixture(t)
	task, end, err := s.beginTask(computerParentTestContext(t), "owner", "outer", id, "fake/model")
	if err != nil {
		t.Fatal(err)
	}
	defer end(nil)
	data, _, _, err := computerQualificationImage()
	if err != nil {
		t.Fatal(err)
	}
	screen, _ := base64.StdEncoding.DecodeString(data)
	s.mu.Lock()
	epoch := s.tabs[id].epoch
	s.mu.Unlock()
	o := browserObserved{Observation: browserhost.Observation{TabID: id, URL: "https://example.test", DocumentID: "document", Generation: 1, Screenshot: screen, MIME: "image/png", ViewportWidth: 160, ViewportHeight: 80}, epoch: epoch}
	approvals := 0
	s.emit = func(owner string, state BrowserState) {
		if state.Approval != nil {
			approvals++
			if !strings.Contains(state.Approval.Reason, "Send exactly this current browser screenshot") {
				t.Error("missing exact screenshot consent")
			}
			s.approve(owner, BrowserCommand{ApprovalID: state.Approval.ID, Allow: false})
		}
	}
	p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
		t.Fatal("provider invoked before screenshot consent")
		return nil, nil
	}}
	vision := true
	message, err := s.observationForModel(task, o, p, &vision)
	if err == nil || len(message.Images) != 0 || approvals != 1 {
		t.Fatal("denied screenshot survived", err)
	}
}

func TestBrowserQualifiedVisionKeepsFreshImageWithDOM(t *testing.T) {
	for _, tc := range []struct {
		name      string
		vision    bool
		blocked   string
		wantImage bool
	}{
		{name: "unqualified-dom"},
		{name: "qualified-mixed", vision: true, wantImage: true},
		{name: "qualified-blocked", vision: true, blocked: "protected frame"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, b, id := browserFixture(t)
			task, end, err := s.beginTask(computerParentTestContext(t), "owner", "outer", id, "fake/model")
			if err != nil {
				t.Fatal(err)
			}
			defer end(nil)
			data, _, _, err := computerQualificationImage()
			if err != nil {
				t.Fatal(err)
			}
			screen, _ := base64.StdEncoding.DecodeString(data)
			b.observe = func(o *browserhost.Observation) {
				o.Elements = []browserhost.Element{{ID: "canvas", Role: "canvas"}, {ID: "button", Role: "button"}}
				o.Screenshot, o.MIME, o.ScreenshotBlocked = screen, "image/png", tc.blocked
				o.ViewportWidth, o.ViewportHeight = 160, 80
			}
			approvals := 0
			s.emit = func(owner string, state BrowserState) {
				if state.Approval != nil {
					approvals++
					if !strings.Contains(state.Approval.Reason, "Send exactly this current browser screenshot") {
						t.Error("missing exact image consent")
					}
					s.approve(owner, BrowserCommand{ApprovalID: state.Approval.ID, Allow: true})
				}
			}
			p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
				t.Fatal("capability re-probed or screenshot sent unexpectedly")
				return nil, nil
			}}
			var previous uint64
			for i := 0; i < 2; i++ {
				o, err := s.observeReady(task, tc.vision)
				if err != nil || o.Generation == previous {
					t.Fatal("observation not refreshed", err)
				}
				previous = o.Generation
				m, err := s.observationForModel(task, o, p, &tc.vision)
				if tc.blocked != "" {
					if err == nil || !strings.Contains(err.Error(), "manual takeover") || len(m.Images) != 0 {
						t.Fatal("blocked mixed page exposed", err)
					}
				} else if err != nil || (len(m.Images) == 1) != tc.wantImage {
					t.Fatal("incorrect mixed page image policy", err, len(m.Images))
				}
			}
			if (tc.wantImage && approvals != 2) || (!tc.wantImage && approvals != 0) {
				t.Fatal("incorrect screenshot approval count", approvals)
			}
		})
	}
}

func TestBrowserNavigationErrorEventStopsTask(t *testing.T) {
	s, _, id := browserFixture(t)
	task, end, err := s.beginTask(computerParentTestContext(t), "owner", "outer", id, "fake/model")
	if err != nil {
		t.Fatal(err)
	}
	defer end(context.Canceled)
	s.onEvent(browserhost.Event{TabID: id, Kind: "navigation", URL: "https://example.test/failure"})
	s.onEvent(browserhost.Event{TabID: id, Kind: "error", Error: "navigation failed"})
	state := s.state("owner")
	if state.Tabs[0].Loading || state.Tabs[0].Error == "" || task.ctx.Err() == nil {
		t.Fatal("failed navigation still healthy", state)
	}
}

func TestBrowserCanvasWaitsForApprovedPanelRestoration(t *testing.T) {
	s, b, id := browserFixture(t)
	autoBrowserConsent(s)
	view := BrowserViewport{TabID: id, Visible: true, Width: 640, Height: 480, Scale: 1}
	if err := s.present(context.Background(), "owner", view); err != nil {
		t.Fatal(err)
	}
	task, end, err := s.beginTask(computerParentTestContext(t), "owner", "outer", id, "fake/model")
	if err != nil {
		t.Fatal(err)
	}
	defer end(nil)
	if err = s.consent(task, "https://example.test", "Synthetic consent"); err != nil {
		t.Fatal(err)
	}
	view.Visible = false
	if err = s.present(context.Background(), "owner", view); err != nil {
		t.Fatal(err)
	}
	b.observe = func(o *browserhost.Observation) {
		o.Elements = nil
		if !b.view.Visible {
			o.Screenshot = nil
		}
	}
	done := make(chan browserObserved, 1)
	failed := make(chan error, 1)
	go func() { o, err := s.observeReady(task, false); done <- o; failed <- err }()
	select {
	case <-done:
		t.Fatal("did not wait for approved panel restoration")
	case <-time.After(80 * time.Millisecond):
	}
	view.Visible = true
	if err = s.present(context.Background(), "owner", view); err != nil {
		t.Fatal(err)
	}
	select {
	case observed := <-done:
		if err := <-failed; err != nil || len(observed.Screenshot) == 0 {
			t.Fatal("visible screenshot not refreshed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("panel restoration did not release observation")
	}
}

func TestBrowserShutdownUnconfirmedRetainsSnapshot(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint(legacy), func(t *testing.T) {
			s, b, id := browserFixture(t)
			snap, err := newBrowserUploadSnapshot(syntheticUploadFile(t), s.root)
			if err != nil {
				t.Fatal(err)
			}
			defer snap.remove()
			s.snapshots[id] = []*browserUploadSnapshot{snap}
			b.shutdownErr = context.DeadlineExceeded
			if legacy {
				s.host = struct{ browserhost.Backend }{b}
			}
			s.shutdown()
			if _, err := os.Stat(snap.path); err != nil {
				t.Fatal("unconfirmed shutdown deleted renderer-held snapshot", err)
			}
		})
	}
}
