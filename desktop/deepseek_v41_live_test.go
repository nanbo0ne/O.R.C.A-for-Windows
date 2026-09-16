//go:build windows && (manual || live)

package main

// From desktop: go test -tags manual -run '^TestDeepSeekV41TaskImagesFixture$' -count=1 -timeout=2m -v .
// Live: ORCA_V41_LIVE=1 AND ORCA_V41_CHILD_LIVE=1, with DEEPSEEK_API_KEY already
// injected into the process. Run '^TestDeepSeekV41TaskImagesLive$' with -timeout=12m.
// Supply ORCA_V41_PRIOR_REQUESTS and ORCA_V41_PRIOR_MICRO_CNY, plus
// ORCA_V41_CHILD_REPORT=<new output path>. Never run concurrently with the parent.
// Each HTTP attempt is reserved before sending: 163840 micro-CNY, using
// 65536 input tokens at CNY 2/M and 4096 output at CNY 8/M. Verify tariff ceilings.
// Three requests are planned; hard caps are six NEW requests/983040 micro-CNY,
// 40 requests/CNY 5 cumulative and 90 seconds per request. No failed-attempt refunds.
// Offline uses only loopback, never reads a key. Reports contain no output text.
import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/netclient"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/permission"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	_ "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/openai"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type v41ChildCap struct{ provider.Provider }

func (p v41ChildCap) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	req.MaxTokens = 4096
	return p.Provider.Stream(ctx, req)
}
func TestDeepSeekV41TaskImagesFixture(t *testing.T) {
	v41RunTaskImages(t, false)
}
func TestDeepSeekV41TaskImagesLive(t *testing.T) {
	if os.Getenv("ORCA_V41_LIVE") != "1" || os.Getenv("ORCA_V41_CHILD_LIVE") != "1" {
		t.Skip("child live gates closed")
	}
	v41RunTaskImages(t, true)
}
func v41RunTaskImages(t *testing.T, live bool) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal("child_fixture_setup_or_run_failed")
		}
	}
	const reserve = int64(65536*2 + 4096*8)
	reportPath := os.Getenv("ORCA_V41_CHILD_REPORT")
	priorCount, priorCost := os.Getenv("ORCA_V41_PRIOR_REQUESTS"), os.Getenv("ORCA_V41_PRIOR_MICRO_CNY")
	if live && (priorCount == "" || priorCost == "" || reportPath == "") {
		t.Fatal("live_requires_prior_counters_and_new_report_path")
	}
	if !live && priorCount == "" && priorCost == "" {
		priorCount, priorCost = "0", "0"
	}
	priorRequests, err := strconv.Atoi(priorCount)
	check(err)
	priorReserved, err := strconv.ParseInt(priorCost, 10, 64)
	check(err)
	if priorRequests < 0 || priorRequests > 40 || priorReserved < 0 || priorReserved > 5_000_000 {
		t.Fatal("invalid_prior_counters")
	}
	if priorRequests+3 > 40 || priorReserved+3*reserve > 5_000_000 {
		t.Fatal("insufficient_combined_budget_for_three_requests")
	}
	root := t.TempDir()
	var nonce [16]byte
	_, err = rand.Read(nonce[:])
	check(err)
	want := "\u6d4b\u8bd5\u6210\u529f " + strings.ToUpper(hex.EncodeToString(nonce[:2]))
	fontBytes, err := os.ReadFile(`C:\Windows\Fonts\msyh.ttc`)
	check(err)
	collection, err := opentype.ParseCollection(fontBytes)
	check(err)
	f, err := collection.Font(0)
	check(err)
	for _, r := range want {
		glyph, err := f.GlyphIndex(nil, r)
		check(err)
		if glyph == 0 {
			t.Fatal("chinese_font_glyph_missing")
		}
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 42, DPI: 72, Hinting: font.HintingFull})
	check(err)
	defer face.Close()
	im := image.NewRGBA(image.Rect(0, 0, 640, 160))
	draw.Draw(im, im.Bounds(), image.White, image.Point{}, draw.Src)
	drawer := font.Drawer{Dst: im, Src: image.Black, Face: face, Dot: fixed.P(32, 98)}
	drawer.DrawString(want)
	var encoded bytes.Buffer
	check(png.Encode(&encoded, im))
	check(os.WriteFile(filepath.Join(root, "frame.png"), encoded.Bytes(), 0600))
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	var mu sync.Mutex
	requests, imageRequests, parentResults, taskDispatches := 0, 0, 0, 0
	mode, key := "offline_fixture", ""
	if live {
		mode = "live"
	}
	if reportPath == "" {
		reportPath = filepath.Join(root, "child-report.json")
	}
	out, err := os.OpenFile(reportPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	check(err)
	defer out.Close()
	report := map[string]any{"schema": 1, "mode": mode, "passed": false, "model": "deepseek-flash", "effort": "high", "max_tokens": 4096, "vision_capability_source": "explicit_test_override", "requests": priorRequests, "reserved_micro_cny": priorReserved, "prior_requests": priorRequests, "prior_reserved_micro_cny": priorReserved, "new_request_limit": 6, "new_budget_micro_cny": 983040, "request_limit": 40, "budget_micro_cny": 5000000, "request_timeout_seconds": 90, "first_delta_ms": int64(-1), "http_statuses": []int{}, "gaps": []string{"boot and desktop App UI", "tariff ceilings are assumptions, not an invoice"}}
	defer func() {
		report["passed"] = !t.Failed()
		if err := json.NewEncoder(out).Encode(report); err != nil {
			t.Error("final_report_write_failed")
		}
		if err := out.Sync(); err != nil {
			t.Error("final_report_sync_failed")
		}
	}()
	if live {
		key = os.Getenv("DEEPSEEK_API_KEY")
		if key == "" {
			t.Fatal("DEEPSEEK_API_KEY_missing")
		}
	}
	transport := &http.Transport{DisableKeepAlives: true, ResponseHeaderTimeout: 90 * time.Second, TLSHandshakeTimeout: 15 * time.Second}
	client := &http.Client{Transport: transport, Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer transport.CloseIdleConnections()
	stopped := false
	stop := func(w http.ResponseWriter, class string) {
		mu.Lock()
		stopped = true
		report["error_class"] = class
		mu.Unlock()
		http.Error(w, "child_harness_stopped", 422)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close")
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
		if err != nil {
			stop(w, "request_body_limit")
			return
		}
		var wire struct {
			Model    string            `json:"model"`
			Max      int               `json:"max_tokens"`
			Tools    []json.RawMessage `json:"tools"`
			Messages []struct {
				Role      string          `json:"role"`
				Content   json.RawMessage `json:"content"`
				Reasoning *string         `json:"reasoning_content"`
			} `json:"messages"`
		}
		if json.Unmarshal(body, &wire) != nil || wire.Model != "deepseek-flash" || wire.Max != 4096 || r.Method != "POST" || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer "+hex.EncodeToString(nonce[:]) {
			stop(w, "wire_contract")
			return
		}
		isChild, hasResult := len(wire.Tools) == 0, false
		images := 0
		for _, m := range wire.Messages {
			if m.Role == "tool" {
				var result string
				_ = json.Unmarshal(m.Content, &result)
				hasResult = strings.Contains(strings.Join(strings.Fields(result), ""), strings.Join(strings.Fields(want), ""))
			}
			if !isChild && m.Role == "assistant" && m.Reasoning == nil {
				stop(w, "parent_reasoning_history_missing")
				return
			}
			var parts []struct {
				Type string `json:"type"`
				URL  struct {
					URL string `json:"url"`
				} `json:"image_url"`
			}
			if json.Unmarshal(m.Content, &parts) == nil {
				for _, part := range parts {
					if part.Type == "image_url" {
						images++
						if part.URL.URL != dataURL {
							stop(w, "pinned_image_bytes_changed")
							return
						}
					}
				}
			}
		}
		if (isChild && images != 1) || (!isChild && images != 0) {
			stop(w, "native_image_contract")
			return
		}
		mu.Lock()
		if stopped || requests >= 6 || priorRequests+requests >= 40 || priorReserved+int64(requests+1)*reserve > 5_000_000 {
			mu.Unlock()
			stop(w, "combined_budget_or_previous_failure")
			return
		}
		requests++
		if isChild {
			imageRequests++
		}
		if hasResult {
			parentResults++
		}
		report["requests"], report["reserved_micro_cny"] = priorRequests+requests, priorReserved+int64(requests)*reserve
		report["new_requests"], report["native_image_parts"] = requests, imageRequests
		mu.Unlock()
		var responseBody io.ReadCloser
		if live {
			ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
			defer cancel()
			up, err := http.NewRequestWithContext(ctx, "POST", "https://api.deepseek.com/v1/chat/completions", bytes.NewReader(body))
			if err != nil {
				stop(w, "request_build_failed")
				return
			}
			up.GetBody = nil
			up.Header.Set("Authorization", "Bearer "+key)
			up.Header.Set("Content-Type", "application/json")
			response, err := client.Do(up)
			if err != nil {
				stop(w, "transport_or_timeout")
				return
			}
			responseBody = response.Body
			mu.Lock()
			report["http_statuses"] = append(report["http_statuses"].([]int), response.StatusCode)
			mu.Unlock()
			if response.StatusCode != 200 {
				responseBody.Close()
				stop(w, "http_"+fmt.Sprint(response.StatusCode))
				return
			}
		} else {
			delta := map[string]any{"reasoning_content": "synthetic reasoning"}
			finish := "stop"
			switch {
			case isChild:
				delta["content"] = want
			case hasResult:
				delta["content"] = want
			default:
				args := `{"prompt":"Read the Chinese text and code in the image. Return only the visible text.","images":["frame.png"],"max_steps":1}`
				delta["tool_calls"] = []any{map[string]any{"index": 0, "id": "fixture-task", "type": "function", "function": map[string]any{"name": "task", "arguments": args}}}
				finish = "tool_calls"
			}
			eventData, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 40, "total_tokens": 140}})
			responseBody = io.NopCloser(strings.NewReader(fmt.Sprintf("data: %s\n\ndata: [DONE]\n\n", eventData)))
		}
		defer responseBody.Close()
		w.Header().Set("Content-Type", "text/event-stream")
		scanner := bufio.NewScanner(io.LimitReader(responseBody, 2<<20))
		scanner.Buffer(make([]byte, 4096), 128<<10)
		for scanner.Scan() {
			line := scanner.Text()
			if _, err := fmt.Fprintln(w, line); err != nil {
				stop(w, "downstream_closed")
				return
			}
			w.(http.Flusher).Flush()
			if line == "data: [DONE]" {
				fmt.Fprintln(w)
				return
			}
		}
		stop(w, "incomplete_or_timed_out_stream")
	}))
	defer server.Close()
	prov, err := provider.New("openai", provider.Config{Name: "fixture", BaseURL: server.URL, APIKey: hex.EncodeToString(nonce[:]), Model: "deepseek-flash", Extra: map[string]any{"reasoning_protocol": "deepseek", "effort": "high", "proxy_spec": netclient.ProxySpec{Mode: netclient.ModeOff}}})
	check(err)
	p := v41ChildCap{prov}
	reg := tool.NewRegistry()
	const model = "deepseek/deepseek-flash"
	policy := permission.New("deny", []string{"task", "read_file(frame.png)", "image_send(" + model + ")"}, nil, nil)
	task := agent.NewTaskTool(p, nil, reg, 2, 0, 0, 0, 0, 0, "", "Read only the supplied image; answer briefly.", permission.NewGate(policy, nil), "", "", nil).
		WithTranscripts(agent.NewSubagentStore(filepath.Join(root, "children")), root, model, "high").
		WithVisionDefault(model).WithVision("on", func(string) string { return "supported" }, nil)
	reg.Add(task)
	start := time.Now()
	usageCount, promptTokens, completionTokens := 0, 0, 0
	sink := event.FuncSink(func(e event.Event) {
		mu.Lock()
		defer mu.Unlock()
		if e.Kind == event.ToolDispatch && e.Tool.Name == "task" && !e.Tool.Partial {
			taskDispatches++
		}
		if (e.Kind == event.Text || e.Kind == event.Reasoning) && report["first_delta_ms"] == int64(-1) {
			report["first_delta_ms"] = time.Since(start).Milliseconds()
		}
		if e.Kind == event.Usage && e.Usage != nil {
			usageCount++
			promptTokens += e.Usage.PromptTokens
			completionTokens += e.Usage.CompletionTokens
			if e.Usage.PromptTokens <= 0 || e.Usage.PromptTokens > 65536 || e.Usage.CompletionTokens <= 0 || e.Usage.CompletionTokens > 4096 || (e.Usage.FinishReason != "stop" && e.Usage.FinishReason != "tool_calls") {
				stopped = true
				report["error_class"] = "usage_bound"
			}
		}
		if (e.Err != nil || e.Tool.Err != "") && report["error_class"] == nil {
			stopped = true
			report["error_class"] = "agent_or_tool_error"
		}
		report["usage_receipts"], report["prompt_tokens"], report["completion_tokens"] = usageCount, promptTokens, completionTokens
	})
	session := agent.NewSession("Delegate image reading to the task tool, then return its visible answer.")
	executor := agent.New(p, reg, session, agent.Options{MaxSteps: 3}, sink)
	c := control.New(control.Options{Runner: executor, Executor: executor, Sink: sink, Registry: reg, WorkspaceRoot: root, VisionMode: "on", Policy: policy})
	defer c.Close()
	c.SetToolApprovalMode(control.ToolApprovalAuto)
	c.EnableInteractiveApproval()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	check(c.RunTurn(ctx, "Use task(images) to read frame.png, then return exactly the child's answer."))
	server.Close()
	if stopped || requests != 3 || imageRequests != 1 || parentResults != 1 || taskDispatches != 1 || usageCount != requests {
		t.Fatal("host_task_delegation_incomplete")
	}
	history := session.Snapshot()
	if strings.Join(strings.Fields(history[len(history)-1].Content), "") != strings.Join(strings.Fields(want), "") {
		t.Fatal("parent_did_not_return_child_answer")
	}
	snapshots, err := filepath.Glob(filepath.Join(root, ".orca", "attachments", "*.png"))
	check(err)
	if len(snapshots) != 1 {
		t.Fatal("host_image_snapshot_missing")
	}
	snapshot, err := os.ReadFile(snapshots[0])
	check(err)
	if !bytes.Equal(snapshot, encoded.Bytes()) {
		t.Fatal("host_snapshot_bytes_changed")
	}
	report["host_task_dispatches"], report["chinese_glyphs_and_snapshot_verified"] = taskDispatches, true
	t.Logf("mode=%s new_requests=%d combined_requests=%d combined_reserved_micro_cny=%d", mode, requests, priorRequests+requests, priorReserved+int64(requests)*reserve)
}
