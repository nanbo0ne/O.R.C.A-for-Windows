package webbeta

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// Issued is not executed: only the host Agent may execute and return a result.
// Hashes retain replay protection without duplicating tool contents on disk.
type CallRecord struct {
	Owner         string `json:"owner"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	ArgumentsHash string `json:"argumentsHash"`
	RequestID     string `json:"requestID"`
	TaskID        string `json:"taskID,omitempty"`
	Status        string `json:"status"`
	ResultHash    string `json:"resultHash,omitempty"`
	// Status is host receipt only; delivery is confirmed by a completed page response.
	ResultDelivery  string `json:"resultDelivery,omitempty"`
	ResultRequestID string `json:"resultRequestID,omitempty"`
}

func contentHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}

// Caller holds e.mu. Validate the entire set before acknowledging any result.
func (e *Engine) acknowledgeResultsLocked(r Request) error {
	updates := make(map[int]string)
	for i, issued := range e.state.Calls {
		if issued.Owner != r.Owner {
			continue
		}
		found := false
		invocationFound := false
		for _, m := range r.Messages {
			if m.Role == provider.RoleAssistant {
				for _, call := range m.ToolCalls {
					if call.ID == issued.ID {
						if invocationFound || call.Name != issued.Name || contentHash(call.Arguments) != issued.ArgumentsHash {
							return errors.New("issued tool call changed or duplicated")
						}
						invocationFound = true
					}
				}
			}
			if m.Role != provider.RoleTool || m.ToolCallID != issued.ID {
				continue
			}
			if found || m.Name != issued.Name {
				return errors.New("tool result identity mismatch")
			}
			found = true
			hash := contentHash(m.Content)
			if issued.Status == "returned" && hash != issued.ResultHash {
				return errors.New("acknowledged tool result changed")
			}
			updates[i] = hash
		}
		if found && !invocationFound {
			return errors.New("tool result has no matching original invocation")
		}
		if issued.Status == "issued" && !found {
			return errors.New("previous tool delivery has no confirmed host result; automatic replay is disabled")
		}
	}
	for i, hash := range updates {
		if e.state.Calls[i].Status != "returned" {
			e.state.Calls[i].ResultDelivery = "pending"
		}
		e.state.Calls[i].Status = "returned"
		e.state.Calls[i].ResultHash = hash
	}
	return nil
}

// Caller holds e.mu. Only results actually included in this incremental send
// belong to its delivery checkpoint; never credit old or another owner's calls.
func (e *Engine) sendingResultsLocked(r Request, messages []provider.Message) {
	for i := range e.state.Calls {
		call := &e.state.Calls[i]
		if call.Owner != r.Owner || call.Status != "returned" {
			continue
		}
		for _, m := range messages {
			if m.Role == provider.RoleTool && m.ToolCallID == call.ID && m.Name == call.Name && contentHash(m.Content) == call.ResultHash {
				call.ResultDelivery, call.ResultRequestID = "sending", r.ID
			}
		}
	}
}

func (e *Engine) finishResultDeliveryLocked(r Request, status string) {
	for i := range e.state.Calls {
		call := &e.state.Calls[i]
		if call.Owner == r.Owner && call.ResultRequestID == r.ID && call.ResultDelivery == "sending" {
			call.ResultDelivery = status
		}
	}
}

// Keep a host owner's checkpoint and delivery atomic with respect to another
// stream for that owner, including the interval after the website confirms.
func (e *Engine) acquireStream(owner string, scopes ...*taskScope) (func(), error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, ErrIdentityClosed
	}
	if scope := e.taskScopes[owner]; scope != nil && scope.draining {
		return nil, ErrTaskDraining
	}
	scope := e.taskScopes[owner]
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	if scope != nil && (scope.engine != e || scope.draining || e.taskScopes[scope.owner] != scope) {
		return nil, ErrTaskDraining
	}
	if e.streams == nil {
		e.streams = make(map[string]bool)
	}
	if e.streams[owner] {
		return nil, errors.New("a webpage request for this conversation is already in progress")
	}
	e.streams[owner] = true
	if e.streamDone == nil {
		e.streamDone = make(map[string]chan struct{})
	}
	done := make(chan struct{})
	e.streamDone[owner] = done
	if scope != nil {
		if scope.streams == nil {
			scope.streams = make(map[chan struct{}]struct{})
		}
		scope.streams[done] = struct{}{}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			e.mu.Lock()
			defer e.mu.Unlock()
			delete(e.streams, owner)
			delete(e.streamDone, owner)
			if scope != nil {
				delete(scope.streams, done)
			}
			close(done)
		})
	}, nil
}

// Persist all validated calls before exposing even the first call to the Agent.
func (e *Engine) recordTurn(r Request, turn decodedTurn) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrIdentityClosed
	}
	var c *Conversation
	for i := range e.state.Conversations {
		v := &e.state.Conversations[i]
		if v.Owner == r.Owner && v.Phase == "bound" && v.Epoch == r.Epoch && v.RequestID == r.ID {
			c = v
			break
		}
	}
	if c == nil || c.Inflight != "" || len(c.Prefix) != len(r.Messages) || !prefixMatch(c.Prefix, fingerprints(r.Messages)) {
		return errors.New("tool delivery has no matching confirmed request")
	}
	ids := make(map[string]bool)
	for _, call := range turn.Calls {
		if ids[call.ID] {
			return errors.New("duplicate tool ID in batch")
		}
		ids[call.ID] = true
		for _, issued := range e.state.Calls {
			if issued.Owner == r.Owner && issued.ID == call.ID {
				return errors.New("tool call already issued; automatic replay is disabled")
			}
		}
	}
	if err := e.reserveTaskLocked(r.TaskID, 0, len(turn.Calls), 0); err != nil {
		return err
	}
	for _, call := range turn.Calls {
		e.state.Calls = append(e.state.Calls, CallRecord{Owner: r.Owner, ID: call.ID, Name: call.Name,
			ArgumentsHash: contentHash(call.Arguments), RequestID: r.ID, TaskID: r.TaskID, Status: "issued"})
	}
	c.Reply = replyFingerprint(provider.Message{Role: provider.RoleAssistant, Content: turn.Progress + turn.Final, ToolCalls: turn.Calls})
	return e.saveCompletionLocked(c)
}
