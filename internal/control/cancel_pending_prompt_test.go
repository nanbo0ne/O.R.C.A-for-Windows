package control

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
)

func pendingCancelReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for controller cancellation/prompt boundary")
		var zero T
		return zero
	}
}

func pendingCancelAssertCleared(t *testing.T, c *Controller) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.approvals) != 0 || len(c.asks) != 0 || c.paused || c.pauseWait != nil || c.cancelRequested || c.running {
		t.Fatalf("pending state: approvals=%d asks=%d paused=%v pauseWait=%v cancel=%v running=%v",
			len(c.approvals), len(c.asks), c.paused, c.pauseWait != nil, c.cancelRequested, c.running)
	}
}

func TestCancelVisiblePendingPromptAndIgnoreLateReply(t *testing.T) {
	for _, kind := range []string{"approval", "ask"} {
		t.Run(kind, func(t *testing.T) {
			prompts := make(chan string, 4)
			done := make(chan event.Event, 4)
			c := New(Options{Sink: event.FuncSink(func(e event.Event) {
				switch e.Kind {
				case event.ApprovalRequest:
					prompts <- e.Approval.ID
				case event.AskRequest:
					prompts <- e.Ask.ID
				case event.TurnDone:
					done <- e
				}
			})})
			t.Cleanup(func() { c.Cancel(); c.Close() })
			type replyResult struct {
				allow   bool
				answers []event.AskAnswer
				err     error
			}
			results := make(chan replyResult, 2)
			start := func() {
				c.runGuarded(func(ctx context.Context) error {
					var result replyResult
					if kind == "approval" {
						result.allow, _, result.err = (gateApprover{c}).Approve(ctx, "bash", "synthetic-command", nil)
					} else {
						result.answers, result.err = c.Ask(ctx, []event.AskQuestion{{
							ID: "choice", Prompt: "Choose a synthetic result", Options: []event.AskOption{{Label: "current"}, {Label: "stale"}},
						}})
					}
					results <- result
					return result.err
				})
			}
			reply := func(id, label string) {
				if kind == "approval" {
					// A stale reply must not grant permission to the successor.
					c.Approve(id, true, label == "stale", false)
				} else {
					c.AnswerQuestion(id, []event.AskAnswer{{QuestionID: "choice", Selected: []string{label}}})
				}
			}

			start()
			oldPrompt := pendingCancelReceive(t, prompts)
			oldTurn := c.TurnStatus().TurnID
			if oldPrompt == "" || oldTurn == "" {
				t.Fatal("visible prompt/turn identity missing")
			}
			c.mu.Lock()
			pending := len(c.approvals) + len(c.asks)
			c.mu.Unlock()
			if pending != 1 {
				t.Fatalf("visible prompt not pending: %d", pending)
			}
			if ack := c.CancelTurn(oldTurn); !ack.Accepted || !ack.CancelRequested {
				t.Fatalf("cancel visible prompt: %+v", ack)
			}
			cancelled := pendingCancelReceive(t, results)
			if !errors.Is(cancelled.err, context.Canceled) || cancelled.allow || len(cancelled.answers) != 0 {
				t.Fatalf("cancelled prompt returned an answer: %+v", cancelled)
			}
			terminal := pendingCancelReceive(t, done)
			if terminal.TurnID != oldTurn || terminal.Outcome != event.TurnOutcomeCancelled {
				t.Fatalf("cancelled terminal: %+v", terminal)
			}
			pendingCancelAssertCleared(t, c)
			c.ReplayPendingPrompts()
			select {
			case id := <-prompts:
				t.Fatalf("cancelled prompt replayed: %s", id)
			default:
			}

			start()
			newPrompt := pendingCancelReceive(t, prompts)
			newTurn := c.TurnStatus().TurnID
			if newPrompt == oldPrompt || newTurn == oldTurn {
				t.Fatal("successor reused cancelled identity")
			}
			reply(oldPrompt, "stale")
			select {
			case result := <-results:
				t.Fatalf("late reply released successor: %+v", result)
			case <-time.After(50 * time.Millisecond):
			}
			c.mu.Lock()
			_, approvalPending := c.approvals[newPrompt]
			_, askPending := c.asks[newPrompt]
			grants := len(c.granted)
			c.mu.Unlock()
			if (kind == "approval" && !approvalPending) || (kind == "ask" && !askPending) || grants != 0 {
				t.Fatal("late reply changed successor pending state or permission grants")
			}
			if status := c.TurnStatus(); !status.Running || status.TurnID != newTurn || status.CancelRequested {
				t.Fatalf("late reply changed successor status: %+v", status)
			}
			reply(newPrompt, "current")
			resolved := pendingCancelReceive(t, results)
			if resolved.err != nil || (kind == "approval" && !resolved.allow) || (kind == "ask" && !reflect.DeepEqual(resolved.answers, []event.AskAnswer{{QuestionID: "choice", Selected: []string{"current"}}})) {
				t.Fatalf("current reply did not release successor correctly: %+v", resolved)
			}
			terminal = pendingCancelReceive(t, done)
			if terminal.TurnID != newTurn || terminal.Outcome != event.TurnOutcomeSuccess {
				t.Fatalf("successor terminal: %+v", terminal)
			}
			pendingCancelAssertCleared(t, c)
		})
	}
}

func TestCancelWhileBlockedOnPauseClearsPendingState(t *testing.T) {
	done := make(chan event.Event, 2)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnDone {
			done <- e
		}
	})})
	t.Cleanup(func() { c.Cancel(); c.Close() })
	paused := make(chan struct{}, 1)
	results := make(chan error, 2)
	c.runGuarded(func(ctx context.Context) error {
		c.SetPaused(true)
		paused <- struct{}{}
		err := c.waitIfPaused(ctx)
		results <- err
		return err
	})
	pendingCancelReceive(t, paused)
	select {
	case err := <-results:
		t.Fatalf("pause gate did not block: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	turn := c.TurnStatus().TurnID
	if !c.Paused() || turn == "" {
		t.Fatal("turn is not paused")
	}
	if ack := c.CancelTurn(turn); !ack.Accepted {
		t.Fatalf("cancel paused turn: %+v", ack)
	}
	if err := pendingCancelReceive(t, results); !errors.Is(err, context.Canceled) {
		t.Fatalf("pause gate returned %v, want context.Canceled", err)
	}
	if terminal := pendingCancelReceive(t, done); terminal.TurnID != turn || terminal.Outcome != event.TurnOutcomeCancelled {
		t.Fatalf("paused terminal: %+v", terminal)
	}
	pendingCancelAssertCleared(t, c)
	c.runGuarded(func(ctx context.Context) error {
		err := c.waitIfPaused(ctx)
		results <- err
		return err
	})
	if err := pendingCancelReceive(t, results); err != nil {
		t.Fatalf("successor inherited cancelled pause: %v", err)
	}
	if terminal := pendingCancelReceive(t, done); terminal.TurnID == turn || terminal.Outcome != event.TurnOutcomeSuccess {
		t.Fatalf("unpaused successor terminal: %+v", terminal)
	}
	pendingCancelAssertCleared(t, c)
}
