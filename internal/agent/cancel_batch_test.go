package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type batchCancellationTool struct {
	fakeTool
	run func(context.Context) (string, error)
}

func (t batchCancellationTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	return t.run(ctx)
}

func TestExecuteBatchCancellationStopsPendingCalls(t *testing.T) {
	for _, when := range []string{"before batch", "during dispatch", "after first writer"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var executed atomic.Int32
			reg := tool.NewRegistry()
			reg.Add(batchCancellationTool{fakeTool: fakeTool{name: "writer"}, run: func(context.Context) (string, error) {
				executed.Add(1)
				cancel()
				return "first writer completed", nil
			}})
			reg.Add(batchCancellationTool{fakeTool: fakeTool{name: "reader", readOnly: true}, run: func(context.Context) (string, error) {
				executed.Add(1)
				return "must not run", nil
			}})
			calls := []provider.ToolCall{
				{ID: "1", Name: "writer", Arguments: `{}`},
				{ID: "2", Name: "writer", Arguments: `{}`},
				{ID: "3", Name: "reader", Arguments: `{}`},
				{ID: "4", Name: "reader", Arguments: `{}`},
			}
			var dispatches int
			var receipts []event.Tool
			a := New(nil, reg, NewSession(""), Options{}, event.FuncSink(func(e event.Event) {
				switch e.Kind {
				case event.ToolDispatch:
					dispatches++
					if when == "during dispatch" {
						cancel()
					}
				case event.ToolResult:
					receipts = append(receipts, e.Tool)
				}
			}))
			if when == "before batch" {
				cancel()
			}
			results := a.executeBatch(ctx, calls)
			wantExecuted, wantDispatches := 0, 0
			switch when {
			case "during dispatch":
				wantDispatches = 1
			case "after first writer":
				wantExecuted, wantDispatches = 1, len(calls)
			}
			if got := executed.Load(); got != int32(wantExecuted) {
				t.Errorf("executed %d tools, want %d", got, wantExecuted)
			}
			if dispatches != wantDispatches {
				t.Errorf("dispatches=%d, want %d", dispatches, wantDispatches)
			}
			if len(results) != len(calls) || len(receipts) != len(calls) {
				t.Fatalf("incomplete tool results: %d outputs, %d receipts", len(results), len(receipts))
			}
			for i := range calls {
				if receipts[i].ID != calls[i].ID || receipts[i].Output != results[i] {
					t.Errorf("receipt %d is out of order or mismatched", i)
				}
				if i < wantExecuted {
					if results[i] != "first writer completed" || receipts[i].Err != "" {
						t.Errorf("completed result was lost: %+v", receipts[i])
					}
				} else if !strings.Contains(results[i], context.Canceled.Error()) || receipts[i].Err != context.Canceled.Error() {
					t.Errorf("skipped tool must have a cancellation receipt: %+v", receipts[i])
				}
			}
			if a.stormCount != 0 {
				t.Error("cancellation must not count as a tool failure storm")
			}
		})
	}
}

func TestExecuteBatchCancellationSkipsQueuedParallelCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var started atomic.Int32
	reg := tool.NewRegistry()
	reg.Add(batchCancellationTool{fakeTool: fakeTool{name: "reader", readOnly: true}, run: func(ctx context.Context) (string, error) {
		// Fill all eight worker slots before cancelling. Remaining calls are
		// still waiting for a slot and must never enter Execute.
		if started.Add(1) == 8 {
			cancel()
		}
		<-ctx.Done()
		return "", ctx.Err()
	}})
	calls := make([]provider.ToolCall, 12)
	for i := range calls {
		calls[i] = provider.ToolCall{ID: fmt.Sprint(i), Name: "reader", Arguments: `{}`}
	}
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	results := a.executeBatch(ctx, calls)
	if got := started.Load(); got != 8 {
		t.Fatalf("started %d readers, want only the 8 admitted before cancellation", got)
	}
	if len(results) != len(calls) {
		t.Fatalf("got %d results, want %d", len(results), len(calls))
	}
	for i, result := range results {
		if !strings.Contains(result, context.Canceled.Error()) {
			t.Errorf("tool %d missing cancellation result: %q", i, result)
		}
	}
}
