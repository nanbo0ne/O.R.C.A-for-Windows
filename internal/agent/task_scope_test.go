package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/jobs"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type taskScopeRecord struct {
	ctx                    context.Context
	conversationID, taskID string
}

// The synthetic identity cancels scopes, not request contexts or API parents.
type taskScopeProvider struct {
	*mockProvider
	identity context.Context
	revoke   context.CancelFunc
	beginErr error
	mu       sync.Mutex
	records  []taskScopeRecord
	released int
	active   int
	onStream func(context.Context, provider.Request)
}

var _ provider.TaskProvider = (*taskScopeProvider)(nil)

func newTaskScopeProvider(t *testing.T, streams ...[]provider.Chunk) *taskScopeProvider {
	t.Helper()
	identity, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &taskScopeProvider{
		mockProvider: &mockProvider{name: "synthetic-web", streams: streams},
		identity:     identity, revoke: cancel,
	}
}

func (p *taskScopeProvider) BeginTask(ctx context.Context, conversationID, parentTaskID string) (context.Context, func(), error) {
	if p.beginErr != nil {
		return nil, nil, p.beginErr
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(p.identity, cancel)
	if p.identity.Err() != nil {
		cancel()
	}
	p.mu.Lock()
	p.records = append(p.records, taskScopeRecord{ctx, conversationID, parentTaskID})
	p.active++
	p.mu.Unlock()
	return ctx, func() {
		stop()
		cancel()
		p.mu.Lock()
		p.active--
		p.released++
		p.mu.Unlock()
	}, nil
}

func (p *taskScopeProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if p.onStream != nil {
		p.onStream(ctx, req)
	}
	return p.mockProvider.Stream(ctx, req)
}

func (p *taskScopeProvider) counts() (active, released int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active, p.released
}

func (p *taskScopeProvider) cancelScopes() {
	p.revoke()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range p.records {
		<-r.ctx.Done()
	}
}

type taskScopeTool struct {
	name string
	run  func(context.Context) (string, error)
}

func (t taskScopeTool) Name() string            { return t.name }
func (t taskScopeTool) Description() string     { return "synthetic task-scope fixture" }
func (t taskScopeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t taskScopeTool) ReadOnly() bool          { return false }
func (t taskScopeTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	return t.run(ctx)
}

type taskScopeGate func(context.Context) (bool, string, error)

func (g taskScopeGate) Check(ctx context.Context, _ string, _ json.RawMessage, _ bool) (bool, string, error) {
	return g(ctx)
}

type taskScopeHooks struct {
	pre  func(context.Context)
	post func(context.Context)
}

func (h taskScopeHooks) PreToolUse(ctx context.Context, _ string, _ json.RawMessage) (bool, string) {
	if h.pre != nil {
		h.pre(ctx)
	}
	return false, ""
}
func (h taskScopeHooks) PostToolUse(ctx context.Context, _ string, _ json.RawMessage, _ string) {
	if h.post != nil {
		h.post(ctx)
	}
}
func (taskScopeHooks) PostLLMCall(_ context.Context, text string, _ int) string { return text }
func (taskScopeHooks) HasPostLLMCall() bool                                     { return false }
func (taskScopeHooks) SubagentStop(context.Context, string)                     {}
func (taskScopeHooks) PreCompact(context.Context, string) string                { return "" }

func taskScopeCalls(names ...string) []provider.Chunk {
	var chunks []provider.Chunk
	for _, name := range names {
		chunks = append(chunks, provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "synthetic-" + name, Name: name, Arguments: `{}`,
		}})
	}
	return append(chunks, provider.Chunk{Type: provider.ChunkDone})
}

func taskScopeAnswer() []provider.Chunk {
	return []provider.Chunk{{Type: provider.ChunkText, Text: "synthetic answer"}, {Type: provider.ChunkDone}}
}

func taskScopeAwait(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal(description)
	}
}

func taskScopeResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("task did not finish")
		return nil
	}
}

func TestTaskScopeMixedSourceCancelsToolsWithoutCancelingAPIParent(t *testing.T) {
	p := newTaskScopeProvider(t, taskScopeCalls("blocking", "forbidden"), taskScopeAnswer())
	entered, canceled, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var finishOnce sync.Once
	defer finishOnce.Do(func() { close(finish) })
	var forbidden atomic.Int32
	reg := tool.NewRegistry()
	reg.Add(taskScopeTool{"blocking", func(ctx context.Context) (string, error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-finish
		return "", ctx.Err()
	}})
	reg.Add(taskScopeTool{"forbidden", func(context.Context) (string, error) {
		forbidden.Add(1)
		return "unexpected", nil
	}})
	task := newTestTaskTool(t, p, reg, "synthetic", "", "", nil)
	parentReg := tool.NewRegistry()
	parentReg.Add(task)
	api := &mockProvider{name: "synthetic-api", streams: [][]provider.Chunk{
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "delegate", Name: "task", Arguments: `{"prompt":"synthetic child"}`}}, {Type: provider.ChunkDone}},
		taskScopeAnswer(),
	}}
	parent := New(api, parentReg, NewSession("synthetic"), Options{}, event.Discard)
	ctx, cancel := context.WithTimeout(testTaskContext(), 8*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- parent.Run(ctx, "synthetic parent") }()
	taskScopeAwait(t, entered, "web child never reached tool")
	p.cancelScopes()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		cancel()
		taskScopeAwait(t, canceled, "tool did not honor caller cleanup cancellation")
		finishOnce.Do(func() { close(finish) })
		_ = taskScopeResult(t, done)
		t.Fatal("identity cancellation did not reach the active web-origin tool")
	}
	if active, released := p.counts(); active != 1 || released != 0 {
		t.Errorf("scope released before tool cleanup: active=%d released=%d", active, released)
	}
	if ctx.Err() != nil {
		t.Error("identity cancellation escaped to the ordinary API parent")
	}
	ordinary := New(&mockProvider{name: "unrelated-api", chunks: taskScopeAnswer()}, tool.NewRegistry(), NewSession("synthetic"), Options{}, event.Discard)
	if err := ordinary.Run(context.Background(), "independent API task"); err != nil {
		t.Errorf("unrelated API task failed: %v", err)
	}
	finishOnce.Do(func() { close(finish) })
	if err := taskScopeResult(t, done); err != nil {
		t.Errorf("API parent could not handle its canceled child: %v", err)
	}
	// The failed child causes the existing final-readiness check to request one
	// additional API answer; it must not replay the revoked Web child.
	if forbidden.Load() != 0 || len(api.requests) != 3 || len(p.requests) != 1 {
		t.Errorf("forbidden web calls=%d API requests=%d", forbidden.Load(), len(api.requests))
	}
	if len(api.requests) == 3 && lastUser(api.requests[2]) != finalReadinessRetryMessage("latest task action failed") {
		t.Error("unexpected API continuation instead of the existing failed-child readiness check")
	}
	if active, released := p.counts(); active != 0 || released != 1 {
		t.Errorf("scope not released exactly once: active=%d released=%d", active, released)
	}
}

func TestTaskScopeRejectsToolAfterGateReturnsLateApproval(t *testing.T) {
	p := newTaskScopeProvider(t, taskScopeCalls("writer"), taskScopeAnswer())
	var calls int
	reg := tool.NewRegistry()
	reg.Add(taskScopeTool{"writer", func(context.Context) (string, error) { calls++; return "synthetic", nil }})
	a := New(p, reg, NewSession("synthetic"), Options{Gate: taskScopeGate(func(ctx context.Context) (bool, string, error) {
		p.cancelScopes()
		if ctx.Err() == nil {
			t.Error("gate did not receive task cancellation")
		}
		if active, released := p.counts(); active != 1 || released != 0 {
			t.Errorf("gate outlived its scope: active=%d released=%d", active, released)
		}
		return true, "late synthetic approval", nil
	})}, event.Discard)
	if err := a.Run(context.Background(), "synthetic"); !errors.Is(err, context.Canceled) {
		t.Errorf("Run = %v, want canceled", err)
	}
	if calls != 0 {
		t.Fatalf("revoked identity executed %d already-issued tools after gate", calls)
	}
}

func TestTaskScopeAdmissionAndRelease(t *testing.T) {
	for _, outcome := range []string{"success", "stream-error", "begin-error"} {
		t.Run(outcome, func(t *testing.T) {
			wantErr := errors.New("synthetic admission or stream failure")
			p := newTaskScopeProvider(t, taskScopeAnswer())
			if outcome == "begin-error" {
				p.beginErr = wantErr
			} else if outcome == "stream-error" {
				p.streams = [][]provider.Chunk{{{Type: provider.ChunkError, Err: wantErr}}}
			}
			session := NewSession("synthetic")
			a := New(p, tool.NewRegistry(), session, Options{}, event.Discard)
			ctx, end := WithParentTurn(context.Background())
			defer end()
			taskID, _ := ParentTurn(ctx)
			err := a.Run(ctx, "synthetic")
			if outcome == "success" && err != nil || outcome != "success" && !errors.Is(err, wantErr) {
				t.Errorf("Run = %v", err)
			}
			if outcome == "begin-error" {
				if len(p.requests) != 0 || len(session.Messages) != 2 || session.Messages[1].Role != provider.RoleUser || session.Messages[1].Content != "synthetic" {
					t.Error("failed admission lost the accepted user input or called the provider")
				}
				return
			}
			if active, released := p.counts(); active != 0 || released != 1 {
				t.Fatalf("active=%d released=%d", active, released)
			}
			r := p.records[0]
			if r.conversationID != session.ConversationID() || r.taskID != taskID || p.lastReq.ParentTaskID != taskID {
				t.Error("scope and stream lost stable conversation/parent task attribution")
			}
			if ctx.Err() != nil {
				t.Error("release canceled the caller context")
			}
		})
	}
}

func TestTaskScopeRejectsToolAfterPreToolHookRevocation(t *testing.T) {
	p := newTaskScopeProvider(t, taskScopeCalls("writer"))
	var calls int
	reg := tool.NewRegistry()
	reg.Add(taskScopeTool{"writer", func(context.Context) (string, error) { calls++; return "synthetic", nil }})
	a := New(p, reg, NewSession("synthetic"), Options{Hooks: taskScopeHooks{pre: func(context.Context) { p.cancelScopes() }}}, event.Discard)
	if err := a.Run(context.Background(), "synthetic"); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Errorf("revoked pre-tool hook still executed tool: error=%v calls=%d", err, calls)
	}
	if active, released := p.counts(); active != 0 || released != 1 {
		t.Errorf("hook scope: active=%d released=%d", active, released)
	}
}

func TestTaskScopeReleaseWaitsForPostToolHook(t *testing.T) {
	p := newTaskScopeProvider(t, taskScopeCalls("writer"))
	entered, canceled, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(finish) })
	reg := tool.NewRegistry()
	reg.Add(taskScopeTool{"writer", func(context.Context) (string, error) { return "synthetic", nil }})
	a := New(p, reg, NewSession("synthetic"), Options{Hooks: taskScopeHooks{post: func(ctx context.Context) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-finish
	}}}, event.Discard)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, "synthetic") }()
	taskScopeAwait(t, entered, "post-tool hook not entered")
	p.cancelScopes()
	taskScopeAwait(t, canceled, "post-tool hook did not receive task cancellation")
	if active, released := p.counts(); active != 1 || released != 0 {
		t.Errorf("scope released before post-tool hook returned: active=%d released=%d", active, released)
	}
	once.Do(func() { close(finish) })
	if err := taskScopeResult(t, done); !errors.Is(err, context.Canceled) {
		t.Errorf("Run = %v, want canceled", err)
	}
	if active, released := p.counts(); active != 0 || released != 1 {
		t.Errorf("hook scope not released exactly once: active=%d released=%d", active, released)
	}
}

func TestTaskScopeNestedSameProviderReleasesInsideOut(t *testing.T) {
	p := newTaskScopeProvider(t,
		[]provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "delegate", Name: "task", Arguments: `{"prompt":"synthetic child"}`}}, {Type: provider.ChunkDone}},
		taskScopeCalls("probe"), taskScopeAnswer(), taskScopeAnswer())
	reg := tool.NewRegistry()
	reg.Add(taskScopeTool{"probe", func(context.Context) (string, error) {
		if active, released := p.counts(); active != 2 || released != 0 {
			t.Errorf("nested task lost a live scope: active=%d released=%d", active, released)
		}
		return "synthetic", nil
	}})
	reg.Add(newTestTaskTool(t, p, reg, "synthetic", "", "", nil))
	p.onStream = func(_ context.Context, _ provider.Request) {
		if len(p.requests) == 3 {
			if active, released := p.counts(); active != 1 || released != 1 {
				t.Errorf("child did not release independently: active=%d released=%d", active, released)
			}
		}
	}
	a := New(p, reg, NewSession("synthetic"), Options{}, event.Discard)
	ctx, cancel := context.WithTimeout(testTaskContext(), 3*time.Second)
	defer cancel()
	if err := a.Run(ctx, "synthetic parent"); err != nil {
		t.Fatal(err)
	}
	if active, released := p.counts(); active != 0 || released != 2 {
		t.Fatalf("nested scopes: active=%d released=%d", active, released)
	}
	if p.records[0].taskID == "" || p.records[0].taskID != p.records[1].taskID || p.records[0].conversationID == p.records[1].conversationID {
		t.Error("nested scopes must share parent task but have separate conversations")
	}
}

func TestTaskScopeBackgroundSubagentRetainsScopeAfterParentReturns(t *testing.T) {
	p := newTaskScopeProvider(t, taskScopeCalls("blocking"))
	entered, finish := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(finish) })
	reg := tool.NewRegistry()
	reg.Add(taskScopeTool{"blocking", func(ctx context.Context) (string, error) {
		close(entered)
		select {
		case <-finish:
		case <-ctx.Done():
		}
		return "", ctx.Err()
	}})
	manager := jobs.NewManager(event.Discard)
	t.Cleanup(manager.Close)
	task := newTestTaskTool(t, p, reg, "synthetic", "", "", nil)
	ctx, end := WithParentTurn(jobs.WithManager(testTaskContext(), manager))
	defer end()
	parentID, _ := ParentTurn(ctx)
	if _, err := task.Execute(ctx, []byte(`{"prompt":"synthetic child","run_in_background":true}`)); err != nil {
		t.Fatal(err)
	}
	taskScopeAwait(t, entered, "background child never reached tool")
	end()
	if active, released := p.counts(); active != 1 || released != 0 {
		t.Errorf("background scope followed foreground return: active=%d released=%d", active, released)
	}
	p.mu.Lock()
	for _, r := range p.records {
		if r.ctx.Err() != nil {
			t.Error("foreground completion canceled a session-owned background scope")
		}
	}
	p.mu.Unlock()
	p.cancelScopes()
	wait, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	rows := manager.Wait(wait, []string{"task-1"}, 0)
	if len(rows) != 1 || rows[0].Status == jobs.Running {
		t.Error("identity cancellation did not stop background task")
		once.Do(func() { close(finish) })
		manager.Stop()
	}
	drain, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	manager.Stop()
	if err := manager.WaitStopped(drain); err != nil {
		t.Fatal(err)
	}
	if active, released := p.counts(); active != 0 || released != 1 {
		t.Fatalf("background scope not drained: active=%d released=%d", active, released)
	}
	if p.records[0].taskID != parentID {
		t.Error("background scope lost originating task attribution")
	}
}

func TestTaskScopeCoordinatorPlannerCoversExecutor(t *testing.T) {
	for _, withTools := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-tools", true: "tools"}[withTools], func(t *testing.T) {
			planner := newTaskScopeProvider(t, taskScopeAnswer())
			reg := tool.NewRegistry()
			var calls int
			reg.Add(taskScopeTool{"revoke", func(ctx context.Context) (string, error) {
				planner.cancelScopes()
				if ctx.Err() == nil {
					t.Error("planner identity cancellation did not reach executor tool")
				}
				if active, _ := planner.counts(); active != 1 {
					t.Errorf("planner scope released before dependent executor: active=%d", active)
				}
				return "", ctx.Err()
			}})
			reg.Add(taskScopeTool{"forbidden", func(context.Context) (string, error) { calls++; return "synthetic", nil }})
			api := &mockProvider{name: "synthetic-api", streams: [][]provider.Chunk{taskScopeCalls("revoke", "forbidden"), taskScopeAnswer()}}
			executor := New(api, reg, NewSession("synthetic"), Options{}, event.Discard)
			var plannerTools *tool.Registry
			if withTools {
				plannerTools = tool.NewRegistry()
			}
			c := NewCoordinator(planner, NewSession("synthetic planner"), nil, plannerTools, Options{}, executor, 0, event.Discard, nil)
			if err := c.Run(context.Background(), "synthetic task"); !errors.Is(err, context.Canceled) {
				t.Errorf("Run = %v, want canceled", err)
			}
			if calls != 0 {
				t.Errorf("revoked plan executed %d subsequent tools", calls)
			}
			wantReleased := 1
			if withTools {
				wantReleased++
			}
			if active, released := planner.counts(); active != 0 || released != wantReleased {
				t.Errorf("planner scopes: active=%d released=%d", active, released)
			}
		})
	}
}

func TestTaskScopeCoordinatorDoesNotHandoffCanceledOrRejectedPlan(t *testing.T) {
	for _, failure := range []string{"identity-canceled", "admission-rejected", "skip-planner"} {
		t.Run(failure, func(t *testing.T) {
			planner := newTaskScopeProvider(t, taskScopeAnswer())
			wantErr := errors.New("synthetic identity closed")
			if failure == "identity-canceled" {
				wantErr = context.Canceled
				planner.onStream = func(context.Context, provider.Request) { planner.cancelScopes() }
			} else {
				planner.beginErr = wantErr
			}
			api := &mockProvider{name: "synthetic-api", chunks: taskScopeAnswer()}
			executor := New(api, tool.NewRegistry(), NewSession("synthetic"), Options{}, event.Discard)
			c := NewCoordinator(planner, NewSession("synthetic"), nil, nil, Options{}, executor, 0, event.Discard, func(string) bool { return failure != "skip-planner" })
			err := c.Run(context.Background(), "synthetic")
			if failure == "skip-planner" {
				if err != nil || len(api.requests) != 1 || len(planner.requests) != 0 {
					t.Errorf("unused planner blocked ordinary API turn: %v", err)
				}
			} else if !errors.Is(err, wantErr) || len(api.requests) != 0 {
				t.Errorf("invalid plan handed off: error=%v executor requests=%d", err, len(api.requests))
			}
		})
	}
}

type taskScopeTerminalPlanner struct {
	*taskScopeProvider
	requestCtx context.Context
	finished   chan struct{}
}

func (p *taskScopeTerminalPlanner) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.requestCtx = ctx
	out := make(chan provider.Chunk, 2)
	out <- provider.Chunk{Type: provider.ChunkText, Text: "synthetic plan"}
	out <- provider.Chunk{Type: provider.ChunkDone}
	go func() {
		defer close(p.finished)
		defer close(out)
		<-ctx.Done()
	}()
	return out, nil
}

func TestTaskScopeCoordinatorEndsRequestBeforeExecutorButKeepsTaskScope(t *testing.T) {
	p := &taskScopeTerminalPlanner{taskScopeProvider: newTaskScopeProvider(t), finished: make(chan struct{})}
	reg := tool.NewRegistry()
	reg.Add(taskScopeTool{"probe", func(ctx context.Context) (string, error) {
		if p.requestCtx.Err() == nil {
			t.Error("planner request remained live while executor tools were running")
		}
		if active, released := p.counts(); active != 1 || released != 0 || ctx.Err() != nil {
			t.Errorf("ending planner request also ended task scope: active=%d released=%d context=%v", active, released, ctx.Err())
		}
		return "synthetic", nil
	}})
	api := &mockProvider{name: "synthetic-api", streams: [][]provider.Chunk{taskScopeCalls("probe"), taskScopeAnswer()}}
	executor := New(api, reg, NewSession("synthetic"), Options{}, event.Discard)
	c := NewCoordinator(p, NewSession("synthetic planner"), nil, nil, Options{}, executor, 0, event.Discard, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Run(ctx, "synthetic"); err != nil {
		t.Fatal(err)
	}
	taskScopeAwait(t, p.finished, "planner request goroutine did not exit")
	if active, released := p.counts(); active != 0 || released != 1 {
		t.Errorf("planner scope did not release at task end: active=%d released=%d", active, released)
	}
}

func TestTaskScopeParentTaskIDSurvivesToolLoopsAndSteering(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary-api", true: "task-provider"}[scoped], func(t *testing.T) {
			p := newTaskScopeProvider(t, taskScopeCalls("first"), taskScopeCalls("guide"), taskScopeAnswer())
			var prov provider.Provider = p.mockProvider
			if scoped {
				prov = p
			}
			var a *Agent
			var ack <-chan error
			var toolsRun int
			reg := tool.NewRegistry()
			reg.Add(taskScopeTool{"first", func(context.Context) (string, error) {
				toolsRun++
				return "synthetic first result", nil
			}})
			const guidance = "synthetic guidance changes the latest user message"
			reg.Add(taskScopeTool{"guide", func(context.Context) (string, error) {
				toolsRun++
				var accepted bool
				ack, accepted = a.TrySteerRich(RichInput{Text: guidance}, guidance, nil, "synthetic-steer")
				if !accepted {
					t.Error("active tool loop rejected synthetic guidance")
				}
				select {
				case <-ack:
					t.Error("guidance was acknowledged before the next loop consumed it")
				default:
				}
				return "synthetic second result", nil
			}})
			var turns []string
			a = New(prov, reg, NewSession("synthetic"), Options{}, event.FuncSink(func(e event.Event) {
				if e.Kind == event.TurnStarted {
					turns = append(turns, e.TurnID)
				}
			}))
			const input = "synthetic original task"
			if err := a.Run(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			if ack == nil {
				t.Fatal("tool never queued guidance")
			}
			if err := guidanceReceipt(t, ack); err != nil {
				t.Fatal(err)
			}
			if toolsRun != 2 || len(p.requests) != 3 || len(turns) != 1 || turns[0] == "" {
				t.Fatalf("unexpected lifecycle: tools=%d requests=%d turns=%v", toolsRun, len(p.requests), turns)
			}
			seenRequests := make(map[string]bool)
			for i, req := range p.requests {
				if req.ParentTaskID != turns[0] || req.ConversationID != a.Session().ConversationID() {
					t.Errorf("request %d changed parent/conversation attribution: %+v", i, req)
				}
				if req.RequestID == "" || seenRequests[req.RequestID] {
					t.Errorf("request %d reused a per-request ID", i)
				}
				seenRequests[req.RequestID] = true
				wantUser := input
				if i == 2 {
					wantUser = midTurnSteerMessage(guidance)
				}
				if lastUser(req) != wantUser {
					t.Errorf("request %d did not exercise the expected user-message transition", i)
				}
				if i > 0 {
					found := false
					for _, msg := range req.Messages {
						found = found || msg.Role == provider.RoleTool && msg.ToolCallID == "synthetic-first"
					}
					if !found {
						t.Errorf("request %d lost the actual tool continuation", i)
					}
				}
			}
			if scoped && (len(p.records) != 1 || p.records[0].taskID != turns[0]) {
				t.Error("tool/guidance loops opened a new provider task scope")
			}
		})
	}
}

func TestTaskScopeParentTaskIDSurvivesHostGuidanceButChangesNextTurn(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary-api", true: "task-provider"}[scoped], func(t *testing.T) {
			p := newTaskScopeProvider(t, []provider.Chunk{{Type: provider.ChunkDone}}, taskScopeAnswer(), taskScopeAnswer())
			var prov provider.Provider = p.mockProvider
			if scoped {
				prov = p
			}
			var turns []string
			a := New(prov, tool.NewRegistry(), NewSession("synthetic"), Options{}, event.FuncSink(func(e event.Event) {
				if e.Kind == event.TurnStarted {
					turns = append(turns, e.TurnID)
				}
			}))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			const input = "identical synthetic input in two independent turns"
			if err := a.Run(ctx, input); err != nil {
				t.Fatal(err)
			}
			if len(p.requests) != 2 || len(turns) != 1 || turns[0] == "" {
				t.Fatalf("first turn did not perform exactly one host continuation: requests=%d turns=%v", len(p.requests), turns)
			}
			if lastUser(p.requests[0]) != input || lastUser(p.requests[1]) != emptyFinalRetryMessage() {
				t.Fatal("fixture did not change the latest user message via real host guidance")
			}
			if p.requests[0].ParentTaskID != turns[0] || p.requests[1].ParentTaskID != turns[0] {
				t.Error("host guidance changed the parent task ID within one turn")
			}
			if scoped {
				if active, released := p.counts(); active != 0 || released != 1 {
					t.Errorf("first turn scope did not end before next turn: active=%d released=%d", active, released)
				}
			}
			// Reuse the same caller context, session and input. Only the independent
			// Run boundary, not message content or conversation, creates a new ID.
			if err := a.Run(ctx, input); err != nil {
				t.Fatal(err)
			}
			if len(p.requests) != 3 || len(turns) != 2 || turns[1] == "" || turns[1] == turns[0] {
				t.Fatalf("independent turn reused attribution: requests=%d turns=%v", len(p.requests), turns)
			}
			if lastUser(p.requests[2]) != input || p.requests[2].ParentTaskID != turns[1] {
				t.Error("independent turn did not receive its own host task ID")
			}
			seenRequests := make(map[string]bool)
			for i, req := range p.requests {
				if req.ConversationID != a.Session().ConversationID() || req.RequestID == "" || seenRequests[req.RequestID] {
					t.Errorf("request %d changed conversation or reused a request ID", i)
				}
				seenRequests[req.RequestID] = true
			}
			if ctx.Err() != nil {
				t.Error("ending a turn canceled the reusable caller context")
			}
			if scoped {
				if active, released := p.counts(); active != 0 || released != 2 {
					t.Errorf("independent turn scopes: active=%d released=%d", active, released)
				}
				if len(p.records) != 2 || p.records[0].taskID != turns[0] || p.records[1].taskID != turns[1] {
					t.Error("scope attribution did not track the two independent turns")
				}
			}
		})
	}
}
