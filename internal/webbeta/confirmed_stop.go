package webbeta

import (
	"context"
	"errors"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// StopConfirmation is internal native-host evidence, never a model/tool result.
type StopConfirmation struct {
	RequestID, URL, Document string
	HTTPDone                 bool `json:"httpDone"`
	UIIdle                   bool `json:"uiIdle"`
	TaskExited               bool `json:"taskExited"`
	Revoked                  bool `json:"revoked"`
	OutputDrained            bool `json:"outputDrained"`
}

type confirmedStop struct {
	cause error
	proof StopConfirmation
}

func (e *confirmedStop) Error() string {
	return "web task cancelled with confirmed native drain; replay forbidden"
}
func (e *confirmedStop) Unwrap() error { return e.cause }

// Only a fully bound native acknowledgement can create the typed disposition.
func ConfirmStopped(cause error, p StopConfirmation) error {
	if !errors.Is(cause, context.Canceled) || p.RequestID == "" || p.Document == "" || !ValidChatURL(p.URL) || !p.HTTPDone || !p.UIIdle || !p.TaskExited || !p.Revoked || !p.OutputDrained {
		return provider.NoRequestReplay(cause)
	}
	return provider.NoRequestReplay(&confirmedStop{cause, p})
}
func confirmedStoppedFor(err error, id, url string) bool {
	var stopped *confirmedStop
	return errors.Is(err, context.Canceled) && errors.As(err, &stopped) && stopped.proof.RequestID == id && stopped.proof.URL == url
}

type CancelledRequest struct {
	Owner          string `json:"owner"`
	RequestID      string `json:"requestID"`
	ConversationID string `json:"conversationID"`
}

func (e *Engine) cancelledRequestLocked(owner, id string) bool {
	for _, old := range e.state.CancelledRequests {
		if old.Owner == owner && old.RequestID == id {
			return true
		}
	}
	return false
}
func (e *Engine) archiveStoppedLocked(c *Conversation, owner, id string) error {
	if c == nil || c.Owner != owner || c.Inflight != id || len(e.state.CancelledRequests) >= 4096 {
		return errors.New("cancel checkpoint cannot be safely saved")
	}
	old := *c
	c.Phase = "archived"
	c.Inflight = ""
	c.Reply = ""
	c.Recovery = nil
	e.state.CancelledRequests = append(e.state.CancelledRequests, CancelledRequest{owner, id, c.ID})
	if err := saveState(e.path, e.state); err != nil {
		*c = old
		e.state.CancelledRequests = e.state.CancelledRequests[:len(e.state.CancelledRequests)-1]
		return err
	}
	return nil
}
