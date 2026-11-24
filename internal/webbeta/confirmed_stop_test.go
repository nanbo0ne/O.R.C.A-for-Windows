package webbeta

import (
	"context"
	"errors"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestConfirmedStopRequiresEveryEvidence(t *testing.T) {
	p := StopConfirmation{RequestID: "request", URL: Origin + "/a/chat/s/synthetic", Document: "document", HTTPDone: true, UIIdle: true, TaskExited: true, Revoked: true, OutputDrained: true}
	if !confirmedStoppedFor(ConfirmStopped(context.Canceled, p), p.RequestID, p.URL) {
		t.Fatal("complete evidence rejected")
	}
	for _, field := range []string{"http", "ui", "exit", "revoke", "output", "document"} {
		bad := p
		switch field {
		case "http":
			bad.HTTPDone = false
		case "ui":
			bad.UIIdle = false
		case "exit":
			bad.TaskExited = false
		case "revoke":
			bad.Revoked = false
		case "output":
			bad.OutputDrained = false
		case "document":
			bad.Document = ""
		}
		if confirmedStoppedFor(ConfirmStopped(context.Canceled, bad), p.RequestID, p.URL) {
			t.Fatal("missing evidence accepted", field)
		}
	}
	for _, err := range []error{context.Canceled, errors.New("web task cancelled with confirmed native drain; replay forbidden"), ConfirmStopped(context.DeadlineExceeded, p), ConfirmStopped(errors.New("network"), p)} {
		if confirmedStoppedFor(err, p.RequestID, p.URL) {
			t.Fatal("untyped or non-user cancellation accepted")
		}
	}
	if confirmedStoppedFor(ConfirmStopped(context.Canceled, p), "other-request", p.URL) {
		t.Fatal("stale receipt accepted")
	}
}

type stopFixture struct {
	fakeBrowser
	cancel       context.CancelFunc
	complete     bool
	beforeReturn func()
}

func (b *stopFixture) Run(ctx context.Context, c Command, emit func(Event) error) error {
	if c.Operation == "opening" {
		return b.fakeBrowser.Run(ctx, c, emit)
	}
	if b.cancel == nil {
		return b.fakeBrowser.Run(ctx, c, emit)
	}
	b.cancel()
	b.cancel = nil
	if b.beforeReturn != nil {
		b.beforeReturn()
	}
	return ConfirmStopped(context.Canceled, StopConfirmation{RequestID: c.ID, URL: c.URL, Document: "fixture-document", HTTPDone: b.complete, UIIdle: true, TaskExited: true, Revoked: true, OutputDrained: true})
}

func TestConfirmedStopSaveFailureKeepsUnknown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &stopFixture{cancel: cancel, complete: true}
	e := testEngine(t, b)
	oldPath := e.path
	e.state.Conversations = append(e.state.Conversations, Conversation{ID: "synthetic", URL: Origin + "/a/chat/s/synthetic", Phase: "ready"})
	b.beforeReturn = func() { e.path = oldPath + "/invalid-child" }
	r := Request{Owner: "owner", ID: "cancelled-request", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}}
	if err := e.Send(ctx, r, func(Event) error { return nil }); err == nil {
		t.Fatal("failed persistence reported success")
	}
	if e.Status().Blocked == "" || e.Status().Settings.Enabled || len(e.state.CancelledRequests) != 0 {
		t.Fatal("failed checkpoint cleared unknown state")
	}
	saved, err := loadState(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Blocked == "" || saved.Settings.Enabled {
		t.Fatal("restart did not retain interrupted-send protection")
	}
}
func TestConfirmedStopArchivesWithoutReplaying(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing-http", true: "complete"}[complete], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b := &stopFixture{cancel: cancel, complete: complete}
			e := testEngine(t, b)
			e.state.Conversations = append(e.state.Conversations, Conversation{ID: "synthetic", URL: Origin + "/a/chat/s/synthetic", Phase: "ready"})
			r := Request{Owner: "owner", ID: "cancelled-request", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}}
			err := e.Send(ctx, r, func(Event) error { return nil })
			if !errors.Is(err, context.Canceled) {
				t.Fatal("cancel identity lost", err)
			}
			saved, err := loadState(e.path)
			if err != nil {
				t.Fatal(err)
			}
			if !complete {
				if saved.Blocked == "" || saved.Settings.Enabled {
					t.Fatal("incomplete stop cleared protection")
				}
				return
			}
			if saved.Blocked != "" || !saved.Settings.Enabled || len(saved.CancelledRequests) != 1 || saved.Conversations[0].Phase != "archived" || saved.Conversations[0].Inflight != "" || saved.Conversations[0].Reply != "" {
				t.Fatal("confirmed stop not archived safely")
			}
			if err = e.Send(context.Background(), r, func(Event) error { return nil }); err == nil || !provider.ReplayForbidden(err) {
				t.Fatal("old request replay accepted")
			}
			restarted, restartErr := NewEngine(e.path, &fakeBrowser{})
			if restartErr != nil {
				t.Fatal(restartErr)
			}
			if replayErr := restarted.Send(context.Background(), r, func(Event) error { return nil }); replayErr == nil || !provider.ReplayForbidden(replayErr) {
				restarted.Close()
				t.Fatal("restart lost cancellation tombstone")
			}
			restarted.Close()
			fresh := r
			fresh.ID = "fresh-request"
			if err = e.Send(context.Background(), fresh, func(Event) error { return nil }); err != nil {
				t.Fatal("new task blocked", err)
			}
		})
	}
}
