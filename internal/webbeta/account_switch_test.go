package webbeta

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestAccountSwitchPreservesGrantAndArchivesOldIdentity(t *testing.T) {
	for _, authorized := range []bool{true, false} {
		t.Run(map[bool]string{true: "authorized", false: "revoked"}[authorized], func(t *testing.T) {
			e := testEngine(t, &fakeBrowser{})
			settings := e.Status().Settings
			settings.Enabled = authorized
			if err := e.Configure(settings); err != nil {
				t.Fatal(err)
			}
			e.state.Conversations = []Conversation{{ID: "old", Owner: "old-owner", Phase: "uncertain", Inflight: "old-call", URL: Origin + "/a/chat/s/old"}}
			generation := e.Status().Generation
			if err := e.StopForAccountSwitch(); err != nil {
				t.Fatal(err)
			}
			if err := e.CloseContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			e.fail(&Failure{Code: "stop_unconfirmed", Sent: true})
			if e.Status().Blocked != IdentitySwitchPending {
				t.Fatal("late old failure overwrote the identity transition")
			}
			if err := e.PrepareAccountSwitch(); err != nil {
				t.Fatal(err)
			}
			stored, err := loadState(e.path)
			if err != nil || stored.Blocked != IdentitySwitchPending || stored.SendAuthorized != authorized || stored.Settings.Enabled || stored.Generation == generation {
				t.Fatalf("invalid pending transition: %+v %v", stored, err)
			}
			b := &fakeBrowser{}
			next, err := NewEngineWithRequestLimit(e.path, b, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			if err := next.ConfirmRecovery(); !errors.Is(err, ErrIdentityClosed) {
				t.Fatal("generic recovery bypassed switch", err)
			}
			if err := next.ConfirmAccountSwitch(); err != nil {
				t.Fatal(err)
			}
			status := next.Status()
			if status.Blocked != "" || status.Settings.Enabled != authorized || status.SendAuthorized != authorized || !status.Settings.ReservePaused {
				t.Fatalf("grant changed: %+v", status)
			}
			if len(b.commands) != 0 {
				t.Fatal("switch auto-sent")
			}
			if end, err := next.beginOperation("old-owner"); !errors.Is(err, ErrIdentityChanged) {
				if end != nil {
					end()
				}
				t.Fatal("old owner was admitted", err)
			}
			if status.Conversations[0].Phase != "archived" || status.Conversations[0].URL != Origin+"/a/chat/s/old" {
				t.Fatal("old evidence lost or not archived")
			}
			if authorized {
				err := next.Send(context.Background(), Request{Owner: "fresh-owner", ID: "fresh-request", Messages: []provider.Message{{Role: provider.RoleUser, Content: "fresh synthetic task"}}}, func(Event) error { return nil })
				if err != nil {
					t.Fatal(err)
				}
				for _, cmd := range b.commands {
					if cmd.URL == Origin+"/a/chat/s/old" {
						t.Fatal("old chat reused")
					}
				}
			}
		})
	}
}

func TestAccountSwitchWaitsForExecutionAndRejectsLateResult(t *testing.T) {
	b := &drainingBrowser{started: make(chan struct{}), stopping: make(chan struct{}), release: make(chan struct{}), lateError: make(chan error, 1)}
	var once sync.Once
	defer once.Do(func() { close(b.release) })
	e := testEngine(t, b)
	e.state.Conversations = []Conversation{{ID: "old", Owner: "old-owner", Phase: "bound", URL: Origin + "/a/chat/s/old"}}
	done := make(chan error, 1)
	go func() {
		done <- e.Send(context.Background(), Request{Owner: "old-owner", ID: "r", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}}, func(Event) error { t.Error("late output escaped"); return nil })
	}()
	<-b.started
	if err := e.StopForAccountSwitch(); err != nil {
		t.Fatal(err)
	}
	<-b.stopping
	if err := e.PrepareAccountSwitch(); !errors.Is(err, ErrTaskDraining) {
		t.Fatal("switch completed before drain", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := e.CloseContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	once.Do(func() { close(b.release) })
	if err := <-done; err == nil {
		t.Fatal("old send succeeded")
	}
	if err := <-b.lateError; !errors.Is(err, ErrIdentityClosed) {
		t.Fatal("late callback accepted", err)
	}
	if err := e.CloseContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.PrepareAccountSwitch(); err != nil {
		t.Fatal(err)
	}
	if b.calls.Load() != 1 {
		t.Fatal("old request replayed")
	}
}
