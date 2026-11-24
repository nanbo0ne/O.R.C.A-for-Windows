package jobs

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func TestStopWaitsForKilledExecutionAndRejectsNewJobs(t *testing.T) {
	m := NewManager(nil)
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }); m.Close() })
	j := m.Start("task", "synthetic", func(ctx context.Context, _ io.Writer) (string, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release
		return "finished", nil
	})
	<-started
	m.Kill(j.ID)
	<-cancelled
	m.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := m.WaitStopped(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("killed status was mistaken for actual completion", err)
	}
	rejected := m.Start("task", "stale", func(context.Context, io.Writer) (string, error) {
		t.Error("work started after Stop")
		return "", nil
	})
	if _, status, ok := m.Output(rejected.ID); !ok || status != Killed {
		t.Fatal("late job accepted")
	}
	once.Do(func() { close(release) })
	if err := m.WaitStopped(context.Background()); err != nil {
		t.Fatal(err)
	}
}
