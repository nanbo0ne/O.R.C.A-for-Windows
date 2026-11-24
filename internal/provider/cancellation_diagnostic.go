package provider

import (
	"context"
	"errors"
	"sync"
)

// CancellationDiagnostic is an immutable, content-free snapshot. Implementations
// must only wrap cause; they must not wait for network or provider cleanup.
type CancellationDiagnostic interface {
	CancellationError(cause error) error
}

type cancellationDiagnosticKey struct{}
type cancellationDiagnosticSlot struct {
	mu       sync.RWMutex
	snapshot CancellationDiagnostic
}

// WithCancellationDiagnostic gives one completion its own slot. Child contexts
// share it, but subsequent completions must create a fresh slot.
func WithCancellationDiagnostic(ctx context.Context) context.Context {
	return context.WithValue(ctx, cancellationDiagnosticKey{}, &cancellationDiagnosticSlot{})
}

func RecordCancellationDiagnostic(ctx context.Context, snapshot CancellationDiagnostic) {
	if slot, ok := ctx.Value(cancellationDiagnosticKey{}).(*cancellationDiagnosticSlot); ok {
		slot.mu.Lock()
		slot.snapshot = snapshot
		slot.mu.Unlock()
	}
}

// CancellationError never waits for a final provider event. Missing diagnostics
// and providers that do not implement this optional facility retain ctx.Err().
func CancellationError(ctx context.Context) error {
	cause := ctx.Err()
	if cause == nil {
		return nil
	}
	result := cause
	if slot, ok := ctx.Value(cancellationDiagnosticKey{}).(*cancellationDiagnosticSlot); ok {
		slot.mu.RLock()
		snapshot := slot.snapshot
		slot.mu.RUnlock()
		if snapshot != nil {
			if err := snapshot.CancellationError(cause); errors.Is(err, cause) {
				result = err
			}
		}
	}
	// Only explicitly safe diagnostics may annotate a task-level cancel cause.
	// Arbitrary context causes can contain private provider text.
	if detail, ok := context.Cause(ctx).(CancellationDiagnostic); ok {
		if err := detail.CancellationError(result); errors.Is(err, result) {
			return err
		}
	}
	return result
}
