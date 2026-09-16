//go:build live || manual

package agent

// Offline (no key lookup, all HTTP stays on loopback):
//   go test -tags manual ./internal/agent -run '^TestDeepSeekV41Fixture' -count=1 -timeout=2m -v
// Gate/compile check (leave ORCA_V41_LIVE unset):
//   go test -tags live ./internal/agent -run '^TestDeepSeekV41Live$' -count=1 -v
// ONLY AFTER the main author authorizes live API use, in a process with an
// already provisioned DEEPSEEK_API_KEY, set ORCA_V41_LIVE=1 and optionally
// ORCA_V41_LIVE_MAX_TOKENS=8192 (default 4096), ORCA_V41_LIVE_REPORT=<new.json>:
//   go test -tags live ./internal/agent -run '^TestDeepSeekV41Live$' -count=1 -timeout=20m -v
// Carry ALL earlier attempts forward with ORCA_V41_PRIOR_REQUESTS and
// ORCA_V41_PRIOR_MICRO_CNY (each defaults to zero). Use the latest cumulative
// report's requests/reserved_micro_cny; the first 402 used 1 and 163840.
// These are caller-supplied totals, not a cross-process lock: runs must be serial.
// Reports use exclusive creation; an existing path is never overwritten.
// No config.Load, dotenv, persisted session, OS tool, or user attachment is used.
// The 5 CNY ceiling is a conservative reservation at the current built-in peak
// tariff, NOT a provider invoice: verify tariff ceilings before authorizing.
// Each attempt reserves 65536 input tokens plus max_tokens, with no refunds
// even on transport errors/missing usage. JSON is capped at 32 KiB, images at
// the single generated 640x400 PNG; this deliberately over-reserves inputs.
// This leaves room for a later child case sharing this SAME harness/ledger.
// Both caps cover ALL cases and retries together, not each individual subtest.
// A task(images) child-agent integration fixture is intentionally still needed
// for host authorization, attachment pinning, model routing and child telemetry.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/netclient"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	_ "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/openai"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"golang.org/x/text/unicode/norm"
)

const (
	v41MaxRequests = 40
	v41BudgetMicro = int64(5_000_000)
	v41InputBound  = 65536
	v41BodyLimit   = 32 << 10
	v41Timeout     = 90 * time.Second
	v41Endpoint    = "https://api.deepseek.com/v1/chat/completions"
	v41Unicode     = "\u4e2d\u6587\u6d41\u5f0f|caf\u00e9|\u03a9|\U0001f680|e\u0301"
	v41Display     = "reasoning hidden by live harness"
)

type v41Case struct{ name, model, effort string }

var v41Cases = []v41Case{
	{"flash-low-unicode", "deepseek-flash", "low"},
	{"flash-high-unicode", "deepseek-flash", "high"},
	{"flash-max-unicode", "deepseek-flash", "max"},
	{"flash-native-vision", "deepseek-flash", "high"},
	{"flash-tool-history", "deepseek-flash", "high"},
	{"pro-text-canary", "deepseek-v4-pro", "high"},
}

// This is the only accounting authority. Attempts are reserved immediately
// before client.Do; redirects, HTTP transport retries and keepalive are disabled.
type v41Ledger struct {
	mu       sync.Mutex
	requests int
	reserved int64
	stopped  string
}

func (b *v41Ledger) reserve(micro int64) (int, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped == "" {
		switch {
		case b.requests >= v41MaxRequests:
			b.stopped = "request_budget"
		case micro <= 0 || micro > v41BudgetMicro-b.reserved:
			b.stopped = "cost_budget"
		}
	}
	if b.stopped != "" {
		return 0, b.stopped
	}
	b.requests++
	b.reserved += micro
	return b.requests, ""
}

func (b *v41Ledger) stop(class string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped == "" {
		b.stopped = class
	}
}

type v41HTTPRecord struct {
	Attempt    int    `json:"attempt"`
	Case       string `json:"case"`
	Model      string `json:"model"`
	Effort     string `json:"effort"`
	MaxTokens  int    `json:"max_tokens"`
	Reserved   int64  `json:"reserved_micro_cny"`
	Status     int    `json:"http_status"`
	ElapsedMS  int64  `json:"elapsed_ms"`
	ErrorClass string `json:"error_class,omitempty"`
	Images     int    `json:"native_image_parts"`
	History    int    `json:"reasoning_history_checked"`
}

type v41StreamRecord struct {
	Case         string `json:"case"`
	FirstDeltaMS int64  `json:"first_delta_ms"`
	FirstKind    string `json:"first_delta_kind,omitempty"`
	TextDeltas   int    `json:"text_deltas"`
	ReasonDeltas int    `json:"reasoning_deltas"`
	ToolCalls    int    `json:"tool_calls"`
	Prompt       int    `json:"prompt_tokens"`
	Completion   int    `json:"completion_tokens"`
	CacheHit     int    `json:"cache_hit_tokens"`
	Reasoning    int    `json:"reasoning_tokens"`
	Finish       string `json:"finish_reason"`
	Done         bool   `json:"done"`
	ErrorClass   string `json:"error_class,omitempty"`
}

type v41Report struct {
	Schema         int               `json:"schema"`
	Mode           string            `json:"mode"`
	Passed         bool              `json:"passed"`
	Started        string            `json:"started_utc"`
	RequestLimit   int               `json:"request_limit"`
	BudgetMicro    int64             `json:"budget_micro_cny"`
	InputBound     int               `json:"input_token_reservation_per_attempt"`
	TimeoutSeconds int               `json:"request_timeout_seconds"`
	Requests       int               `json:"requests"`
	Reserved       int64             `json:"reserved_micro_cny"`
	PriorRequests  int               `json:"prior_requests"`
	PriorReserved  int64             `json:"prior_reserved_micro_cny"`
	StopClass      string            `json:"stop_class,omitempty"`
	BudgetBasis    string            `json:"budget_basis"`
	HTTP           []v41HTTPRecord   `json:"http"`
	Streams        []v41StreamRecord `json:"streams"`
	Checks         map[string]bool   `json:"checks"`
	Gaps           []string          `json:"gaps"`
}

type v41WireMessage struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Reasoning *string         `json:"reasoning_content"`
	Stored    json.RawMessage `json:"protocol_reasoning_content"`
}

type v41Wire struct {
	Model         string                `json:"model"`
	Effort        string                `json:"reasoning_effort"`
	Max           int                   `json:"max_tokens"`
	Stream        bool                  `json:"stream"`
	Messages      []v41WireMessage      `json:"messages"`
	Tools         []json.RawMessage     `json:"tools"`
	Thinking      struct{ Type string } `json:"thinking"`
	StreamOptions struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

type v41Harness struct {
	ledger   v41Ledger
	mu       sync.Mutex
	report   v41Report
	expected map[string][][32]byte
	image    provider.ImageContent
	answer   string
	bridge   *httptest.Server
	upstream *http.Client
	target   string
	key      string
	max      int
	timeout  time.Duration
}

func v41NewHarness(t *testing.T, mode, target, key string, maxTokens int) *v41Harness {
	t.Helper()
	im, answer := v41SyntheticImage(t)
	h := &v41Harness{
		target: target, key: key, max: maxTokens, timeout: v41Timeout,
		image: im, answer: answer, expected: make(map[string][][32]byte),
		report: v41Report{
			Schema: 1, Mode: mode, Started: time.Now().UTC().Format(time.RFC3339),
			RequestLimit: v41MaxRequests, BudgetMicro: v41BudgetMicro,
			InputBound: v41InputBound, TimeoutSeconds: 90, Checks: make(map[string]bool),
			BudgetBasis: "No refunds; peak ceilings CNY/M input/output: Flash 2/8, Pro 9/27; verify current tariff before live authorization",
			Gaps:        []string{"main Agent task(images) child-agent host authorization, pinned bytes, routing and child telemetry", "Chinese image OCR: synthetic image uses ASCII text and a bar chart", "desktop/UI, persisted-history restore and compaction", "tariff ceilings are assumptions, not an authoritative invoice"},
		},
	}
	// No environment/system proxy, redirect, cookie jar, or shared transport.
	tr := &http.Transport{
		Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: false,
		DialContext:         (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
		TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: v41Timeout,
	}
	h.upstream = &http.Client{Transport: tr, Timeout: v41Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	h.bridge = httptest.NewUnstartedServer(http.HandlerFunc(h.serve))
	h.bridge.Config.ErrorLog = log.New(io.Discard, "", 0)
	h.bridge.Config.ReadHeaderTimeout = 5 * time.Second
	h.bridge.Config.ReadTimeout = v41Timeout
	h.bridge.Config.WriteTimeout = v41Timeout
	h.bridge.Start()
	t.Cleanup(func() { h.bridge.Close(); tr.CloseIdleConnections() })
	return h
}

func (h *v41Harness) provider(t *testing.T, c v41Case) provider.Provider {
	t.Helper()
	p, err := provider.New("openai", provider.Config{
		Name: "deepseek", BaseURL: h.bridge.URL + "/" + c.name,
		Model: c.model, APIKey: "loopback-fixture-only",
		Extra: map[string]any{"reasoning_protocol": "deepseek", "effort": c.effort,
			"proxy_spec": netclient.ProxySpec{Mode: netclient.ModeOff}},
	})
	if err != nil {
		t.Fatal("provider_construction_failed")
	}
	return &v41ObservedProvider{Provider: p, h: h, c: c}
}

func v41Reservation(model string, maxTokens int) int64 {
	if model == "deepseek-v4-pro" {
		return v41InputBound*9 + int64(maxTokens)*27
	}
	return v41InputBound*2 + int64(maxTokens)*8
}

// The relay observes the real adapter's wire format, including retries, without
// changing messages, effort, images, tools or reasoning. Only auth is replaced.
func (h *v41Harness) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Connection", "close")
	reject := func(class string) {
		h.ledger.stop(class)
		http.Error(w, "live_harness_stopped", http.StatusUnprocessableEntity)
	}
	var c v41Case
	for _, candidate := range v41Cases {
		if r.URL.Path == "/"+candidate.name+"/chat/completions" {
			c = candidate
			break
		}
	}
	if r.Method != http.MethodPost || c.name == "" {
		reject("invalid_route")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, v41BodyLimit))
	if err != nil {
		reject("request_body_bound")
		return
	}
	var wire v41Wire
	if json.Unmarshal(body, &wire) != nil || wire.Model != c.model || wire.Effort != c.effort ||
		wire.Max != h.max || (wire.Max != 4096 && wire.Max != 8192) || !wire.Stream ||
		!wire.StreamOptions.IncludeUsage || wire.Thinking.Type != "enabled" || len(wire.Messages) > 32 || len(wire.Tools) > 1 {
		reject("wire_contract")
		return
	}
	h.mu.Lock()
	expected := append([][32]byte(nil), h.expected[c.name]...)
	h.mu.Unlock()
	images, history := 0, 0
	for _, m := range wire.Messages {
		if len(m.Stored) != 0 {
			reject("storage_metadata_on_wire")
			return
		}
		if m.Role == "assistant" && len(wire.Tools) > 0 {
			if history >= len(expected) || m.Reasoning == nil || sha256.Sum256([]byte(*m.Reasoning)) != expected[history] {
				reject("reasoning_history_mismatch")
				return
			}
			history++
		}
		var parts []struct {
			Type     string `json:"type"`
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		if json.Unmarshal(m.Content, &parts) == nil {
			for _, part := range parts {
				if part.Type != "image_url" {
					continue
				}
				images++
				if part.ImageURL.URL != "data:image/png;base64,"+h.image.Data {
					reject("unexpected_image")
					return
				}
			}
		}
	}
	if history != len(expected) || (c.name == "flash-native-vision" && images != 1) || (c.name != "flash-native-vision" && images != 0) {
		reject("history_or_image_contract")
		return
	}
	reservation := v41Reservation(c.model, wire.Max)
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	up, err := http.NewRequestWithContext(ctx, http.MethodPost, h.target, bytes.NewReader(body))
	if err != nil {
		reject("upstream_request")
		return
	}
	up.Header.Set("Content-Type", "application/json")
	up.Header.Set("Accept", "text/event-stream")
	if h.key != "" {
		up.Header.Set("Authorization", "Bearer "+h.key)
	}
	// GetBody would permit net/http to replay an HTTP request on a stale connection.
	up.GetBody = nil
	attempt, class := h.ledger.reserve(reservation)
	if class != "" {
		reject(class)
		return
	}
	start := time.Now()
	rec := v41HTTPRecord{Attempt: attempt, Case: c.name, Model: c.model, Effort: c.effort,
		MaxTokens: wire.Max, Reserved: reservation, Images: images, History: history}
	defer func() {
		rec.ElapsedMS = time.Since(start).Milliseconds()
		h.mu.Lock()
		h.report.HTTP = append(h.report.HTTP, rec)
		h.mu.Unlock()
	}()
	resp, err := h.upstream.Do(up)
	if err != nil {
		rec.ErrorClass = v41ErrorClass(err)
		reject(rec.ErrorClass)
		return
	}
	defer resp.Body.Close()
	rec.Status = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		rec.ErrorClass = v41StatusClass(resp.StatusCode)
		reject(rec.ErrorClass)
		return // Never copy/log an upstream error body or response headers.
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()
	// The bounded scanner preserves SSE data (including split UTF-8 on the wire).
	// It also bounds hostile/malformed responses independently of max_tokens.
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 2<<20))
	scanner.Buffer(make([]byte, 4096), 128<<10)
	done := false
	for scanner.Scan() {
		line := scanner.Text()
		if _, err := fmt.Fprintln(w, line); err != nil {
			rec.ErrorClass = "downstream_closed"
			break
		}
		w.(http.Flusher).Flush()
		if strings.TrimSpace(line) == "data: [DONE]" {
			fmt.Fprintln(w)
			w.(http.Flusher).Flush()
			done = true
			break
		}
	}
	if !done {
		if ctx.Err() != nil {
			rec.ErrorClass = "timeout_or_cancelled"
		} else if rec.ErrorClass == "" {
			rec.ErrorClass = "incomplete_stream"
		}
		h.ledger.stop(rec.ErrorClass)
	}
}

func v41StatusClass(status int) string {
	switch {
	case status == 401 || status == 403:
		return "authentication"
	case status == 429:
		return "rate_limit"
	case status >= 300 && status < 400:
		return "redirect_blocked"
	case status >= 500:
		return "upstream_5xx"
	default:
		return "request_rejected"
	}
}

func v41ErrorClass(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var history *provider.ReasoningHistoryError
	if errors.As(err, &history) {
		return "reasoning_history"
	}
	var auth *provider.AuthError
	if errors.As(err, &auth) {
		return "authentication"
	}
	var api *provider.APIError
	if errors.As(err, &api) {
		return v41StatusClass(api.Status)
	}
	if provider.IsStreamInterrupted(err) {
		return "stream_interrupted"
	}
	return "provider_or_transport"
}

type v41ObservedProvider struct {
	provider.Provider
	h *v41Harness
	c v41Case
}

func (p *v41ObservedProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	req.MaxTokens = p.h.max
	var expected [][32]byte
	if len(req.Tools) > 0 {
		for _, m := range req.Messages {
			if m.Role != provider.RoleAssistant {
				continue
			}
			if m.ProtocolReasoningContent == nil {
				p.h.ledger.stop("missing_protocol_reasoning")
				return nil, errors.New("missing_protocol_reasoning")
			}
			expected = append(expected, sha256.Sum256([]byte(*m.ProtocolReasoningContent)))
		}
	}
	p.h.mu.Lock()
	p.h.expected[p.c.name] = expected
	p.h.mu.Unlock()
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, p.h.timeout)
	start := time.Now()
	rec := v41StreamRecord{Case: p.c.name, FirstDeltaMS: -1}
	save := func() { p.h.mu.Lock(); p.h.report.Streams = append(p.h.report.Streams, rec); p.h.mu.Unlock() }
	source, err := p.Provider.Stream(ctx, req)
	if err != nil {
		cancel()
		rec.ErrorClass = v41ErrorClass(err)
		p.h.ledger.stop(rec.ErrorClass)
		save()
		return nil, errors.New(rec.ErrorClass)
	}
	out := make(chan provider.Chunk, 1)
	go func() {
		defer close(out)
		defer cancel()
		defer save()
		usageSeen := false
		for chunk := range source {
			kind := ""
			switch chunk.Type {
			case provider.ChunkText:
				rec.TextDeltas++
				kind = "text"
				if !utf8.ValidString(chunk.Text) || strings.ContainsRune(chunk.Text, utf8.RuneError) {
					rec.ErrorClass = "invalid_utf8"
				}
			case provider.ChunkReasoning:
				rec.ReasonDeltas++
				kind = "reasoning"
			case provider.ChunkToolCallStart:
				kind = "tool"
			case provider.ChunkToolCall:
				rec.ToolCalls++
			case provider.ChunkUsage:
				if u := chunk.Usage; u != nil {
					usageSeen = true
					rec.Prompt, rec.Completion, rec.CacheHit, rec.Reasoning = u.PromptTokens, u.CompletionTokens, u.CacheHitTokens, u.ReasoningTokens
					switch u.FinishReason {
					case "stop", "tool_calls", "length", "content_filter":
						rec.Finish = u.FinishReason
					default:
						rec.Finish = "unknown"
					}
					if u.PromptTokens <= 0 || u.PromptTokens > v41InputBound || u.CompletionTokens <= 0 || u.CompletionTokens > p.h.max {
						rec.ErrorClass = "usage_bound"
					}
					if rec.Finish != "stop" && rec.Finish != "tool_calls" {
						rec.ErrorClass = "abnormal_finish"
					}
				}
			case provider.ChunkDone:
				rec.Done = true
			case provider.ChunkError:
				rec.ErrorClass = v41ErrorClass(chunk.Err)
				chunk.Err = errors.New(rec.ErrorClass)
			}
			if kind != "" && rec.FirstDeltaMS < 0 {
				rec.FirstDeltaMS = time.Since(start).Milliseconds()
				rec.FirstKind = kind
			}
			if rec.ErrorClass != "" {
				p.h.ledger.stop(rec.ErrorClass)
				cancel()
				for range source {
				}
				break
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				cancel()
			}
		}
		if rec.ErrorClass == "" {
			switch {
			case ctx.Err() != nil:
				rec.ErrorClass = v41ErrorClass(ctx.Err())
			case !rec.Done:
				rec.ErrorClass = "missing_done"
			case !usageSeen:
				rec.ErrorClass = "missing_usage"
			case rec.FirstDeltaMS < 0:
				rec.ErrorClass = "missing_delta"
			}
		}
		if rec.ErrorClass != "" {
			p.h.ledger.stop(rec.ErrorClass)
			select {
			case out <- provider.Chunk{Type: provider.ChunkError, Err: errors.New(rec.ErrorClass)}:
			case <-parent.Done():
			}
		}
	}()
	return out, nil
}

func v41Collect(ctx context.Context, p provider.Provider, req provider.Request) (string, error) {
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	var text strings.Builder
	for chunk := range ch {
		if chunk.Type == provider.ChunkText {
			text.WriteString(chunk.Text)
		}
		if chunk.Type == provider.ChunkError {
			err = chunk.Err
		}
	}
	return text.String(), err
}

// In-memory-only tool: the second call depends on an unpredictable receipt from
// the first, so two tool rounds cannot be satisfied by parallel guessed calls.
type v41ReceiptTool struct {
	mu    sync.Mutex
	token string
	phase int
}

func (*v41ReceiptTool) Name() string { return "fixture_receipt" }
func (*v41ReceiptTool) Description() string {
	return "Read a synthetic receipt in two sequential steps. First issue; then verify the returned token."
}
func (*v41ReceiptTool) ReadOnly() bool { return true }
func (*v41ReceiptTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["issue","verify"]},"token":{"type":"string"}},"required":["action"],"additionalProperties":false}`)
}
func (f *v41ReceiptTool) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var args struct{ Action, Token string }
	if json.Unmarshal(raw, &args) != nil {
		return "", errors.New("invalid_fixture_arguments")
	}
	if args.Action == "issue" && f.phase == 0 {
		f.phase = 1
		out, _ := json.Marshal(map[string]string{"token": f.token})
		return string(out), nil
	}
	if args.Action == "verify" && f.phase == 1 && args.Token == f.token {
		f.phase = 2
		return `{"result":"VERIFIED"}`, nil
	}
	return "", errors.New("invalid_fixture_sequence")
}

type v41HideReasoning struct{}

func (v41HideReasoning) PreToolUse(context.Context, string, json.RawMessage) (bool, string) {
	return false, ""
}
func (v41HideReasoning) PostToolUse(context.Context, string, json.RawMessage, string) {}
func (v41HideReasoning) PreCompact(context.Context, string) string                    { return "" }
func (v41HideReasoning) SubagentStop(context.Context, string)                         {}
func (v41HideReasoning) HasPostLLMCall() bool                                         { return true }
func (v41HideReasoning) PostLLMCall(context.Context, string, int) string              { return v41Display }

func (h *v41Harness) suite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	for _, c := range v41Cases {
		if !t.Run(c.name, func(t *testing.T) {
			p := h.provider(t, c)
			switch c.name {
			case "flash-tool-history":
				f := &v41ReceiptTool{token: v41Nonce(t)}
				reg := tool.NewRegistry()
				reg.Add(f)
				session := NewSession("Use only the fixture tool when requested. Keep reasoning brief. Follow exact output instructions.")
				a := New(p, reg, session, Options{MaxSteps: 4, Hooks: v41HideReasoning{}}, event.Discard)
				err := a.Run(ctx, "Call fixture_receipt with action issue. Wait for its receipt, then call it with action verify and the exact returned token. After the second result reply exactly VERIFIED. Do not guess tokens or combine these dependent steps.")
				if err != nil {
					t.Fatal("tool_loop_failed: " + v41ErrorClass(err))
				}
				f.mu.Lock()
				phase := f.phase
				f.mu.Unlock()
				intermediate, finals := 0, 0
				for _, m := range session.Snapshot() {
					if m.Role != provider.RoleAssistant {
						continue
					}
					if m.ProtocolReasoningContent == nil {
						t.Fatal("original_reasoning_not_preserved")
					}
					if original := *m.ProtocolReasoningContent; original != "" && (m.ReasoningContent != v41Display || original == v41Display) {
						t.Fatal("original_reasoning_replaced_by_display")
					}
					if len(m.ToolCalls) > 0 {
						intermediate++
					} else {
						finals++
						if strings.TrimSpace(m.Content) != "VERIFIED" {
							t.Fatal("tool_final_mismatch")
						}
					}
				}
				if phase != 2 || intermediate != 2 || finals != 1 {
					t.Fatal("two_sequential_tool_rounds_required")
				}
				if err := a.Run(ctx, "New question: what is 17 plus 25? Reply exactly 42 without using any tool."); err != nil {
					t.Fatal("new_question_failed: " + v41ErrorClass(err))
				}
				messages := session.Snapshot()
				last := messages[len(messages)-1]
				if last.Role != provider.RoleAssistant || strings.TrimSpace(last.Content) != "42" || len(last.ToolCalls) != 0 {
					t.Fatal("new_question_mismatch")
				}
			case "flash-native-vision":
				session := NewSession("Read the supplied image directly. Keep reasoning brief. Follow exact output instructions.")
				a := New(p, tool.NewRegistry(), session, Options{MaxSteps: 1, ImageLoader: func(_ context.Context, im provider.ImageContent) (provider.ImageContent, error) {
					if im.Path != h.image.Path {
						return provider.ImageContent{}, errors.New("unknown_synthetic_image")
					}
					return h.image, nil
				}}, event.Discard)
				ref := h.image
				ref.Data = ""
				if err := a.RunRich(ctx, RichInput{Text: "Read the CODE and the label of the tallest bar in the image. Reply exactly CODE|LABEL, replacing these placeholders with the visible values; no extra text.", Images: []provider.ImageContent{ref}}); err != nil {
					t.Fatal("native_vision_failed: " + v41ErrorClass(err))
				}
				messages := session.Snapshot()
				if strings.TrimSpace(messages[len(messages)-1].Content) != h.answer {
					t.Fatal("native_image_answer_mismatch")
				}
			default:
				want := v41Unicode
				if c.name == "pro-text-canary" {
					want = "ORCA_PRO_OK"
				}
				got, err := v41Collect(ctx, p, provider.Request{Messages: []provider.Message{
					{Role: provider.RoleSystem, Content: "Keep reasoning brief; reproduce the requested text exactly."},
					{Role: provider.RoleUser, Content: "Reply exactly with this text and no quotes or explanations: " + want},
				}})
				if err != nil {
					t.Fatal("stream_failed: " + v41ErrorClass(err))
				}
				visible := strings.TrimSpace(got)
				if visible != want {
					// The synthetic echo may use canonically equivalent Unicode.
					// Log only this fixture's visible answer, never protocol reasoning.
					equivalent := norm.NFC.String(visible) == norm.NFC.String(want)
					t.Logf("synthetic_echo=%+q nfc_equivalent=%t", visible, equivalent)
					if !equivalent {
						t.Fatal("stream_content_mismatch")
					}
				}
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			for _, s := range h.report.Streams {
				if s.Case == c.name && (s.ErrorClass != "" || !s.Done || s.FirstDeltaMS < 0) {
					t.Fatal("stream_metadata_failed")
				}
			}
			h.report.Checks[c.name] = true
		}) {
			h.ledger.stop("case_failed")
			return
		}
	}
}

func (h *v41Harness) snapshot(passed bool) v41Report {
	h.mu.Lock()
	report := h.report
	h.mu.Unlock()
	h.ledger.mu.Lock()
	report.Requests, report.Reserved, report.StopClass = h.ledger.requests, h.ledger.reserved, h.ledger.stopped
	h.ledger.mu.Unlock()
	report.Passed = passed && report.StopClass == ""
	return report
}

func (h *v41Harness) finish(t *testing.T, path string) {
	t.Helper()
	// Close waits for the last relay record before serializing the report.
	h.bridge.Close()
	report := h.snapshot(!t.Failed())
	if report.StopClass != "" && !t.Failed() {
		t.Error("harness_stopped: " + report.StopClass)
	}
	if path != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Error("report_encode_failed")
			return
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Error("report_create_failed (use an existing directory and a new filename)")
			return
		}
		_, writeErr := f.Write(append(data, '\n'))
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			t.Error("report_write_failed")
			return
		}
	}
	for _, s := range report.Streams {
		t.Logf("case=%s first_delta_ms=%d first_kind=%s prompt=%d completion=%d reasoning_tokens=%d error_class=%s", s.Case, s.FirstDeltaMS, s.FirstKind, s.Prompt, s.Completion, s.Reasoning, s.ErrorClass)
	}
	t.Logf("mode=%s requests=%d/%d reserved_micro_cny=%d/%d report_written=%t", report.Mode, report.Requests, v41MaxRequests, report.Reserved, v41BudgetMicro, path != "")
}

func TestDeepSeekV41Live(t *testing.T) {
	if os.Getenv("ORCA_V41_LIVE") != "1" {
		t.Skip("manual live gate closed: ORCA_V41_LIVE=1 requires main-author authorization")
	}
	maxTokens := 4096
	switch os.Getenv("ORCA_V41_LIVE_MAX_TOKENS") {
	case "", "4096":
	case "8192":
		maxTokens = 8192
	default:
		t.Fatal("max_tokens_must_be_4096_or_8192")
	}
	priorRequests, priorMicro, err := v41ParsePriorBudget(os.Getenv("ORCA_V41_PRIOR_REQUESTS"), os.Getenv("ORCA_V41_PRIOR_MICRO_CNY"))
	if err != nil {
		t.Fatal("invalid_prior_budget: use integers within 0..40 requests and 0..5000000 micro-CNY")
	}
	key := os.Getenv("DEEPSEEK_API_KEY") // Only key source; never read when gate is closed.
	if key == "" {
		t.Fatal("DEEPSEEK_API_KEY_missing")
	}
	h := v41NewHarness(t, "live", v41Endpoint, key, maxTokens)
	h.ledger.requests, h.ledger.reserved = priorRequests, priorMicro
	h.report.PriorRequests, h.report.PriorReserved = priorRequests, priorMicro
	defer h.finish(t, os.Getenv("ORCA_V41_LIVE_REPORT"))
	h.suite(t)
}

func v41ParsePriorBudget(requestsRaw, microRaw string) (int, int64, error) {
	parse := func(raw string, limit uint64) (uint64, error) {
		if raw == "" {
			return 0, nil
		}
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || n > limit {
			return 0, errors.New("invalid_prior_budget")
		}
		return n, nil
	}
	requests, err := parse(requestsRaw, v41MaxRequests)
	if err != nil {
		return 0, 0, err
	}
	micro, err := parse(microRaw, uint64(v41BudgetMicro))
	return int(requests), int64(micro), err
}

func TestDeepSeekV41FixturePriorBudget(t *testing.T) {
	for _, tc := range []struct{ requests, micro string }{
		{"-1", "0"}, {"41", "0"}, {"0", "-1"}, {"0", "5000001"},
		{"one", "0"}, {"0", "1.5"}, {"18446744073709551616", "0"},
	} {
		if _, _, err := v41ParsePriorBudget(tc.requests, tc.micro); err == nil {
			t.Fatal("invalid_prior_budget_accepted")
		}
	}
	for _, tc := range []struct {
		requests, micro string
		wantAttempt     int
		wantMicro       int64
	}{
		{"", "", 1, 163840}, {"1", "163840", 2, 327680},
		{"39", "163840", 40, 327680}, {"1", "4836160", 2, 5000000},
	} {
		requests, micro, err := v41ParsePriorBudget(tc.requests, tc.micro)
		if err != nil {
			t.Fatal("valid_prior_budget_rejected")
		}
		h := &v41Harness{ledger: v41Ledger{requests: requests, reserved: micro}}
		attempt, class := h.ledger.reserve(163840)
		report := h.snapshot(false)
		if class != "" || attempt != tc.wantAttempt || report.Requests != tc.wantAttempt || report.Reserved != tc.wantMicro {
			t.Fatal("prior_budget_not_included_in_counter_or_report")
		}
		if attempt == 40 || report.Reserved == v41BudgetMicro {
			if _, class := h.ledger.reserve(163840); class == "" {
				t.Fatal("combined_budget_cap_bypassed")
			}
		}
	}
}

func v41Nonce(t *testing.T) string {
	t.Helper()
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal("fixture_random_failed")
	}
	return strings.ToUpper(hex.EncodeToString(raw[:]))
}

func v41SyntheticImage(t *testing.T) (provider.ImageContent, string) {
	t.Helper()
	code := v41Nonce(t)
	var pick [1]byte
	if _, err := rand.Read(pick[:]); err != nil {
		t.Fatal("fixture_random_failed")
	}
	tallest := int(pick[0]) % 3
	canvas := image.NewRGBA(image.Rect(0, 0, 640, 400))
	draw.Draw(canvas, canvas.Bounds(), image.White, image.Point{}, draw.Src)
	label := func(x, y int, value string) {
		// Enlarge a standard bitmap font without OS fonts or additional files.
		mask := image.NewRGBA(image.Rect(0, 0, len(value)*7, 16))
		drawer := font.Drawer{Dst: mask, Src: image.Black, Face: basicfont.Face7x13, Dot: fixed.P(0, 13)}
		drawer.DrawString(value)
		for sy := 0; sy < 16; sy++ {
			for sx := 0; sx < mask.Bounds().Dx(); sx++ {
				if mask.RGBAAt(sx, sy).A > 0 {
					draw.Draw(canvas, image.Rect(x+sx*3, y+sy*3, x+sx*3+3, y+sy*3+3), image.Black, image.Point{}, draw.Src)
				}
			}
		}
	}
	label(28, 18, "CODE "+code)
	colors := []color.RGBA{{R: 200, G: 45, B: 55, A: 255}, {R: 30, G: 145, B: 80, A: 255}, {R: 45, G: 95, B: 210, A: 255}}
	for i, name := range []string{"A", "B", "C"} {
		height := 90 + i*20
		if i == tallest {
			height = 240
		}
		x := 100 + i*175
		draw.Draw(canvas, image.Rect(x, 340-height, x+90, 340), image.NewUniform(colors[i]), image.Point{}, draw.Src)
		label(x+30, 347, name)
	}
	var out bytes.Buffer
	if png.Encode(&out, canvas) != nil {
		t.Fatal("synthetic_png_failed")
	}
	return provider.ImageContent{Path: "synthetic.png", Name: "synthetic.png", MediaType: "image/png", Size: int64(out.Len()), Data: base64.StdEncoding.EncodeToString(out.Bytes())}, code + "|" + string(rune('A'+tallest))
}

// The fixture produces real SSE consumed by provider.New/openai and Agent.Run.
// It is explicitly not evidence of a real model's vision/reasoning capability.
func TestDeepSeekV41Fixture(t *testing.T) {
	var h *v41Harness
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var wire v41Wire
		if json.NewDecoder(r.Body).Decode(&wire) != nil {
			http.Error(w, "fixture_decode", 400)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("offline_auth_present")
		}
		answer, call := v41Unicode, map[string]any(nil)
		reasoning := "fixture-private-reasoning"
		if wire.Model == "deepseek-v4-pro" {
			answer = "ORCA_PRO_OK"
		}
		if len(wire.Tools) > 0 {
			var results []string
			for _, m := range wire.Messages {
				if m.Role == "tool" {
					var s string
					_ = json.Unmarshal(m.Content, &s)
					results = append(results, s)
				}
			}
			switch len(results) {
			case 0:
				call = map[string]any{"index": 0, "id": "fixture-issue", "type": "function", "function": map[string]any{"name": "fixture_receipt", "arguments": `{"action":"issue"}`}}
			case 1:
				var receipt struct{ Token string }
				_ = json.Unmarshal([]byte(results[0]), &receipt)
				args, _ := json.Marshal(map[string]string{"action": "verify", "token": receipt.Token})
				call = map[string]any{"index": 0, "id": "fixture-verify", "type": "function", "function": map[string]any{"name": "fixture_receipt", "arguments": string(args)}}
			default:
				answer = "VERIFIED"
				if len(wire.Messages) >= 8 {
					answer = "42"
				}
			}
		}
		for _, m := range wire.Messages {
			if bytes.Contains(m.Content, []byte(`"image_url"`)) {
				answer = h.answer
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(value any) {
			data, _ := json.Marshal(value)
			// Byte writes split multi-byte code points below the SSE parser.
			for _, b := range append(append([]byte("data: "), data...), '\n', '\n') {
				_, _ = w.Write([]byte{b})
				w.(http.Flusher).Flush()
			}
		}
		delta := func(d any, finish any) {
			emit(map[string]any{"choices": []any{map[string]any{"delta": d, "finish_reason": finish}}})
		}
		delta(map[string]any{"reasoning_content": reasoning}, nil)
		finish := "stop"
		if call != nil {
			delta(map[string]any{"tool_calls": []any{call}}, nil)
			finish = "tool_calls"
		} else {
			for _, r := range answer {
				delta(map[string]any{"content": string(r)}, nil)
			}
		}
		delta(map[string]any{}, finish)
		emit(map[string]any{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 40, "total_tokens": 140, "prompt_cache_hit_tokens": 10, "completion_tokens_details": map[string]any{"reasoning_tokens": 20}}})
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer fixture.Close()
	h = v41NewHarness(t, "offline_fixture", fixture.URL, "", 4096)
	defer h.finish(t, os.Getenv("ORCA_V41_LIVE_REPORT"))
	h.suite(t)
	h.bridge.Close()
	report := h.snapshot(!t.Failed())
	if report.Requests != 9 || len(report.HTTP) != 9 || len(report.Streams) != 9 || len(report.Checks) != len(v41Cases) {
		t.Error("fixture_coverage_incomplete")
	}
	encoded, _ := json.Marshal(report)
	for _, secret := range []string{"fixture-private-reasoning", h.answer, v41Display, "loopback-fixture-only", h.image.Data} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Error("report_contains_payload")
		}
	}
}

func TestDeepSeekV41FixtureBudgets(t *testing.T) {
	t.Run("requests", func(t *testing.T) {
		var b v41Ledger
		for i := 1; i <= 40; i++ {
			if n, class := b.reserve(1); n != i || class != "" {
				t.Fatal("early_request_stop")
			}
		}
		if n, class := b.reserve(1); n != 0 || class != "request_budget" || b.requests != 40 {
			t.Fatal("request_cap_bypassed")
		}
	})
	t.Run("cost", func(t *testing.T) {
		var b v41Ledger
		if _, class := b.reserve(v41BudgetMicro); class != "" {
			t.Fatal("exact_budget_rejected")
		}
		if n, class := b.reserve(1); n != 0 || class != "cost_budget" || b.reserved != v41BudgetMicro {
			t.Fatal("cost_cap_bypassed")
		}
	})
	t.Run("concurrent_reservations", func(t *testing.T) {
		var b v41Ledger
		var wg sync.WaitGroup
		for i := 0; i < 80; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); b.reserve(200_000) }()
		}
		wg.Wait()
		if b.requests != 25 || b.reserved != v41BudgetMicro || b.stopped != "cost_budget" {
			t.Fatal("atomic_budget_failed")
		}
	})
	for _, maxTokens := range []int{4096, 8192} {
		var b v41Ledger
		for i := 0; i < 8; i++ {
			b.reserve(v41Reservation("deepseek-flash", maxTokens))
		}
		if _, class := b.reserve(v41Reservation("deepseek-v4-pro", maxTokens)); class != "" {
			t.Fatal("planned_matrix_exceeds_budget")
		}
	}
}

func TestDeepSeekV41FixtureFailures(t *testing.T) {
	for _, mode := range []string{"503", "redirect", "timeout", "stream_timeout", "truncated", "missing_usage", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			release := make(chan struct{})
			releaseHandler := sync.OnceFunc(func() { close(release) })
			var active, received atomic.Int32
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received.Add(1)
				active.Add(1)
				defer active.Add(-1)
				_, _ = io.Copy(io.Discard, r.Body)
				switch mode {
				case "503":
					http.Error(w, "private-error-payload", 503)
				case "redirect":
					w.Header().Set("Location", "https://must-not-follow.invalid")
					w.WriteHeader(307)
				case "timeout", "stream_timeout":
					if mode == "stream_timeout" {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
						w.(http.Flusher).Flush()
					}
					// Deliberately ignore request cancellation: cleanup must release
					// this handler before Server.Close waits for active connections.
					<-release
				case "truncated":
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
				case "missing_usage":
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n\n")
				case "malformed":
					fmt.Fprint(w, "data: private-error-payload\n\ndata: [DONE]\n\n")
				}
			}))
			defer func() {
				releaseHandler()
				fixture.Close()
				if active.Load() != 0 {
					t.Error("fixture_rpc_still_active_after_cleanup")
				}
			}()
			h := v41NewHarness(t, "offline_fixture", fixture.URL, "", 4096)
			if mode == "timeout" || mode == "stream_timeout" {
				h.timeout = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			p := h.provider(t, v41Cases[0])
			_, err := v41Collect(ctx, p, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}})
			if err == nil {
				t.Error("failure_not_reported")
			}
			if _, err := v41Collect(ctx, p, provider.Request{}); err == nil {
				t.Error("request_after_failure_was_accepted")
			}
			releaseHandler()
			h.bridge.Close()
			report := h.snapshot(false)
			if received.Load() != 1 || report.Requests != 1 || report.StopClass == "" {
				t.Error("failure_did_not_stop_upstream")
			}
			if len(report.Streams) != 2 {
				t.Error("provider_stream_did_not_finish")
			}
			encoded, _ := json.Marshal(report)
			if bytes.Contains(encoded, []byte("private-error-payload")) {
				t.Error("error_body_leaked")
			}
		})
	}
}
