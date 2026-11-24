package webbeta

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

var _ provider.TaskProvider = (*Adapter)(nil)

type taskEngineKey struct{}
type taskScopeKey struct{}

var ErrTaskDraining = errors.New("网页任务正在停止，请等待当前请求退出后再发送。")

type taskScope struct {
	engine       *Engine
	owner        string
	parentTaskID string
	draining     bool // protected by engine.mu
	done         chan struct{}
	streams      map[chan struct{}]struct{} // actual producer leases, including auxiliary owners
}

// BeginTask holds the identity through host-side tool execution, not just the
// webpage stream. The Agent must defer finish until its tools actually exit.
func (a *Adapter) BeginTask(ctx context.Context, conversationID, parentTaskID string) (context.Context, func(), error) {
	if conversationID == "" || parentTaskID == "" {
		return nil, nil, errors.New("web task has no host conversation/parent identity")
	}
	wait, stop := context.WithTimeout(ctx, 8*time.Second)
	e, err := engineForRequest(wait)
	stop()
	if err != nil {
		return nil, nil, err
	}
	finish, err := e.beginOperation(conversationID)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancelIdentity := e.requestContext(ctx)
	ctx, cancelTask, err := e.taskContext(ctx, parentTaskID)
	if err != nil {
		cancelIdentity()
		finish()
		return nil, nil, err
	}
	parent, _ := ctx.Value(taskScopeKey{}).(*taskScope)
	scope, root, err := e.enterTaskScope(parent, conversationID, parentTaskID)
	if err != nil {
		cancelTask()
		cancelIdentity()
		finish()
		return nil, nil, err
	}
	var once sync.Once
	end := func() {
		once.Do(func() {
			if !root {
				cancelTask()
				cancelIdentity()
				finish()
				return
			}
			e.releaseTaskScope(scope, 8*time.Second, func() { cancelTask(); cancelIdentity() }, finish)
		})
	}
	wait, stop = context.WithTimeout(ctx, 8*time.Second)
	err = WaitUntilAvailable(wait, e)
	stop()
	if err != nil {
		end()
		return nil, nil, err
	}
	ctx = provider.WithSerialTaskTools(ctx, true)
	ctx = context.WithValue(ctx, taskScopeKey{}, scope)
	return context.WithValue(ctx, taskEngineKey{}, e), end, nil
}

func (e *Engine) enterTaskScope(parent *taskScope, owner, parentTaskID string) (*taskScope, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, false, ErrIdentityClosed
	}
	if e.stoppingLocked() {
		return nil, false, ErrTaskDraining
	}
	if existing := e.taskScopes[owner]; existing != nil {
		if existing == parent && parent.engine == e && existing.parentTaskID == parentTaskID && !existing.draining {
			return existing, false, nil
		}
		return nil, false, ErrTaskDraining
	}
	if e.taskScopes == nil {
		e.taskScopes = make(map[string]*taskScope)
	}
	scope := &taskScope{engine: e, owner: owner, parentTaskID: parentTaskID, done: make(chan struct{})}
	e.taskScopes[owner] = scope
	return scope, true, nil
}

func (e *Engine) stoppingLocked() bool {
	for _, scope := range e.taskScopes {
		if scope.draining {
			return true
		}
	}
	return false
}

// Wait only for this owner's producer, never the global operation barrier
// (which includes this scope). A bounded timeout retains admission blocking
// and the identity lease until the producer really exits.
func (e *Engine) releaseTaskScope(scope *taskScope, limit time.Duration, cancel, finish func()) {
	e.mu.Lock()
	scope.draining = true
	var pending []chan struct{}
	for done := range scope.streams {
		pending = append(pending, done)
	}
	e.mu.Unlock()
	cancel()
	complete := func() {
		// Release the operation before publishing owner completion.
		finish()
		e.mu.Lock()
		if e.taskScopes[scope.owner] == scope {
			delete(e.taskScopes, scope.owner)
		}
		close(scope.done)
		e.mu.Unlock()
	}
	if len(pending) == 0 {
		complete()
		return
	}
	timer := time.NewTimer(limit)
	defer timer.Stop()
	for index, done := range pending {
		select {
		case <-done:
		case <-timer.C:
			go func() {
				for _, remaining := range pending[index:] {
					<-remaining
				}
				complete()
			}()
			return
		}
	}
	complete()
}

// WaitOwnerDrain observes only an already-releasing task on this identity.
// Revocation cancels work but never fabricates producer completion. Callers
// must not mark the owner idle on a wait error.
func (e *Engine) WaitOwnerDrain(ctx context.Context, owner string) error {
	e.mu.Lock()
	scope := e.taskScopes[owner]
	if scope == nil || !scope.draining {
		e.mu.Unlock()
		return nil
	}
	done := scope.done
	e.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WaitTaskDrain joins already-releasing Web scopes belonging to one host task,
// including child agents with different owners. Active scopes are not joined:
// callers finalize after the Agent body and its scoped releases have returned.
func (e *Engine) WaitTaskDrain(ctx context.Context, parentTaskID string) error {
	if parentTaskID == "" {
		return nil
	}
	e.mu.Lock()
	var pending []<-chan struct{}
	for _, scope := range e.taskScopes {
		if scope.draining && scope.parentTaskID == parentTaskID {
			pending = append(pending, scope.done)
		}
	}
	e.mu.Unlock()
	for _, done := range pending {
		select {
		case <-done:
			continue
		default:
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func engineForRequest(ctx context.Context) (*Engine, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e, ok := ctx.Value(taskEngineKey{}).(*Engine); ok {
		if e != CurrentEngine() || e.ctx.Err() != nil {
			return nil, ErrIdentityClosed
		}
		return e, nil
	}
	return AwaitEngine(ctx)
}
