package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/monitor"
)

type workMonitor struct {
	mu      sync.Mutex
	store   *monitor.Store
	tab     string
	current atomic.Pointer[monitor.Store]
}

// MonitorSubscribe replaces the single visible subscription. Nothing is persisted.
func (a *App) MonitorSubscribe(tabID string) (monitor.Snapshot, error) {
	a.mu.RLock()
	valid := tabID != "" && a.activeTabID == tabID && a.tabs[tabID] != nil
	a.mu.RUnlock()
	if !valid {
		return monitor.Snapshot{}, fmt.Errorf("monitor requires the active tab")
	}
	a.monitor.mu.Lock()
	defer a.monitor.mu.Unlock()
	a.monitor.store.Close()
	a.monitor.store = monitor.New(tabID)
	a.monitor.current.Store(a.monitor.store)
	a.monitor.tab = tabID
	v := a.monitor.store.Snapshot(0)
	return v, nil
}

func (a *App) MonitorUnsubscribe(generation string) {
	a.monitor.mu.Lock()
	defer a.monitor.mu.Unlock()
	if s := a.monitor.store; s != nil && s.Generation() == generation {
		s.Close()
		a.monitor.store = nil
		a.monitor.current.Store(nil)
		a.monitor.tab = ""
	}
}

func (a *App) MonitorSnapshot(generation string, after uint64) monitor.Snapshot {
	a.monitor.mu.Lock()
	defer a.monitor.mu.Unlock()
	s := a.monitor.store
	if s == nil || s.Generation() != generation {
		return monitor.Snapshot{Generation: generation, Entries: []monitor.Entry{}, Expired: true}
	}
	a.mu.RLock()
	valid := a.activeTabID == a.monitor.tab && a.tabs[a.monitor.tab] != nil
	a.mu.RUnlock()
	if !valid {
		s.Close()
	}
	return s.Snapshot(after)
}

// The producer uses try-locks, including the active-tab gate. It never waits on UI.
func (a *App) monitorForTab(tabID string) *monitor.Store {
	if a == nil {
		return nil
	}
	if !a.mu.TryRLock() {
		a.monitor.current.Load().Dropped()
		return nil
	}
	valid := a.activeTabID == tabID && a.tabs[tabID] != nil
	a.mu.RUnlock()
	if !valid {
		return nil
	}
	if !a.monitor.mu.TryLock() {
		a.monitor.current.Load().Dropped()
		return nil
	}
	defer a.monitor.mu.Unlock()
	if a.monitor.tab != tabID {
		return nil
	}
	return a.monitor.store
}

func (s *tabEventSink) WorkMonitorContext(ctx context.Context, turn string) context.Context {
	return monitor.WithBinding(ctx, monitor.Binding{Turn: turn, Sink: func() *monitor.Store { return s.app.monitorForTab(s.tabID) }})
}

func (s *tabEventSink) WorkMonitorState(turn, phase string) {
	if m := s.app.monitorForTab(s.tabID); m != nil {
		m.Record(turn, "", "phase", phase, nil)
		if phase == "error" {
			m.Record(turn, "", "diagnostic", "", func() (json.RawMessage, bool) {
				return json.RawMessage(`{"code":"control_failed","responseCompleteness":"incomplete"}`), true
			})
		}
	}
}

func monitorPhase(e event.Event) string {
	switch e.Kind {
	case event.TurnStarted:
		return "input"
	case event.Reasoning:
		return "reasoning"
	case event.Text:
		return "decode"
	case event.ToolDispatch, event.ToolProgress:
		return "tool"
	case event.ToolResult, event.ApprovalRequest, event.AskRequest, event.Retrying, event.CompactionStarted, event.CompactionDone, event.Phase:
		return "wait"
	case event.TurnDone:
		if e.Outcome == event.TurnOutcomeCancelled || e.Outcome == event.TurnOutcomeInterrupted {
			return "stopped"
		}
		if e.Err != nil || e.Outcome == event.TurnOutcomeFailed {
			return "error"
		}
		if e.Outcome == event.TurnOutcomeSuccess && e.FinalMessageID != "" {
			return "final"
		}
		return "stopped"
	}
	return ""
}

func (s *tabEventSink) observeWorkMonitor(e event.Event) {
	m := s.app.monitorForTab(s.tabID)
	if !m.Active() {
		return
	}
	phase := monitorPhase(e)
	if phase == "" && e.Kind != event.Usage && e.Kind != event.ChildStarted && e.Kind != event.ChildDone && e.Kind != event.AnswerCommitted {
		return
	}
	// Response events are explicitly turn-scoped: auxiliary/parallel requests
	// cannot be reliably joined to a provider request by this event contract.
	turn := e.TurnID
	if e.ParentTurnID != "" {
		turn = e.ParentTurnID
	}
	if phase != "" {
		m.Record(turn, e.RequestID, "phase", phase, nil)
	}
	switch e.Kind {
	case event.Retrying:
		m.Record(turn, e.RequestID, "diagnostic", "", func() (json.RawMessage, bool) {
			d, _ := json.Marshal(map[string]any{"code": "retry_observed", "attempt": e.RetryAttempt, "maxAttempts": e.RetryMax, "responseCompleteness": "unknown"})
			return d, true
		})
	case event.TurnDone:
		if e.Err != nil || e.Outcome == event.TurnOutcomeFailed || e.Outcome == event.TurnOutcomeInterrupted || e.Outcome == event.TurnOutcomeCancelled {
			m.Record(turn, e.RequestID, "diagnostic", "", func() (json.RawMessage, bool) {
				d, _ := json.Marshal(map[string]any{"code": "turn_terminated", "phase": phase, "responseCompleteness": "incomplete"})
				return d, true
			})
		}
	case event.AnswerCommitted:
		m.Record(turn, e.RequestID, "commit", "", func() (json.RawMessage, bool) {
			d, _ := json.Marshal(map[string]string{"finalMessageId": e.FinalMessageID})
			return d, false
		})
	case event.Usage:
		m.Record(turn, e.RequestID, "usage", "", func() (json.RawMessage, bool) {
			d, _ := json.Marshal(map[string]any{"usage": e.Usage, "childId": e.ChildID})
			return d, false
		})
	case event.ChildStarted, event.ChildDone:
		m.Record(turn, e.RequestID, "child", "", func() (json.RawMessage, bool) {
			d, _ := json.Marshal(map[string]any{"id": e.ChildID, "running": e.Kind == event.ChildStarted})
			return d, false
		})
	case event.Reasoning, event.Text:
		m.Record(e.TurnID, e.RequestID, "response", "", func() (json.RawMessage, bool) {
			text, p := monitor.Text(e.Text)
			d, _ := json.Marshal(map[string]string{"channel": phase, "text": text})
			return d, p
		})
	case event.ToolDispatch, event.ToolResult, event.ToolProgress:
		m.Record(e.TurnID, e.RequestID, "tool", "", func() (json.RawMessage, bool) {
			args, ap := monitor.Text(e.Tool.Args)
			output, op := monitor.Text(e.Tool.Output)
			d, _ := json.Marshal(map[string]any{"id": e.Tool.ID, "name": e.Tool.Name, "args": args, "output": output, "failed": e.Tool.Err != "", "partial": e.Tool.Partial, "kind": kindNames[e.Kind]})
			clean, p := monitor.SanitizeJSON(d)
			return clean, p || ap || op
		})
	}
}
