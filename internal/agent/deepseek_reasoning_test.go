package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/openai"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func TestDeepSeekHookSessionRestoreAllTurnsOnWire(t *testing.T) {
	const plainReasoning = " original plain\n"
	const toolReasoning = "original tool reasoning"
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]json.RawMessage
			Tools    []json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if len(req.Tools) == 0 {
			t.Error("agent request lost tools")
		}
		n := int(requests.Add(1))
		var got []string
		for _, m := range req.Messages {
			if string(m["role"]) != `"assistant"` {
				continue
			}
			var reasoning string
			if value, ok := m["reasoning_content"]; !ok {
				t.Error("historical assistant lost reasoning_content")
			} else if err := json.Unmarshal(value, &reasoning); err != nil {
				t.Error(err)
			}
			got = append(got, reasoning)
			if _, ok := m["protocol_reasoning_content"]; ok {
				t.Error("storage metadata sent to API")
			}
		}
		want := []string{plainReasoning, toolReasoning, ""}[:n-1]
		if strings.Join(got, "|") != strings.Join(want, "|") || len(got) != len(want) {
			t.Errorf("request %d originals=%q, want %q", n, got, want)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		delta := map[string]any{"reasoning_content": "", "content": "answer"}
		switch n {
		case 1:
			delta["reasoning_content"] = plainReasoning
		case 2:
			delta["reasoning_content"] = toolReasoning
			delta["content"] = "checking"
			delta["tool_calls"] = []any{map[string]any{"index": 0, "id": "c1", "type": "function", "function": map[string]any{"name": "echo", "arguments": `{"text":"test"}`}}}
		}
		if reasoning := delta["reasoning_content"].(string); reasoning != "" {
			first, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"reasoning_content": reasoning[:len(reasoning)/2]}}}})
			fmt.Fprintf(w, "data: %s\n\n", first)
			delta["reasoning_content"] = reasoning[len(reasoning)/2:]
		}
		data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
	}))
	defer srv.Close()
	p, err := openai.New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "deepseek-flash", Extra: map[string]any{"reasoning_protocol": "deepseek"}})
	if err != nil {
		t.Fatal(err)
	}
	hooks := &stubHooks{hasPostLLM: true, postLLMOut: "TRANSLATED", blockPre: map[string]bool{"echo": true}}
	sink := &recordSink{}
	session := NewSession("test system")
	path := filepath.Join(t.TempDir(), "session.jsonl")
	for i := 0; i < 3; i++ {
		a := New(p, echoRegistry(), session, Options{Hooks: hooks}, sink)
		if err := a.Run(context.Background(), "continue"); err != nil {
			t.Fatal(err)
		}
		if err := session.Save(path); err != nil {
			t.Fatal(err)
		}
		session, err = LoadSession(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 4 {
		t.Fatalf("requests=%d", requests.Load())
	}
	if !reflect.DeepEqual(hooks.postLLMSeen, []string{plainReasoning, toolReasoning}) {
		t.Fatalf("hook input=%q", hooks.postLLMSeen)
	}
	if !reflect.DeepEqual(hooks.preSeen, []string{"echo"}) || len(hooks.postSeen) != 0 {
		t.Fatalf("security hook bypassed: pre=%v post=%v", hooks.preSeen, hooks.postSeen)
	}
	for _, e := range sink.kinds(event.Reasoning) {
		if e.Text != "TRANSLATED" {
			t.Fatalf("raw reasoning leaked live: %q", e.Text)
		}
	}
	for _, e := range sink.kinds(event.Message) {
		if e.Reasoning != "TRANSLATED" && e.Reasoning != "" {
			t.Fatalf("raw reasoning leaked to message: %q", e.Reasoning)
		}
	}
	for _, m := range session.Snapshot() {
		if m.Role != provider.RoleAssistant {
			continue
		}
		if m.ProtocolReasoningContent == nil {
			t.Fatal("restore lost captured original, including empty blocks")
		}
		if *m.ProtocolReasoningContent != "" && m.ReasoningContent != "TRANSLATED" {
			t.Fatal("restore changed visible reasoning")
		}
	}
}

type hidingReasoningHooks struct{ stubHooks }

func (h *hidingReasoningHooks) PostLLMCall(_ context.Context, reasoning string, turn int) string {
	h.postLLMSeen = append(h.postLLMSeen, reasoning)
	return ""
}

func TestPostLLMCallHiddenReasoningSurvivesInterruptedRestore(t *testing.T) {
	interrupted := &provider.StreamInterruptedError{Err: errors.New("interrupted")}
	prov := &scriptedProvider{name: "test", turns: [][]provider.Chunk{
		{{Type: provider.ChunkReasoning, Text: "original"}, {Type: provider.ChunkText, Text: "partial"}, {Type: provider.ChunkError, Err: interrupted}},
		{{Type: provider.ChunkReasoning, Text: "next"}, {Type: provider.ChunkText, Text: "finished"}, {Type: provider.ChunkDone}},
	}}
	hooks := &hidingReasoningHooks{stubHooks: stubHooks{hasPostLLM: true}}
	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{Hooks: hooks}, sink)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if len(sink.kinds(event.Reasoning)) != 0 || !reflect.DeepEqual(hooks.postLLMSeen, []string{"original", "next"}) {
		t.Fatal("hidden reasoning hook behavior changed")
	}
	for _, e := range sink.kinds(event.Message) {
		if e.Reasoning != "" {
			t.Fatal("hidden reasoning became visible")
		}
	}
	path := filepath.Join(t.TempDir(), "interrupted.jsonl")
	if err := a.session.Save(path); err != nil {
		t.Fatal(err)
	}
	restored, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	var originals []string
	for _, m := range restored.Snapshot() {
		if m.Role == provider.RoleAssistant {
			if m.ReasoningContent != "" || m.ProtocolReasoningContent == nil {
				t.Fatalf("hidden/original mismatch: %+v", m)
			}
			originals = append(originals, *m.ProtocolReasoningContent)
		}
	}
	if !reflect.DeepEqual(originals, []string{"original", "next"}) {
		t.Fatalf("restored originals=%v", originals)
	}
}

func TestDeepSeekRejectedLegacyHistoryDoesNotRecoverOrRepeatTools(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"message":"missing reasoning_content"}}`)
	}))
	defer srv.Close()
	p, err := openai.New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "deepseek-flash", Extra: map[string]any{"reasoning_protocol": "deepseek"}})
	if err != nil {
		t.Fatal(err)
	}
	session := NewSession("")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "done task"})
	session.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{}`}}})
	session.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "c1", Content: "completed"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "old final without reasoning"})
	before := session.Snapshot()
	path := filepath.Join(t.TempDir(), "legacy.jsonl")
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	session, err = LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	hooks := &stubHooks{}
	sink := &recordSink{}
	a := New(p, echoRegistry(), session, Options{Hooks: hooks}, sink)
	err = a.Run(context.Background(), "next")
	var historyErr *provider.ReasoningHistoryError
	if !errors.As(err, &historyErr) || !strings.Contains(err.Error(), "Start a new conversation with a summary") {
		t.Fatalf("error=%v", err)
	}
	if requests.Load() != 1 || len(hooks.preSeen) != 0 || len(sink.kinds(event.Retrying)) != 0 {
		t.Fatal("legacy history caused HTTP retry or tool recovery")
	}
	if !reflect.DeepEqual(session.Snapshot()[:len(before)], before) || len(session.Messages) != len(before)+1 {
		t.Fatal("legacy history was changed or recovery messages were added")
	}
}

func TestDeepSeekUnknownHistoryCanContinueAfterRestore(t *testing.T) {
	for _, source := range []string{"legacy-tools", "imported", "non-thinking", "synthetic-summary"} {
		t.Run(source, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct{ Messages []map[string]json.RawMessage }
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				for _, m := range req.Messages {
					if _, ok := m["reasoning_content"]; ok {
						t.Error("invented reasoning for non-thinking history")
					}
				}
				requests.Add(1)
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer without thinking\"}}]}\n\ndata: [DONE]\n\n")
			}))
			defer srv.Close()
			p, err := openai.New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "deepseek-flash", Extra: map[string]any{"reasoning_protocol": "deepseek"}})
			if err != nil {
				t.Fatal(err)
			}
			session := NewSession("system")
			switch source {
			case "imported":
				src, dest := t.TempDir(), t.TempDir()
				if err := os.WriteFile(filepath.Join(src, "legacy.events.jsonl"), []byte(legacyEventLog), 0o600); err != nil {
					t.Fatal(err)
				}
				if n, err := MigrateLegacySessions(src, dest, nil); err != nil || n != 1 {
					t.Fatalf("imported=%d err=%v", n, err)
				}
				session, err = LoadSession(filepath.Join(dest, "legacy.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
			case "non-thinking":
				a := New(p, echoRegistry(), session, Options{}, event.Discard)
				if err := a.Run(context.Background(), "first"); err != nil {
					t.Fatal(err)
				}
			case "synthetic-summary":
				session.Add(provider.Message{Role: provider.RoleAssistant, Content: "Summary of completed work."})
			case "legacy-tools":
				session.Add(provider.Message{Role: provider.RoleUser, Content: "old task"})
				session.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "old", Name: "echo", Arguments: `{}`}}})
				session.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "old", Content: "already completed"})
				session.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
			}
			before := session.Snapshot()
			path := filepath.Join(t.TempDir(), "restored.jsonl")
			if err := session.Save(path); err != nil {
				t.Fatal(err)
			}
			session, err = LoadSession(path)
			if err != nil {
				t.Fatal(err)
			}
			hooks := &stubHooks{}
			sink := &recordSink{}
			a := New(p, echoRegistry(), session, Options{Hooks: hooks}, sink)
			beforeRequests := requests.Load()
			if err := a.Run(context.Background(), "continue"); err != nil {
				t.Fatalf("%s history blocked: %v", source, err)
			}
			if requests.Load() != beforeRequests+1 || len(hooks.preSeen) != 0 || len(sink.kinds(event.Retrying)) != 0 {
				t.Fatal("restored history retried or repeated old tools")
			}
			if !reflect.DeepEqual(session.Snapshot()[:len(before)], before) {
				t.Fatal("restored history was changed")
			}
		})
	}
}

func TestDeepSeekCompactionBoundariesContinueAfterRestore(t *testing.T) {
	for _, mode := range []string{"compact", "from", "up-to"} {
		t.Run(mode, func(t *testing.T) {
			type wireRequest struct {
				Messages []struct {
					Role      provider.Role `json:"role"`
					Content   string        `json:"content"`
					Reasoning *string       `json:"reasoning_content"`
				}
				Tools []json.RawMessage
			}
			received := make(chan wireRequest, 2)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req wireRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				received <- req
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"completed work summary\"}}]}\n\ndata: [DONE]\n\n")
			}))
			defer srv.Close()
			p, err := openai.New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "deepseek-flash", Extra: map[string]any{"reasoning_protocol": "deepseek"}})
			if err != nil {
				t.Fatal(err)
			}
			session := NewSession("system")
			for i := 0; i < 10; i++ {
				session.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("question %d", i)})
				m := provider.Message{Role: provider.RoleAssistant, Content: "answer"}
				if i%3 != 0 {
					original := ""
					if i%3 == 1 {
						original = "captured original"
					}
					m.ProtocolReasoningContent, m.ReasoningContent = &original, "DISPLAY"
				}
				session.Add(m)
			}
			a := New(p, echoRegistry(), session, Options{RecentKeep: 4, ArchiveDir: t.TempDir()}, event.Discard)
			switch mode {
			case "compact":
				err = a.compact(context.Background(), "manual", "", true)
			case "from":
				err = a.SummarizeFrom(context.Background(), 17)
			case "up-to":
				err = a.SummarizeUpTo(context.Background(), 5)
			}
			if err != nil {
				t.Fatal(err)
			}
			summaryRequest := <-received
			if len(summaryRequest.Tools) != 0 || len(summaryRequest.Messages) != 2 || summaryRequest.Messages[0].Role != provider.RoleSystem || summaryRequest.Messages[1].Role != provider.RoleUser {
				t.Fatal("summary request shape changed")
			}
			for _, m := range summaryRequest.Messages {
				if m.Reasoning != nil {
					t.Fatal("reasoning sent without tools")
				}
			}
			expected := session.Snapshot()
			var summaryFound bool
			for _, m := range expected {
				if strings.Contains(m.Content, "completed work summary") {
					summaryFound = true
					if m.Role != provider.RoleUser || m.ProtocolReasoningContent != nil || m.ReasoningContent != "" {
						t.Fatal("summary gained synthetic reasoning")
					}
				}
			}
			if !summaryFound {
				t.Fatal("summary missing from session")
			}
			path := filepath.Join(t.TempDir(), "compacted.jsonl")
			if err := session.Save(path); err != nil {
				t.Fatal(err)
			}
			restored, err := LoadSession(path)
			if err != nil {
				t.Fatal(err)
			}
			a = New(p, echoRegistry(), restored, Options{}, event.Discard)
			if err := a.Run(context.Background(), "continue"); err != nil {
				t.Fatalf("%s restore blocked: %v", mode, err)
			}
			continuation := <-received
			if len(continuation.Tools) == 0 || len(continuation.Messages) != len(expected)+1 {
				t.Fatal("restored request lost tools/history")
			}
			var captured, unknown int
			for i, m := range expected {
				wire := continuation.Messages[i]
				if wire.Role != m.Role || wire.Content != m.Content || !reflect.DeepEqual(wire.Reasoning, m.ProtocolReasoningContent) {
					t.Fatalf("message %d changed across %s/restore/wire", i, mode)
				}
				if m.Role == provider.RoleAssistant {
					if wire.Reasoning == nil {
						unknown++
					} else {
						captured++
					}
				}
			}
			if captured == 0 || unknown == 0 {
				t.Fatalf("fixture missing captured/unknown history: %d/%d", captured, unknown)
			}
		})
	}
}

func TestProtocolReasoningSurvivesCompactionArchiveAndRestore(t *testing.T) {
	session := NewSession("system")
	for i := 0; i < 10; i++ {
		original := fmt.Sprintf("original %d", i)
		if i%3 == 0 {
			original = ""
		}
		session.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("question %d", i)})
		m := provider.Message{Role: provider.RoleAssistant, Content: "answer", ReasoningContent: "DISPLAY", ProtocolReasoningContent: &original}
		if i == 1 || i == 8 {
			m.ReasoningContent, m.ReasoningSignature = original, "signed-proof"
		}
		session.Add(m)
	}
	before := session.Snapshot()
	archiveDir := t.TempDir()
	p := &fakeProvider{reply: "summary of completed work"}
	a := New(p, echoRegistry(), session, Options{ArchiveDir: archiveDir, RecentKeep: 4}, event.Discard)
	head, start, ok := a.planCompaction(before, minCompactMessages)
	if !ok {
		t.Fatal("test history did not permit compaction")
	}
	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatal(err)
	}
	if len(p.req.Tools) != 0 {
		t.Fatal("summarization exposed tools")
	}
	if !reflect.DeepEqual(session.Messages[head+1:], before[start:]) {
		t.Fatal("compaction changed retained protocol/display reasoning")
	}
	archives, err := filepath.Glob(filepath.Join(archiveDir, "*.jsonl"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("archives=%v err=%v", archives, err)
	}
	archived, err := LoadSession(archives[0])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(archived.Messages, before[head:start]) {
		t.Fatal("archive lost protocol/display distinction")
	}
	path := filepath.Join(t.TempDir(), "compacted.jsonl")
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Snapshot(), session.Snapshot()) {
		t.Fatal("compacted session did not restore exactly")
	}
	restored := append([]provider.Message(nil), before[:head]...)
	restored = append(restored, archived.Messages...)
	restored = append(restored, loaded.Messages[head+1:]...)
	loaded.Replace(restored)
	if !reflect.DeepEqual(loaded.Snapshot(), before) {
		t.Fatal("original history cannot be reconstructed from archive and tail")
	}
}

func TestCompactionEstimatesOriginalReasoningNotDisplay(t *testing.T) {
	original := strings.Repeat("r", 500)
	m := provider.Message{Role: provider.RoleAssistant, Content: "answer", ProtocolReasoningContent: &original}
	want := EstimateContextTokens([]provider.Message{m})
	m.ReasoningContent = strings.Repeat("display", 1000)
	if EstimateContextTokens([]provider.Message{m}) != want {
		t.Fatal("display transformation changed protocol token estimate")
	}
	m.ProtocolReasoningContent = nil
	if EstimateContextTokens([]provider.Message{m}) <= want {
		t.Fatal("legacy reasoning estimate no longer works")
	}
}
