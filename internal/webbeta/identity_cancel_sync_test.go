package webbeta

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

// Hold asynchronous identity callbacks until after the gate returns. This is
// a legal scheduling delay, made deterministic without sleeps or CPU timing.
type heldIdentityCallbacks struct {
	context.Context
	mu        sync.Mutex
	callbacks []func()
}

func (*heldIdentityCallbacks) Value(any) any { return nil }
func (c *heldIdentityCallbacks) AfterFunc(f func()) func() bool {
	c.mu.Lock()
	c.callbacks = append(c.callbacks, f)
	c.mu.Unlock()
	return func() bool { return false }
}
func (c *heldIdentityCallbacks) release() {
	c.mu.Lock()
	callbacks := c.callbacks
	c.callbacks = nil
	c.mu.Unlock()
	for _, f := range callbacks {
		f()
	}
}

type revokeIdentityGate struct {
	e      *Engine
	checks int
	err    error
	ctxErr error
}

func (g *revokeIdentityGate) Check(ctx context.Context, _ string, _ json.RawMessage, _ bool) (bool, string, error) {
	g.checks++
	g.err = g.e.StopForIdentityChange()
	g.ctxErr = ctx.Err()
	return true, "synthetic allow immediately after identity revocation", nil
}

func TestActualAgentGateCannotAllowToolAfterIdentityRevocation(t *testing.T) {
	b := &toolLoopBrowser{}
	e := testEngine(t, b)
	held := &heldIdentityCallbacks{Context: e.ctx}
	e.ctx = held
	defer held.release()
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	registry := tool.NewRegistry()
	capability := &probeTool{}
	registry.Add(capability)
	gate := &revokeIdentityGate{e: e}
	a := agent.New(&Adapter{}, registry, agent.NewSession("isolated revocation regression"), agent.Options{Gate: gate}, event.Discard)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := a.Run(parent, "Use synthetic_echo once.")
	if gate.err != nil || gate.checks != 1 {
		t.Fatalf("revocation=%v gate=%d", gate.err, gate.checks)
	}
	if capability.calls != 0 {
		t.Errorf("tool executed %d times after identity was revoked", capability.calls)
	}
	if !errors.Is(gate.ctxErr, context.Canceled) {
		t.Errorf("gate context still live after StopForIdentityChange: %v", gate.ctxErr)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Agent did not observe synchronous cancellation: %v", err)
	}
	if parent.Err() != nil {
		t.Fatal("web identity revoked its unrelated API parent context")
	}
	if b.taskCalls != 1 {
		t.Fatalf("unexpected post-revocation web request: %d", b.taskCalls)
	}
}

func TestRequestContextRegisteredAfterCloseIsAlreadyCancelled(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	held := &heldIdentityCallbacks{Context: e.ctx}
	e.ctx = held
	defer held.release()
	e.startClose()
	ctx, cancel := e.requestContext(context.Background())
	defer cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("late context was handed out live after close")
	}
}

func TestIdentityCloseRacesRegistrationAndReleaseWithoutLiveContexts(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	const count = 64
	contexts := make([]context.Context, count)
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(count)
	done.Add(count + 1)
	for i := range contexts {
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-start
			ctx, cancel := e.requestContext(parent)
			contexts[i] = ctx
			if i%2 == 0 {
				cancel()
				cancel()
			}
		}(i)
	}
	go func() { defer done.Done(); <-start; e.startClose() }()
	ready.Wait()
	close(start)
	done.Wait()
	for i, ctx := range contexts {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Errorf("context %d escaped revocation", i)
		}
	}
	if parent.Err() != nil {
		t.Fatal("identity close cancelled caller's API context")
	}
	e.mu.Lock()
	remaining := len(e.requestCancels)
	e.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("retained %d request registrations after close", remaining)
	}
}

func TestCompletedRequestUnregistersWithoutAffectingSibling(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	first, cancelFirst := e.requestContext(context.Background())
	second, cancelSecond := e.requestContext(context.Background())
	defer cancelSecond()
	cancelFirst()
	cancelFirst()
	if first.Err() == nil || second.Err() != nil {
		t.Fatal("request release cancelled wrong context")
	}
	e.mu.Lock()
	remaining := len(e.requestCancels)
	e.mu.Unlock()
	if remaining != 1 {
		t.Fatalf("completed request still registered: %d", remaining)
	}
	e.startClose()
	if second.Err() == nil {
		t.Fatal("remaining request not synchronously cancelled")
	}
}
