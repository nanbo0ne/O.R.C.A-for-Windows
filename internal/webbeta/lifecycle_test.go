package webbeta

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type drainingBrowser struct {
	fakeBrowser
	started, stopping, release chan struct{}
	lateError                  chan error
	calls                      atomic.Int32
}

func (b *drainingBrowser) Run(ctx context.Context, cmd Command, emit func(Event) error) error {
	b.calls.Add(1)
	close(b.started)
	<-ctx.Done()
	close(b.stopping)
	<-b.release
	b.lateError <- emit(Event{ID: cmd.ID, Kind: "text", Text: "stale identity"})
	return ctx.Err()
}

func TestIdentityResetDrainsRejectsLateResultsAndRevokesSnapshots(t *testing.T) {
	b := &drainingBrowser{started: make(chan struct{}), stopping: make(chan struct{}), release: make(chan struct{}), lateError: make(chan error, 1)}
	var release sync.Once
	defer release.Do(func() { close(b.release) })
	e := testEngine(t, b)
	e.state.Conversations = []Conversation{{ID: "bound", Owner: "old-task", Phase: "bound", URL: Origin + "/a/chat/s/old"}}
	oldGeneration := e.Status().Generation
	grant, err := e.Vault().Grant("old-task", "r", "test.txt", []byte("synthetic snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	var emitted atomic.Int32
	request := Request{Owner: "old-task", ID: "r", Attachments: []string{grant}, Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}}
	sent := make(chan error, 1)
	go func() {
		sent <- e.Send(context.Background(), request, func(Event) error { emitted.Add(1); return nil })
	}()
	<-b.started
	queued := make(chan error, 1)
	go func() {
		queued <- e.Send(context.Background(), Request{Owner: "queued-task", ID: "queued", Messages: request.Messages}, func(Event) error { return nil })
	}()
	deadline := time.After(time.Second)
	for e.queue.Pending() == 0 {
		select {
		case <-deadline:
			t.Fatal("request did not queue")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	reset := make(chan error, 1)
	go func() { reset <- e.BeginIdentityReset() }()
	<-b.stopping
	select {
	case err := <-reset:
		t.Fatalf("reset completed before stop receipt: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if _, err := e.Vault().Grant("old-task", "late", "late.txt", []byte("late")); !errors.Is(err, ErrIdentityClosed) {
		t.Fatal(err)
	}
	if _, err := e.Vault().Resolve("old-task", "r", []string{grant}); !errors.Is(err, ErrIdentityClosed) {
		t.Fatal(err)
	}
	release.Do(func() { close(b.release) })
	if err := <-reset; err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err == nil {
		t.Fatal("old send accepted")
	}
	if err := <-queued; err == nil {
		t.Fatal("queued send accepted")
	}
	if err := <-b.lateError; !errors.Is(err, ErrIdentityClosed) {
		t.Fatal("late result not rejected", err)
	}
	if emitted.Load() != 0 || b.calls.Load() != 1 || e.queue.Pending() != 0 {
		t.Fatal("old generation escaped drain")
	}
	if pending, err := IdentityResetPending(e.path); err != nil || !pending {
		t.Fatal("reset journal missing", err)
	}
	if err := CompleteIdentityReset(e.path); err != nil {
		t.Fatal(err)
	}
	newBrowser := &fakeBrowser{}
	next, err := NewEngineWithRequestLimit(e.path, newBrowser, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	status := next.Status()
	if status.Generation == oldGeneration || status.Settings.Enabled || !status.Settings.ReservePaused || len(status.Conversations) != 0 {
		t.Fatalf("identity not reset: %+v", status)
	}
	s := status.Settings
	s.Enabled = true
	if err := next.Configure(s); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"old-task", "queued-task"} {
		request.Owner = owner
		request.Attachments = nil
		if err := next.Send(context.Background(), request, func(Event) error { return nil }); !errors.Is(err, ErrIdentityChanged) {
			t.Fatal("old context reused", err)
		}
	}
	if len(newBrowser.commands) != 0 {
		t.Fatal("identity switch auto-sent")
	}
}

func TestIdentityResetJournalSurvivesRestartAndCannotRecover(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	if err := BeginStoredIdentityReset(p); err != nil {
		t.Fatal(err)
	}
	e, err := NewEngineWithRequestLimit(p, &fakeBrowser{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.ConfirmRecovery(); !errors.Is(err, ErrIdentityClosed) {
		t.Fatal("recovery bypassed profile cleanup", err)
	}
	s := e.Status().Settings
	s.Enabled = true
	if err := e.Configure(s); err == nil {
		t.Fatal("enabled pending reset")
	}
}

func TestCloseReleasesUnreadAdapterAndRejectsOldEngine(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	SetEngine(e)
	defer SetEngine(nil)
	stream, err := (&Adapter{}).Stream(context.Background(), provider.Request{ParentTaskID: "synthetic-parent", ConversationID: "old-task", RequestID: "r", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { e.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unread stream blocked logout")
	}
	for chunk := range stream {
		if chunk.Type == provider.ChunkToolCall || chunk.Type == provider.ChunkDone {
			t.Fatal("stale completion escaped")
		}
	}
	if err := e.Configure(DefaultSettings()); !errors.Is(err, ErrIdentityClosed) {
		t.Fatal(err)
	}
	if err := WaitUntilAvailable(context.Background(), e); err == nil {
		t.Fatal("closed engine ready")
	}
}

func TestReadyEnvironmentCannotAuthorizeRuntime(t *testing.T) {
	t.Setenv("ORCA_WEB_BETA_READY", "1")
	e, err := NewEngineWithRequestLimit(filepath.Join(t.TempDir(), "state.json"), &fakeBrowser{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := AvailabilityError(e); err == nil {
		t.Fatal("global environment authorized disabled engine")
	}
	if e.Status().Sent != 0 || e.Status().Settings.Enabled {
		t.Fatal("cold start sent or enabled")
	}
}

func TestIdentityDrainTimeoutKeepsGenerationAndCannotRecoverAfterRestart(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	before := e.Status().Generation
	finish, err := e.beginOperation("still-running")
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	defer once.Do(finish)
	if err := e.StopForIdentityChange(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := e.CloseContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("operation did not hold drain open", err)
	}
	saved, err := loadState(e.path)
	if err != nil || saved.Generation != before || saved.Blocked != identityDrainPending || saved.Settings.Enabled {
		t.Fatalf("drain timeout changed identity or lost journal: %+v, %v", saved, err)
	}
	if len(saved.InvalidOwners) != 1 || saved.InvalidOwners[0] != "still-running" {
		t.Fatal("unbound old owner was lost at restart")
	}
	if pending, err := IdentityResetPending(e.path); err != nil || pending {
		t.Fatal("drain journal authorized profile deletion", err)
	}
	restarted, err := NewEngineWithRequestLimit(e.path, &fakeBrowser{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if err := restarted.ConfirmRecovery(); !errors.Is(err, ErrIdentityClosed) {
		t.Fatal("restart recovery bypassed drain boundary", err)
	}
	s := restarted.Status().Settings
	s.Enabled = true
	if err := restarted.Configure(s); err == nil {
		t.Fatal("restart reenabled interrupted logout")
	}
	once.Do(finish)
	if err := e.CloseContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestUncertainOpeningPausesOtherOwnersWithoutReplay(t *testing.T) {
	b := &fakeBrowser{fail: errors.New("unknown receipt")}
	e := testEngine(t, b)
	r := Request{Owner: "first", ID: "one", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}}
	if err := e.Send(context.Background(), r, func(Event) error { return nil }); err == nil {
		t.Fatal("opening uncertainty ignored")
	}
	b.fail = nil
	r.Owner, r.ID = "second", "two"
	if err := e.Send(context.Background(), r, func(Event) error { return nil }); err == nil {
		t.Fatal("another owner bypassed uncertain opening")
	}
	if len(b.commands) != 1 || e.Status().Settings.Enabled {
		t.Fatal("uncertain opening replayed")
	}
}
