package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func TestSerialTaskToolsExecuteInOrderAndHonorCancellation(t *testing.T) {
	for _, cancelFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelFirst), func(t *testing.T) {
			ctx, cancel := context.WithCancel(provider.WithSerialTaskTools(context.Background(), true))
			defer cancel()
			var mu sync.Mutex
			var order []int
			reg := tool.NewRegistry()
			var calls []provider.ToolCall
			for i := 0; i < 32; i++ {
				name := fmt.Sprintf("read_%d", i)
				reg.Add(batchCancellationTool{fakeTool: fakeTool{name: name, readOnly: true}, run: func(context.Context) (string, error) {
					mu.Lock()
					defer mu.Unlock()
					if len(order) != i {
						t.Errorf("execution %d started after %v", i, order)
					}
					order = append(order, i)
					if i == 0 && cancelFirst {
						cancel()
					}
					return name, nil
				}})
				calls = append(calls, provider.ToolCall{ID: name, Name: name, Arguments: `{}`})
			}
			a := New(nil, reg, NewSession(""), Options{}, event.Discard)
			results := a.executeBatch(ctx, calls)
			want := 32
			if cancelFirst {
				want = 1
			}
			if len(order) != want || len(results) != len(calls) {
				t.Fatalf("executed=%v results=%d", order, len(results))
			}
		})
	}
}

type serialScopeProvider struct{ *taskScopeProvider }

func (p *serialScopeProvider) BeginTask(ctx context.Context, conversation, task string) (context.Context, func(), error) {
	ctx, end, err := p.taskScopeProvider.BeginTask(ctx, conversation, task)
	if err != nil {
		return nil, nil, err
	}
	return provider.WithSerialTaskTools(ctx, true), end, nil
}

// A wrapper that forwards the existing TaskProvider contract needs no new
// optional interface to preserve the source's execution policy.
type serialScopeWrapper struct {
	provider.Provider
	task provider.TaskProvider
}

func (p serialScopeWrapper) BeginTask(ctx context.Context, conversation, task string) (context.Context, func(), error) {
	return p.task.BeginTask(ctx, conversation, task)
}

func TestSerialTaskToolsWrapperAndNestedSourceIsolation(t *testing.T) {
	answer := []provider.Chunk{{Type: provider.ChunkText, Text: "Synthetic answer"}, {Type: provider.ChunkDone}}
	child := newTaskScopeProvider(t, answer)
	childCalls := 0
	child.onStream = func(ctx context.Context, _ provider.Request) {
		childCalls++
		if provider.SerialTaskTools(ctx) {
			t.Error("API child inherited web dispatch policy")
		}
	}
	parent := &serialScopeProvider{newTaskScopeProvider(t, answer)}
	parentCalls := 0
	parent.onStream = func(ctx context.Context, _ provider.Request) {
		parentCalls++
		if !provider.SerialTaskTools(ctx) {
			t.Error("wrapper lost web dispatch policy")
		}
		a := New(child, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
		if err := a.Run(ctx, "Synthetic child question"); err != nil {
			t.Error(err)
		}
		if !provider.SerialTaskTools(ctx) {
			t.Error("child changed parent's dispatch policy")
		}
	}
	wrapper := serialScopeWrapper{Provider: parent, task: parent}
	a := New(wrapper, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "Synthetic parent question"); err != nil {
		t.Fatal(err)
	}
	if parentCalls != 1 || childCalls != 1 {
		t.Fatalf("parent=%d child=%d", parentCalls, childCalls)
	}
}
