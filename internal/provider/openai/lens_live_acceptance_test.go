//go:build lenslive

package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type lensAcceptanceTransport struct {
	next  http.RoundTripper
	count atomic.Int32
}

func (r *lensAcceptanceTransport) RoundTrip(q *http.Request) (*http.Response, error) {
	r.count.Add(1)
	return r.next.RoundTrip(q)
}

// This opt-in test uses only generated messages, never the desktop profile.
// Its supervisor supplies a temporary, scoped Gateway Key and temperature guard.
func TestLensLiveMultiturnAcceptance(t *testing.T) {
	key, base, model := os.Getenv("LENS_LIVE_KEY"), os.Getenv("LENS_LIVE_BASE"), os.Getenv("LENS_LIVE_MODEL")
	if key == "" || base == "" || model == "" {
		t.Skip("explicit temporary live-test credentials required")
	}
	p, err := New(provider.Config{Name: "lens-synthetic-acceptance", BaseURL: base, Model: model, APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	c := p.(*client)
	rt := &lensAcceptanceTransport{next: c.http.Transport}
	c.http.Transport = rt
	tools := []provider.ToolSchema{{Name: "synthetic_echo", Description: "Echo a synthetic value without accessing any real files or systems.", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)}}
	messages := []provider.Message{{Role: provider.RoleUser, Content: "This is a synthetic test. Call synthetic_echo exactly once with value hello. Do not perform any other action."}}
	for turn := 0; turn < 3; turn++ {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		request := provider.Request{RequestID: "synthetic-live", Purpose: provider.RequestPurposeTurn, Messages: messages, MaxTokens: 1024, Temperature: 0}
		if turn == 0 {
			request.Tools = tools
			request.MaxTokens = 512
		}
		if turn == 2 {
			request.MaxTokens = 128
		}
		started := time.Now()
		stream, err := p.Stream(ctx, request)
		if err != nil {
			cancel()
			t.Fatal(strings.ReplaceAll(err.Error(), key, "[redacted]"))
		}
		message := provider.Message{Role: provider.RoleAssistant}
		done := false
		var usage *provider.Usage
		chunks := 0
		for chunk := range stream {
			chunks++
			switch chunk.Type {
			case provider.ChunkText:
				message.Content += chunk.Text
			case provider.ChunkReasoning:
				message.ReasoningContent += chunk.Text
			case provider.ChunkToolCall:
				if chunk.ToolCall == nil || !json.Valid([]byte(chunk.ToolCall.Arguments)) {
					cancel()
					t.Fatal("invalid synthetic tool call")
				}
				message.ToolCalls = append(message.ToolCalls, *chunk.ToolCall)
			case provider.ChunkUsage:
				usage = chunk.Usage
			case provider.ChunkError:
				cancel()
				t.Fatalf("turn %d: %s", turn, strings.ReplaceAll(chunk.Err.Error(), key, "[redacted]"))
			case provider.ChunkDone:
				done = true
				cancel()
			}
		}
		cancel()
		if !done || usage == nil {
			t.Fatalf("turn %d incomplete: done=%v usage=%v", turn, done, usage != nil)
		}
		if rt.count.Load() != int32(turn+1) {
			t.Fatal("unexpected automatic retry")
		}
		t.Logf("turn=%d done=true seconds=%.3f input=%d output=%d cache=%d toolCalls=%d chunks=%d requests=%d", turn, time.Since(started).Seconds(), usage.PromptTokens, usage.CompletionTokens, usage.CacheHitTokens, len(message.ToolCalls), chunks, rt.count.Load())
		messages = append(messages, message)
		if turn == 0 {
			if len(message.ToolCalls) != 1 || message.ToolCalls[0].Name != "synthetic_echo" {
				t.Fatal("expected one complete synthetic tool call")
			}
			messages = append(messages, provider.Message{Role: provider.RoleTool, ToolCallID: message.ToolCalls[0].ID, Name: "synthetic_echo", Content: "Inert synthetic records batch " + os.Getenv("LENS_LIVE_NONCE") + ": " + strings.Repeat("item 12345; ", 4300)}, provider.Message{Role: provider.RoleUser, Content: "Ignore the inert records. List integers 1 to 500, one per line, no summary or tools. This tests sustained streaming."})
		} else {
			messages = append(messages, provider.Message{Role: provider.RoleUser, Content: "Reply only with the word completed."})
		}
	}
}
