package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"strings"
	"testing"
)

func TestWorkMonitorSubscriptionAndEvents(t *testing.T) {
	a := &App{activeTabID: "a", tabs: map[string]*WorkspaceTab{"a": {}, "b": {}}}
	if _, err := a.MonitorSubscribe("b"); err == nil {
		t.Fatal("inactive tab subscribed")
	}
	v, err := a.MonitorSubscribe("a")
	if err != nil {
		t.Fatal(err)
	}
	s := &tabEventSink{app: a, tabID: "a"}
	s.observeWorkMonitor(event.Event{Kind: event.TurnStarted, TurnID: "turn"})
	s.observeWorkMonitor(event.Event{Kind: event.Reasoning, TurnID: "turn", Text: "POST_HOOK_DISPLAY"})
	s.observeWorkMonitor(event.Event{Kind: event.Usage, TurnID: "turn", RequestID: "provider-request", Usage: &provider.Usage{PromptTokens: 7}})
	s.observeWorkMonitor(event.Event{Kind: event.ChildStarted, ParentTurnID: "turn", ChildID: "child"})
	s.observeWorkMonitor(event.Event{Kind: event.ToolDispatch, TurnID: "turn", Tool: event.Tool{ID: "tool", Args: `{"api_key":"TOOL_SECRET","query":"visible"}`}})
	s.observeWorkMonitor(event.Event{Kind: event.TurnDone, TurnID: "turn", Outcome: event.TurnOutcomeSuccess, FinalMessageID: "final"})
	s.observeWorkMonitor(event.Event{Kind: event.TurnDone, TurnID: "turn", Outcome: event.TurnOutcomeSuccess})
	got := a.MonitorSnapshot(v.Generation, 0)
	raw, _ := json.Marshal(got)
	if !strings.Contains(string(raw), "POST_HOOK_DISPLAY") || strings.Contains(string(raw), "TOOL_SECRET") || !strings.Contains(string(raw), "provider-request") {
		t.Fatalf("privacy/receipt: %s", raw)
	}
	if got.Entries[len(got.Entries)-1].Phase != "final" {
		t.Fatal("duplicate completion changed final")
	}
	v2, _ := a.MonitorSubscribe("a")
	a.MonitorUnsubscribe(v.Generation)
	if a.MonitorSnapshot(v2.Generation, 0).Expired {
		t.Fatal("stale cleanup closed current generation")
	}
	a.mu.Lock()
	a.activeTabID = "b"
	a.mu.Unlock()
	if a.monitorForTab("a") != nil || !a.MonitorSnapshot(v2.Generation, 0).Expired {
		t.Fatal("tab switch still capturing")
	}
	a.MonitorUnsubscribe(v2.Generation)
}

func TestWorkMonitorCompletionSemantics(t *testing.T) {
	for _, tc := range []struct {
		e     event.Event
		phase string
	}{
		{event.Event{Kind: event.TurnDone, Outcome: event.TurnOutcomeSuccess}, "stopped"},
		{event.Event{Kind: event.TurnDone, Outcome: event.TurnOutcomeSuccess, FinalMessageID: "m"}, "final"},
		{event.Event{Kind: event.TurnDone, Outcome: event.TurnOutcomeCancelled, FinalMessageID: "m"}, "stopped"},
		{event.Event{Kind: event.TurnDone, Outcome: event.TurnOutcomeFailed}, "error"},
	} {
		if got := monitorPhase(tc.e); got != tc.phase {
			t.Fatalf("%s != %s", got, tc.phase)
		}
	}
	s := &tabEventSink{}
	_ = s.WorkMonitorContext(context.Background(), "t")
	s.observeWorkMonitor(event.Event{Kind: event.Text, Text: "hidden"})
}

func TestWorkMonitorResponseToolPrivacyAndIncompleteDiagnostics(t *testing.T) {
	a := &App{activeTabID: "a", tabs: map[string]*WorkspaceTab{"a": {}}}
	v, err := a.MonitorSubscribe("a")
	if err != nil {
		t.Fatal(err)
	}
	defer a.MonitorUnsubscribe(v.Generation)
	s := &tabEventSink{app: a, tabID: "a"}
	const secret = "SYNTHETIC_PRIVATE_VALUE"
	s.observeWorkMonitor(event.Event{Kind: event.Text, TurnID: "turn", Text: `{"token":"` + secret + `"}`})
	s.observeWorkMonitor(event.Event{Kind: event.ToolResult, TurnID: "turn", Tool: event.Tool{ID: "tool", Args: `{"input":"{\"token\":\"` + secret + `\"}"}`, Output: `prefix "token": "` + secret + `"`}})
	s.observeWorkMonitor(event.Event{Kind: event.Retrying, TurnID: "turn", RetryAttempt: 1, RetryMax: 3, Text: "https://private.invalid/" + secret, Err: errors.New(secret)})
	s.observeWorkMonitor(event.Event{Kind: event.Text, TurnID: "turn", Text: "continued post-Hook output"})
	s.observeWorkMonitor(event.Event{Kind: event.TurnDone, TurnID: "turn", Outcome: event.TurnOutcomeFailed, Err: errors.New("https://private.invalid/" + secret)})
	got := a.MonitorSnapshot(v.Generation, 0)
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "private.invalid") {
		t.Fatalf("unsafe diagnostic/payload: %s", raw)
	}
	diagnostics := 0
	for _, entry := range got.Entries {
		if entry.Kind == "diagnostic" {
			diagnostics++
			if !entry.Incomplete {
				t.Fatal("diagnostic falsely complete")
			}
		}
	}
	if diagnostics != 2 || !strings.Contains(string(raw), "continued post-Hook output") {
		t.Fatalf("retry/terminal diagnostics missing: %s", raw)
	}
	s.WorkMonitorState("compact", "error")
	last := a.MonitorSnapshot(v.Generation, got.Cursor).Entries
	if len(last) != 2 || last[1].Kind != "diagnostic" || !last[1].Incomplete {
		t.Fatal("control failure lacks incomplete marker")
	}
}
