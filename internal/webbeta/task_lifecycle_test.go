package webbeta

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestTaskScopeIncludesHostToolLifetimeWithoutHoldingGenerationQueue(t *testing.T) {
	b := &fakeBrowser{}
	e := testEngine(t, b)
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	a := &Adapter{}
	ctx, finish, err := a.BeginTask(context.Background(), "owner", "parent")
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	if !provider.SerialTaskTools(ctx) || provider.SerialTaskTools(context.Background()) {
		t.Fatal("web task did not install a local serial tool policy")
	}
	if e.Status().Sent != 0 {
		t.Fatal("BeginTask sent a request")
	}
	if _, _, err := e.taskContext(ctx, "different-parent"); err == nil {
		t.Fatal("nested request reset parent budget")
	}
	ch, err := a.Stream(ctx, provider.Request{ConversationID: "owner", ParentTaskID: "parent", RequestID: "r", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}})
	if err != nil {
		t.Fatal(err)
	}
	for c := range ch {
		if c.Err != nil {
			t.Fatal(c.Err)
		}
	}
	// Stream is finished, but a tool is still executing in the returned context.
	deadline, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if err := e.CloseContext(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("identity drained before host tool exited", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("host tool context not cancelled")
	}
	if _, err := a.Stream(ctx, provider.Request{ConversationID: "owner", ParentTaskID: "parent", RequestID: "late"}); err == nil {
		t.Fatal("cancelled tool replayed")
	}
	finish()
	finish()
	deadline2, stop2 := context.WithTimeout(context.Background(), time.Second)
	defer stop2()
	if err := e.CloseContext(deadline2); err != nil {
		t.Fatal("finished tool held drain", err)
	}
}

func TestTaskScopePinsIdentityAndDoesNotAutoEnable(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	a := &Adapter{}
	ctx, finish, err := a.BeginTask(context.Background(), "owner", "parent")
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	other := testEngine(t, &fakeBrowser{})
	SetEngine(other)
	if _, err := engineForRequest(ctx); !errors.Is(err, ErrIdentityClosed) {
		t.Fatal("task moved to different identity", err)
	}
	if _, _, err := a.BeginTask(ctx, "nested-owner", "parent"); !errors.Is(err, ErrIdentityClosed) {
		t.Fatal("nested scope changed identity", err)
	}
	s := other.Status().Settings
	s.Enabled = false
	if err := other.Configure(s); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.BeginTask(context.Background(), "new-owner", "new-parent"); err == nil {
		t.Fatal("task scope authorized disabled account")
	}
	if other.Status().Sent != 0 || other.Status().Settings.Enabled {
		t.Fatal("task scope sent or enabled")
	}
}

func TestTaskDeadlineCancelsHostToolContext(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	e.mu.Lock()
	e.state.Tasks = map[string]TaskBudget{"parent": {GenerationHash: contentHash(e.state.Generation), StartedAt: time.Now().Add(-taskLifetime + 100*time.Millisecond).UnixMilli()}}
	e.mu.Unlock()
	ctx, finish, err := (&Adapter{}).BeginTask(context.Background(), "owner", "parent")
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatal(ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("host tool escaped parent deadline")
	}
}

func TestIdentityResetAndRestartCannotReuseParentTaskBudget(t *testing.T) {
	for _, stored := range []bool{false, true} {
		t.Run(map[bool]string{false: "live-reset", true: "stored-reset"}[stored], func(t *testing.T) {
			e := testEngine(t, &fakeBrowser{})
			_, cancel, err := e.taskContext(context.Background(), "old-parent")
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			e.mu.Lock()
			if err := e.reserveTaskLocked("old-parent", 3, 2, 1); err != nil {
				t.Fatal(err)
			}
			if err := saveState(e.path, e.state); err != nil {
				t.Fatal(err)
			}
			e.mu.Unlock()
			if stored {
				e.Close()
				err = BeginStoredIdentityReset(e.path)
			} else {
				err = e.BeginIdentityReset()
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := CompleteIdentityReset(e.path); err != nil {
				t.Fatal(err)
			}
			restored, err := NewEngine(e.path, &fakeBrowser{})
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			if _, _, err := restored.taskContext(context.Background(), "old-parent"); !errors.Is(err, ErrTaskIdentityChanged) {
				t.Fatal("new identity resumed old parent task", err)
			}
			_, stop, err := restored.taskContext(context.Background(), "new-user-turn")
			if err != nil {
				t.Fatal("new user turn rejected", err)
			}
			stop()
			saved, err := loadState(e.path)
			if err != nil {
				t.Fatal(err)
			}
			old := saved.Tasks["old-parent"]
			if old.Generations != 3 || old.Actions != 2 || old.Corrections != 1 || len(saved.Conversations) != 0 || len(saved.Calls) != 0 || saved.Settings.Enabled {
				t.Fatal("identity reset lost guard metadata or retained account contents")
			}
		})
	}
}
