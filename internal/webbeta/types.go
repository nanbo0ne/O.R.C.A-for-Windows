package webbeta

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
)

const Origin = "https://chat.deepseek.com"
const MaxAttachmentBytes = 10 << 20
const MaxBatchBytes = 20 << 20

type Settings struct {
	Enabled       bool     `json:"enabled"`
	RPM           int      `json:"rpm"`
	ReservePaused bool     `json:"reservePaused"`
	OpeningRounds int      `json:"openingRounds"`
	Openings      []string `json:"openings"`
	Thinking      bool     `json:"thinking"`
	Search        bool     `json:"search"`
}

func DefaultSettings() Settings {
	return Settings{RPM: 2, ReservePaused: true, OpeningRounds: 1, Thinking: true, Openings: []string{"你好，请用一句话介绍你能帮助处理的日常事务。", "谢谢，请再给一个简单的时间管理建议。"}}
}
func (s Settings) Validate() error {
	if s.RPM < 1 || s.RPM > 10 || s.OpeningRounds < 1 || s.OpeningRounds > 2 || len(s.Openings) < s.OpeningRounds {
		return errors.New("invalid queue or opening settings")
	}
	for _, v := range s.Openings {
		if strings.TrimSpace(v) == "" || len(v) > 2000 {
			return errors.New("opening must contain 1-2000 bytes")
		}
	}
	return nil
}
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func AllowedSource(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "chat.deepseek.com" && u.User == nil
}
func ValidChatURL(raw string) bool {
	if !AllowedSource(raw) {
		return false
	}
	u, _ := url.Parse(raw)
	return strings.HasPrefix(u.Path, "/a/chat/s/") && len(strings.TrimPrefix(u.Path, "/a/chat/s/")) > 0 && u.RawQuery == "" && u.Fragment == ""
}

type BrowserState struct {
	Ready   bool   `json:"ready"`
	Busy    bool   `json:"busy"`
	URL     string `json:"url"`
	Blocked string `json:"blocked,omitempty"`
}
type Command struct {
	Operation   string   `json:"-"`
	ID          string   `json:"id"`
	Action      string   `json:"action"`
	URL         string   `json:"url,omitempty"`
	Text        string   `json:"text,omitempty"`
	Thinking    bool     `json:"thinking"`
	Search      bool     `json:"search"`
	Attachments []Upload `json:"attachments,omitempty"`
}
type Event struct {
	Sent           bool                 `json:"sent,omitempty"`
	Generation     string               `json:"generation,omitempty"`
	ID             string               `json:"id"`
	Kind           string               `json:"kind"`
	Text           string               `json:"text,omitempty"`
	URL            string               `json:"url,omitempty"`
	Code           string               `json:"code,omitempty"`
	State          *BrowserState        `json:"state,omitempty"`
	Stop           *StopConfirmation    `json:"stop,omitempty"`
	StopDiagnostic *StopDrainDiagnostic `json:"stopDiagnostic,omitempty"`
}

// Diagnostic observations cannot authorize a stopped disposition.
type StopDrainDiagnostic struct {
	HTTPDone      bool `json:"httpDone"`
	UIIdle        bool `json:"uiIdle"`
	TaskExited    bool `json:"taskExited"`
	Revoked       bool `json:"revoked"`
	OutputDrained bool `json:"outputDrained"`
}

// Browser must not return from Run after cancellation until page stop is
// confirmed, or until it has disconnected the page and made future sends fail.
type Browser interface {
	State() BrowserState
	Run(context.Context, Command, func(Event) error) error
	Logout(context.Context) error
}

type Failure struct {
	Code string
	Sent bool
}

func (e *Failure) Error() string { return "web beta: " + e.Code }
func IsAccountBlocked(err error) bool {
	var f *Failure
	if !errors.As(err, &f) {
		return false
	}
	switch f.Code {
	case "restricted", "challenge", "signed_out", "identity_changed", "manual_intervention", "stop_unconfirmed", "rate_limited":
		return true
	}
	return false
}
