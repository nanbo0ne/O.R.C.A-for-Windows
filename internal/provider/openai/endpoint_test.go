package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestEndpointNormalizationOfficialBases(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://api.deepseek.com", "https://api.deepseek.com"},
		{"https://api.deepseek.com/", "https://api.deepseek.com"},
		{"https://api.deepseek.com/chat/completions", "https://api.deepseek.com"},
		{"https://api.deepseek.com/v1/chat/completions/", "https://api.deepseek.com/v1"},
		{" https://api.openai.com/v1/responses/ ", "https://api.openai.com/v1"},
		{"https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1"},
	} {
		p, err := New(provider.Config{BaseURL: tc.raw, Model: "custom-alias"})
		if err != nil {
			t.Fatal(err)
		}
		if c := p.(*client); c.baseURL != tc.want || c.model != "custom-alias" {
			t.Errorf("New(%q): base=%s model=%s, want %s/custom-alias", tc.raw, c.baseURL, c.model, tc.want)
		}
	}
}

func TestEndpointValidationBeforeRequest(t *testing.T) {
	for _, raw := range []string{
		"/v1", "relay.example/v1", "ftp://relay.example/v1", "https:///v1", "https://relay.example/%zz",
		"https://user:secret-value@relay.example/v1", "https://relay.example/v1?token=secret-value",
		"https://relay.example/v1?", "https://relay.example/v1#secret-value", "https://relay.example/v1#",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := New(provider.Config{BaseURL: raw, Model: "m"})
			if err == nil {
				t.Fatal("invalid endpoint accepted")
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("URL credential leaked: %v", err)
			}
		})
	}
	for _, path := range []string{"/api/v1/chat", "/proxy/api/v1/chat/", "/v1/messages"} {
		_, err := New(provider.Config{BaseURL: "http://localhost:1234" + path, Model: "m"})
		if err == nil || !strings.Contains(err.Error(), "OpenAI-compatible") || !strings.Contains(err.Error(), "/v1") {
			t.Errorf("incompatible protocol %s: %v", path, err)
		}
	}
}

func TestEndpointRejectionIsActionableWithoutReplay(t *testing.T) {
	const reason = "\u6b64\u63a8\u7406\u63a5\u53e3\u5c1a\u672a\u652f\u6301"
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/proxy/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"type": "unsupported_feature", "message": reason}})
	}))
	defer srv.Close()
	p, err := New(provider.Config{Name: "custom", BaseURL: srv.URL + "/proxy/v1", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	p.(*client).http = srv.Client()
	ctx := provider.WithRetryNotify(context.Background(), func(provider.RetryInfo) { t.Error("route failure retried") })
	_, err = p.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected endpoint error")
	}
	for _, text := range []string{"custom", "HTTP 400", "/proxy/v1/chat/completions", "OpenAI-compatible", "base_url", reason} {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("endpoint error lacks %q: %v", text, err)
		}
	}
	var apiErr *provider.APIError
	var endpointErr *provider.EndpointError
	if !errors.As(err, &apiErr) || !errors.As(err, &endpointErr) || apiErr.Status != 400 || endpointErr.Protocol != "openai" {
		t.Error("lost endpoint classification or original HTTP diagnostic")
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d, want 1", requests.Load())
	}
}

func TestEndpointNormalizationStream(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"", "/chat/completions"},
		{"/v1///", "/v1/chat/completions"},
		{"/v1/chat/completions", "/v1/chat/completions"},
		{"/v1/chat/completions/", "/v1/chat/completions"},
		{"/v1/responses/", "/v1/chat/completions"},
		{"/proxy/tenant/v1/responses", "/proxy/tenant/v1/chat/completions"},
		{"/proxy/tenant/v1", "/proxy/tenant/v1/chat/completions"},
		{"/api/coding/paas/v4/", "/api/coding/paas/v4/chat/completions"},
		{"/proxy%2Ftenant/v1/chat/completions", "/proxy%2Ftenant/v1/chat/completions"},
		{"/responses/team/chat/completions", "/responses/team/chat/completions"},
		{"/v1/chat/completions-extra", "/v1/chat/completions-extra/chat/completions"},
	} {
		for _, protocol := range []string{"", "deepseek"} {
			t.Run(tc.path+"/protocol="+protocol, func(t *testing.T) {
				var requests atomic.Int32
				var wire chatRequest
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					if r.Method != http.MethodPost || r.RequestURI != tc.want {
						t.Errorf("request = %s %s, want POST %s", r.Method, r.RequestURI, tc.want)
					}
					if r.Header.Get("Authorization") != "Bearer synthetic-key" || r.Header.Get("Content-Type") != "application/json" {
						t.Error("lost request headers")
					}
					if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
						t.Error(err)
					}
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: [DONE]\n\n")
				}))
				defer srv.Close()
				extra := map[string]any{"effort": "high", "reasoning_protocol": protocol}
				if protocol == "deepseek" {
					extra["effort"] = "max"
				}
				p, err := New(provider.Config{
					Name: "deepseek-custom-alias", BaseURL: srv.URL + tc.path,
					Model: "deepseek-v4-flash-vision-exp", APIKey: "synthetic-key", Extra: extra,
				})
				if err != nil {
					t.Fatal(err)
				}
				p.(*client).http = srv.Client()
				ch, err := p.Stream(context.Background(), provider.Request{
					Messages: []provider.Message{
						{Role: provider.RoleAssistant, Content: "prior", ReasoningContent: "original reasoning"},
						{Role: provider.RoleUser, Content: "hello"},
					},
					Tools:     []provider.ToolSchema{{Name: "echo", Parameters: json.RawMessage(`{"type":"object"}`)}},
					MaxTokens: 91234, Temperature: 0.7,
				})
				if err != nil {
					t.Fatal(err)
				}
				for chunk := range ch {
					if chunk.Err != nil {
						t.Fatal(chunk.Err)
					}
				}
				if requests.Load() != 1 || wire.Model != "deepseek-v4-flash-vision-exp" || wire.MaxTokens != 91234 || wire.Temperature != 0.7 || !wire.Stream {
					t.Fatalf("requests=%d wire=%+v", requests.Load(), wire)
				}
				if wire.StreamOptions == nil || !wire.StreamOptions.IncludeUsage || len(wire.Tools) != 1 || len(wire.Messages) != 2 {
					t.Fatalf("lost stream options/tools/messages: %+v", wire)
				}
				if protocol == "deepseek" {
					if wire.Thinking == nil || wire.Thinking.Type != "enabled" || wire.ReasoningEffort != "max" || wire.Messages[0].ReasoningContent == nil || *wire.Messages[0].ReasoningContent != "original reasoning" {
						t.Fatalf("explicit reasoning changed: %+v", wire)
					}
				} else if wire.Thinking != nil || wire.ReasoningEffort != "high" || wire.Messages[0].ReasoningContent != nil {
					t.Fatalf("custom alias inferred DeepSeek or changed effort: %+v", wire)
				}
			})
		}
	}
}
