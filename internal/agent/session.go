// Package agent wires a Provider, a tool Registry, and a Session into the
// harness loop that drives a coding task to completion.
package agent

import (
	"sync"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// Session holds the conversation history for one task. The run loop (one turn at
// a time) is the only writer, but a frontend can read History/Save from another
// goroutine while a turn appends, so mu guards Messages. Direct Messages reads on
// the run-loop goroutine stay lock-free (serial with its own writes); cross-
// goroutine access goes through Snapshot.
type Session struct {
	mu             sync.RWMutex
	saveMu         sync.Mutex
	Messages       []provider.Message
	rewriteVersion int // bumped each time the log is rewritten (compact/fold)
	display        *displayState
}

// NewSession initializes a session with an optional system prompt.
func NewSession(system string) *Session {
	s := &Session{}
	if system != "" {
		s.Messages = append(s.Messages, provider.Message{Role: provider.RoleSystem, Content: system})
	}
	return s
}

// Add appends a message.
func (s *Session) Add(m provider.Message) string {
	return s.AddWithDisplay(m, "")
}

// AddWithDisplay keeps attachment display references out of the model input.
func (s *Session) AddWithDisplay(m provider.Message, display string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDisplayLocked()
	s.Messages = append(s.Messages, m)
	s.appendDisplayLocked(m)
	s.display.Entries[len(s.display.Entries)-1].DisplayText = display
	return s.display.ContextIDs[len(s.display.ContextIDs)-1]
}

// Replace swaps the whole message log — used by compaction, which rewrites the
// middle of the history.
func (s *Session) Replace(msgs []provider.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDisplayLocked()
	// Same-size edits (for example removing an internal goal marker) update
	// their original display entries without discarding archived conversation.
	if len(msgs) == len(s.Messages) {
		byID := make(map[string]int, len(s.display.Entries))
		for i, e := range s.display.Entries {
			byID[e.ID] = i
		}
		for i, m := range msgs {
			if j, ok := byID[s.display.ContextIDs[i]]; ok && s.display.Entries[j].Kind == "message" {
				s.display.Entries[j].Message = m
			}
		}
	} else {
		s.display = nil
	}
	s.Messages = msgs
}

// Snapshot returns a copy of the messages, safe to read from another goroutine
// while a turn appends. Frontends (History, Save) use it instead of touching the
// live slice.
func (s *Session) Snapshot() []provider.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]provider.Message(nil), s.Messages...)
}

// RewriteVersion returns the current rewrite version.
func (s *Session) RewriteVersion() int { s.mu.RLock(); defer s.mu.RUnlock(); return s.rewriteVersion }

// IncrementRewrite bumps the rewrite version by 1.
func (s *Session) IncrementRewrite() { s.mu.Lock(); defer s.mu.Unlock(); s.rewriteVersion++ }

// HasContent returns true when the session carries at least one user,
// assistant, or tool message — i.e. more than just a system prompt. An
// "empty" conversation that has never been used should not be persisted.
func (s *Session) HasContent() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.Messages {
		if m.Role != provider.RoleSystem {
			return true
		}
	}
	return false
}
