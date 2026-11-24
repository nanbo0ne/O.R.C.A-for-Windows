package webbeta

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type heldTaskStop struct {
	entered, release chan struct{}
	started          chan struct{}
	confirmed        bool
}

func (*heldTaskStop) State() BrowserState          { return BrowserState{Ready: true, URL: Origin} }
func (*heldTaskStop) Logout(context.Context) error { return nil }
func (b *heldTaskStop) Run(ctx context.Context, c Command, emit func(Event) error) error {
	_ = emit(Event{ID: c.ID, Kind: "reasoning", Text: "synthetic"})
	if b.started != nil {
		close(b.started)
	}
	<-ctx.Done()
	close(b.entered)
	<-b.release
	if b.confirmed {
		return ConfirmStopped(ctx.Err(), StopConfirmation{RequestID: c.ID, URL: c.URL, Document: "synthetic", HTTPDone: true, UIIdle: true, TaskExited: true, Revoked: true, OutputDrained: true})
	}
	return ctx.Err()
}

func TestTaskReleaseTimeoutRetainsLeaseAndAdmissionBlock(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	finish, err := e.beginOperation("owner")
	if err != nil {
		t.Fatal(err)
	}
	scope, _, err := e.enterTaskScope(nil, "owner", "task")
	if err != nil {
		t.Fatal(err)
	}
	release, err := e.acquireStream("owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	ctx, cancel := context.WithCancel(context.Background())
	e.releaseTaskScope(scope, 20*time.Millisecond, cancel, finish)
	if ctx.Err() != context.Canceled || !e.Status().Stopping {
		t.Fatal("timeout claimed drained")
	}
	if _, err := e.beginOperation("another-owner"); !errors.Is(err, ErrTaskDraining) {
		t.Fatal("new task admitted", err)
	}
	if _, err := e.acquireStream("owner"); !errors.Is(err, ErrTaskDraining) {
		t.Fatal("owner stream admitted", err)
	}
	if !errors.Is(AvailabilityError(e), ErrTaskDraining) {
		t.Fatal("availability reported idle")
	}
	ownerWait, stopOwner := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stopOwner()
	if err := e.WaitOwnerDrain(ownerWait, "owner"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("owner wait fabricated completion", err)
	}
	if err := e.WaitOwnerDrain(context.Background(), "different-owner"); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	e.state.Blocked = "synthetic_existing_guard"
	e.mu.Unlock()
	deadline, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if err := e.CloseContext(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("close lost retained lease", err)
	}
	release()
	deadline2, stop2 := context.WithTimeout(context.Background(), time.Second)
	defer stop2()
	if err := e.CloseContext(deadline2); err != nil {
		t.Fatal(err)
	}
	if err := e.WaitOwnerDrain(deadline2, "owner"); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.activeOperations != 0 || len(e.taskScopes) != 0 || e.state.Blocked != "synthetic_existing_guard" {
		t.Fatal("drain leaked lease or cleared guard")
	}
}

func TestNestedTaskReleaseDoesNotJoinParentStream(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	a := &Adapter{}
	ctx, finish, err := a.BeginTask(context.Background(), "owner", "task")
	if err != nil {
		t.Fatal(err)
	}
	if scope, _ := ctx.Value(taskScopeKey{}).(*taskScope); scope == nil || scope.parentTaskID != "task" {
		t.Fatal("task scope did not retain the real parent task identity")
	}
	defer finish()
	release, err := e.acquireStream("owner")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	child, finishChild, err := a.BeginTask(ctx, "owner", "task")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { finishChild(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("nested release joined parent")
	}
	if child.Err() != context.Canceled || ctx.Err() != nil || e.Status().Stopping {
		t.Fatal("nested release cancelled parent")
	}
	release()
	finish()
}

func TestTaskDrainJoinsOnlyReleasedScopesOfSameParent(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	type lease struct {
		scope           *taskScope
		finish, release func()
	}
	newLease := func(owner, task string) lease {
		t.Helper()
		finish, err := e.beginOperation(owner)
		if err != nil {
			t.Fatal(err)
		}
		scope, _, err := e.enterTaskScope(nil, owner, task)
		if err != nil {
			t.Fatal(err)
		}
		release, err := e.acquireStream(owner)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(release)
		return lease{scope, finish, release}
	}
	// Same parent, distinct Web child owners; another task is intentionally held.
	main := newLease("main", "parent")
	a := newLease("child-a", "parent")
	b := newLease("child-b", "parent")
	other := newLease("other", "other-task")
	// An active unreleased main scope must never wait for itself.
	if err := e.WaitTaskDrain(context.Background(), "parent"); err != nil {
		t.Fatal(err)
	}
	main.release()
	e.releaseTaskScope(main.scope, 0, func() {}, main.finish)
	for _, v := range []lease{a, b, other} {
		e.releaseTaskScope(v.scope, time.Millisecond, func() {}, v.finish)
	}
	timeout, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := e.WaitTaskDrain(timeout, "parent"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("parent returned before child producer drain", err)
	}
	if err := e.WaitTaskDrain(context.Background(), "pure-api-with-no-web-child"); err != nil {
		t.Fatal(err)
	}
	a.release()
	if err := e.WaitOwnerDrain(context.Background(), "child-a"); err != nil {
		t.Fatal(err)
	}
	timeout2, cancel2 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel2()
	if err := e.WaitTaskDrain(timeout2, "parent"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("only one of two children was joined", err)
	}
	b.release()
	ready, cancelReady := context.WithTimeout(context.Background(), time.Second)
	defer cancelReady()
	if err := e.WaitTaskDrain(ready, "parent"); err != nil {
		t.Fatal("unrelated parent held completed task", err)
	}
	if !e.Status().Stopping {
		t.Fatal("other task drain was cleared")
	}
	other.release()
	if err := e.WaitTaskDrain(ready, "other-task"); err != nil {
		t.Fatal(err)
	}
}

func TestTaskReleaseDoesNotJoinOtherOwnerOperations(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	a := &Adapter{}
	ctx, finish, err := a.BeginTask(context.Background(), "owner", "task")
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	_, finishOther, err := a.BeginTask(ctx, "other", "task")
	if err != nil {
		t.Fatal(err)
	}
	defer finishOther()
	releaseOther, err := e.acquireStream("other")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOther()
	done := make(chan struct{})
	go func() { finish(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("owner release joined unrelated operation")
	}
	if e.Status().Stopping {
		t.Fatal("unrelated stream marked stopping")
	}
	releaseOther()
	finishOther()
}

func TestTaskReleaseQueuedCancellationNeverDispatches(t *testing.T) {
	b := &fakeBrowser{}
	e := testEngine(t, b)
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	block := make(chan struct{})
	entered := make(chan struct{})
	blockDone := make(chan error, 1)
	go func() {
		blockDone <- e.queue.Do(context.Background(), false, func(context.Context) error { close(entered); <-block; return nil })
	}()
	<-entered
	var once sync.Once
	unlock := func() { once.Do(func() { close(block) }) }
	t.Cleanup(unlock)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &Adapter{}
	ctx, finish, err := a.BeginTask(ctx, "owner", "task")
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	ch, err := a.Stream(ctx, provider.Request{ConversationID: "owner", ParentTaskID: "task", RequestID: "synthetic", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}})
	if err != nil {
		t.Fatal(err)
	}
	until := time.After(time.Second)
	for e.queue.Pending() == 0 {
		select {
		case <-until:
			t.Fatal("not queued")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	finished := make(chan struct{})
	go func() { finish(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("queued cancellation waited for other owner")
	}
	for range ch {
	}
	unlock()
	<-blockDone
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.commands) != 0 || e.Status().Stopping {
		t.Fatal("cancelled queued request dispatched or leaked")
	}
}
func TestTaskReleaseJoinsItsCancelledProducer(t *testing.T) {
	for _, outcome := range []string{"confirmed", "unconfirmed", "identity-close"} {
		t.Run(outcome, func(t *testing.T) { testTaskReleaseJoinsProducer(t, outcome) })
	}
}

func testTaskReleaseJoinsProducer(t *testing.T, outcome string) {
	b := &heldTaskStop{entered: make(chan struct{}), release: make(chan struct{}), confirmed: outcome == "confirmed"}
	var once sync.Once
	release := func() { once.Do(func() { close(b.release) }) }
	defer release()
	e := testEngine(t, b)
	t.Cleanup(release)
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	e.state.Conversations = []Conversation{{ID: "fixture", URL: Origin + "/a/chat/s/fixture", Phase: "ready"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := agent.New(&Adapter{name: "offline"}, tool.NewRegistry(), agent.NewSession("synthetic"), agent.Options{MaxSteps: 1}, event.FuncSink(func(ev event.Event) {
		if ev.Kind == event.Reasoning {
			if outcome == "identity-close" {
				e.startClose()
			} else {
				cancel()
			}
		}
	}))
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, "synthetic cancellation") }()
	select {
	case <-b.entered:
	case <-time.After(time.Second):
		t.Fatal("producer not stopping")
	}
	select {
	case <-done:
		t.Fatal("Agent returned while native producer still held")
	case <-time.After(30 * time.Millisecond):
	}
	release()
	select {
	case err := <-done:
		if err == nil || (outcome != "identity-close" && !errors.Is(err, context.Canceled)) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("owner drain did not finish")
	}
	if e.Status().Stopping {
		t.Fatal("producer finished but owner drain retained")
	}
	if outcome == "confirmed" && e.Status().Blocked != "" {
		t.Fatal("confirmed cancellation became unknown")
	}
	if outcome == "unconfirmed" && e.Status().Blocked == "" {
		t.Fatal("unconfirmed cancellation bypassed protection")
	}
}
