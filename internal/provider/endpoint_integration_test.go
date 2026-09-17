package provider_test

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

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/netclient"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	_ "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/anthropic"
	_ "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/openai"
)

func TestEndpointErrorsKeepAuthAndOtherReasonsWithoutReplay(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic"} {
		for _, tc := range []struct {
			name, body string
			status     int
		}{
			{"authentication", `{"error":{"type":"unsupported_feature","message":"unsupported endpoint"}}`, 401},
			{"authorization", `{"error":{"code":"unsupported_endpoint","message":"do not expose auth reason"}}`, 403},
			{"effort", `{"error":{"type":"unsupported_feature","message":"reasoning_effort xhigh is not supported"}}`, 400},
			{"tools", `{"error":{"code":"unsupported_feature","message":"tools are not supported"}}`, 400},
			{"upstream", `{"error":{"type":"upstream_error","message":"\u6b64\u63a8\u7406\u63a5\u53e3\u5c1a\u672a\u652f\u6301"}}`, 400},
			{"context", `{"error":{"message":"maximum context length exceeded"}}`, 422},
			{"model", `{"error":{"code":"model_not_found","message":"model not found"}}`, 404},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					path := "/proxy/v1/chat/completions"
					if kind == "anthropic" {
						path = "/proxy/v1/messages"
					}
					if r.Method != http.MethodPost || r.RequestURI != path {
						t.Errorf("unexpected fallback: %s %s", r.Method, r.RequestURI)
					}
					var wire map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
						t.Error(err)
					}
					if string(wire["model"]) != `"deepseek-private-alias"` || string(wire["max_tokens"]) != "91234" {
						t.Errorf("request was rewritten: %v", wire)
					}
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.body)
				}))
				defer srv.Close()
				p, err := provider.New(kind, provider.Config{
					Name: "custom", BaseURL: srv.URL + "/proxy/v1", Model: "deepseek-private-alias", APIKey: "synthetic-key",
					Extra: map[string]any{"api_key_env": "SYNTHETIC_KEY", "proxy_spec": netclient.ProxySpec{Mode: netclient.ModeOff}},
				})
				if err != nil {
					t.Fatal(err)
				}
				ctx := provider.WithRetryNotify(context.Background(), func(provider.RetryInfo) { t.Error("non-retryable error retried") })
				_, err = p.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}}, MaxTokens: 91234})
				var endpointErr *provider.EndpointError
				if err == nil || errors.As(err, &endpointErr) || calls.Load() != 1 {
					t.Fatalf("error=%v calls=%d", err, calls.Load())
				}
				if tc.status == 401 || tc.status == 403 {
					var authErr *provider.AuthError
					if !errors.As(err, &authErr) || authErr.KeyEnv != "SYNTHETIC_KEY" || strings.Contains(err.Error(), tc.body) {
						t.Fatalf("lost safe authentication diagnostic: %v", err)
					}
				} else {
					var apiErr *provider.APIError
					if !errors.As(err, &apiErr) || apiErr.Body != tc.body || apiErr.Status != tc.status {
						t.Fatalf("original failure changed: %v", err)
					}
				}
			})
		}
	}
}
