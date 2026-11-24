package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent/testutil"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type noReplayProgressTool struct{ calls *int }

func (noReplayProgressTool) Name() string            { return "synthetic_write" }
func (noReplayProgressTool) Description() string     { return "synthetic execution counter" }
func (noReplayProgressTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (noReplayProgressTool) ReadOnly() bool          { return false }
func (t noReplayProgressTool) Execute(context.Context, json.RawMessage) (string, error) {
	*t.calls++
	return "must not execute", nil
}

type noReplayProgressHooks struct{ stubHooks }

func (h *noReplayProgressHooks) PostLLMCall(_ context.Context, reasoning string, turn int) string {
	h.postLLMSeen = append(h.postLLMSeen, reasoning)
	h.postLLMTurns = append(h.postLLMTurns, turn)
	return h.postLLMOut
}

func TestNoReplayPreservesPartialProgressWithoutExecutingTools(t *testing.T) {
	const raw = "synthetic original reasoning"
	const partialText = "synthetic received text"
	for _, tc := range []struct {
		name      string
		text      string
		reasoning []string
		signature string
		hook      bool
		display   string
	}{
		{name: "text-and-reasoning", text: partialText, reasoning: []string{"synthetic original ", "reasoning"}},
		{name: "reasoning-only", reasoning: []string{raw}},
		{name: "transformed", text: partialText, reasoning: []string{raw}, hook: true, display: "SYNTHETIC DISPLAY"},
		{name: "hidden-reasoning-only", reasoning: []string{raw}, hook: true},
		{name: "signed", text: partialText, reasoning: []string{raw}, signature: "synthetic-signature", hook: true, display: "SYNTHETIC DISPLAY"},
		{name: "explicit-empty-reasoning", reasoning: []string{""}, hook: true, display: "must not invent reasoning"},
		{name: "text-without-reasoning", text: partialText, hook: true, display: "must not invent reasoning"},
		{name: "no-progress", hook: true, display: "must not invent reasoning"},
	} {
		for _, interrupted := range []bool{false, true} {
			for _, entry := range []string{"stream", "Run"} {
				t.Run(tc.name+"/"+map[bool]string{false: "ordinary", true: "interrupted"}[interrupted]+"/"+entry, func(t *testing.T) {
					cause := error(errors.New("synthetic unknown completion or parse failure"))
					if interrupted {
						cause = &provider.StreamInterruptedError{Err: cause}
					}
					failure := provider.NoRequestReplay(cause)
					if provider.IsStreamInterrupted(failure) != interrupted {
						t.Fatal("fixture does not exercise the intended ordinary/interrupted distinction")
					}
					var chunks []provider.Chunk
					for i, text := range tc.reasoning {
						chunk := provider.Chunk{Type: provider.ChunkReasoning, Text: text}
						if i == len(tc.reasoning)-1 {
							chunk.Signature = tc.signature
						}
						chunks = append(chunks, chunk)
					}
					if tc.text != "" {
						chunks = append(chunks, provider.Chunk{Type: provider.ChunkText, Text: tc.text[:8]}, provider.Chunk{Type: provider.ChunkText, Text: tc.text[8:]})
					}
					usage := &provider.Usage{PromptTokens: 3, CompletionTokens: 6, TotalTokens: 9}
					chunks = append(chunks,
						provider.Chunk{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: "unfinished", Name: "synthetic_write", Arguments: `{"incomplete":`}},
						provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "complete", Name: "synthetic_write", Arguments: `{}`}},
						provider.Chunk{Type: provider.ChunkUsage, Usage: usage},
						provider.Chunk{Type: provider.ChunkError, Err: failure},
					)
					p := testutil.NewMock("synthetic", testutil.Turn{Chunks: chunks}, testutil.Turn{Text: "must not recover"})
					toolCalls := 0
					reg := tool.NewRegistry()
					reg.Add(noReplayProgressTool{calls: &toolCalls})
					hooks := &noReplayProgressHooks{stubHooks: stubHooks{hasPostLLM: tc.hook, postLLMOut: tc.display}}
					sink := &recordSink{}
					session := NewSession("synthetic system")
					a := New(p, reg, session, Options{Hooks: hooks}, sink)
					original := strings.Join(tc.reasoning, "")
					wantStored := original
					var wantReasoningEvents []string
					if tc.hook && original != "" {
						wantStored = tc.display
						if tc.display != "" {
							wantReasoningEvents = []string{tc.display}
						}
					} else if !tc.hook {
						for _, text := range tc.reasoning {
							if text != "" {
								wantReasoningEvents = append(wantReasoningEvents, text)
							}
						}
					}
					if tc.signature != "" {
						wantStored = original
					}
					var wantProtocol *string
					if tc.reasoning != nil {
						wantProtocol = &original
					}
					var err error
					if entry == "stream" {
						var text, signature string
						var stored turnReasoning
						var calls []provider.ToolCall
						var gotUsage *provider.Usage
						var recoverable, toolStarted bool
						text, stored, signature, calls, gotUsage, recoverable, toolStarted, err = a.stream(context.Background(), 1, "synthetic-request")
						if text != tc.text || stored.content != wantStored || !reflect.DeepEqual(stored.protocol, wantProtocol) || signature != tc.signature {
							t.Error("stream discarded received progress or crossed the protocol/display reasoning boundary")
						}
						if calls != nil || recoverable || !toolStarted || gotUsage != usage {
							t.Errorf("unsafe partial return: calls=%v recoverable=%v toolStarted=%v usage=%v", calls, recoverable, toolStarted, gotUsage)
						}
					} else {
						err = a.Run(context.Background(), "synthetic user request")
						want := []provider.Message{{Role: provider.RoleSystem, Content: "synthetic system"}, {Role: provider.RoleUser, Content: "synthetic user request"}}
						if tc.text != "" || tc.reasoning != nil {
							want = append(want, provider.Message{Role: provider.RoleAssistant, Content: tc.text, ReasoningContent: wantStored, ProtocolReasoningContent: wantProtocol, ReasoningSignature: tc.signature})
						}
						if !reflect.DeepEqual(session.Snapshot(), want) {
							t.Errorf("history lost real progress, retained tools or added recovery content: got=%+v want=%+v", session.Snapshot(), want)
						}
						path := filepath.Join(t.TempDir(), "synthetic-no-replay.jsonl")
						if saveErr := session.Save(path); saveErr != nil {
							t.Fatal(saveErr)
						}
						restored, loadErr := LoadSession(path)
						if loadErr != nil {
							t.Fatal(loadErr)
						}
						if !reflect.DeepEqual(restored.Snapshot(), want) {
							t.Error("saved partial progress did not restore with its display/protocol distinction")
						}
					}
					if !errors.Is(err, cause) || !provider.ReplayForbidden(err) {
						t.Errorf("lost no-replay error: %v", err)
					}
					if p.CallCount() != 1 || len(p.Requests()) != 1 || toolCalls != 0 || len(hooks.preSeen) != 0 || len(hooks.postSeen) != 0 {
						t.Errorf("replayed or executed tools: streams=%d tools=%d pre=%v post=%v", p.CallCount(), toolCalls, hooks.preSeen, hooks.postSeen)
					}
					for _, kind := range []event.Kind{event.Retrying, event.ToolResult, event.Message, event.AnswerCommitted} {
						if len(sink.kinds(kind)) != 0 {
							t.Errorf("failure emitted a recovery/completion event: %v", kind)
						}
					}
					for _, e := range sink.kinds(event.ToolDispatch) {
						if !e.Tool.Partial || e.Tool.ID != "unfinished" {
							t.Error("complete tool dispatch escaped the no-replay boundary")
						}
					}
					var textEvents strings.Builder
					for _, e := range sink.kinds(event.Text) {
						textEvents.WriteString(e.Text)
					}
					if textEvents.String() != tc.text {
						t.Error("received visible text was changed or duplicated")
					}
					var reasoningEvents []string
					for _, e := range sink.kinds(event.Reasoning) {
						reasoningEvents = append(reasoningEvents, e.Text)
					}
					if !reflect.DeepEqual(reasoningEvents, wantReasoningEvents) {
						t.Errorf("reasoning display bypassed or duplicated hooks: got=%q want=%q", reasoningEvents, wantReasoningEvents)
					}
					if tc.hook && original != "" {
						if !reflect.DeepEqual(hooks.postLLMSeen, []string{original}) || !reflect.DeepEqual(hooks.postLLMTurns, []int{1}) {
							t.Errorf("hook did not receive the original partial reasoning exactly once: %q", hooks.postLLMSeen)
						}
					} else if len(hooks.postLLMSeen) != 0 {
						t.Error("hook invented reasoning without a received nonempty block")
					}
				})
			}
		}
	}
}
