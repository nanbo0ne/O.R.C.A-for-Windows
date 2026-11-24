package control

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/jobs"
)

func TestRetireWaitsForExecutionAndClosesAdmission(t *testing.T) {
	c := New(Options{Sink: event.Discard, Executor: agent.New(nil, nil, agent.NewSession(""), agent.Options{}, event.Discard)})
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }); c.WaitIdle(context.Background()) })
	c.runGuarded(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release // Deliberately non-cooperative execution.
		return ctx.Err()
	})
	<-started
	c.Retire()
	<-cancelled
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := c.WaitIdle(ctx); !errors.Is(err, context.DeadlineExceeded) || !c.Running() {
		t.Fatalf("cancellation falsely reported completion: %v", err)
	}
	once.Do(func() { close(release) })
	if err := c.WaitIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.runGuarded(func(context.Context) error { t.Error("retired execution started"); return nil })
	if err := c.RunTurn(context.Background(), "stale task"); !errors.Is(err, ErrControllerRetired) {
		t.Fatal("retired synchronous turn admitted", err)
	}
	if err := c.Compact(context.Background(), ""); !errors.Is(err, ErrControllerRetired) {
		t.Fatal("retired compaction admitted", err)
	}
	if !c.Retired() || c.Running() {
		t.Fatal("retired controller resumed")
	}
}

func TestRetiredControllerWaitIncludesBackgroundTools(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	c := New(Options{Sink: event.Discard, Jobs: m})
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }); m.Close() })
	m.Start("task", "synthetic", func(ctx context.Context, _ io.Writer) (string, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release
		return "", ctx.Err()
	})
	<-started
	c.Retire()
	<-cancelled
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if c.Running() {
		t.Fatal("fixture unexpectedly has a foreground turn")
	}
	if err := c.WaitIdle(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("background execution omitted", err)
	}
	once.Do(func() { close(release) })
	if err := c.WaitIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}
