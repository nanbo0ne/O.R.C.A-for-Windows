package config

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBuildModelFetchURLsFullEndpoints(t *testing.T) {
	for _, tc := range []struct{ base, want string }{
		{"https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/models"},
		{"https://relay.example/proxy/v1/responses/", "https://relay.example/proxy/v1/models"},
		{"https://relay.example/proxy%2Ftenant/v1/chat/completions/", "https://relay.example/proxy%2Ftenant/v1/models"},
	} {
		got, err := BuildModelFetchURLs(tc.base, "")
		if err != nil || !reflect.DeepEqual(got, []string{tc.want}) {
			t.Errorf("BuildModelFetchURLs(%s) = %v, %v; want %s", tc.base, got, err, tc.want)
		}
	}
}

func TestBuildModelFetchURLsRejectsUnsafeURLs(t *testing.T) {
	for _, raw := range []string{
		"/v1", "ftp://relay.example/v1", "https:///v1", "https://user:secret-value@relay.example/v1",
		"https://relay.example/v1?token=secret-value", "https://relay.example/v1#secret-value",
	} {
		for _, override := range []bool{false, true} {
			if override && strings.Contains(raw, "?") {
				continue
			}
			base, models := raw, ""
			if override {
				base, models = "https://relay.example/v1", raw
			}
			_, err := BuildModelFetchURLs(base, models)
			if err == nil {
				t.Errorf("unsafe URL accepted (override=%v): %s", override, raw)
			} else if strings.Contains(err.Error(), "secret-value") {
				t.Errorf("URL credential leaked: %v", err)
			}
		}
	}
}

func TestFetchModelsPreservesQueryOverride(t *testing.T) {
	for _, path := range []string{"/proxy?path=/v1/models", "/v1/models?limit=20&cursor=a%2Fb"} {
		t.Run(path, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet || r.RequestURI != path {
					t.Errorf("unexpected request %s %s", r.Method, r.RequestURI)
				}
				fmt.Fprint(w, `{"data":[{"id":"query-model"}]}`)
			}))
			defer srv.Close()
			t.Setenv("ENDPOINT_QUERY_KEY", "synthetic-key")
			entry := ProviderEntry{Name: "query-provider", BaseURL: srv.URL + "/v1", ModelsURL: srv.URL + path, APIKeyEnv: "ENDPOINT_QUERY_KEY"}
			models, err := entry.FetchModels(context.Background())
			if err != nil || !reflect.DeepEqual(models, []string{"query-model"}) || calls.Load() != 1 {
				t.Fatalf("models=%v err=%v calls=%d", models, err, calls.Load())
			}
		})
	}
}

func TestFetchModelsFullEndpointUsesSiblingModels(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.RequestURI != "/proxy/v1/models" || r.Header.Get("Authorization") != "Bearer synthetic-key" {
			t.Errorf("unexpected request: %s %s", r.Method, r.RequestURI)
		}
		fmt.Fprint(w, `{"data":[{"id":"custom-alias"}]}`)
	}))
	defer srv.Close()
	t.Setenv("ENDPOINT_TEST_API_KEY", "synthetic-key")
	p := ProviderEntry{Name: "custom", BaseURL: srv.URL + "/proxy/v1/chat/completions/", APIKeyEnv: "ENDPOINT_TEST_API_KEY"}
	got, err := p.FetchModels(context.Background())
	if err != nil || !reflect.DeepEqual(got, []string{"custom-alias"}) || calls.Load() != 1 {
		t.Fatalf("models=%v err=%v calls=%d", got, err, calls.Load())
	}
}

func TestFetchModelsDoesNotProbeAfterNonRouteFailures(t *testing.T) {
	for _, status := range []int{400, 401, 403, 422, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.RequestURI != "/proxy/models" {
					t.Errorf("non-route error caused a probe: %s", r.RequestURI)
				}
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":{"code":"upstream_error","message":"original reason"}}`)
			}))
			defer srv.Close()
			t.Setenv("ENDPOINT_TEST_API_KEY", "synthetic-key")
			p := ProviderEntry{Name: "custom", BaseURL: srv.URL + "/proxy", APIKeyEnv: "ENDPOINT_TEST_API_KEY"}
			_, err := p.FetchModels(context.Background())
			if err == nil || calls.Load() != 1 || !strings.Contains(err.Error(), "original reason") {
				t.Fatalf("error=%v calls=%d", err, calls.Load())
			}
		})
	}
}
