package control

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestInteractivePromptsPauseOnlyOptedInTaskBudget(t *testing.T) {
	for _, kind := range []string{"approval", "ask"} {
		for _, cancelled := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "-answer", true: "-cancel"}[cancelled], func(t *testing.T) {
				var waiting, resumed atomic.Int32
				prompts := make(chan event.Event, 1)
				c := New(Options{Sink: event.FuncSink(func(e event.Event) {
					if e.Kind == event.ApprovalRequest || e.Kind == event.AskRequest {
						prompts <- e
					}
				})})
				defer c.Close()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				ctx = provider.WithTaskHumanWait(ctx, func() func() {
					waiting.Add(1)
					return func() { waiting.Add(-1); resumed.Add(1) }
				})
				done := make(chan error, 1)
				go func() {
					if kind == "approval" {
						_, _, err := c.requestApproval(ctx, "write_file", "synthetic.txt")
						done <- err
					} else {
						_, err := c.Ask(ctx, []event.AskQuestion{{ID: "synthetic", Prompt: "Choose"}})
						done <- err
					}
				}()
				prompt := pendingCancelReceive(t, prompts)
				if waiting.Load() != 1 {
					t.Fatal("visible prompt consumed active task time")
				}
				if cancelled {
					cancel()
				} else if kind == "approval" {
					c.Approve(prompt.Approval.ID, true, false, false)
				} else {
					c.AnswerQuestion(prompt.Ask.ID, []event.AskAnswer{{QuestionID: "synthetic"}})
				}
				err := pendingCancelReceive(t, done)
				if (err != nil) != cancelled || waiting.Load() != 0 || resumed.Load() != 1 {
					t.Fatal("prompt did not resume once or lost cancellation", err)
				}
			})
		}
	}
}
