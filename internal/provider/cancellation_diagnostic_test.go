package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type testCancellationSnapshot string

func (s testCancellationSnapshot) CancellationError(cause error) error {
	return NoRequestReplay(fmt.Errorf("%s: %w", s, cause))
}

type invalidCancellationSnapshot struct{}

func (invalidCancellationSnapshot) CancellationError(error) error { return errors.New("unrelated") }

func TestCancellationDiagnosticOptionalAndPreservesCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracked := WithCancellationDiagnostic(ctx)
	RecordCancellationDiagnostic(tracked, testCancellationSnapshot("dispatch_ack"))
	if err := CancellationError(tracked); err != nil {
		t.Fatalf("live request has no cancellation: %v", err)
	}
	cancel()
	if CancellationError(ctx) != context.Canceled {
		t.Fatal("ordinary provider semantics changed")
	}
	err := CancellationError(tracked)
	if !errors.Is(err, context.Canceled) || !ReplayForbidden(err) || err.Error() != "dispatch_ack: context canceled" {
		t.Fatal(err)
	}
	RecordCancellationDiagnostic(tracked, invalidCancellationSnapshot{})
	if CancellationError(tracked) != context.Canceled {
		t.Fatal("diagnostic replaced cancellation cause")
	}
	RecordCancellationDiagnostic(tracked, nil)
	if CancellationError(tracked) != context.Canceled {
		t.Fatal("cleared diagnostic changed cause")
	}
}

func TestCancellationDiagnosticsDoNotCrossCompletions(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	old := WithCancellationDiagnostic(ctx)
	current := WithCancellationDiagnostic(old)
	RecordCancellationDiagnostic(current, testCancellationSnapshot("current"))
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 100; j++ {
				RecordCancellationDiagnostic(old, testCancellationSnapshot("late old request"))
				_ = CancellationError(old)
			}
		}()
	}
	workers.Wait()
	err := CancellationError(current)
	if !errors.Is(err, context.DeadlineExceeded) || err.Error() != "current: context deadline exceeded" {
		t.Fatalf("old completion contaminated new one: %v", err)
	}
	if CancellationError(WithCancellationDiagnostic(current)) != context.DeadlineExceeded {
		t.Fatal("new completion inherited stale diagnostic")
	}
}
