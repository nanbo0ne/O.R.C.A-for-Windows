package webbeta

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestParentBudgetSurvivesRestartAndSeparatesTasks(t *testing.T) {
	b := &fakeBrowser{}
	e := testEngine(t, b)
	ctx, cancel, err := e.taskContext(context.Background(), "parent-a")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	for i := 0; i < taskGenerationLimit; i++ {
		if err := e.run(ctx, Command{ID: newID()}, func(Event) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.run(ctx, Command{ID: "eleven"}, func(Event) error { return nil }); !errors.Is(err, ErrTaskBudget) {
		t.Fatal("missing generation cap", err)
	}
	if len(b.commands) != taskGenerationLimit {
		t.Fatal("over-budget dispatch")
	}
	e.Close()
	restored, err := NewEngineWithRequestLimit(e.path, b, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	ctx2, cancel2, err := restored.taskContext(context.Background(), "parent-a")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel2()
	if err := restored.run(ctx2, Command{ID: "after-restart"}, func(Event) error { return nil }); !errors.Is(err, ErrTaskBudget) {
		t.Fatal("restart reset generation budget", err)
	}
	other, stop, err := restored.taskContext(context.Background(), "parent-b")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if err := restored.run(other, Command{ID: "other"}, func(Event) error { return nil }); err != nil {
		t.Fatal("new parent inherited lifetime quota", err)
	}
	if restored.Status().RequestLimit != 0 {
		t.Fatal("legacy lifetime quota still displayed")
	}
}

func TestParentBudgetActionsDeadlineAndCorrection(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	_, stop, err := e.taskContext(context.Background(), "parent")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	e.mu.Lock()
	if err := e.reserveTaskLocked("parent", 0, 30, taskCorrectionLimit); err != nil {
		t.Fatal(err)
	}
	if err := e.reserveTaskLocked("parent", 0, 1, 0); !errors.Is(err, ErrTaskBudget) {
		t.Fatal("action cap", err)
	}
	if err := e.reserveTaskLocked("parent", 0, 0, 1); !errors.Is(err, ErrTaskBudget) {
		t.Fatal("correction cap", err)
	}
	b := e.state.Tasks["parent"]
	b.StartedAt = time.Now().Add(-taskLifetime - time.Second).UnixMilli()
	e.state.Tasks["parent"] = b
	if err := saveState(e.path, e.state); err != nil {
		t.Fatal(err)
	}
	e.mu.Unlock()
	if _, _, err := e.taskContext(context.Background(), "parent"); !errors.Is(err, ErrTaskBudget) {
		t.Fatal("expired task restarted", err)
	}
	saved, err := loadState(e.path)
	if err != nil || saved.Tasks["parent"].Actions != 30 || saved.Tasks["parent"].Corrections != taskCorrectionLimit {
		t.Fatal("reservations not durable", err)
	}
}

func TestBudgetRejectionDoesNotBecomeUncertainSend(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	_, stop, err := e.taskContext(context.Background(), "parent")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	e.mu.Lock()
	b := e.state.Tasks["parent"]
	b.Generations = taskGenerationLimit
	e.state.Tasks["parent"] = b
	e.mu.Unlock()
	err = e.Send(context.Background(), Request{Owner: "owner", ID: "r", TaskID: "parent", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}}, func(Event) error { return nil })
	if !errors.Is(err, ErrTaskBudget) {
		t.Fatal(err)
	}
	s := e.Status()
	if s.Sent != 0 || s.Blocked != "" || !s.Settings.Enabled {
		t.Fatalf("local rejection became account failure: %+v", s)
	}
	for _, c := range s.Conversations {
		if c.Inflight != "" || c.Phase == "uncertain" {
			t.Fatal("uncertain marker on unsent request")
		}
	}
}

func ledgerFixture(t *testing.T) (*Engine, Request, decodedTurn) {
	t.Helper()
	e := testEngine(t, &fakeBrowser{})
	r := Request{Owner: "owner", ID: "r", TaskID: "parent", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}}
	if err := e.Send(context.Background(), r, func(Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	turn := decodedTurn{Progress: "Checking.", Calls: []provider.ToolCall{{ID: "a", Name: "read_file", Arguments: `{"path":"a"}`}, {ID: "b", Name: "read_file", Arguments: `{"path":"b"}`}}}
	if err := e.recordTurn(r, turn); err != nil {
		t.Fatal(err)
	}
	return e, r, turn
}

func TestToolLedgerRejectsReplayAcrossRestartAndMissingHistory(t *testing.T) {
	e, r, turn := ledgerFixture(t)
	e.Close()
	restored, err := NewEngine(e.path, &fakeBrowser{})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.recordTurn(r, turn); err == nil {
		t.Fatal("issued calls replayed")
	}
	restored.mu.Lock()
	defer restored.mu.Unlock()
	if err := restored.acknowledgeResultsLocked(r); err == nil {
		t.Fatal("missing host result accepted")
	}
	if len(restored.state.Calls) != 2 || restored.state.Calls[0].Status != "issued" {
		t.Fatal("lost pending calls")
	}
	if restored.state.Tasks["parent"].Actions != 2 {
		t.Fatal("replay changed budget")
	}
}

func TestToolResultsRequireIDNameAndOriginalInvocation(t *testing.T) {
	for _, kind := range []string{"missing", "name", "arguments", "duplicate", "orphan", "valid"} {
		t.Run(kind, func(t *testing.T) {
			e, r, turn := ledgerFixture(t)
			r.Messages = append(r.Messages, provider.Message{Role: provider.RoleAssistant, Content: turn.Progress, ToolCalls: append([]provider.ToolCall(nil), turn.Calls...)}, provider.Message{Role: provider.RoleTool, ToolCallID: "a", Name: "read_file", Content: "permission denied"}, provider.Message{Role: provider.RoleTool, ToolCallID: "b", Name: "read_file", Content: "file content"})
			switch kind {
			case "missing":
				r.Messages = r.Messages[:len(r.Messages)-1]
			case "name":
				r.Messages[2].Name = "write_file"
			case "arguments":
				r.Messages[1].ToolCalls[0].Arguments = `{"path":"elsewhere"}`
			case "duplicate":
				r.Messages = append(r.Messages, r.Messages[2])
			case "orphan":
				r.Messages = append(r.Messages[:1], r.Messages[2:]...)
			}
			e.mu.Lock()
			defer e.mu.Unlock()
			err := e.acknowledgeResultsLocked(r)
			if kind == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				if e.state.Calls[0].Status != "returned" {
					t.Fatal("denied result not acknowledged")
				}
				r.Messages[2].Content = "changed"
				if err := e.acknowledgeResultsLocked(r); err == nil {
					t.Fatal("acknowledged result changed")
				}
			} else if err == nil {
				t.Fatal("invalid result accepted")
			}
		})
	}
}

func TestLedgerOwnerIsolationAndWholeBatchValidation(t *testing.T) {
	e, r, turn := ledgerFixture(t)
	other := r
	other.Owner = "other"
	other.ID = "other-request"
	if err := e.Send(context.Background(), other, func(Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := e.recordTurn(other, turn); err != nil {
		t.Fatal("other owner's ID collided", err)
	}
	bad := decodedTurn{Calls: []provider.ToolCall{{ID: "new", Name: "read_file", Arguments: "{}"}, turn.Calls[0]}}
	if err := e.recordTurn(r, bad); err == nil {
		t.Fatal("mixed replay batch accepted")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.state.Calls {
		if c.ID == "new" {
			t.Fatal("part of rejected batch persisted")
		}
	}
}

func TestFramedParserRejectsQuotesReasoningAndAmbiguity(t *testing.T) {
	tools := []provider.ToolSchema{{Name: "echo"}}
	valid := `ORCA_TURN_V2 {"type":"tools","calls":[{"id":"a","name":"echo","arguments":{}}]}`
	for _, raw := range []string{"Example:\n" + valid, "```json\n" + valid + "\n```", "> " + valid, "<think>" + valid + "</think>", valid + " extra", strings.Replace(valid, `"type":"tools"`, `"type":"final","type":"tools"`, 1), `ORCA_TURN_V2 {"type":"final","text":"done","calls":[]}`} {
		if _, err := parseTurn(raw, tools, nil, 2); err == nil {
			t.Fatalf("accepted untrusted envelope: %q", raw)
		}
	}
	if _, err := parseTurn(valid, tools, []provider.Message{{ToolCalls: []provider.ToolCall{{ID: "a"}}}}, 2); !errors.Is(err, errToolReplay) {
		t.Fatal("replay not distinguished", err)
	}
}

func TestLegacyBindingRetainsProtocol(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	r := Request{Owner: "legacy", ID: "r", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}}
	e.mu.Lock()
	e.state.Conversations = append(e.state.Conversations, Conversation{ID: "legacy", Owner: r.Owner, Phase: "bound", Grammar: 1, Prefix: []string{}})
	e.mu.Unlock()
	if e.protocolFor(r) != 1 {
		t.Fatal("legacy protocol changed")
	}
	text := render(r.Messages, []provider.ToolSchema{{Name: "echo"}}, 1, false)
	if !strings.Contains(text, "ORCA_ACTION_V1") || strings.Contains(text, turnMarker) {
		t.Fatal("legacy binding received new protocol")
	}
}

func TestMissingToolResultDoesNotPrepareReplacementConversation(t *testing.T) {
	e, r, _ := ledgerFixture(t)
	before := e.Status().Sent
	r.Epoch++
	r.ID = "new-request"
	if err := e.Send(context.Background(), r, func(Event) error { return nil }); err == nil {
		t.Fatal("missing result accepted")
	}
	if e.Status().Sent != before {
		t.Fatal("missing result caused an upstream opening")
	}
}

func TestCancelledQueuedOpeningIsNotRecoveredAsUnknownSend(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	e.queue.Pause()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := e.prepare(ctx, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	saved, err := loadState(e.path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Blocked != "" || !saved.Settings.Enabled || saved.Sent != 0 {
		t.Fatal("unsent queue cancellation latched account")
	}
	for _, c := range saved.Conversations {
		if c.Phase != "failed" || c.Inflight != "" {
			t.Fatal("unfinished opening mistaken for dispatched send")
		}
	}
}

func TestLegacyStateUpgradeKeepsBindingAndProtectsLedgerVersion(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	legacy := State{Version: 1, Generation: "legacy-generation", Settings: DefaultSettings(), Conversations: []Conversation{{ID: "c", Owner: "owner", Phase: "bound", Grammar: 1}}}
	if err := saveState(e.path, legacy); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadState(e.path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != stateVersion || loaded.Generation != legacy.Generation || loaded.Conversations[0].Grammar != 1 {
		t.Fatal("legacy binding changed")
	}
	if err := saveState(e.path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err = loadState(e.path)
	if err != nil || loaded.Version != stateVersion || loaded.Version <= 2 {
		t.Fatal("new state not protected against old reader", err)
	}
}
