package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func TestCancelAtFinalPresentationDoesNotCommit(t *testing.T) {
	for _, at := range []event.Kind{event.Message, event.Usage, event.Notice} {
		t.Run(fmt.Sprint(at), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &mockProvider{name: "synthetic", streams: [][]provider.Chunk{{
				{Type: provider.ChunkText, Text: "Received synthetic answer"},
				{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2, FinishReason: "length"}},
				{Type: provider.ChunkDone},
			}}}
			commits, usageEvents, observed := 0, 0, false
			a := New(p, tool.NewRegistry(), NewSession(""), Options{MaxSteps: 1}, event.FuncSink(func(e event.Event) {
				if e.Kind == at {
					observed = true
					cancel()
				}
				if e.Kind == event.AnswerCommitted {
					commits++
				}
				if e.Kind == event.Usage {
					usageEvents++
				}
			}))
			if err := a.Run(ctx, "Synthetic question"); !errors.Is(err, context.Canceled) || commits != 0 || !observed {
				t.Fatalf("err=%v commits=%d cancellation stage observed=%v", err, commits, observed)
			}
			if usageEvents != 1 {
				t.Fatalf("real usage events=%d, want 1", usageEvents)
			}
			messages := a.Session().Snapshot()
			if messages[len(messages)-1].Content != "Received synthetic answer" {
				t.Fatal("received answer was lost")
			}
		})
	}
}

func TestCancelBeforeDispatchRetainsUnexecutedToolReceipts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var executed int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "synthetic", readOnly: true, calls: &executed})
	calls := []provider.ToolCall{{ID: "first", Name: "synthetic", Arguments: `{}`}, {ID: "second", Name: "synthetic", Arguments: `{}`}}
	p := &mockProvider{name: "synthetic", streams: [][]provider.Chunk{{
		{Type: provider.ChunkToolCall, ToolCall: &calls[0]},
		{Type: provider.ChunkToolCall, ToolCall: &calls[1]},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{TotalTokens: 2}},
		{Type: provider.ChunkDone},
	}}}
	a := New(p, reg, NewSession(""), Options{MaxSteps: 1}, event.FuncSink(func(e event.Event) {
		if e.Kind == event.Usage {
			cancel()
		}
	}))
	if err := a.Run(ctx, "Synthetic task"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if executed != 0 {
		t.Fatal("cancelled tool executed")
	}
	messages := a.Session().Snapshot()
	if len(messages) != 4 || len(messages[1].ToolCalls) != 2 {
		t.Fatalf("cancelled tool protocol was dropped: %+v", messages)
	}
	for i, call := range calls {
		m := messages[i+2]
		if m.Role != provider.RoleTool || m.ToolCallID != call.ID || m.Name != call.Name || m.Content != "error: context canceled (not executed)" {
			t.Fatalf("missing cancellation receipt %d: %+v", i, m)
		}
	}
}
