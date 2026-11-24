package agent

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// DisplayEntry belongs to the visible transcript, never to a model request.
// Context IDs let a real checkpoint truncate this transcript without treating
// an archived visual row as an executable checkpoint.
type DisplayEntry struct {
	ID          string           `json:"id"`
	Kind        string           `json:"kind"`
	Message     provider.Message `json:"message,omitempty"`
	DisplayText string           `json:"displayText,omitempty"`
	ToolStatus  string           `json:"toolStatus,omitempty"`
	ToolError   string           `json:"toolError,omitempty"`
	Summary     string           `json:"summary,omitempty"`
	Trigger     string           `json:"trigger,omitempty"`
	Messages    int              `json:"messages,omitempty"`
	Archive     string           `json:"archive,omitempty"`
	Legacy      bool             `json:"legacy,omitempty"`
}

type displayState struct {
	Version      int            `json:"version"`
	Namespace    string         `json:"namespace"`
	ContextEpoch uint64         `json:"contextEpoch,omitempty"`
	NextID       uint64         `json:"nextId"`
	Entries      []DisplayEntry `json:"entries"`
	ContextIDs   []string       `json:"contextIds"`
}

var timelineSequence atomic.Uint64

func timelineNamespace() string {
	return fmt.Sprintf("%x-%x", time.Now().UnixNano(), timelineSequence.Add(1))
}

func (s *Session) nextDisplayIDLocked() string {
	s.display.NextID++
	return fmt.Sprintf("display-%s-%016x", s.display.Namespace, s.display.NextID)
}

func (s *Session) appendDisplayLocked(m provider.Message) {
	id := s.nextDisplayIDLocked()
	e := DisplayEntry{ID: id, Kind: "message", Message: m}
	if legacyContextCheckpoint(m) {
		e.Kind, e.Summary, e.Legacy = "compaction", m.Content, true
	}
	s.display.Entries = append(s.display.Entries, e)
	s.display.ContextIDs = append(s.display.ContextIDs, id)
}

func (s *Session) ensureDisplayLocked() {
	if s.display != nil {
		return
	}
	s.display = &displayState{Version: 1, Namespace: timelineNamespace()}
	for _, m := range s.Messages {
		s.appendDisplayLocked(m)
		if legacyContextCheckpoint(m) {
			s.display.ContextEpoch = 1
		}
	}
}

func legacyContextCheckpoint(m provider.Message) bool {
	return m.Role == provider.RoleUser && strings.HasPrefix(m.Content, checkpointTagOpen+"\nCONTEXT CHECKPOINT\n") && strings.HasSuffix(m.Content, "\n"+checkpointTagClose)
}

func (s *Session) ContextEpoch() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDisplayLocked()
	return s.display.ContextEpoch
}

// ConversationID survives reload and context rewrite; forks get a new identity.
func (s *Session) ConversationID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDisplayLocked()
	return s.display.Namespace
}

func (s *Session) DisplaySnapshot() []DisplayEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDisplayLocked()
	entries := append([]DisplayEntry(nil), s.display.Entries...)
	for i := range entries {
		entries[i].Message.Files = cloneFileSnapshots(entries[i].Message.Files)
	}
	return entries
}

// RewriteContext retains the complete visible transcript. A marker is appended
// at the time of compression, not inserted where the compressed prefix lived.
func (s *Session) RewriteContext(msgs []provider.Message, marker *DisplayEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rewriteContextLocked(msgs, marker)
}

// Cancellation and source-history checks share the actual commit lock. A
// summary waiting behind another session operation must not overwrite it.
func (s *Session) commitCompaction(ctx context.Context, before, msgs []provider.Message, marker *DisplayEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !reflect.DeepEqual(s.Messages, before) {
		return fmt.Errorf("conversation changed before summary commit; original history was kept")
	}
	s.rewriteContextLocked(msgs, marker)
	s.rewriteVersion++
	return nil
}

func (s *Session) rewriteContextLocked(msgs []provider.Message, marker *DisplayEntry) {
	s.ensureDisplayLocked()
	ids := make([]string, len(msgs))
	if marker != nil || len(msgs) != len(s.Messages) {
		s.display.ContextEpoch++
	}
	if marker != nil {
		markerCopy := *marker
		markerCopy.ID, markerCopy.Kind = s.nextDisplayIDLocked(), "compaction"
		s.display.Entries = append(s.display.Entries, markerCopy)
		for i := range ids {
			ids[i] = markerCopy.ID
		}
	}
	// Retained head/tail messages preserve their identity, even if identical
	// text occurs more than once. Unmatched summary messages point to the marker.
	cursor := 0
	for i, m := range msgs {
		for j := cursor; j < len(s.Messages); j++ {
			if reflect.DeepEqual(m, s.Messages[j]) {
				ids[i] = s.display.ContextIDs[j]
				cursor = j + 1
				break
			}
		}
		if ids[i] == "" && len(msgs) == len(s.Messages) {
			ids[i] = s.display.ContextIDs[i]
		}
	}
	s.Messages = cloneMessageFileSnapshots(msgs)
	s.display.ContextIDs = ids
}

// ClonePrefix is used only with a validated live context boundary.
func (s *Session) ClonePrefix(boundary int) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDisplayLocked()
	boundary = max(0, min(boundary, len(s.Messages)))
	end := len(s.display.Entries)
	if boundary < len(s.Messages) {
		id := s.display.ContextIDs[boundary]
		for i, e := range s.display.Entries {
			if e.ID == id {
				end = i
				break
			}
		}
	}
	d := *s.display
	d.Namespace = timelineNamespace()
	d.Entries = append([]DisplayEntry(nil), s.display.Entries[:end]...)
	for i := range d.Entries {
		d.Entries[i].Message.Files = cloneFileSnapshots(d.Entries[i].Message.Files)
	}
	d.ContextIDs = append([]string(nil), s.display.ContextIDs[:boundary]...)
	return &Session{Messages: cloneMessageFileSnapshots(s.Messages[:boundary]), display: &d, rewriteVersion: s.rewriteVersion}
}

// WithContext carries display history across a controller rebuild. Only the
// runtime context changes (usually its system prompt); old transcript is kept.
func (s *Session) WithContext(msgs []provider.Message) *Session {
	identity := s.ConversationID()
	next := s.ClonePrefix(len(s.Snapshot()))
	next.display.Namespace = identity
	next.RewriteContext(msgs, nil)
	return next
}

var displayAttachmentPath = regexp.MustCompile(`(?:\.orca|\.deepseek-orca)/attachments/[A-Za-z0-9_-][A-Za-z0-9._-]*`)

// RelocateImages changes attachment references in both histories when a branch is moved
// to another workspace. Detached slices keep the source branch immutable.
func (s *Session) RelocateImages(resolve func(string) string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDisplayLocked()
	mapMessage := func(m provider.Message) provider.Message {
		m.Images = append([]provider.ImageContent(nil), m.Images...)
		for i := range m.Images {
			m.Images[i].Path = resolve(m.Images[i].Path)
		}
		if m.Role == provider.RoleUser {
			m.Content = displayAttachmentPath.ReplaceAllStringFunc(m.Content, resolve)
		}
		return m
	}
	for i, m := range s.Messages {
		s.Messages[i] = mapMessage(m)
	}
	for i, e := range s.display.Entries {
		s.display.Entries[i].Message = mapMessage(e.Message)
		s.display.Entries[i].DisplayText = displayAttachmentPath.ReplaceAllStringFunc(e.DisplayText, resolve)
	}
}

func SteerDisplayText(content string) (string, bool) {
	if !strings.HasPrefix(content, midTurnSteerPrefix+"\n") {
		return content, false
	}
	return strings.TrimPrefix(content, midTurnSteerPrefix+"\n"), true
}
