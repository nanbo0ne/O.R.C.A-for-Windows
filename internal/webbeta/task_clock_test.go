package webbeta

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestActiveTaskClockExcludesOnlyHumanWait(t *testing.T) {
	var saved time.Duration
	ctx, clock := newTaskClock(context.Background(), 150*time.Millisecond, func(d time.Duration) error { saved += d; return nil })
	defer clock.close()
	resume := clock.pause()
	nested := clock.pause()
	time.Sleep(200 * time.Millisecond)
	resume()
	resume()
	if ctx.Err() != nil || saved != 0 {
		t.Fatal("nested human wait expired or was credited twice")
	}
	nested()
	if saved < 200*time.Millisecond || ctx.Err() != nil {
		t.Fatal("human wait was not credited", saved, ctx.Err())
	}
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatal(ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("active work escaped deadline")
	}
}

func TestActiveTaskTimeoutKeepsBudgetReasonAndNoReplay(t *testing.T) {
	ctx, clock := newTaskClock(context.Background(), time.Millisecond, func(time.Duration) error { return nil })
	defer clock.close()
	<-ctx.Done()
	err := provider.CancellationError(ctx)
	if ctx.Err() != context.DeadlineExceeded || !errors.Is(err, ErrTaskBudget) || !provider.ReplayForbidden(err) || !strings.Contains(err.Error(), "实际运行时间") {
		t.Fatalf("task timeout lost its specific cause: %v", err)
	}
}

func TestActiveTaskClockNeverPausesCancellationOrCallerDeadline(t *testing.T) {
	for _, timed := range []bool{false, true} {
		parent, cancel := context.WithCancel(context.Background())
		if timed {
			cancel()
			parent, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
		}
		ctx, clock := newTaskClock(parent, time.Second, func(time.Duration) error { t.Error("credited cancelled wait"); return nil })
		resume := clock.pause()
		if !timed {
			cancel()
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("human wait hid cancellation")
		}
		resume()
		clock.close()
		cancel()
	}
}

func TestActiveTaskClockPersistenceFailureStopsWork(t *testing.T) {
	failure := errors.New("synthetic storage failure")
	ctx, clock := newTaskClock(context.Background(), time.Second, func(time.Duration) error { return failure })
	defer clock.close()
	clock.pause()()
	if ctx.Err() == nil || !errors.Is(context.Cause(ctx), failure) {
		t.Fatal("continued without persisting wait credit", ctx.Err())
	}
}

func TestTaskHumanWaitCreditIsDurableAndNestedRequestsShareClock(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	ctx, stop, err := e.taskContext(context.Background(), "parent")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	resume := provider.BeginTaskHumanWait(ctx)
	time.Sleep(25 * time.Millisecond)
	resume()
	resume()
	saved, err := loadState(e.path)
	if err != nil || saved.Tasks["parent"].HumanWaitMillis < 20 {
		t.Fatal("wait was not persisted", err, saved.Tasks["parent"])
	}
	nested, done, err := e.taskContext(ctx, "parent")
	if err != nil || nested.Value(taskClockKey{}) != ctx.Value(taskClockKey{}) {
		t.Fatal("nested request created another timer", err)
	}
	done()
	if ctx.Err() != nil {
		t.Fatal("nested request cancelled parent")
	}
	e.Close()
	restored, err := NewEngine(e.path, &fakeBrowser{})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restored.mu.Lock()
	err = restored.reserveTaskLocked("parent", 0, 0, 0)
	restored.mu.Unlock()
	if err != nil {
		t.Fatal("restart lost credited wait", err)
	}
}
