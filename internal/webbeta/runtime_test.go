package webbeta

import (
	"context"
	"errors"
	"testing"
	"time"
)

func isolatedRuntime(t *testing.T) {
	t.Helper()
	old := CurrentEngine()
	SetEngine(nil)
	t.Cleanup(func() { SetEngine(old) })
}

func TestAwaitAnnouncedBackgroundStartupWithoutSending(t *testing.T) {
	isolatedRuntime(t)
	complete := BeginEngineStartup()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	b := &fakeBrowser{}
	e := testEngine(t, b)
	go func() {
		got, err := AwaitEngine(ctx)
		if err == nil && got != e {
			err = errors.New("wrong engine")
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatal("did not await startup", err)
	case <-time.After(10 * time.Millisecond):
	}
	SetEngine(e)
	complete(nil)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if e.Status().Sent != 0 || len(b.commands) != 0 {
		t.Fatal("startup sent a request")
	}
}

func TestStartupWaitCancelledAndFailed(t *testing.T) {
	isolatedRuntime(t)
	complete := BeginEngineStartup()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AwaitEngine(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	failure := errors.New("synthetic WebView initialization failure")
	done := make(chan error, 1)
	go func() { _, err := AwaitEngine(context.Background()); done <- err }()
	// Ensure the waiter has observed the announced startup without depending on
	// timing: keep the startup registered while reading it below.
	runtimeEngine.RLock()
	start := runtimeEngine.startup
	runtimeEngine.RUnlock()
	complete(failure)
	<-start.done
	if start.err != failure {
		t.Fatal("initializer error lost")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("failure hidden")
		}
	case <-time.After(time.Second):
		t.Fatal("failed startup waiter hung")
	}
}

func TestLogoutInvalidatesStartupWaiterAndLateCompletion(t *testing.T) {
	isolatedRuntime(t)
	complete := BeginEngineStartup()
	runtimeEngine.RLock()
	start := runtimeEngine.startup
	runtimeEngine.RUnlock()
	SetEngine(nil)
	newComplete := BeginEngineStartup()
	complete(errors.New("late failure"))
	runtimeEngine.RLock()
	next := runtimeEngine.startup
	runtimeEngine.RUnlock()
	if next == nil || next == start {
		t.Fatal("late completion replaced new startup")
	}
	if !errors.Is(start.err, ErrIdentityClosed) {
		t.Fatal("logout did not close old waiters")
	}
	newComplete(errors.New("finish synthetic startup"))
}
