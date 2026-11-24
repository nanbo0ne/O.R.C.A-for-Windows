package webbeta

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type checkpointFailureBrowser struct {
	fakeBrowser
	afterDone func() error
}

func (b *checkpointFailureBrowser) Run(ctx context.Context, c Command, emit func(Event) error) error {
	if err := b.fakeBrowser.Run(ctx, c, emit); err != nil {
		return err
	}
	return b.afterDone()
}

func TestCompletionCheckpointFailureStopsAccountBeforeNextJob(t *testing.T) {
	for _, stage := range []string{"send", "opening", "correction"} {
		t.Run(stage, func(t *testing.T) {
			b := &checkpointFailureBrowser{}
			e := testEngine(t, b)
			messages := []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}
			r := Request{Owner: "owner", ID: "request", Messages: messages}
			if stage != "opening" {
				e.state.Conversations = []Conversation{{ID: "conversation", Owner: r.Owner, Phase: "bound", URL: Origin + "/a/chat/s/synthetic"}}
				if stage == "correction" {
					e.state.Conversations[0].Prefix = fingerprints(messages)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			next := make(chan error, 1)
			var dispatched atomic.Bool
			backup := e.path + ".last-durable"
			b.afterDone = func() error {
				// Keep the last durable Inflight journal, but make the final atomic
				// replace fail. All paths are inside this test's temporary profile.
				if err := os.Rename(e.path, backup); err != nil {
					return err
				}
				if err := os.Mkdir(e.path, 0700); err != nil {
					return err
				}
				go func() {
					next <- e.queue.Do(ctx, false, func(context.Context) error {
						dispatched.Store(true)
						return nil
					})
				}()
				for e.queue.Pending() == 0 {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(time.Millisecond):
					}
				}
				return nil
			}
			var err error
			switch stage {
			case "send":
				err = e.Send(ctx, r, func(Event) error { return nil })
			case "opening":
				err = e.prepare(ctx, false)
			case "correction":
				_, err = e.correctToolFormat(ctx, r)
			}
			if err == nil {
				t.Fatal("final checkpoint unexpectedly succeeded")
			}
			if !provider.ReplayForbidden(err) {
				t.Fatal("checkpoint failure lost cross-layer no-replay policy", err)
			}
			select {
			case err := <-next:
				if err == nil || errors.Is(err, context.DeadlineExceeded) || dispatched.Load() {
					t.Errorf("next job was not rejected before slot release: dispatched=%v err=%v", dispatched.Load(), err)
				}
			case <-ctx.Done():
				t.Fatal("next job was not promptly rejected")
			}
			status := e.Status()
			if status.Settings.Enabled || !status.Settings.ReservePaused || status.Blocked != "send_unconfirmed" || len(status.Conversations) != 1 || status.Conversations[0].Phase != "uncertain" {
				t.Errorf("checkpoint failure did not latch uncertainty: %+v", status)
			}
			if err := os.Remove(e.path); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(backup, e.path); err != nil {
				t.Fatal(err)
			}
			// Restoring storage availability must not implicitly authorize retries.
			if err := e.Send(ctx, Request{Owner: "other", ID: "next", Messages: messages}, func(Event) error { return nil }); err == nil {
				t.Error("storage recovery silently reopened account")
			}
			if len(b.commands) != 1 {
				t.Errorf("replayed send: %d", len(b.commands))
			}
			restored, err := loadState(e.path)
			if err != nil || restored.Settings.Enabled || restored.Blocked != "send_unconfirmed" {
				t.Fatalf("durable Inflight journal did not fail closed on restart: %+v %v", restored, err)
			}
		})
	}
}
