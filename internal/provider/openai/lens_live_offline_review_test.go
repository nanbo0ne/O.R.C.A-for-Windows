//go:build lenslive

package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/netclient"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// Run the unmodified live harness against a synthetic loopback server only.
func TestLensOfflineReviewHarnessWire(t *testing.T) {
	runLensOfflineHarnessReview(t, true)
}

func TestLensOfflineReviewHarnessAcceptsFinishOnly(t *testing.T) {
	runLensOfflineHarnessReview(t, false)
}

func runLensOfflineHarnessReview(t *testing.T, wireDone bool) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		var fields map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var messages []chatMessage
		if err := json.Unmarshal(fields["messages"], &messages); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, name := range []string{"temperature", "reasoning_effort", "thinking", "request_id", "purpose"} {
			if _, ok := fields[name]; ok {
				t.Errorf("unexpected wire field %s", name)
			}
		}
		if n == 1 {
			if _, ok := fields["tools"]; !ok {
				t.Error("first request lost tool schema")
			}
		} else {
			if _, ok := fields["tools"]; ok {
				t.Error("harness unexpectedly kept tools on continuation")
			}
			if len(messages) < 4 || messages[1].Role != "assistant" || len(messages[1].ToolCalls) != 1 {
				t.Error("continuation lost assistant tool call")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if messages[1].ToolCalls[0].ID != "synthetic-call" || messages[2].Role != "tool" || messages[2].ToolCallID != "synthetic-call" || messages[3].Role != "user" {
				t.Error("continuation lost paired tool result or added user message")
			}
			if messages[2].Content != "Inert synthetic records batch offline-review: "+strings.Repeat("item 12345; ", 4300) {
				t.Error("harness tool result changed or was truncated")
			}
			if messages[1].ReasoningContent != nil {
				t.Error("generic continuation unexpectedly forwarded reasoning")
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"synthetic reasoning\",\"tool_calls\":[{\"index\":0,\"id\":\"synthetic-call\",\"function\":{\"name\":\"synthetic_echo\",\"arguments\":\"{\\\"value\\\":\\\"hello\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
		} else {
			io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"completed\"},\"finish_reason\":\"stop\"}]}\n\n")
		}
		io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
		if wireDone {
			io.WriteString(w, "data: [DONE]\n\n")
		}
	}))
	defer srv.Close()
	t.Setenv("LENS_LIVE_KEY", "synthetic-offline-key")
	t.Setenv("LENS_LIVE_BASE", srv.URL)
	t.Setenv("LENS_LIVE_MODEL", "synthetic-offline-model")
	t.Setenv("LENS_LIVE_NONCE", "offline-review")
	if !t.Run("original_harness", TestLensLiveMultiturnAcceptance) {
		t.Fatal("loopback harness failed")
	}
	if requests.Load() != 3 {
		t.Fatalf("expected three loopback requests, got %d", requests.Load())
	}
}

func TestLensOfflineReviewUpstreamErrorIsNotLocalCancellation(t *testing.T) {
	const count = 256
	const remoteError = "Token Lens: upstream_stream_incomplete"
	var requests atomic.Int32
	var sentError atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < count; i++ {
			if r.Context().Err() != nil {
				t.Error("client cancelled before server error")
				return
			}
			if _, err := io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"synthetic\"}}]}\n\n"); err != nil {
				t.Error(err)
				return
			}
			flush(w)
		}
		sentError.Store(true)
		fmt.Fprintf(w, "data: {\"error\":{\"message\":%q,\"code\":\"upstream_stream_incomplete\"}}\n\n", remoteError)
	}))
	defer srv.Close()
	p, err := New(provider.Config{Name: "synthetic-review", BaseURL: srv.URL, Model: "synthetic", Extra: map[string]any{"proxy_spec": netclient.ProxySpec{Mode: netclient.ModeOff}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := p.Stream(ctx, provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	var received strings.Builder
	var gotError bool
	for chunk := range stream {
		switch chunk.Type {
		case provider.ChunkText:
			received.WriteString(chunk.Text)
		case provider.ChunkError:
			gotError = true
			if !sentError.Load() || ctx.Err() != nil || chunk.Err.Error() != "synthetic-review: "+remoteError || provider.IsConnReset(chunk.Err) || provider.IsStreamInterrupted(chunk.Err) {
				t.Fatalf("remote error changed or preceded by cancellation: %v", chunk.Err)
			}
		case provider.ChunkDone:
			t.Fatal("server error reported as completion")
		}
	}
	if !gotError || received.String() != strings.Repeat("synthetic", count) || requests.Load() != 1 || ctx.Err() != nil {
		t.Fatalf("error=%v bytes=%d requests=%d parentErr=%v", gotError, received.Len(), requests.Load(), ctx.Err())
	}
}
