package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestDeepSeekWireJSONAllAssistantTurns(t *testing.T) {
	original, empty := "  original\nreasoning\t", ""
	messages := []provider.Message{
		{Role: provider.RoleSystem, Content: "system", ReasoningContent: "not assistant"},
		{Role: provider.RoleUser, Content: "first"},
		{Role: provider.RoleAssistant, Content: "plain", ReasoningContent: "DISPLAY", ProtocolReasoningContent: &original},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, ReasoningContent: "legacy original", ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Content: "done", ReasoningContent: "not assistant"},
		{Role: provider.RoleAssistant, Content: "final", ReasoningContent: "DISPLAY", ProtocolReasoningContent: &empty},
		{Role: provider.RoleAssistant, Content: "synthetic summary without reasoning"},
		{Role: provider.RoleUser, Content: "continue"},
	}
	before, _ := json.Marshal(messages)
	for _, tc := range []struct {
		name                      string
		deepseek, tools, disabled bool
	}{
		{"tools", true, true, false},
		{"without-tools", true, false, false},
		{"disabled-classifier", true, false, true},
		{"disabled-with-tools", true, true, true},
		{"other-provider", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var received map[string]json.RawMessage
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" {
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer srv.Close()
			c := &client{name: "test", model: "deepseek-flash", deepseek: tc.deepseek, baseURL: srv.URL, http: srv.Client()}
			req := provider.Request{Messages: messages, DisableThinking: tc.disabled}
			if tc.tools {
				req.Tools = []provider.ToolSchema{{Name: "echo", Parameters: json.RawMessage(`{"type":"object"}`)}}
			}
			ch, err := c.Stream(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			for chunk := range ch {
				if chunk.Err != nil {
					t.Fatal(chunk.Err)
				}
			}
			var wire []map[string]json.RawMessage
			if err := json.Unmarshal(received["messages"], &wire); err != nil {
				t.Fatal(err)
			}
			if len(wire) != len(messages) {
				t.Fatalf("messages=%d, want %d", len(wire), len(messages))
			}
			for i, m := range messages {
				value, present := wire[i]["reasoning_content"]
				want := tc.deepseek && tc.tools && m.Role == provider.RoleAssistant && protocolReasoning(m) != nil
				if present != want {
					t.Fatalf("message %d reasoning presence=%v, want %v", i, present, want)
				}
				if want {
					var got string
					if err := json.Unmarshal(value, &got); err != nil {
						t.Fatal(err)
					}
					if got != *protocolReasoning(m) {
						t.Fatalf("message %d reasoning=%q", i, got)
					}
				}
				if _, ok := wire[i]["protocol_reasoning_content"]; ok {
					t.Fatal("local storage field leaked onto wire")
				}
			}
			if tc.disabled {
				if _, ok := received["reasoning_effort"]; ok {
					t.Fatal("disabled request includes effort")
				}
			} else if tc.deepseek && string(received["reasoning_effort"]) != `"high"` {
				t.Fatalf("default effort=%s", received["reasoning_effort"])
			}
		})
	}
	after, _ := json.Marshal(messages)
	if string(before) != string(after) {
		t.Fatal("request construction mutated history")
	}
}

func TestDeepSeekUnknownHistoryIsSentWithoutInventedReasoning(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Messages []map[string]json.RawMessage }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		for _, m := range req.Messages {
			if _, ok := m["reasoning_content"]; ok {
				t.Error("unknown history acquired invented reasoning")
			}
		}
		requests.Add(1)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	c := &client{name: "test", deepseek: true, baseURL: srv.URL, http: srv.Client()}
	for _, withCalls := range []bool{false, true} {
		m := provider.Message{Role: provider.RoleAssistant, Content: "legacy answer"}
		if withCalls {
			m.ToolCalls = []provider.ToolCall{{ID: "c", Name: "echo", Arguments: `{}`}}
		}
		messages := []provider.Message{m}
		if withCalls {
			messages = append(messages, provider.Message{Role: provider.RoleTool, ToolCallID: "c", Content: "completed"})
		}
		ch, err := c.Stream(context.Background(), provider.Request{Messages: messages, Tools: []provider.ToolSchema{{Name: "echo"}}})
		if err != nil {
			t.Fatalf("unknown history rejected locally: %v", err)
		}
		for chunk := range ch {
			if chunk.Err != nil {
				t.Fatal(chunk.Err)
			}
		}
	}
	if requests.Load() != 2 {
		t.Fatalf("unknown history reached HTTP %d times, want 2", requests.Load())
	}
}

func TestDeepSeekReasoningHistoryErrorOnlyForRequestRejections(t *testing.T) {
	for _, deepseek := range []bool{false, true} {
		for _, status := range []int{400, 422, 401, 429, 500} {
			for _, body := range []string{"missing reasoning_content", "maximum context length exceeded"} {
				apiErr := &provider.APIError{Provider: "test", Status: status, Body: body}
				err := (&client{name: "test", deepseek: deepseek}).requestError(apiErr)
				var historyErr *provider.ReasoningHistoryError
				want := deepseek && (status == 400 || status == 422) && strings.Contains(body, "reasoning_content")
				if errors.As(err, &historyErr) != want {
					t.Fatalf("deepseek=%v status=%d body=%s: %v", deepseek, status, body, err)
				}
				if !errors.Is(err, apiErr) {
					t.Fatal("lost original diagnostic error")
				}
			}
		}
	}
}

func TestDeepSeekRejectedHistoryAfterReconnectAndInSSE(t *testing.T) {
	for _, reconnect := range []bool{false, true} {
		t.Run(fmt.Sprintf("reconnect=%v", reconnect), func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				if reconnect {
					if n == 1 {
						w.WriteHeader(200)
						return
					}
					w.WriteHeader(400)
					fmt.Fprint(w, `{"error":{"message":"missing reasoning_content"}}`)
				} else {
					fmt.Fprint(w, "data: {\"error\":{\"message\":\"missing reasoning_content\"}}\n\n")
				}
			}))
			defer srv.Close()
			c := &client{name: "test", deepseek: true, baseURL: srv.URL, http: srv.Client()}
			ch, err := c.Stream(context.Background(), provider.Request{
				Messages: []provider.Message{{Role: provider.RoleAssistant, Content: "legacy"}},
				Tools:    []provider.ToolSchema{{Name: "echo"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var failures int
			for chunk := range ch {
				if chunk.Err == nil {
					continue
				}
				failures++
				var historyErr *provider.ReasoningHistoryError
				if !errors.As(chunk.Err, &historyErr) || provider.IsStreamInterrupted(chunk.Err) {
					t.Fatalf("wrong error type: %v", chunk.Err)
				}
			}
			want := int32(1)
			if reconnect {
				want = 2
			}
			if failures != 1 || requests.Load() != want {
				t.Fatalf("failures=%d requests=%d", failures, requests.Load())
			}
		})
	}
}

func TestDeepSeekRejectedLegacyReasoningIsActionableAndNotRetried(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"invalid reasoning_content"}}`)
	}))
	defer srv.Close()
	c := &client{name: "test", deepseek: true, baseURL: srv.URL, http: srv.Client()}
	_, err := c.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleAssistant, Content: "answer", ReasoningContent: "legacy display of unknown origin"}},
		Tools:    []provider.ToolSchema{{Name: "echo"}},
	})
	var historyErr *provider.ReasoningHistoryError
	var apiErr *provider.APIError
	if !errors.As(err, &historyErr) || !errors.As(err, &apiErr) || apiErr.Status != 400 || requests.Load() != 1 {
		t.Fatalf("error=%v, requests=%d", err, requests.Load())
	}
}

func TestDeepSeekModelAliasesAreOfficialEndpointOnly(t *testing.T) {
	for _, base := range []string{"https://api.deepseek.com/v1", "https://relay.example/v1"} {
		for _, model := range []string{"deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp", "deepseek-v4-pro", "private-deepseek-v4-flash", "deepseek-reasoner"} {
			p, err := New(provider.Config{Name: "custom", BaseURL: base, Model: model, Extra: map[string]any{"reasoning_protocol": "deepseek"}})
			if err != nil {
				t.Fatal(err)
			}
			want := model
			if IsDeepSeek(base) && (model == "deepseek-v4-flash" || model == "deepseek-v4-flash-vision-exp") {
				want = "deepseek-flash"
			}
			if got := p.(*client).buildRequest(provider.Request{}).Model; got != want {
				t.Errorf("base=%s model=%s got=%s", base, model, got)
			}
		}
	}
}

func TestDeepSeekStreamKeepsExplicitEmptyReasoning(t *testing.T) {
	c := &client{name: "test"}
	// An empty field is valid captured protocol data, unlike an absent field.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	c.baseURL, c.http = srv.URL, srv.Client()
	ch, err := c.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	var reasoning []string
	for chunk := range ch {
		if chunk.Type == provider.ChunkReasoning {
			reasoning = append(reasoning, chunk.Text)
		}
	}
	if !reflect.DeepEqual(reasoning, []string{""}) {
		t.Fatalf("captured=%#v", reasoning)
	}
}
