//go:build windows && production && browserlive

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/netclient"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	openai "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/openai"
)

var errBrowserLiveBudget = errors.New("browser acceptance request/cost budget exhausted")

type browserLiveMetrics struct {
	ProviderCalls    int     `json:"provider_calls"`
	HTTPAttempts     int     `json:"http_attempts"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	UsageReceipts    int     `json:"usage_receipts"`
	MissingUsage     int     `json:"missing_usage"`
	EstimatedCNY     float64 `json:"estimated_cny_upper_bound"`
	UsageCostCNY     float64 `json:"usage_cost_cny_estimate"`
}

func (m browserLiveMetrics) minus(b browserLiveMetrics) browserLiveMetrics {
	return browserLiveMetrics{ProviderCalls: m.ProviderCalls - b.ProviderCalls, HTTPAttempts: m.HTTPAttempts - b.HTTPAttempts,
		PromptTokens: m.PromptTokens - b.PromptTokens, CompletionTokens: m.CompletionTokens - b.CompletionTokens,
		ReasoningTokens: m.ReasoningTokens - b.ReasoningTokens, UsageReceipts: m.UsageReceipts - b.UsageReceipts,
		MissingUsage: m.MissingUsage - b.MissingUsage, EstimatedCNY: m.EstimatedCNY - b.EstimatedCNY, UsageCostCNY: m.UsageCostCNY - b.UsageCostCNY}
}

type browserLiveBudget struct {
	mu       sync.Mutex
	metrics  browserLiveMetrics
	limit    int
	cnyLimit float64
	blocked  bool
}

func newBrowserLiveBudget(requestLimit int, cnyLimit float64) *browserLiveBudget {
	return &browserLiveBudget{limit: requestLimit, cnyLimit: cnyLimit}
}
func (b *browserLiveBudget) snapshot() browserLiveMetrics {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.metrics
}
func (b *browserLiveBudget) exhausted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.blocked || b.metrics.HTTPAttempts >= b.limit || b.metrics.EstimatedCNY >= b.cnyLimit
}
func (b *browserLiveBudget) reserveAttempt(cost float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.blocked || b.metrics.HTTPAttempts >= b.limit || !(cost >= 0) || b.metrics.EstimatedCNY+cost > b.cnyLimit {
		b.blocked = true
		return errBrowserLiveBudget
	}
	b.metrics.HTTPAttempts++
	b.metrics.EstimatedCNY += cost
	return nil
}
func (b *browserLiveBudget) finish(usage *provider.Usage, deepseek bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if usage == nil {
		b.metrics.MissingUsage++
		return
	}
	b.metrics.UsageReceipts++
	b.metrics.PromptTokens += int64(max(usage.PromptTokens, 0))
	b.metrics.CompletionTokens += int64(max(usage.CompletionTokens, 0))
	// Reasoning is a subset of completion, never added again to cost.
	b.metrics.ReasoningTokens += int64(max(usage.ReasoningTokens, 0))
	if deepseek {
		b.metrics.UsageCostCNY += (float64(max(usage.PromptTokens, 0))*9 + float64(max(usage.CompletionTokens, 0))*27) / 1e6
		if b.metrics.UsageCostCNY > b.cnyLimit {
			b.blocked = true
		}
	}
}

// Conservative reservation rates (not a billing receipt): CNY 9/M input and
// 27/M output, ignoring cache/discounts, cover the supported Flash/Pro prices
// in this checkout. Reserve each real HTTP attempt BEFORE forwarding, including
// adapter retries. Wire bytes + 4096 are an intentionally high prompt estimate;
// images remain in that byte bound. Never refund failed/unknown-usage attempts.
// The ceiling applies to this estimate, not a guarantee about upstream billing.
func browserLiveReservation(deepseek bool, wireBytes, maxOutput int) float64 {
	if !deepseek {
		return 0
	}
	return (float64(wireBytes+4096)*9 + float64(maxOutput)*27) / 1e6
}

type browserLiveProvider struct {
	inner    provider.Provider
	budget   *browserLiveBudget
	deepseek bool
	serial   chan struct{}
	trace    *browserLiveTrace
}

func (p *browserLiveProvider) Name() string { return p.inner.Name() }
func (p *browserLiveProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.trace.enter(browserLiveRequestPhase(req))
	select {
	case p.serial <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if p.budget.exhausted() {
		<-p.serial
		return nil, errBrowserLiveBudget
	}
	// Bound the main agent too. The production browser loop already disables
	// thinking on its classifiers/actions. This test never asks for long reasoning.
	if req.MaxTokens <= 0 || req.MaxTokens > 2048 {
		req.MaxTokens = 2048
	}
	req.DisableThinking = true
	p.budget.mu.Lock()
	p.budget.metrics.ProviderCalls++
	p.budget.mu.Unlock()
	streamCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	source, err := p.inner.Stream(streamCtx, req)
	if err != nil {
		p.trace.fail(err, false)
		cancel()
		<-p.serial
		p.budget.finish(nil, p.deepseek)
		return nil, err
	}
	out := make(chan provider.Chunk, 64)
	go func() {
		var usage *provider.Usage
		defer func() { cancel(); p.budget.finish(usage, p.deepseek); <-p.serial; close(out) }()
		for {
			select {
			case <-streamCtx.Done():
				p.trace.fail(streamCtx.Err(), false)
				return
			case chunk, ok := <-source:
				if !ok {
					return
				}
				if chunk.Type == provider.ChunkUsage && chunk.Usage != nil {
					// Streaming usage snapshots replace, not accumulate, within a request.
					copy := *chunk.Usage
					usage = &copy
				}
				if chunk.Type == provider.ChunkError {
					p.trace.fail(chunk.Err, false)
				}
				if chunk.Type == provider.ChunkToolCall && chunk.ToolCall != nil {
					p.trace.recordCanvasDecision(*chunk.ToolCall)
				}
				select {
				case out <- chunk:
				case <-streamCtx.Done():
					p.trace.fail(streamCtx.Err(), false)
					return
				}
				if chunk.Type == provider.ChunkDone {
					return
				}
			}
		}
	}()
	return out, nil
}

func newBrowserLiveProvider(ctx context.Context, model browserLiveModel, budget *browserLiveBudget, traces ...*browserLiveTrace) (provider.Provider, func(), error) {
	var trace *browserLiveTrace
	if len(traces) != 0 {
		trace = traces[0]
	}
	base, err := openai.NormalizeBaseURL(model.endpoint)
	if err != nil {
		return nil, nil, errors.New("invalid explicit model endpoint")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Neither the synthetic local relay nor LAN model uses the user's proxy setup.
	// Official HTTPS traffic may use explicit process proxy env, never config files.
	if !model.deepseek {
		transport.Proxy = nil
	}
	client := &http.Client{Transport: transport, Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	secret := browserID()
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deny := func(code int) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = io.WriteString(w, `{"error":{"message":"isolated acceptance relay rejected request","type":"acceptance_guard"}}`)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer "+secret {
			deny(403)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2*1024*1024))
		if err != nil {
			deny(413)
			return
		}
		var envelope struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
		}
		if json.Unmarshal(body, &envelope) != nil || envelope.Model != model.model || envelope.MaxTokens < 1 || envelope.MaxTokens > 2048 {
			deny(400)
			return
		}
		if err := budget.reserveAttempt(browserLiveReservation(model.deepseek, len(body), envelope.MaxTokens)); err != nil {
			deny(403)
			return
		}
		callCtx, cancel := context.WithCancel(r.Context())
		stop := context.AfterFunc(ctx, cancel)
		defer func() { stop(); cancel() }()
		upstream, err := http.NewRequestWithContext(callCtx, http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			deny(400)
			return
		}
		upstream.Header.Set("Content-Type", "application/json")
		upstream.Header.Set("Accept", "text/event-stream")
		if model.key != "" {
			upstream.Header.Set("Authorization", "Bearer "+model.key)
		}
		response, err := client.Do(upstream)
		if err != nil {
			deny(502)
			return
		}
		defer response.Body.Close()
		// No redirect forwarding, cookies or upstream diagnostic headers.
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			deny(403)
			return
		}
		w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(browserLiveFlushWriter{w}, response.Body)
	}))
	extra := map[string]any{"proxy_spec": netclient.ProxySpec{Mode: netclient.ModeOff}}
	if model.deepseek {
		extra["reasoning_protocol"] = "deepseek"
	}
	inner, err := openai.New(provider.Config{Name: model.label, BaseURL: relay.URL + "/v1", Model: model.model, APIKey: secret, Extra: extra})
	closeAll := func() { relay.CloseClientConnections(); relay.Close(); transport.CloseIdleConnections() }
	if err != nil {
		closeAll()
		return nil, nil, errors.New("could not construct isolated provider")
	}
	return &browserLiveProvider{inner: inner, budget: budget, deepseek: model.deepseek, serial: make(chan struct{}, 1), trace: trace}, closeAll, nil
}

func browserLiveRequestPhase(req provider.Request) string {
	for _, schema := range req.Tools {
		switch schema.Name {
		case "browser_task":
			return "main_agent_request"
		case "browser_capability_check":
			return "tool_capability_probe"
		case "report_image":
			return "image_capability_probe"
		case "browser_action", "browser_complete":
			return "browser_action_decision"
		}
	}
	return "completion_verification"
}

type browserLiveFlushWriter struct{ http.ResponseWriter }

func (w browserLiveFlushWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
	return n, err
}

// Offline guard checks: no Wails, credentials, external HTTP, or model inference.
func TestBrowserLiveBudgetGuardsOffline(t *testing.T) {
	b := newBrowserLiveBudget(80, 5)
	for i := 0; i < 80; i++ {
		if b.reserveAttempt(0) != nil {
			t.Fatal("early request ceiling")
		}
	}
	if b.reserveAttempt(0) == nil || b.snapshot().HTTPAttempts != 80 {
		t.Fatal("request ceiling bypassed")
	}
	cost := newBrowserLiveBudget(80, 5)
	if cost.reserveAttempt(4.99) != nil || cost.reserveAttempt(0.02) == nil {
		t.Fatal("cost reservation not enforced")
	}
	if cost.snapshot().HTTPAttempts != 1 || cost.snapshot().EstimatedCNY > 5 {
		t.Fatal("denied request charged or forwarded")
	}
	u := newBrowserLiveBudget(80, 5)
	u.finish(&provider.Usage{PromptTokens: 100, CompletionTokens: 40, ReasoningTokens: 30}, true)
	if got := u.snapshot(); got.CompletionTokens != 40 || got.ReasoningTokens != 30 || got.UsageCostCNY != (100.0*9+40*27)/1e6 {
		t.Fatal("reasoning counted twice")
	}
}

func TestBrowserLiveRelayRetriesOffline(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer synthetic-only" {
			t.Error("relay did not use the supplied synthetic credential")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		_, _ = io.WriteString(w, `{"error":{"message":"synthetic unavailable"}}`)
	}))
	defer upstream.Close()
	b := newBrowserLiveBudget(1, 5)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, cleanup, err := newBrowserLiveProvider(ctx, browserLiveModel{label: "qwen", endpoint: upstream.URL + "/v1", model: "synthetic", key: "synthetic-only"}, b)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	_, err = p.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}})
	if err == nil {
		t.Fatal("expected guarded synthetic failure")
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 || b.snapshot().HTTPAttempts != 1 {
		t.Fatal("adapter retries escaped HTTP budget")
	}
}

func TestBrowserLiveConfigDefaultsOffline(t *testing.T) {
	for _, entry := range []string{"MODELS", "CASES", "REPEAT", "REQUEST_LIMIT", "CNY_LIMIT", "REPORT", "DEEPSEEK_ENDPOINT", "DEEPSEEK_MODEL", "DEEPSEEK_API_KEY", "BASE_URL", "MODEL", "API_KEY"} {
		t.Setenv("ORCA_BROWSER_LIVE_"+entry, "")
	}
	t.Setenv("ORCA_BROWSER_LIVE_QWEN_ENDPOINT", "http://127.0.0.1:12345/v1")
	t.Setenv("ORCA_BROWSER_LIVE_QWEN_MODEL", "synthetic-model")
	t.Setenv("ORCA_BROWSER_LIVE_QWEN_API_KEY", "")
	c, err := browserLiveConfigFromEnv()
	if err != nil || len(c.cases) != 1 || c.cases[0] != "click" || c.repeat != 1 {
		t.Fatal("default must be one click and one repeat")
	}
	t.Setenv("ORCA_BROWSER_LIVE_REQUEST_LIMIT", "81")
	if _, err := browserLiveConfigFromEnv(); err == nil {
		t.Fatal("request ceiling increased through env")
	}
	t.Setenv("ORCA_BROWSER_LIVE_REQUEST_LIMIT", "80")
	t.Setenv("ORCA_BROWSER_LIVE_CNY_LIMIT", "NaN")
	if _, err := browserLiveConfigFromEnv(); err == nil {
		t.Fatal("NaN budget accepted")
	}
	if strings.Contains(c.models[0].model, "secret") {
		t.Fatal("unexpected model identity")
	}
}

func TestBrowserLiveFailureClassificationOffline(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want string
	}{
		{context.Canceled, "cancelled"},
		{context.DeadlineExceeded, "deadline"},
		{errBrowserStopped, "parent_turn_stopped"},
		{&browserResponseError{Kind: "output_limit"}, "output_limit"},
		{&browserResponseError{Kind: "content_filter"}, "content_filter"},
		{&browserResponseError{Kind: "repetition_truncation"}, "repetition_truncation"},
		{&browserResponseError{Kind: "abnormal_finish"}, "abnormal_finish"},
		{&browserResponseError{Kind: "sensitive synthetic marker"}, "browser_response_unknown_kind"},
		{&browserVerificationError{Kind: "negative_evidence"}, "negative_evidence"},
		{&browserVerificationError{Kind: "invalid_json"}, "invalid_json"},
		{&browserVerificationError{Kind: "missing_fields"}, "missing_fields"},
		{&browserVerificationError{Kind: "sensitive synthetic marker"}, "browser_verification_unknown_kind"},
		{browserhost.ErrStaleObservation, "stale_observation"},
		{errors.New("browser input outcome uncertain after dispatch; synthetic marker"), "native_input_outcome_uncertain"},
		{errors.New("browser is under manual control"), "manual_takeover"},
		{errors.New("browser operation failed: 0x80004005"), "native_cdp_failed"},
		{errors.New("browser action outside viewport"), "coordinates_outside_viewport"},
		{errors.New("browser provider did not pass structured-tool capability probe"), "tool_probe_rejected"},
		{errors.New("browser agent requires exactly one identified tool call"), "tool_call_count_or_id_invalid"},
		{errors.New("browser completion is not confirmed by fresh evidence"), "completion_evidence_rejected"},
		{errors.New("select requires an enabled option value from the current observation"), "select_option_invalid"},
		{&provider.APIError{Status: 402, Body: "sensitive synthetic marker"}, "http_402"},
		{errors.New("arbitrary sensitive synthetic marker"), "unclassified_error"},
	} {
		if got := browserLiveFailureCode(errors.Join(tt.err, errBrowserStopped)); got != tt.want {
			t.Fatalf("safe classification = %s, want %s", got, tt.want)
		}
	}
	trace := &browserLiveTrace{}
	trace.enter("native_action_click")
	trace.fail(browserhost.ErrStaleObservation, true)
	trace.enter("main_agent_request")
	trace.fail(&provider.APIError{Status: 400, Body: "sensitive synthetic marker"}, false)
	var r browserLiveCaseReport
	trace.copyTo(&r)
	if r.ErrorPhase != "native_action_click" || r.ErrorKind != "stale_observation" || r.NativeErrorKind != "stale_observation" {
		t.Fatal("late parent error replaced first native failure")
	}
	body, _ := json.Marshal(r)
	if strings.Contains(string(body), "sensitive") {
		t.Fatal("raw error leaked into report")
	}
}

func TestBrowserLiveActionCountsOffline(t *testing.T) {
	trace := &browserLiveTrace{}
	trace.recordAction("click", nil)
	trace.recordAction("click", context.Canceled)
	trace.recordAction("type", nil)
	trace.recordAction("never-report-kind", errors.New("never-report-error"))
	var r browserLiveCaseReport
	trace.copyTo(&r)
	if r.NativeActionKinds["click"] != (browserLiveActionCount{Attempts: 2, Completed: 1, Failed: 1}) || r.NativeActionKinds["type"].Completed != 1 || r.NativeActionKinds["unsupported"].Failed != 1 {
		t.Fatal("native action counters do not distinguish attempts and completion")
	}
	trace.recordAction("click", nil)
	if r.NativeActionKinds["click"].Attempts != 2 {
		t.Fatal("report counters changed after snapshot")
	}
	body, _ := json.Marshal(r)
	if strings.Contains(string(body), "never-report") {
		t.Fatal("raw action kind or error leaked into report")
	}
}

func TestBrowserLiveRelayUsageOffline(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			MaxTokens int `json:"max_tokens"`
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.MaxTokens != 2048 {
			t.Error("main model output was not capped")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"synthetic\"},\"finish_reason\":null}]}\n\n"+
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":40,\"total_tokens\":140,\"completion_tokens_details\":{\"reasoning_tokens\":30}}}\n\n"+
			"data: [DONE]\n\n")
	}))
	defer upstream.Close()
	budget := newBrowserLiveBudget(2, 5)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, cleanup, err := newBrowserLiveProvider(ctx, browserLiveModel{label: "synthetic", endpoint: upstream.URL + "/v1", model: "synthetic", deepseek: true}, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	stream, err := p.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}, MaxTokens: 5000})
	if err != nil {
		t.Fatal("synthetic relay failed")
	}
	for chunk := range stream {
		if chunk.Type == provider.ChunkError {
			t.Fatal("synthetic stream failed")
		}
	}
	m := budget.snapshot()
	if m.HTTPAttempts != 1 || m.ProviderCalls != 1 || m.PromptTokens != 100 || m.CompletionTokens != 40 || m.ReasoningTokens != 30 || m.UsageReceipts != 1 || m.MissingUsage != 0 || m.EstimatedCNY <= 0 || m.EstimatedCNY > 5 {
		t.Fatalf("usage accounting failed: %+v", m)
	}
}
