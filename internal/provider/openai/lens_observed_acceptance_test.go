//go:build lenslive

package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// This observer keeps counters and marker state, never HTTP headers or bodies.
type lensObservedAttempt struct {
	Attempt      int    `json:"attempt"`
	StartMS      int64  `json:"start_ms"`
	HeadersMS    int64  `json:"headers_ms"`
	Status       int    `json:"status"`
	ReadCalls    int    `json:"read_calls"`
	ReadBytes    int64  `json:"read_bytes"`
	FirstByteMS  int64  `json:"first_byte_ms"`
	LastByteMS   int64  `json:"last_byte_ms"`
	MaxReadGapMS int64  `json:"max_read_gap_ms"`
	WireDone     bool   `json:"wire_done"`
	WireDoneMS   int64  `json:"wire_done_ms"`
	ReadEnd      string `json:"read_end"`
	RequestError string `json:"request_error"`
	BodyClosed   bool   `json:"body_closed"`
	CloseMS      int64  `json:"close_ms"`
	CloseContext string `json:"close_context"`
}

type lensObservedReport struct {
	Request              int                   `json:"request"`
	ElapsedMS            int64                 `json:"elapsed_ms"`
	AttemptCount         int                   `json:"attempt_count"`
	Attempts             []lensObservedAttempt `json:"attempts"`
	ProviderDone         bool                  `json:"provider_done"`
	Chunks               int                   `json:"chunks"`
	TextBytes            int                   `json:"text_bytes"`
	ReasoningBytes       int                   `json:"reasoning_bytes"`
	ToolCalls            int                   `json:"tool_calls"`
	InputTokens          int                   `json:"input_tokens"`
	OutputTokens         int                   `json:"output_tokens"`
	UsagePresent         bool                  `json:"usage_present"`
	ContextBeforeCleanup string                `json:"context_before_cleanup"`
	Result               string                `json:"result"`
}

type lensObservedTrace struct {
	mu       sync.Mutex
	start    time.Time
	attempts []*lensObservedAttempt
}

type lensObservedTraceKey struct{}

type lensObservedTransport struct{ next http.RoundTripper }

func (r *lensObservedTransport) RoundTrip(q *http.Request) (*http.Response, error) {
	trace, ok := q.Context().Value(lensObservedTraceKey{}).(*lensObservedTrace)
	if !ok {
		return r.next.RoundTrip(q)
	}
	trace.mu.Lock()
	a := &lensObservedAttempt{Attempt: len(trace.attempts) + 1, StartMS: time.Since(trace.start).Milliseconds(),
		HeadersMS: -1, FirstByteMS: -1, LastByteMS: -1, WireDoneMS: -1, CloseMS: -1,
		ReadEnd: "none", RequestError: "none", CloseContext: "not_closed"}
	trace.attempts = append(trace.attempts, a)
	trace.mu.Unlock()
	resp, err := r.next.RoundTrip(q)
	trace.mu.Lock()
	a.RequestError = lensObservedError(err)
	if resp != nil {
		a.Status = resp.StatusCode
		a.HeadersMS = time.Since(trace.start).Milliseconds()
		if resp.Body != nil {
			resp.Body = &lensObservedBody{ReadCloser: resp.Body, trace: trace, attempt: a, ctx: q.Context()}
		}
	}
	trace.mu.Unlock()
	return resp, err
}

// Match a complete SSE data line without retaining any part of its contents.
// JSON containing "[DONE]", comments, and partial markers cannot count as DONE.
type lensWireDoneProbe struct {
	matched int
	invalid bool
}

func (p *lensWireDoneProbe) consume(data []byte, eof bool) bool {
	const marker = "data:[DONE]"
	found := false
	endLine := func() {
		found = found || (!p.invalid && p.matched == len(marker))
		p.matched, p.invalid = 0, false
	}
	for _, b := range data {
		if b == '\n' {
			endLine()
			continue
		}
		if p.invalid {
			continue
		}
		space := b == ' ' || b == '\t' || b == '\r'
		if space && (p.matched == 0 || p.matched == 5 || p.matched == len(marker)) {
			continue
		}
		if p.matched < len(marker) && b == marker[p.matched] {
			p.matched++
		} else {
			p.invalid = true
		}
	}
	if eof {
		endLine()
	}
	return found
}

type lensObservedBody struct {
	io.ReadCloser
	trace   *lensObservedTrace
	attempt *lensObservedAttempt
	ctx     context.Context
	probe   lensWireDoneProbe
}

func (b *lensObservedBody) Read(dst []byte) (int, error) {
	n, err := b.ReadCloser.Read(dst)
	b.trace.mu.Lock()
	defer b.trace.mu.Unlock()
	a := b.attempt
	a.ReadCalls++
	now := time.Since(b.trace.start).Milliseconds()
	if n > 0 {
		previous := a.LastByteMS
		if previous < 0 {
			previous = a.HeadersMS
			a.FirstByteMS = now
		}
		if gap := now - previous; gap > a.MaxReadGapMS {
			a.MaxReadGapMS = gap
		}
		a.LastByteMS = now
		a.ReadBytes += int64(n)
	}
	if b.probe.consume(dst[:n], err == io.EOF) && !a.WireDone {
		a.WireDone, a.WireDoneMS = true, now
	}
	if err != nil {
		a.ReadEnd = lensObservedError(err)
		previous := a.LastByteMS
		if previous < 0 {
			previous = a.HeadersMS
		}
		if gap := now - previous; gap > a.MaxReadGapMS {
			a.MaxReadGapMS = gap
		}
	}
	return n, err
}

func (b *lensObservedBody) Close() error {
	b.trace.mu.Lock()
	if !b.attempt.BodyClosed {
		b.attempt.BodyClosed = true
		b.attempt.CloseMS = time.Since(b.trace.start).Milliseconds()
		b.attempt.CloseContext = lensObservedError(b.ctx.Err())
	}
	b.trace.mu.Unlock()
	return b.ReadCloser.Close()
}

// Only allowlisted classes leave the harness; raw errors can echo credentials.
func lensObservedError(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, provider.ErrStreamConsumerBlocked):
		return "consumer_blocked"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected_eof"
	case errors.Is(err, io.EOF):
		return "eof"
	}
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		return fmt.Sprintf("http_%d", apiErr.Status)
	}
	var authErr *provider.AuthError
	if errors.As(err, &authErr) {
		return fmt.Sprintf("http_%d", authErr.Status)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "network_timeout"
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{"upstream_stream_incomplete", "stream stalled", "decode stream"} {
		if strings.Contains(text, marker) {
			return strings.ReplaceAll(marker, " ", "_")
		}
	}
	return "provider_error"
}

func lensObserveCompletion(ctx context.Context, c *client, req provider.Request, number int) (message provider.Message, report lensObservedReport) {
	trace := &lensObservedTrace{start: time.Now()}
	ctx, cancel := context.WithCancel(context.WithValue(ctx, lensObservedTraceKey{}, trace))
	report.Request = number
	report.Result = "ok"
	message.Role = provider.RoleAssistant
	var text, reasoning strings.Builder
	defer func() {
		report.ContextBeforeCleanup = lensObservedError(ctx.Err())
		cancel()
		message.Content, message.ReasoningContent = text.String(), reasoning.String()
		report.TextBytes, report.ReasoningBytes = text.Len(), reasoning.Len()
		report.ToolCalls = len(message.ToolCalls)
		report.ElapsedMS = time.Since(trace.start).Milliseconds()
		trace.mu.Lock()
		defer trace.mu.Unlock()
		for _, a := range trace.attempts {
			report.Attempts = append(report.Attempts, *a)
		}
		report.AttemptCount = len(report.Attempts)
	}()
	stream, err := c.Stream(ctx, req)
	if err != nil {
		report.Result = lensObservedError(err)
		return
	}
	for {
		select {
		case <-ctx.Done():
			report.Result = lensObservedError(ctx.Err())
			return
		case chunk, ok := <-stream:
			if !ok {
				if report.Result == "ok" {
					switch {
					case ctx.Err() != nil:
						report.Result = lensObservedError(ctx.Err())
					case !report.ProviderDone:
						report.Result = "missing_provider_done"
					case !report.UsagePresent:
						report.Result = "missing_usage"
					default:
						trace.mu.Lock()
						wireDone := len(trace.attempts) > 0 && trace.attempts[len(trace.attempts)-1].WireDone
						trace.mu.Unlock()
						if !wireDone {
							report.Result = "missing_wire_done"
						}
					}
				}
				return
			}
			report.Chunks++
			switch chunk.Type {
			case provider.ChunkText:
				text.WriteString(chunk.Text)
			case provider.ChunkReasoning:
				reasoning.WriteString(chunk.Text)
				original := reasoning.String()
				message.ProtocolReasoningContent = &original
			case provider.ChunkToolCall:
				if chunk.ToolCall == nil || !json.Valid([]byte(chunk.ToolCall.Arguments)) {
					report.Result = "invalid_tool_call"
					return
				}
				message.ToolCalls = append(message.ToolCalls, *chunk.ToolCall)
			case provider.ChunkUsage:
				if chunk.Usage != nil {
					report.UsagePresent = true
					report.InputTokens, report.OutputTokens = chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens
				}
			case provider.ChunkError:
				report.Result = lensObservedError(chunk.Err)
				if report.Result == "none" {
					report.Result = "empty_error"
				}
			case provider.ChunkDone:
				report.ProviderDone = true
			}
		}
	}
}

func lensObservedAgentScenario(ctx context.Context, c *client, nonce string, log func(lensObservedReport)) bool {
	if len(nonce) > 64 {
		log(lensObservedReport{Result: "nonce_too_long"})
		return false
	}
	result := "Inert synthetic fixture " + nonce + ": " + strings.Repeat("item 12345; ", 2000)
	if len(result) >= 32*1024 {
		log(lensObservedReport{Result: "fixture_too_large"})
		return false
	}
	tools := []provider.ToolSchema{{Name: "synthetic_echo", Description: "Return an inert in-memory fixture. No real files or systems are accessed.", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)}}
	messages := []provider.Message{{Role: provider.RoleUser, Content: "Synthetic test: call synthetic_echo exactly once with value hello. After its result arrives, list integers 1 to 200, one per line. Do not call any further tools."}}
	for step, limit := range []int{512, 1024, 128} {
		requestCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		message, report := lensObserveCompletion(requestCtx, c, provider.Request{
			RequestID: fmt.Sprintf("synthetic-observed-%d", step+1), Purpose: provider.RequestPurposeTurn,
			Messages: messages, Tools: tools, MaxTokens: limit,
		}, step+1)
		cancel()
		if report.Result == "ok" {
			switch {
			case step == 0:
				var args struct {
					Value string `json:"value"`
				}
				if len(message.ToolCalls) != 1 || message.ToolCalls[0].Name != "synthetic_echo" || message.ToolCalls[0].ID == "" {
					report.Result = "unexpected_tool_call"
				} else if json.Unmarshal([]byte(message.ToolCalls[0].Arguments), &args) != nil || args.Value != "hello" {
					report.Result = "unexpected_tool_args"
				}
			case len(message.ToolCalls) != 0:
				report.Result = "unexpected_tool_call"
			case strings.TrimSpace(message.Content) == "":
				report.Result = "empty_answer"
			case step == 2 && strings.TrimSpace(message.Content) != "completed":
				report.Result = "unexpected_answer"
			}
		}
		// Always log before returning, including transport, protocol and task errors.
		log(report)
		if report.Result != "ok" {
			return false
		}
		messages = append(messages, message)
		if step == 0 {
			messages = append(messages, provider.Message{Role: provider.RoleTool, ToolCallID: message.ToolCalls[0].ID, Name: "synthetic_echo", Content: result})
		} else if step == 1 {
			messages = append(messages, provider.Message{Role: provider.RoleUser, Content: "Reply exactly completed without calling any tools."})
		}
	}
	return true
}

// Not selected by offline validation. The operator supplies a temporary key and
// external temperature guard; a separate opt-in avoids reusing the older test.
func TestLensObservedLiveAgentContinuation(t *testing.T) {
	if os.Getenv("LENS_OBSERVED_LIVE") != "1" {
		t.Skip("independent live-test opt-in required")
	}
	key, base, model := os.Getenv("LENS_OBSERVED_KEY"), os.Getenv("LENS_OBSERVED_BASE"), os.Getenv("LENS_OBSERVED_MODEL")
	if key == "" || base == "" || model == "" {
		t.Skip("explicit temporary test configuration required")
	}
	p, err := New(provider.Config{Name: "lens-observed", APIKey: key, BaseURL: base, Model: model})
	if err != nil {
		t.Fatalf("provider setup failed: %s", lensObservedError(err))
	}
	c := p.(*client)
	c.http.Transport = &lensObservedTransport{next: c.http.Transport}
	nonce := os.Getenv("LENS_OBSERVED_NONCE")
	if nonce == "" {
		nonce = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if !lensObservedAgentScenario(context.Background(), c, nonce, func(report lensObservedReport) {
		data, _ := json.Marshal(report)
		t.Logf("lens_observed=%s", data)
	}) {
		t.Fatal("observed scenario failed; see numeric diagnostics above")
	}
}
