package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
)

// Delay delivery before Lifecycle, modeling preemption between publishing idle
// and acquiring the lifecycle sink mutex. No production code is replaced.
func TestCancellationCompletionCannotCorruptSuccessorLifecycle(t *testing.T) {
	events := make(chan event.Event, 32)
	oldDoneReached := make(chan struct{})
	releaseOldDone := make(chan struct{})
	oldDoneDelivered := make(chan struct{})
	releaseSuccessor := make(chan struct{})
	successorDone := make(chan struct{})
	lifecycle := event.Lifecycle(event.FuncSink(func(e event.Event) { events <- e }))
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnDone && e.Outcome == event.TurnOutcomeCancelled {
			close(oldDoneReached)
			<-releaseOldDone
			lifecycle.Emit(e)
			close(oldDoneDelivered)
			return
		}
		lifecycle.Emit(e)
		if e.Kind == event.TurnDone {
			close(successorDone)
		}
	})
	c := New(Options{Sink: sink})
	started := make(chan string, 2)
	c.runGuarded(func(ctx context.Context) error {
		id, _ := agent.ParentTurn(ctx)
		sink.Emit(event.Event{Kind: event.TurnStarted, TurnID: id})
		started <- id
		<-ctx.Done()
		return ctx.Err()
	})
	wait := func(ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for lifecycle boundary")
		}
	}
	oldID := <-started
	c.CancelTurn(oldID)
	wait(oldDoneReached)
	// A corrected controller may hold admission closed until terminal delivery.
	serialized := c.Running()
	if serialized {
		close(releaseOldDone)
		wait(oldDoneDelivered)
		deadline := time.Now().Add(3 * time.Second)
		for c.Running() && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
	}
	c.runGuarded(func(ctx context.Context) error {
		id, _ := agent.ParentTurn(ctx)
		sink.Emit(event.Event{Kind: event.TurnStarted, TurnID: id})
		sink.Emit(event.Event{Kind: event.Text, Text: "before old completion"})
		started <- id
		<-releaseSuccessor
		sink.Emit(event.Event{Kind: event.Text, Text: "after old completion"})
		return nil
	})
	var newID string
	select {
	case newID = <-started:
	case <-time.After(3 * time.Second):
		close(releaseSuccessor)
		t.Fatal("successor was not admitted after terminal delivery")
	}
	if !serialized {
		close(releaseOldDone)
		wait(oldDoneDelivered)
	}
	status := c.TurnStatus()
	close(releaseSuccessor)
	wait(successorDone)
	if !status.Running || status.TurnID != newID || status.Outcome != "" {
		t.Errorf("old completion changed successor controller status: %+v", status)
	}
	close(events)
	var starts int
	for e := range events {
		if e.Kind == event.TurnStarted {
			starts++
			if starts == 2 && e.TurnID != newID {
				t.Errorf("successor start identity = %q, want %q (old %q)", e.TurnID, newID, oldID)
			}
		}
		if e.Kind == event.Text && e.TurnID != newID {
			t.Errorf("successor text %q identity = %q, want %q", e.Text, e.TurnID, newID)
		}
	}
}

type cancellationReviewRunner struct{ err error }

func (r cancellationReviewRunner) Run(context.Context, string) error { return r.err }

func TestSynchronousRunTurnRecordsTerminalOutcome(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want event.TurnOutcome
	}{
		{"success", nil, event.TurnOutcomeSuccess},
		{"failure", errors.New("provider failed"), event.TurnOutcomeFailed},
		{"cancelled", context.Canceled, event.TurnOutcomeCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(Options{Runner: cancellationReviewRunner{err: tc.err}})
			err := c.RunTurn(context.Background(), "test")
			if !errors.Is(err, tc.err) {
				t.Fatalf("RunTurn error = %v, want %v", err, tc.err)
			}
			status := c.TurnStatus()
			if status.Running || status.CancelRequested || status.TurnID == "" || status.Outcome != tc.want {
				t.Fatalf("terminal status = %+v, want %s", status, tc.want)
			}
		})
	}
}
