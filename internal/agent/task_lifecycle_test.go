package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func TestForegroundTaskReportsPairedLifecycleAndUsage(t *testing.T) {
	for _, withUsage := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing-receipt", true: "receipt"}[withUsage], func(t *testing.T) {
			chunks := []provider.Chunk{{Type: provider.ChunkText, Text: "synthetic answer"}}
			if withUsage {
				chunks = append(chunks, provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7}})
			}
			chunks = append(chunks, provider.Chunk{Type: provider.ChunkDone})
			sub := &mockProvider{name: "sub", chunks: chunks}
			task := newTestTaskTool(t, sub, tool.NewRegistry(), "synthetic system", "", "", nil)
			var events []event.Event
			sink := event.FuncSink(func(e event.Event) { events = append(events, e) })
			ctx, cancel := WithParentTurn(testTaskContext())
			defer cancel()
			ctx = withCallContext(ctx, "task-1", sink, nil)
			if _, err := task.Execute(ctx, []byte(`{"prompt":"synthetic task"}`)); err != nil {
				t.Fatal(err)
			}
			if len(events) < 2 || events[0].Kind != event.ChildStarted || events[len(events)-1].Kind != event.ChildDone {
				t.Fatalf("unpaired child lifecycle: %+v", events)
			}
			first, last := events[0], events[len(events)-1]
			if first.ChildID == "" || first.ChildID != last.ChildID || last.ChildUsageReported != withUsage {
				t.Fatalf("wrong child identity or receipt state: %+v", events)
			}
			turn, _ := ParentTurn(ctx)
			for _, e := range events {
				if e.ParentTurnID != turn || e.ChildID != first.ChildID {
					t.Fatalf("event lost parent/child association: %+v", e)
				}
			}
		})
	}
}

func TestForegroundTaskFailureStillEndsObservation(t *testing.T) {
	for _, failure := range []error{errors.New("synthetic failure"), context.Canceled} {
		sub := &mockProvider{name: "sub", chunks: []provider.Chunk{{Type: provider.ChunkError, Err: failure}}}
		task := newTestTaskTool(t, sub, tool.NewRegistry(), "system", "", "", nil)
		var events []event.Event
		ctx, cancel := WithParentTurn(testTaskContext())
		ctx = withCallContext(ctx, "task-1", event.FuncSink(func(e event.Event) { events = append(events, e) }), nil)
		_, err := task.Execute(ctx, []byte(`{"prompt":"synthetic failure"}`))
		cancel()
		if err == nil || len(events) != 2 || events[0].Kind != event.ChildStarted || events[1].Kind != event.ChildDone || events[0].ChildID != events[1].ChildID {
			t.Fatalf("failure left a running child: error=%v events=%+v", err, events)
		}
	}
}
