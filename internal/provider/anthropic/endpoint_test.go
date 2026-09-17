package anthropic

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

func TestEndpointNormalizationStream(t *testing.T) {
	for _, path := range []string{"/proxy", "/proxy/v1/", "/proxy/v1/messages/"} {
		t.Run(path, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.RequestURI != "/proxy/v1/messages" || r.Header.Get("x-api-key") != "synthetic-key" {
					t.Errorf("unexpected request: %s %s", r.Method, r.RequestURI)
				}
				var wire anthRequest
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Error(err)
				}
				if wire.Model != "custom-alias" || wire.MaxTokens != 91234 || wire.Thinking == nil || wire.OutputConfig == nil || wire.OutputConfig.Effort != "max" {
					t.Errorf("payload changed: %+v", wire)
				}
				fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
			}))
			defer srv.Close()
			p, err := New(provider.Config{
				BaseURL: srv.URL + path, Model: "custom-alias", APIKey: "synthetic-key",
				Extra: map[string]any{"thinking": "adaptive", "effort": "max"},
			})
			if err != nil {
				t.Fatal(err)
			}
			p.(*client).http = srv.Client()
			ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}}, MaxTokens: 91234})
			if err != nil {
				t.Fatal(err)
			}
			for chunk := range ch {
				if chunk.Err != nil {
					t.Fatal(chunk.Err)
				}
			}
		})
	}
}

func TestEndpointRejectionIsActionableWithoutProtocolFallback(t *testing.T) {
	const reason = "\u6b64\u63a8\u7406\u63a5\u53e3\u5c1a\u672a\u652f\u6301"
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected protocol fallback: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"type": "unsupported_feature", "message": reason}})
	}))
	defer srv.Close()
	p, err := New(provider.Config{Name: "custom", BaseURL: srv.URL + "/v1", Model: "custom-alias"})
	if err != nil {
		t.Fatal(err)
	}
	p.(*client).http = srv.Client()
	ctx := provider.WithRetryNotify(context.Background(), func(provider.RetryInfo) { t.Error("route failure retried") })
	_, err = p.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected endpoint error")
	}
	for _, text := range []string{"custom", "HTTP 400", "/v1/messages", "Anthropic", "OpenAI-compatible", "openai", reason} {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("endpoint error lacks %q: %v", text, err)
		}
	}
	var apiErr *provider.APIError
	var endpointErr *provider.EndpointError
	if !errors.As(err, &apiErr) || !errors.As(err, &endpointErr) || apiErr.Status != 400 || endpointErr.Protocol != "anthropic" {
		t.Error("lost endpoint classification or original HTTP diagnostic")
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d, want 1", requests.Load())
	}
}

func TestEndpointValidationBeforeRequest(t *testing.T) {
	for _, raw := range []string{
		"relay.example/v1", "ftp://relay.example/v1", "https:///v1",
		"https://user:secret-value@relay.example/v1", "https://relay.example/v1?token=secret-value", "https://relay.example/v1#secret-value",
		"http://localhost:1234/api/v1/chat", "https://relay.example/v1/chat/completions", "https://relay.example/v1/responses",
	} {
		_, err := New(provider.Config{BaseURL: raw, Model: "m"})
		if err == nil {
			t.Errorf("invalid/incompatible endpoint accepted: %s", raw)
		} else if strings.Contains(err.Error(), "secret-value") {
			t.Errorf("URL credential leaked: %v", err)
		}
	}
}
