package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent/testutil"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestRunDoesNotContinueNonReplayableRequest(t *testing.T) {
	for _, partial := range []string{"", "received text"} {
		t.Run(partial, func(t *testing.T) {
			cause := &provider.StreamInterruptedError{Err: errors.New("synthetic interrupted request")}
			mp := testutil.NewMock("synthetic", testutil.Turn{Chunks: []provider.Chunk{
				{Type: provider.ChunkText, Text: partial},
				{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: "partial", Name: "echo"}},
				{Type: provider.ChunkError, Err: provider.NoRequestReplay(cause)},
			}}, testutil.Turn{Text: "must not send"})
			sink := &recordSink{}
			a := New(mp, echoRegistry(), NewSession(""), Options{}, sink)
			if err := a.Run(context.Background(), "synthetic request"); !errors.Is(err, cause) || !provider.ReplayForbidden(err) {
				t.Fatalf("no-replay policy lost: %v", err)
			}
			if mp.CallCount() != 1 || len(sink.kinds(event.Retrying)) != 0 {
				t.Fatalf("uncertain request replayed: calls=%d", mp.CallCount())
			}
			found := false
			for _, msg := range a.Session().Messages {
				if msg.Role == provider.RoleTool || len(msg.ToolCalls) > 0 {
					t.Fatal("partial tool call persisted or executed")
				}
				found = found || msg.Role == provider.RoleAssistant && msg.Content == partial
			}
			if partial != "" && !found {
				t.Fatal("received text was discarded")
			}
		})
	}
}

func TestSummaryDoesNotFallbackNonReplayableRequest(t *testing.T) {
	for _, cause := range []error{
		&provider.StreamInterruptedError{Err: errors.New("synthetic disconnect")},
		&provider.APIError{Status: 503, Body: "synthetic unavailable"},
		&provider.APIError{Status: 422, Body: "reasoning_effort unsupported"},
	} {
		mp := testutil.NewMock("synthetic", testutil.ErrorTurn(provider.NoRequestReplay(cause)), testutil.Turn{Text: "must not send"})
		session := NewSession("original context")
		session.Add(provider.Message{Role: provider.RoleUser, Content: "synthetic history"})
		before := append([]provider.Message(nil), session.Messages...)
		a := New(mp, echoRegistry(), session, Options{}, event.Discard)
		if _, err := a.summarize(context.Background(), session.Messages, ""); !errors.Is(err, cause) || !provider.ReplayForbidden(err) {
			t.Fatalf("no-replay policy lost: %v", err)
		}
		if mp.CallCount() != 1 || !reflect.DeepEqual(before, session.Messages) {
			t.Fatalf("fallback or history mutation: calls=%d", mp.CallCount())
		}
	}
}
