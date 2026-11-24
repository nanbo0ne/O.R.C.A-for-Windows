//go:build windows && production && browserlive

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	winopt "github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Explicitly opt in; ordinary go test neither starts Wails nor contacts a model.
// Minimal run (caller supplies endpoint/model/key in its environment):
//
//	ORCA_BROWSER_LIVE_TEST=1
//	ORCA_BROWSER_LIVE_MODELS=qwen                 (or deepseek, or both)
//	ORCA_BROWSER_LIVE_QWEN_ENDPOINT=http://host:port/v1
//	ORCA_BROWSER_LIVE_QWEN_MODEL=<exact API id>
//	ORCA_BROWSER_LIVE_QWEN_API_KEY=<optional>
//
// Single-model aliases: ORCA_BROWSER_LIVE_BASE_URL, ORCA_BROWSER_LIVE_MODEL,
// ORCA_BROWSER_LIVE_API_KEY. DeepSeek also accepts DEEPSEEK_API_KEY.
//
//	ORCA_BROWSER_LIVE_DEEPSEEK_ENDPOINT=https://api.deepseek.com/v1
//	ORCA_BROWSER_LIVE_DEEPSEEK_MODEL=deepseek-flash
//	ORCA_BROWSER_LIVE_DEEPSEEK_API_KEY=<required>
//	go test . -tags production,browserlive -run '^TestBrowserLiveAcceptance$' -count=1 -timeout=10m -v
//
// Optional: CASES=click,form,scroll,canvas; REPEAT=1..3; REPORT=<absolute JSON path>.
// All optional names use the ORCA_BROWSER_LIVE_ prefix. Run both models in ONE
// invocation to share the 80-HTTP-attempt / estimated CNY 5 batch ceiling. For a
// later invocation, REQUEST_LIMIT and CNY_LIMIT must be the remaining budget.
// Limits can only lower the ceilings. The parent coordinator alone opts in.
// This is a hidden, real Wails/WebView2 fixture, NOT production UI acceptance.
func TestBrowserLiveAcceptance(t *testing.T) {
	if os.Getenv("ORCA_BROWSER_LIVE_TEST") != "1" {
		t.Skip("opt-in real-model browser acceptance; no model requests made")
	}
	cfg, err := browserLiveConfigFromEnv()
	if err != nil {
		t.Fatal(err) // Configuration errors contain env names, never values.
	}
	if os.Getenv("ORCA_BROWSER_LIVE_CHILD") == "1" {
		root := os.Getenv("ORCA_BROWSER_LIVE_ROOT")
		if !filepath.IsAbs(root) {
			t.Fatal("isolated fixture root must be absolute")
		}
		report := runBrowserLiveWindow(cfg, root)
		body, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil || os.WriteFile(filepath.Join(root, "result.json"), body, 0o600) != nil {
			t.Fatal("could not write sanitized fixture report")
		}
		return
	}
	root := browserLiveFixtureRoot(t)
	timeout := time.Duration(len(cfg.models)*len(cfg.cases)*cfg.repeat)*browserMaxDuration + time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBrowserLiveAcceptance$", "-test.count=1", "-test.timeout="+timeout.String())
	cmd.Env = browserLiveChildEnvironment(root)
	// Wails/provider diagnostics must not accidentally print HTTP bodies or keys.
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Run(); err != nil {
		t.Fatal("isolated Wails live fixture did not exit cleanly; raw output suppressed")
	}
	body, err := os.ReadFile(filepath.Join(root, "result.json"))
	if err != nil {
		t.Fatal("isolated live fixture did not produce a report")
	}
	var report browserLiveReport
	if json.Unmarshal(body, &report) != nil {
		t.Fatal("invalid live fixture report")
	}
	if cfg.report != "" {
		if err := os.WriteFile(cfg.report, body, 0o600); err != nil {
			t.Fatal("could not save sanitized live report")
		}
	}
	for _, row := range report.Cases {
		// Only identifiers, classifications, counters and timing; never raw errors.
		t.Logf("model=%s case=%s repeat=%d status=%s elapsed_ms=%d requests=%d prompt=%d completion=%d reasoning=%d estimated_cny=%.6f native_actions=%d verified_events=%d",
			row.Model, row.Case, row.Repeat, row.Status, row.ElapsedMS, row.HTTPAttempts,
			row.PromptTokens, row.CompletionTokens, row.ReasoningTokens, row.EstimatedCNY, row.NativeActions, row.VerifiedEvents)
		if row.ErrorKind != "" {
			t.Logf("phase=%s errorKind=%s nativeErrorKind=%s", row.ErrorPhase, row.ErrorKind, row.NativeErrorKind)
		}
		if row.TaskErrorKind != "" {
			t.Logf("taskErrorKind=%s", row.TaskErrorKind)
		}
		if len(row.NativeActionKinds) > 0 {
			counts, _ := json.Marshal(row.NativeActionKinds)
			t.Logf("native_action_kinds=%s", counts)
		}
	}
	t.Logf("batch requests=%d estimated_cny=%.6f usage_cost_cny=%.6f", report.Budget.HTTPAttempts, report.Budget.EstimatedCNY, report.Budget.UsageCostCNY)
	if !report.Passed {
		t.Fatalf("live acceptance failed: %s; no model text was logged", report.Status)
	}
}

type browserLiveConfig struct {
	models               []browserLiveModel
	cases                []string
	repeat, requestLimit int
	cnyLimit             float64
	report               string
}

type browserLiveModel struct {
	label, endpoint, model, key string
	deepseek                    bool
}

func browserLiveConfigFromEnv() (browserLiveConfig, error) {
	c := browserLiveConfig{repeat: 1, requestLimit: 80, cnyLimit: 5}
	labels := os.Getenv("ORCA_BROWSER_LIVE_MODELS")
	if labels == "" {
		labels = "qwen"
	}
	seen := map[string]bool{}
	for _, label := range strings.Split(labels, ",") {
		label = strings.TrimSpace(label)
		if (label != "qwen" && label != "deepseek") || seen[label] {
			return c, errors.New("invalid ORCA_BROWSER_LIVE_MODELS")
		}
		seen[label] = true
		prefix := "ORCA_BROWSER_LIVE_" + strings.ToUpper(label)
		m := browserLiveModel{label: label, endpoint: strings.TrimSpace(os.Getenv(prefix + "_ENDPOINT")), model: strings.TrimSpace(os.Getenv(prefix + "_MODEL")), key: os.Getenv(prefix + "_API_KEY"), deepseek: label == "deepseek"}
		if !strings.Contains(labels, ",") {
			if m.endpoint == "" {
				m.endpoint = strings.TrimSpace(os.Getenv("ORCA_BROWSER_LIVE_BASE_URL"))
			}
			if m.model == "" {
				m.model = strings.TrimSpace(os.Getenv("ORCA_BROWSER_LIVE_MODEL"))
			}
			if m.key == "" {
				m.key = os.Getenv("ORCA_BROWSER_LIVE_API_KEY")
			}
		}
		if m.deepseek && m.key == "" {
			m.key = os.Getenv("DEEPSEEK_API_KEY")
		}
		u, err := url.Parse(m.endpoint)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return c, fmt.Errorf("invalid or missing %s_ENDPOINT", prefix)
		}
		if m.deepseek && (u.Scheme != "https" || u.Host != "api.deepseek.com" || (u.Path != "" && u.Path != "/" && u.Path != "/v1" && u.Path != "/v1/")) {
			return c, errors.New("DeepSeek acceptance requires its exact official HTTPS endpoint")
		}
		if !m.deepseek && strings.EqualFold(u.Hostname(), "api.deepseek.com") {
			return c, errors.New("official DeepSeek endpoint must use the deepseek budget category")
		}
		if m.model == "" || len(m.model) > 160 || strings.ContainsAny(m.model, "\r\n\t") {
			return c, fmt.Errorf("invalid or missing %s_MODEL", prefix)
		}
		if m.deepseek && m.key == "" {
			return c, fmt.Errorf("missing %s_API_KEY", prefix)
		}
		c.models = append(c.models, m)
	}
	cases := os.Getenv("ORCA_BROWSER_LIVE_CASES")
	if cases == "" {
		cases = "click"
	}
	seen = map[string]bool{}
	for _, name := range strings.Split(cases, ",") {
		name = strings.TrimSpace(name)
		if (name != "click" && name != "form" && name != "scroll" && name != "canvas") || seen[name] {
			return c, errors.New("invalid ORCA_BROWSER_LIVE_CASES")
		}
		seen[name] = true
		c.cases = append(c.cases, name)
	}
	for _, opt := range []struct {
		name    string
		into    *int
		ceiling int
	}{{"REPEAT", &c.repeat, 3}, {"REQUEST_LIMIT", &c.requestLimit, 80}} {
		if raw := os.Getenv("ORCA_BROWSER_LIVE_" + opt.name); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < 1 || v > opt.ceiling {
				return c, fmt.Errorf("invalid ORCA_BROWSER_LIVE_%s", opt.name)
			}
			*opt.into = v
		}
	}
	if raw := os.Getenv("ORCA_BROWSER_LIVE_CNY_LIMIT"); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || !(v > 0 && v <= 5) {
			return c, errors.New("invalid ORCA_BROWSER_LIVE_CNY_LIMIT")
		}
		c.cnyLimit = v
	}
	c.report = os.Getenv("ORCA_BROWSER_LIVE_REPORT")
	if c.report != "" && !filepath.IsAbs(c.report) {
		return c, errors.New("ORCA_BROWSER_LIVE_REPORT must be absolute")
	}
	return c, nil
}

func browserLiveChildEnvironment(root string) []string {
	changes := map[string]string{
		"ORCA_BROWSER_LIVE_CHILD": "1", "ORCA_BROWSER_LIVE_ROOT": root,
		"APPDATA": filepath.Join(root, "roaming"), "LOCALAPPDATA": filepath.Join(root, "local"),
		"HOME": root, "USERPROFILE": root, "XDG_CONFIG_HOME": filepath.Join(root, "config"),
	}
	out := make([]string, 0, len(os.Environ())+len(changes))
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if _, changed := changes[strings.ToUpper(key)]; !changed {
			out = append(out, v)
		}
	}
	for key, value := range changes {
		out = append(out, key+"="+value)
	}
	return out
}

type browserLiveReport struct {
	Passed bool                    `json:"passed"`
	Status string                  `json:"status"`
	Budget browserLiveMetrics      `json:"budget"`
	Cases  []browserLiveCaseReport `json:"cases"`
}
type browserLiveCaseReport struct {
	Model                string                            `json:"model"`
	Case                 string                            `json:"case"`
	Repeat               int                               `json:"repeat"`
	Status               string                            `json:"status"`
	ElapsedMS            int64                             `json:"elapsed_ms"`
	NativeActions        int64                             `json:"native_actions"`
	NativeActionAttempts int64                             `json:"native_action_attempts"`
	NativeObservations   int64                             `json:"native_observations"`
	VerifiedEvents       int                               `json:"verified_events"`
	Delegations          int64                             `json:"delegations"`
	LineageVerified      bool                              `json:"lineage_verified"`
	ErrorPhase           string                            `json:"errorPhase,omitempty"`
	ErrorKind            string                            `json:"errorKind,omitempty"`
	NativeErrorKind      string                            `json:"nativeErrorKind,omitempty"`
	TaskErrorKind        string                            `json:"taskErrorKind,omitempty"`
	NativeActionKinds    map[string]browserLiveActionCount `json:"native_action_kinds,omitempty"`
	CanvasDiagnostics    *browserLiveCanvasReport          `json:"synthetic_canvas,omitempty"`
	browserLiveMetrics
}

type browserLiveActionCount struct {
	Attempts  int64 `json:"attempts"`
	Completed int64 `json:"completed"`
	Failed    int64 `json:"failed"`
}

type browserLiveTrace struct {
	mu                                            sync.Mutex
	phase, errorPhase, errorKind, nativeErrorKind string
	actions                                       map[string]browserLiveActionCount
	canvas                                        *browserLiveCanvasTrace
}

func (t *browserLiveTrace) enter(phase string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.phase = phase
}
func (t *browserLiveTrace) fail(err error, native bool) {
	if t == nil || err == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	kind := browserLiveFailureCode(err)
	if t.errorKind == "" {
		t.errorPhase, t.errorKind = t.phase, kind
	}
	if native && t.nativeErrorKind == "" {
		t.nativeErrorKind = kind
	}
}
func (t *browserLiveTrace) copyTo(r *browserLiveCaseReport) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r.ErrorPhase, r.ErrorKind, r.NativeErrorKind = t.errorPhase, t.errorKind, t.nativeErrorKind
	if len(t.actions) > 0 {
		r.NativeActionKinds = make(map[string]browserLiveActionCount, len(t.actions))
		for kind, count := range t.actions {
			r.NativeActionKinds[kind] = count
		}
	}
	if t.canvas != nil {
		r.CanvasDiagnostics = t.canvas.snapshot()
	}
}

func browserLiveActionKind(kind string) string {
	switch kind {
	case "click", "type", "select", "scroll", "key", "navigate", "wait":
		return kind
	default:
		return "unsupported"
	}
}

func (t *browserLiveTrace) recordAction(kind string, err error) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.actions == nil {
		t.actions = make(map[string]browserLiveActionCount)
	}
	kind = browserLiveActionKind(kind)
	count := t.actions[kind]
	count.Attempts++
	if err == nil {
		count.Completed++
	} else {
		count.Failed++
	}
	t.actions[kind] = count
}

func runBrowserLiveWindow(cfg browserLiveConfig, root string) browserLiveReport {
	report := browserLiveReport{Status: "wails_start_failed"}
	budget := newBrowserLiveBudget(cfg.requestLimit, cfg.cnyLimit)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(len(cfg.models)*len(cfg.cases)*cfg.repeat)*browserMaxDuration+30*time.Second)
	defer cancel()
	results := make(chan browserLiveReport, 1)
	runDone := make(chan error, 1)
	lifecycle := make(chan context.Context, 1)
	var started atomic.Bool
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		runDone <- wails.Run(&options.App{
			Title: "O.R.C.A. isolated model acceptance", Width: 980, Height: 720, StartHidden: true,
			AssetServer: &assetserver.Options{Assets: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte(`<!doctype html><html><head><meta charset="utf-8"><title>Isolated browser fixture</title></head><body>O.R.C.A. browser model acceptance</body></html>`)}}},
			Windows:     &winopt.Options{WindowClassName: browserhost.WindowClass, WebviewUserDataPath: filepath.Join(root, "wails-profile")},
			OnDomReady: func(wctx context.Context) {
				if !started.CompareAndSwap(false, true) {
					return
				}
				lifecycle <- wctx
				go func() {
					defer wruntime.Quit(wctx)
					results <- runBrowserLiveBatch(ctx, cfg, root, budget)
				}()
			},
		})
	}()
	startup := time.NewTimer(50 * time.Second)
	var appContext context.Context
	select {
	case appContext = <-lifecycle:
		startup.Stop()
	case <-runDone:
		startup.Stop()
		report.Budget = budget.snapshot()
		return report
	case <-startup.C:
		cancel()
		report.Status = "wails_startup_timeout"
		report.Budget = budget.snapshot()
		return report
	case <-ctx.Done():
		startup.Stop()
		report.Status = "fixture_timeout"
		report.Budget = budget.snapshot()
		return report
	}
	select {
	case report = <-results:
		select {
		case err := <-runDone:
			if err != nil {
				report.Passed, report.Status = false, "wails_exit_failed"
			}
		case <-time.After(5 * time.Second):
			report.Passed, report.Status = false, "wails_exit_timeout"
		}
	case runErr := <-runDone:
		// Quit can finish before the receiver selects the buffered case result.
		select {
		case report = <-results:
			if runErr != nil {
				report.Passed, report.Status = false, "wails_exit_failed"
			}
		default:
		}
	case <-ctx.Done():
		report.Status = "fixture_timeout"
		wruntime.Quit(appContext)
		select {
		case <-runDone:
		case <-time.After(5 * time.Second):
		}
	}
	report.Budget = budget.snapshot()
	return report
}

func runBrowserLiveBatch(ctx context.Context, cfg browserLiveConfig, root string, budget *browserLiveBudget) browserLiveReport {
	report := browserLiveReport{Passed: true, Status: "passed"}
	for _, model := range cfg.models {
		for _, name := range cfg.cases {
			for repeat := 1; repeat <= cfg.repeat; repeat++ {
				if ctx.Err() != nil || budget.exhausted() {
					report.Passed, report.Status = false, "budget_or_deadline_exhausted"
					return report
				}
				row := runBrowserLiveCase(ctx, root, model, name, repeat, budget)
				report.Cases = append(report.Cases, row)
				if row.Status != "passed" {
					// No retry/replay after uncertain actions. Coordinator chooses next run.
					report.Passed, report.Status = false, row.Status
					return report
				}
			}
		}
	}
	return report
}

type browserLiveTaskTool struct {
	service                    *browserService
	prov                       provider.Provider
	owner, model, address, tab string
	used                       atomic.Bool
	calls                      atomic.Int64
	lineage                    atomic.Bool
	succeeded                  atomic.Bool
	failure                    atomic.Pointer[string]
	trace                      *browserLiveTrace
}

func (t *browserLiveTaskTool) Name() string            { return "browser_task" }
func (t *browserLiveTaskTool) ReadOnly() bool          { return false }
func (t *browserLiveTaskTool) Schema() json.RawMessage { return (browserTool{op: "task"}).Schema() }
func (t *browserLiveTaskTool) Description() string     { return (browserTool{op: "task"}).Description() }
func (t *browserLiveTaskTool) Execute(ctx context.Context, args json.RawMessage) (result string, retErr error) {
	t.trace.enter("parent_delegation")
	defer func() { t.trace.fail(retErr, false) }()
	t.calls.Add(1)
	if !t.used.CompareAndSwap(false, true) {
		return "", errors.New("fixture forbids a second delegation after any result")
	}
	turn, lifetime := agent.ParentTurn(ctx)
	callID, _, _, ok := agent.CallContext(ctx)
	if turn == "" || lifetime == nil || lifetime.Err() != nil || !ok || callID == "" || agent.ParentSession(ctx) != t.owner {
		return "", errors.New("parent Agent lineage was not preserved")
	}
	t.lineage.Store(true)
	var input browserTaskInput
	if err := browserDecode(string(args), &input); err != nil {
		return "", err
	}
	if input.URL != t.address || (input.TabID != "" && input.TabID != t.tab) || strings.TrimSpace(input.Goal) == "" || strings.TrimSpace(input.Criteria) == "" {
		return "", errors.New("delegate only the supplied synthetic URL and owned tab with explicit goal and criteria")
	}
	// Do not recreate ParentTurn/CallContext, rewrite the goal, or execute input.
	result, err := t.service.run(ctx, t.owner, callID, input, t.prov, t.model)
	t.succeeded.Store(err == nil)
	if err != nil {
		code := browserLiveFailureCode(err)
		t.failure.Store(&code)
	}
	return result, err
}

func browserLiveFailureCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, browserhost.ErrStaleObservation) {
		return "stale_observation"
	}
	if errors.Is(err, browserhost.ErrNavigationDenied) {
		return "navigation_denied"
	}
	if errors.Is(err, errBrowserBusy) {
		return "profile_busy"
	}
	if errors.Is(err, errBrowserLiveBudget) {
		return "budget_exhausted"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "stream_incomplete"
	}
	var response *browserResponseError
	if errors.As(err, &response) {
		if response != nil {
			switch response.Kind {
			case "output_limit", "content_filter", "repetition_truncation", "abnormal_finish":
				return response.Kind
			}
		}
		return "browser_response_unknown_kind"
	}
	var verification *browserVerificationError
	if errors.As(err, &verification) {
		if verification != nil {
			switch verification.Kind {
			case "unexpected_tool", "empty_response", "verdict_too_large", "invalid_fence", "non_json_response", "invalid_json", "duplicate_field", "unknown_field", "trailing_content", "missing_fields", "reason_too_large", "negative_evidence":
				return verification.Kind
			}
		}
		return "browser_verification_unknown_kind"
	}
	var api *provider.APIError
	if errors.As(err, &api) {
		return "http_" + strconv.Itoa(api.Status)
	}
	var auth *provider.AuthError
	if errors.As(err, &auth) {
		return "auth_" + strconv.Itoa(auth.Status)
	}
	var syntax *json.SyntaxError
	var valueType *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &valueType) {
		return "invalid_response_json"
	}
	var network *url.Error
	if errors.As(err, &network) {
		return "request_transport_failed"
	}
	// Inspect errors only to select closed enums. Never emit raw error strings.
	message := err.Error()
	for _, match := range []struct{ text, kind string }{
		{"browser input outcome uncertain after dispatch", "native_input_outcome_uncertain"},
		{"structured-tool capability probe", "tool_probe_rejected"},
		{"tool probe returned invalid synthetic evidence", "tool_probe_evidence_mismatch"},
		{"exactly one identified tool call", "tool_call_count_or_id_invalid"},
		{"exactly one valid function call", "vision_probe_tool_invalid"},
		{"synthetic image evidence does not match", "vision_probe_evidence_mismatch"},
		{"completion is not confirmed by fresh evidence", "completion_evidence_rejected"},
		{"completion summary is empty", "empty_completion_summary"},
		{"requires user guidance", "model_escalated"},
		{"browser target is disabled", "disabled_target"},
		{"select requires an enabled option", "select_option_invalid"},
		{"text/select requires a current DOM element", "dom_target_required"},
		{"click requires a current DOM element", "click_target_or_vision_required"},
		{"screenshot coordinate mapping unavailable", "coordinate_mapping_unavailable"},
		{"CSS viewport dimensions unavailable", "viewport_dimensions_unavailable"},
		{"invalid browser coordinates", "coordinates_invalid"},
		{"browser action outside viewport", "coordinates_outside_viewport"},
		{"action exceeds bounds", "action_bounds_exceeded"},
		{"nonfinite action", "action_nonfinite"},
		{"protected input", "protected_input"},
		{"protected browser input", "protected_input"},
		{"under manual control", "manual_takeover"},
		{"focused-key input is unavailable", "focused_key_unsupported"},
		{"browser consent denied", "consent_denied"},
		{"browser host window not ready", "native_parent_not_ready"},
		{"cannot create browser child window", "native_child_create_failed"},
		{"WebView2 unavailable", "native_webview_unavailable"},
		{"WebView2 HRESULT", "native_hresult"},
		{"browser operation failed", "native_cdp_failed"},
		{"page observation failed", "native_observation_failed"},
		{"browser tab closed", "tab_closed"},
		{"browser host stopped", "host_stopped"},
		{"screenshot unavailable", "screenshot_unavailable"},
		{"invalid browser screenshot", "screenshot_invalid"},
		{"unconfirmed; no fallback", "vision_unconfirmed"},
		{"browser response too large", "response_limit_exceeded"},
		{"browser payload too large", "payload_limit_exceeded"},
		{"invalid browser tool-call stream", "tool_stream_invalid"},
		{"data after completion", "stream_after_done"},
		{"unsupported tool", "tool_unsupported"},
		{"unsupported browser action", "action_unsupported"},
		{"parent Agent lineage", "parent_lineage_missing"},
		{"second delegation", "duplicate_delegation"},
		{"delegate only the supplied", "fixture_scope_mismatch"},
		{"json:", "invalid_response_json"},
	} {
		if strings.Contains(message, match.text) {
			return match.kind
		}
	}
	// This sentinel is joined to all task failures, not just user cancellation.
	// Preserve any more specific reason above instead of calling them cancelled.
	if browserLiveOnlyStopped(err) {
		return "parent_turn_stopped"
	}
	return "unclassified_error"
}

func browserLiveOnlyStopped(err error) bool {
	if err == errBrowserStopped {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !browserLiveOnlyStopped(cause) {
				return false
			}
		}
		return true
	}
	if cause := errors.Unwrap(err); cause != nil {
		return browserLiveOnlyStopped(cause)
	}
	return false
}

type browserLiveNative struct {
	browserhost.Backend
	inputs       atomic.Int64
	attempts     atomic.Int64
	observations atomic.Int64
	trace        *browserLiveTrace
}

func (h *browserLiveNative) Create(ctx context.Context, profile, id, address string) error {
	h.trace.enter("native_create")
	err := h.Backend.Create(ctx, profile, id, address)
	h.trace.fail(err, true)
	return err
}
func (h *browserLiveNative) Present(ctx context.Context, view browserhost.Viewport) error {
	h.trace.enter("native_present")
	err := h.Backend.Present(ctx, view)
	h.trace.fail(err, true)
	return err
}
func (h *browserLiveNative) Command(ctx context.Context, id, op, value string) (string, error) {
	if op == "state" {
		h.trace.enter("native_navigation_state")
	}
	result, err := h.Backend.Command(ctx, id, op, value)
	if op == "state" {
		h.trace.fail(err, true)
	}
	return result, err
}

func (h *browserLiveNative) Observe(ctx context.Context, id string) (browserhost.Observation, error) {
	h.trace.enter("native_observe")
	h.observations.Add(1)
	o, err := h.Backend.Observe(ctx, id)
	if err == nil {
		h.trace.recordCanvasObservation(o)
	}
	h.trace.fail(err, true)
	return o, err
}
func (h *browserLiveNative) Execute(ctx context.Context, id string, a browserhost.Action) error {
	phase := "native_action_" + browserLiveActionKind(a.Kind)
	h.trace.enter(phase)
	h.attempts.Add(1)
	err := h.Backend.Execute(ctx, id, a)
	if err == nil {
		h.inputs.Add(1)
	}
	h.trace.recordAction(a.Kind, err)
	h.trace.recordCanvasAction(a, err)
	h.trace.fail(err, true)
	return err
}

func (h *browserLiveNative) Navigate(ctx context.Context, id, address string) error {
	h.trace.enter("native_navigate")
	err := h.Backend.Navigate(ctx, id, address)
	h.trace.fail(err, true)
	return err
}

func runBrowserLiveCase(ctx context.Context, root string, model browserLiveModel, name string, repeat int, budget *browserLiveBudget) (row browserLiveCaseReport) {
	row = browserLiveCaseReport{Model: model.label + "/" + model.model, Case: name, Repeat: repeat, Status: "setup_failed"}
	start, before := time.Now(), budget.snapshot()
	trace := &browserLiveTrace{phase: "setup"}
	defer func() {
		row.ElapsedMS = time.Since(start).Milliseconds()
		row.browserLiveMetrics = budget.snapshot().minus(before)
		trace.copyTo(&row)
	}()
	ctx, cancel := context.WithTimeout(ctx, browserMaxDuration)
	defer cancel()
	fixture := newBrowserLivePage(name)
	trace.canvas = newBrowserLiveCanvasTrace(fixture)
	defer fixture.server.Close()
	prov, closeProvider, err := newBrowserLiveProvider(ctx, model, budget, trace)
	if err != nil {
		row.Status = "provider_setup_failed"
		return
	}
	defer closeProvider()
	var service atomic.Pointer[browserService]
	host := browserhost.New(func(e browserhost.Event) {
		if s := service.Load(); s != nil {
			s.onEvent(e)
		}
	})
	native := &browserLiveNative{Backend: host, trace: trace}
	s := newBrowserService(native, filepath.Join(root, "case-"+browserID()), nil)
	service.Store(s)
	defer s.shutdown()
	owner := "synthetic-" + browserID()
	if s.register(owner, "synthetic-project-"+browserID()) != nil {
		return
	}
	host.SetNavigationGuard(func(id, target string) bool {
		_, origin, err := browserURL(target)
		return err == nil && (target == "about:blank" || origin == fixture.server.URL) && s.navigationAllowed(id, target)
	})
	s.emit = func(gotOwner string, state BrowserState) {
		if state.Approval == nil {
			return
		}
		p := state.Approval
		trace.enter("browser_approval")
		// Test authorization only for this generated page, never real sites or files.
		allow := gotOwner == owner && (p.Origin == fixture.server.URL || (p.Origin == "" && strings.Contains(p.Reason, fixture.address())))
		s.approve(gotOwner, BrowserCommand{Op: "approve", ApprovalID: p.ID, Allow: allow})
	}
	st := s.command(ctx, owner, BrowserCommand{Op: "new"})
	if st.Error != "" || st.ActiveTabID == "" {
		row.Status = "native_create_failed"
		return
	}
	if s.present(ctx, owner, BrowserViewport{TabID: st.ActiveTabID, X: 16, Y: 56, Width: 900, Height: 600, Scale: 1, Visible: true}) != nil {
		row.Status = "native_present_failed"
		return
	}
	task := &browserLiveTaskTool{service: s, prov: prov, owner: owner, model: row.Model, address: fixture.address(), tab: st.ActiveTabID, trace: trace}
	reg := tool.NewRegistry()
	reg.Add(task)
	sess := agent.NewSession("You are the main O.R.C.A. agent in an isolated synthetic browser acceptance test. Delegate the supplied task exactly once using browser_task. Include its URL, goal and observable success_criteria. Never claim completion without a successful browser_task result. Do not retry a failed delegation. No other tools exist. Reply briefly after the tool returns.")
	mainAgent := agent.New(prov, reg, sess, agent.Options{MaxSteps: 3, Temperature: 0}, event.Discard)
	err = mainAgent.Run(agent.WithParentSession(ctx, owner), "Use browser_task on "+fixture.address()+" in tab "+st.ActiveTabID+". "+fixture.goal()+" Restriction: use only this local synthetic website. Do not navigate to other origins or submit more than once.")
	trace.fail(err, false)
	row.NativeActions = native.inputs.Load()
	row.NativeActionAttempts, row.NativeObservations = native.attempts.Load(), native.observations.Load()
	row.Delegations, row.LineageVerified = task.calls.Load(), task.lineage.Load()
	if kind := task.failure.Load(); kind != nil {
		row.TaskErrorKind = *kind
	}
	row.VerifiedEvents, row.Status = fixture.verify()
	if row.Delegations != 1 || !row.LineageVerified {
		row.Status = "real_agent_native_chain_missing"
		return
	}
	if err != nil || !task.succeeded.Load() || s.state(owner).TaskState != "succeeded" {
		row.Status = "agent_or_browser_failed"
		if code := task.failure.Load(); code != nil {
			row.Status = *code
		}
		if ctx.Err() != nil {
			row.Status = "task_timeout"
		}
		if budget.exhausted() {
			row.Status = "budget_exhausted"
		}
		return
	}
	if row.NativeActions == 0 || row.NativeObservations < 2 {
		row.Status = "real_native_actions_or_verification_missing"
	}
	return
}
