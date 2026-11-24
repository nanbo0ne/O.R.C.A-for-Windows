package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/jobs"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type taskProviderObservation struct {
	ctx context.Context
	req provider.Request
}

type taskAttributionProvider struct {
	entered chan taskProviderObservation
	finish  chan struct{}
}

func (*taskAttributionProvider) Name() string { return "ordinary-test-provider" }

func (p *taskAttributionProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.entered <- taskProviderObservation{ctx, req}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.finish:
	}
	out := make(chan provider.Chunk, 3)
	out <- provider.Chunk{Type: provider.ChunkText, Text: "synthetic answer"}
	out <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{TotalTokens: 3}}
	out <- provider.Chunk{Type: provider.ChunkDone}
	close(out)
	return out, nil
}

func awaitTaskObservation(t *testing.T, ch <-chan taskProviderObservation) taskProviderObservation {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("task did not reach provider")
		return taskProviderObservation{}
	}
}

func TestBackgroundTaskProviderKeepsParentTurn(t *testing.T) {
	for _, stopJob := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete-after-parent", true: "job-cancellation"}[stopJob], func(t *testing.T) {
			p := &taskAttributionProvider{entered: make(chan taskProviderObservation, 1), finish: make(chan struct{})}
			manager := jobs.NewManager(event.Discard)
			t.Cleanup(manager.Close)
			task := newTestTaskTool(t, p, tool.NewRegistry(), "synthetic system", "", "", nil)
			ctx, end := WithParentTurn(testTaskContext())
			defer end()
			parentID, parentLifetime := ParentTurn(ctx)
			usage := make(chan event.Event, 1)
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.Usage {
					usage <- e
				}
			})
			ctx = jobs.WithManager(withCallContext(ctx, "task-call", sink, nil), manager)
			if _, err := task.Execute(ctx, []byte(`{"prompt":"synthetic task","run_in_background":true}`)); err != nil {
				t.Fatal(err)
			}
			got := awaitTaskObservation(t, p.entered)
			turn, lifetime := ParentTurn(got.ctx)
			if turn != parentID || lifetime != parentLifetime {
				t.Error("provider context lost the original parent turn or its budget lifetime")
			}
			if got.req.RequestID == "" || got.req.Purpose != provider.RequestPurposeTurn || got.req.ParentTaskID != parentID {
				t.Error("background request lost its request ID or purpose")
			}
			end()
			if got.ctx.Err() != nil {
				t.Fatal("ending the parent turn canceled a session-owned background job")
			}
			if stopJob {
				manager.Stop()
				if !errors.Is(got.ctx.Err(), context.Canceled) {
					t.Error("job cancellation did not reach provider")
				}
			} else {
				close(p.finish)
				wait, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				results := manager.Wait(wait, []string{"task-1"}, 0)
				if len(results) != 1 || results[0].Status != jobs.Done {
					t.Fatalf("background job did not complete: %+v", results)
				}
				select {
				case e := <-usage:
					if e.ParentTurnID != turn || e.RequestID != got.req.RequestID {
						t.Error("usage event and provider receipt attribution disagree")
					}
				default:
					t.Error("completed background job did not emit usage")
				}
				manager.Stop()
			}
			wait, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := manager.WaitStopped(wait); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestForegroundTaskProviderRetainsParentCancellation(t *testing.T) {
	p := &taskAttributionProvider{entered: make(chan taskProviderObservation, 1), finish: make(chan struct{})}
	task := newTestTaskTool(t, p, tool.NewRegistry(), "synthetic system", "", "", nil)
	ctx, cancel := WithParentTurn(testTaskContext())
	defer cancel()
	parentID, parentLifetime := ParentTurn(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := task.Execute(ctx, []byte(`{"prompt":"synthetic task"}`))
		done <- err
	}()
	got := awaitTaskObservation(t, p.entered)
	if got.req.ParentTaskID != parentID {
		t.Error("foreground request lost host parent attribution")
	}
	if turn, lifetime := ParentTurn(got.ctx); turn != parentID || lifetime != parentLifetime {
		t.Error("foreground provider lost parent attribution")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !errors.Is(got.ctx.Err(), context.Canceled) {
			t.Errorf("foreground task ignored cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("foreground task did not stop")
	}
}
