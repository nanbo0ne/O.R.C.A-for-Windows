package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// The first message carries optional display metadata. Keeping both histories
// in the same atomic file prevents a crash from committing only one of them.
// Legacy JSONL readers still see ordinary provider messages on every line.
type savedMessage struct {
	provider.Message
	Display *displayState `json:"_orca_display,omitempty"`
}

// Save atomically persists model context and visible conversation together.
func (s *Session) Save(path string) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if path == "" {
		return fmt.Errorf("empty session path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	// Write to a sibling tmp file then rename, so a crash mid-write can't
	// leave a partial JSONL that won't reload.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session.*.tmp")
	if err != nil {
		return fmt.Errorf("create session tmp: %w", err)
	}
	tmpPath := tmp.Name()
	enc := json.NewEncoder(tmp)
	s.mu.Lock()
	s.ensureDisplayLocked()
	msgs := append([]provider.Message(nil), s.Messages...)
	display := *s.display
	display.Entries = append([]DisplayEntry(nil), s.display.Entries...)
	display.ContextIDs = append([]string(nil), s.display.ContextIDs...)
	s.mu.Unlock()
	for i, m := range msgs {
		record := savedMessage{Message: m}
		if i == 0 {
			record.Display = &display
		}
		if err := enc.Encode(record); err != nil {
			tmp.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("encode message: %w", err)
		}
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	// A failed rename must not fall back to truncating the only good copy.
	// Both files are siblings; report filter-driver/permission failures intact.
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("save session: original preserved; complete recovery copy at %s: %w", tmpPath, err)
	}
	return nil
}

// LoadSession reads a JSONL file written by Save into a fresh Session value.
// Missing files surface as os.IsNotExist so callers can fall through to a
// new session.
func LoadSession(path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := &Session{}
	// Decode a stream of JSON values rather than scanning lines: a single
	// message (e.g. a multi-MiB bash output) can exceed any line-buffer cap, and
	// Save's json.Encoder has no such limit — a Scanner here made sessions that
	// saved fine fail to reload.
	dec := json.NewDecoder(f)
	for {
		var m savedMessage
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		if len(s.Messages) == 0 && m.Display != nil {
			if m.Display.Version != 1 {
				return nil, fmt.Errorf("unsupported display history version %d", m.Display.Version)
			}
			s.display = m.Display
		}
		s.Messages = append(s.Messages, m.Message)
	}
	if s.display != nil && len(s.display.ContextIDs) != len(s.Messages) {
		return nil, fmt.Errorf("display history/context length mismatch")
	}
	return s, nil
}

// SessionInfo summarises a saved session for the --resume picker: where it is on
// disk, when it was created/last active, the first user message as a preview, and
// a rough turn count.
type SessionInfo struct {
	Path           string
	CreatedAt      time.Time
	LastActivityAt time.Time
	ModTime        time.Time // compatibility alias for LastActivityAt
	Preview        string
	Turns          int
	Scope          string
	WorkspaceRoot  string
	TopicID        string
	TopicTitle     string
	Pinned         bool
}

// ListSessions returns every *.jsonl session under dir, most-recently-active
// first, each with a preview line so the picker can show something the user
// recognises. A missing directory is not an error — it just means there's
// nothing to resume yet.
func ListSessions(dir string) ([]SessionInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []SessionInfo
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		full := filepath.Join(dir, e.Name())
		preview, turns := previewSession(full)
		if turns == 0 {
			// Skip sessions that have never had user interaction — they are
			// empty conversations that should not appear in the history panel
			// or the resume picker.
			continue
		}
		createdAt := info.ModTime()
		lastActivityAt := info.ModTime()
		scope := "global"
		workspaceRoot := ""
		topicID := ""
		topicTitle := ""
		pinned := false
		if meta, ok, err := LoadBranchMeta(full); err == nil && ok {
			if !meta.CreatedAt.IsZero() {
				createdAt = meta.CreatedAt
			}
			if !meta.UpdatedAt.IsZero() {
				lastActivityAt = meta.UpdatedAt
			}
			scope = meta.DefaultScope()
			workspaceRoot = meta.WorkspaceRoot
			topicID = meta.TopicID
			topicTitle = meta.TopicTitle
			pinned = meta.Pinned
		}
		out = append(out, SessionInfo{
			Path:           full,
			CreatedAt:      createdAt,
			LastActivityAt: lastActivityAt,
			ModTime:        lastActivityAt,
			Preview:        preview,
			Turns:          turns,
			Scope:          scope,
			WorkspaceRoot:  workspaceRoot,
			TopicID:        topicID,
			TopicTitle:     topicTitle,
			Pinned:         pinned,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastActivityAt.Equal(out[j].LastActivityAt) {
			return out[i].Path < out[j].Path
		}
		return out[i].LastActivityAt.After(out[j].LastActivityAt)
	})
	return out, nil
}

// previewSession returns the first user message (truncated) and the number of
// user-role messages so the picker can show "5 turns · 'help me debug the…'".
// Errors are swallowed — a malformed file just shows up with an empty preview.
func previewSession(path string) (string, int) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	first := ""
	turns := 0
	for {
		var record savedMessage
		if err := dec.Decode(&record); err != nil {
			break // EOF or a malformed tail — return the preview gathered so far
		}
		if record.Display != nil {
			for _, e := range record.Display.Entries {
				m := e.Message
				if e.Kind != "message" || m.Role != provider.RoleUser {
					continue
				}
				if _, steer := SteerDisplayText(m.Content); steer {
					continue
				}
				turns++
				if first == "" {
					first = strings.TrimSpace(HandoffTask(m.Content))
					if r := []rune(first); len(r) > 80 {
						first = string(r[:77]) + "…"
					}
				}
			}
			return first, turns
		}
		m := record.Message
		if m.Role == provider.RoleUser {
			turns++
			if first == "" {
				s := strings.TrimSpace(HandoffTask(m.Content))
				if r := []rune(s); len(r) > 80 {
					s = string(r[:77]) + "…"
				}
				first = s
			}
		}
	}
	return first, turns
}

// ContinueSessionPath returns where a conversation carried into a rebuilt
// controller (model switch, config change) should keep auto-saving: its existing
// file when it has one, so the continued session stays a single file instead of
// the old one being orphaned as an identical duplicate (#2807). A session with no
// file yet gets a fresh path; "" when persistence is disabled.
func ContinueSessionPath(prevPath, dir, model string) string {
	if prevPath != "" {
		return prevPath
	}
	if dir == "" {
		return ""
	}
	return NewSessionPath(dir, model)
}

// NewSessionPath returns the path to use for a fresh session, namespaced by
// the model so the filename hints at what the conversation was with. dir is
// typically config.SessionDir().
func NewSessionPath(dir, model string) string {
	safe := strings.NewReplacer("/", "-", "\\", "-").Replace(model)
	if safe == "" {
		safe = "session"
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%s.jsonl", time.Now().UTC().Format("20060102-150405.000000000"), safe))
}
