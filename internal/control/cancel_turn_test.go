package control

import (
	"context"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
)

func TestCancelTurnIdentityAndUnresponsiveWork(t *testing.T) {
	done := make(chan event.Event, 4)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnDone {
			done <- e
		}
	})})
	started := make(chan string, 1)
	release := make(chan struct{})
	c.runGuarded(func(ctx context.Context) error {
		id, _ := agent.ParentTurn(ctx)
		started <- id
		<-release
		return ctx.Err()
	})
	id := <-started
	if id == "" || c.TurnStatus().TurnID != id {
		t.Fatal("turn identity missing before any model output")
	}
	if ack := c.CancelTurn("stale"); ack.Accepted {
		t.Fatal("stale cancellation accepted")
	}
	c.SetPaused(true)
	if ack := c.CancelTurn(id); !ack.Accepted || !ack.Running || !ack.CancelRequested {
		t.Fatalf("cancel ack: %+v", ack)
	}
	if !c.Running() {
		t.Fatal("uncancellable work incorrectly reported idle")
	}
	close(release)
	select {
	case e := <-done:
		if e.TurnID != id || e.Outcome != event.TurnOutcomeCancelled {
			t.Fatalf("terminal event: %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	if c.Paused() || c.TurnStatus().CancelRequested {
		t.Fatal("terminal state retained pause/cancel")
	}
	next := make(chan struct{})
	c.runGuarded(func(ctx context.Context) error { close(next); <-ctx.Done(); return ctx.Err() })
	<-next
	if c.CancelTurn(id).Accepted {
		t.Fatal("old cancellation stopped next turn")
	}
	if !c.CancelTurn(c.TurnStatus().TurnID).Accepted {
		t.Fatal("new turn cancellation rejected")
	}
	<-done
}
