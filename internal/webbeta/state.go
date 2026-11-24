package webbeta

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const stateVersion = 6

type Conversation struct {
	ID          string                      `json:"id"`
	URL         string                      `json:"url"`
	Owner       string                      `json:"owner,omitempty"`
	Phase       string                      `json:"phase"`
	OpeningDone int                         `json:"openingDone"`
	Prefix      []string                    `json:"prefix,omitempty"`
	Epoch       uint64                      `json:"epoch"`
	Grammar     int                         `json:"grammar"`
	Inflight    string                      `json:"inflight,omitempty"`
	Reply       string                      `json:"reply,omitempty"`
	RequestID   string                      `json:"requestID,omitempty"`
	Recovery    *UncertaintyAcknowledgement `json:"recovery,omitempty"`
}

// Acknowledgement retires only this uncertainty, never the conversation's
// replay barrier. Empty Inflight is possible after a failed completion save.
type UncertaintyAcknowledgement struct {
	Inflight  string `json:"inflight"`
	RequestID string `json:"requestID"`
}

func (c Conversation) uncertaintyAcknowledged() bool {
	return c.Phase == "uncertain" && c.Recovery != nil &&
		c.Recovery.Inflight == c.Inflight && c.Recovery.RequestID == c.RequestID
}

type State struct {
	CancelledRequests []CancelledRequest    `json:"cancelledRequests,omitempty"`
	Generation        string                `json:"generation"`
	InvalidOwners     []string              `json:"invalidOwners,omitempty"`
	Version           int                   `json:"version"`
	Sent              int                   `json:"sent"`
	Blocked           string                `json:"blocked,omitempty"`
	SendAuthorized    bool                  `json:"sendAuthorized"`
	Settings          Settings              `json:"settings"`
	Conversations     []Conversation        `json:"conversations"`
	Tasks             map[string]TaskBudget `json:"tasks,omitempty"`
	Calls             []CallRecord          `json:"calls,omitempty"`
}

func loadState(path string) (State, error) {
	s := State{Version: stateVersion, Generation: newID(), Settings: DefaultSettings()}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(raw, &s); err != nil {
		return s, err
	}
	if s.Version < 1 || s.Version > stateVersion {
		return s, errors.New("unsupported web beta state version")
	}
	// Only a previously enabled source proves legacy opt-in. A disabled legacy
	// state cannot distinguish explicit revocation from a temporary restriction.
	if s.Version < 4 {
		s.SendAuthorized = s.Settings.Enabled
	}
	if !s.SendAuthorized || s.Blocked == identityDrainPending || s.Blocked == identityResetPending {
		s.SendAuthorized = false
		s.Settings.Enabled = false
	}
	// A legacy file keeps its conversation grammar. New writes use a version
	// that old binaries reject rather than silently ignoring the replay ledger.
	s.Version = stateVersion
	for i := range s.Calls {
		if s.Calls[i].ResultDelivery == "sending" {
			s.Calls[i].ResultDelivery = "unconfirmed"
		}
	}
	if err = s.Settings.Validate(); err != nil {
		return s, err
	}
	if s.Blocked != "" {
		s.Settings.Enabled = false
		s.Settings.ReservePaused = true
	}
	// Never resume a send whose result was not durably acknowledged.
	for i := range s.Conversations {
		c := &s.Conversations[i]
		if c.uncertaintyAcknowledged() {
			continue
		}
		c.Recovery = nil
		if c.Inflight != "" || c.Phase == "preparing" || c.Phase == "uncertain" {
			c.Phase = "uncertain"
			s.Settings.ReservePaused = true
			s.Settings.Enabled = false
			if s.Blocked == "" {
				s.Blocked = "send_unconfirmed"
			}
		}
	}
	return s, nil
}
func saveState(path string, s State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".web-beta-state-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceState(name, path)
}
