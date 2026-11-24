package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type syntheticCancellationSnapshot struct{}

func (syntheticCancellationSnapshot) CancellationError(cause error) error {
	return provider.NoRequestReplay(fmt.Errorf("synthetic dispatch_ack: %w", cause))
}

type cancellationExitProvider struct {
	mode     string
	snapshot bool
	cancel   context.CancelFunc
	requests int
	cause    error
}

func (*cancellationExitProvider) Name() string { return "synthetic-cancellation" }
func (p *cancellationExitProvider) BeginTask(ctx context.Context, _, _ string) (context.Context, func(), error) {
	if p.mode == "deadline" {
		ctx, p.cancel = context.WithTimeout(ctx, 40*time.Millisecond)
	} else {
		ctx, p.cancel = context.WithCancel(ctx)
	}
	return ctx, p.cancel, nil
}
func (p *cancellationExitProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.requests++
	if p.snapshot {
		provider.RecordCancellationDiagnostic(ctx, syntheticCancellationSnapshot{})
	}
	if p.mode == "eof" || p.mode == "chunk-error" {
		ch := make(chan provider.Chunk, 1)
		go func() {
			time.Sleep(time.Millisecond)
			p.cancel()
			if p.mode == "chunk-error" {
				ch <- provider.Chunk{Type: provider.ChunkError, Err: p.cause}
			}
			close(ch)
		}()
		return ch, nil
	}
	if p.mode == "hanging" || p.mode == "deadline" {
		if p.mode == "hanging" {
			go p.cancel()
		}
		return make(chan provider.Chunk), nil
	}
	p.cancel()
	if p.mode == "uncertain" {
		return nil, provider.NoRequestReplay(p.cause)
	}
	return nil, ctx.Err()
}

func TestCancellationDiagnosticEarlyAndHangingExits(t *testing.T) {
	for _, entry := range []string{"turn", "compact"} {
		for _, mode := range []string{"synchronous", "uncertain", "hanging", "deadline", "eof", "chunk-error", "ordinary-api"} {
			t.Run(entry+"/"+mode, func(t *testing.T) {
				p := &cancellationExitProvider{mode: mode, snapshot: mode != "ordinary-api", cause: errors.New("synthetic uncertain dispatch")}
				sess := compactionFailureSession()
				before := sess.Snapshot()
				a := New(p, tool.NewRegistry(), sess, Options{RecentKeep: 2}, event.Discard)
				done := make(chan error, 1)
				go func() {
					if entry == "compact" {
						done <- a.CompactNow(context.Background(), "")
					} else {
						done <- a.Run(context.Background(), "synthetic question")
					}
				}()
				var err error
				select {
				case err = <-done:
				case <-time.After(time.Second):
					t.Fatal("cancellation waited for an absent provider terminal event")
				}
				cause := context.Canceled
				if mode == "deadline" {
					cause = context.DeadlineExceeded
				}
				if !errors.Is(err, cause) || p.requests != 1 {
					t.Fatalf("cause lost or replayed: %v (%d requests)", err, p.requests)
				}
				if entry == "compact" && !reflect.DeepEqual(before, sess.Snapshot()) {
					t.Fatal("cancelled summary changed history")
				}
				if mode == "ordinary-api" {
					if entry == "turn" && provider.ReplayForbidden(err) {
						t.Fatal("ordinary API cancellation policy changed")
					}
					return
				}
				if !provider.ReplayForbidden(err) {
					t.Fatalf("lost no-replay: %v", err)
				}
				if mode == "uncertain" && !errors.Is(err, p.cause) {
					t.Fatal("original non-replayable cause was discarded")
				}
				for unwrapped := err; unwrapped != nil; unwrapped = errors.Unwrap(unwrapped) {
					if strings.Contains(unwrapped.Error(), "synthetic dispatch_ack") {
						return
					}
				}
				t.Fatalf("diagnostic lost: %v", err)
			})
		}
	}
}

func TestAgentCancellationKeepsSnapshotWithoutWaitingForProvider(t *testing.T) {
	for _, entry := range []string{"turn", "compact"} {
		t.Run(entry, func(t *testing.T) {
			p := newTaskScopeProvider(t)
			p.onStream = func(ctx context.Context, _ provider.Request) {
				provider.RecordCancellationDiagnostic(ctx, syntheticCancellationSnapshot{})
				p.cancelScopes()
			}
			sess := compactionFailureSession()
			before := sess.Snapshot()
			a := New(p, tool.NewRegistry(), sess, Options{RecentKeep: 2}, event.Discard)
			var err error
			if entry == "compact" {
				err = a.CompactNow(context.Background(), "")
				if !reflect.DeepEqual(before, sess.Snapshot()) {
					t.Fatal("cancelled summary changed history")
				}
			} else {
				err = a.Run(context.Background(), "synthetic question")
			}
			if !errors.Is(err, context.Canceled) || !provider.ReplayForbidden(err) {
				t.Fatalf("cancellation or no-replay lost: %v", err)
			}
			for cause := err; cause != nil; cause = errors.Unwrap(cause) {
				if cause.Error() == "synthetic dispatch_ack: context canceled" {
					return
				}
			}
			t.Fatalf("safe diagnostic overwritten: %v", err)
		})
	}
}
