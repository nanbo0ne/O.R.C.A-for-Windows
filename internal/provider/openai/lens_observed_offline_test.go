//go:build lenslive

package openai

import (
	"context"
	"encoding/json"
	"errors"
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

func lensOfflineObservedClient(t *testing.T, base string) *client {
	t.Helper()
	p, err := New(provider.Config{Name: "synthetic-observed", BaseURL: base, Model: "synthetic",
		Extra: map[string]any{"proxy_spec": netclient.ProxySpec{Mode: netclient.ModeOff}}})
	if err != nil {
		t.Fatal("synthetic provider setup failed")
	}
	c := p.(*client)
	c.http.Transport = &lensObservedTransport{next: c.http.Transport}
	return c
}

func TestLensObservedOfflineMarker(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		eof, want   bool
	}{
		{"complete", "data: [DONE]\n\n", false, true},
		{"crlf", "data:\t[DONE]\r\n", false, true},
		{"eof_line", "data: [DONE]", true, true},
		{"partial", "data: [DONE]", false, false},
		{"truncated", "data: [DON", true, false},
		{"json_text", "data: {\"content\":\"[DONE]\"}\n", false, false},
		{"comment", ": data: [DONE]\n", false, false},
		{"extra_suffix", "data: [DONE]more\n", false, false},
		{"recovery", "data: nonsense\ndata: [DONE]\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for width := 1; width <= len(tc.input); width++ {
				var probe lensWireDoneProbe
				var found bool
				for i := 0; i < len(tc.input); i += width {
					end := min(i+width, len(tc.input))
					found = probe.consume([]byte(tc.input[i:end]), false) || found
				}
				found = probe.consume(nil, tc.eof) || found
				if found != tc.want {
					t.Fatalf("split=%d found=%v want=%v", width, found, tc.want)
				}
			}
		})
	}
}

func TestLensObservedOfflineAgentShape(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		var req chatRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			t.Error("invalid request JSON")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "synthetic_echo" {
			t.Error("tools not retained on every request")
		}
		if n == 2 {
			if len(req.Messages) != 3 || req.Messages[1].Role != "assistant" || len(req.Messages[1].ToolCalls) != 1 || req.Messages[2].Role != "tool" {
				t.Error("continuation must immediately follow tool result")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			body, ok := req.Messages[2].Content.(string)
			if !ok || len(body) < 24000 || len(body) >= 32*1024 || req.Messages[2].ToolCallID != req.Messages[1].ToolCalls[0].ID {
				t.Error("invalid bounded tool result")
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"synthetic-call\",\"function\":{\"name\":\"synthetic_echo\",\"arguments\":\"{\\\"value\\\":\\\"hello\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
		} else {
			io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"completed\"},\"finish_reason\":\"stop\"}]}\n\n")
		}
		io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	var reports []lensObservedReport
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !lensObservedAgentScenario(ctx, lensOfflineObservedClient(t, srv.URL), "offline", func(r lensObservedReport) { reports = append(reports, r) }) {
		t.Fatal("synthetic scenario failed")
	}
	if requests.Load() != 3 || len(reports) != 3 {
		t.Fatal("incorrect report/request count")
	}
	for _, r := range reports {
		if r.Result != "ok" || r.AttemptCount != 1 || !r.ProviderDone || !r.Attempts[0].WireDone || r.Attempts[0].ReadBytes <= 0 || !r.Attempts[0].BodyClosed {
			t.Fatal("missing success observations")
		}
	}
}

func TestLensObservedOfflineFailureKeepsAttempts(t *testing.T) {
	const private = "SYNTHETIC_PRIVATE_NEVER_LOG"
	const payload = "data: {\"choices\":[{\"delta\":{\"content\":\"SYNTHETIC_PRIVATE_NEVER_LOG\"}}]}\n\ndata: {\"error\":{\"message\":\"Token Lens: upstream_stream_incomplete SYNTHETIC_PRIVATE_NEVER_LOG\"}}\n\n"
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, private)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, payload)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, report := lensObserveCompletion(ctx, lensOfflineObservedClient(t, srv.URL), provider.Request{}, 1)
	if report.Result != "upstream_stream_incomplete" || report.AttemptCount != 2 || requests.Load() != 2 || report.ProviderDone {
		t.Fatal("failed request lost attempt/error observations")
	}
	if report.Attempts[0].Status != 503 || report.Attempts[0].ReadBytes != int64(len(private)) || report.Attempts[1].Status != 200 || report.Attempts[1].ReadBytes != int64(len(payload)) {
		t.Fatal("incorrect response byte/status accounting")
	}
	if report.Attempts[1].WireDone || !report.Attempts[1].BodyClosed || report.ContextBeforeCleanup != "none" {
		t.Fatal("server error confused with completion or cancellation")
	}
	data, err := json.Marshal(report)
	if err != nil || strings.Contains(string(data), private) || strings.Contains(string(data), srv.URL) {
		t.Fatal("diagnostics leaked response contents or endpoint")
	}
}

func TestLensObservedOfflineRejectsFinishOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"synthetic\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n")
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, report := lensObserveCompletion(ctx, lensOfflineObservedClient(t, srv.URL), provider.Request{}, 1)
	if report.Result != "missing_wire_done" || !report.ProviderDone || report.Attempts[0].WireDone || report.Attempts[0].ReadEnd != "eof" {
		t.Fatal("finish-only completion was not distinguished")
	}
}

func TestLensObservedOfflineFailedScenarioLogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"error\":{\"message\":\"upstream_stream_incomplete\"}}\n\n")
	}))
	defer srv.Close()
	var reports []lensObservedReport
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	passed := lensObservedAgentScenario(ctx, lensOfflineObservedClient(t, srv.URL), "offline", func(r lensObservedReport) { reports = append(reports, r) })
	if passed || len(reports) != 1 || reports[0].AttemptCount != 1 || reports[0].Attempts[0].ReadBytes == 0 || reports[0].Result != "upstream_stream_incomplete" {
		t.Fatal("failure returned before recording observations")
	}
}

type lensObservedOfflineTransportFunc func(*http.Request) (*http.Response, error)

func (f lensObservedOfflineTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestLensObservedOfflineFailureBeforeHeaders(t *testing.T) {
	c := lensOfflineObservedClient(t, "http://127.0.0.1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.http.Transport = &lensObservedTransport{next: lensObservedOfflineTransportFunc(func(*http.Request) (*http.Response, error) {
		cancel()
		return nil, errors.New("SYNTHETIC_PRIVATE_BEFORE_HEADERS")
	})}
	_, report := lensObserveCompletion(ctx, c, provider.Request{}, 1)
	if report.Result != "cancelled" || report.AttemptCount != 1 || report.Attempts[0].RequestError != "provider_error" || report.Attempts[0].Status != 0 || report.Attempts[0].HeadersMS != -1 || report.Attempts[0].ReadBytes != 0 {
		t.Fatal("missing pre-header error observations")
	}
	data, _ := json.Marshal(report)
	if strings.Contains(string(data), "SYNTHETIC_PRIVATE_BEFORE_HEADERS") {
		t.Fatal("pre-header diagnostics leaked raw error")
	}
}

func TestLensObservedOfflineIdleClosesWithDiagnosis(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"synthetic\"}}]}\n\n")
		flush(w)
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := lensOfflineObservedClient(t, srv.URL)
	c.idleTimeout = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, report := lensObserveCompletion(ctx, c, provider.Request{}, 1)
	if report.Result != "stream_stalled" || report.AttemptCount != 1 || report.ProviderDone || report.Attempts[0].WireDone || !report.Attempts[0].BodyClosed || report.Attempts[0].ReadBytes == 0 || report.ContextBeforeCleanup != "none" {
		t.Fatal("idle closure diagnosis incomplete")
	}
}
