package control

import (
	"context"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"sync"
	"testing"
	"time"
)

type monitorFixtureSink struct {
	mu     sync.Mutex
	phases []string
	turns  []string
	signal chan string
}
type monitorFixtureKey struct{}

func (s *monitorFixtureSink) Emit(event.Event) {}
func (s *monitorFixtureSink) WorkMonitorContext(ctx context.Context, turn string) context.Context {
	return context.WithValue(ctx, monitorFixtureKey{}, turn)
}
func (s *monitorFixtureSink) WorkMonitorState(turn, phase string) {
	s.mu.Lock()
	s.phases = append(s.phases, phase)
	s.turns = append(s.turns, turn)
	s.mu.Unlock()
	select {
	case s.signal <- phase:
	default:
	}
}

func TestWorkMonitorContextAndActualPauseGate(t *testing.T) {
	s := &monitorFixtureSink{signal: make(chan string, 8)}
	c := &Controller{sink: s, activeTurnID: "turn"}
	ctx, cancel := agent.WithParentTurn(context.Background())
	defer cancel()
	id, _ := agent.ParentTurn(ctx)
	if got := c.workMonitorContext(ctx).Value(monitorFixtureKey{}); got != id || id == "" {
		t.Fatal("context identity absent")
	}
	c.SetPaused(true)
	s.mu.Lock()
	first := s.phases[0]
	s.mu.Unlock()
	if first != "wait" {
		t.Fatal("claimed paused before pause gate")
	}
	done := make(chan error, 1)
	go func() { done <- c.waitIfPaused(ctx) }()
	deadline := time.After(time.Second)
	waiting := true
	for waiting {
		select {
		case phase := <-s.signal:
			waiting = phase != "paused"
		case <-deadline:
			t.Fatal("pause gate not observed")
		}
	}
	c.SetPaused(false)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("pause did not release")
	}
	c.mu.Lock()
	c.running = true
	c.cancel = cancel
	c.mu.Unlock()
	if ack := c.CancelTurn("wrong"); ack.Accepted {
		t.Fatal("wrong turn cancelled")
	}
	if ack := c.CancelTurn("turn"); !ack.Accepted {
		t.Fatal("cancel rejected")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phases[len(s.phases)-1] != "cancelling" {
		t.Fatal("accepted cancellation not observed")
	}
}

func TestWorkMonitorAuxiliaryCompletion(t *testing.T) {
	s := &monitorFixtureSink{signal: make(chan string, 8)}
	c := &Controller{sink: s}
	c.workMonitorCompleted("compact", nil)
	c.workMonitorCompleted("cancel", context.Canceled)
	c.workMonitorCompleted("failure", context.DeadlineExceeded)
	if len(s.phases) != 3 || s.phases[0] != "stopped" || s.phases[1] != "stopped" || s.phases[2] != "error" {
		t.Fatal(s.phases)
	}
}
