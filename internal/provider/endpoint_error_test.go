package provider

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestClassifyEndpointErrorRequiresStructuredEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantCode   string
	}{
		{"nested route code", `{"error":{"code":"unsupported_endpoint","message":"not available"}}`, 400, "unsupported_endpoint"},
		{"root route code", `{"code":"unsupported_route","message":"not available"}`, 404, "unsupported_route"},
		{"protocol code", `{"error":{"type":"unsupported_protocol"}}`, 422, "unsupported_protocol"},
		{"feature with explicit route", `{"error":{"type":"unsupported_feature","message":"This endpoint is not supported"}}`, 400, "unsupported_feature"},
		{"escaped route reason", `{"error":{"code":"unsupported_feature","message":"\u6b64\u63a8\u7406\u63a5\u53e3\u5c1a\u672a\u652f\u6301"}}`, 400, "unsupported_feature"},
		{"upstream code wins", `{"error":{"code":"upstream_error","type":"unsupported_feature","message":"unsupported endpoint"}}`, 400, ""},
		{"upstream type", `{"error":{"type":"upstream_error","message":"unsupported endpoint"}}`, 400, ""},
		{"unsupported tools", `{"error":{"type":"unsupported_feature","message":"tools are not supported"}}`, 400, ""},
		{"unsupported effort", `{"error":{"code":"unsupported_feature","message":"reasoning_effort xhigh is not supported"}}`, 400, ""},
		{"unsupported image", `{"error":{"type":"unsupported_feature","message":"image input is not supported"}}`, 400, ""},
		{"unknown unsupported feature", `{"error":{"type":"unsupported_feature"}}`, 400, ""},
		{"bare route reason", `unsupported endpoint`, 400, ""},
		{"missing code", `{"error":{"message":"unsupported endpoint"}}`, 400, ""},
		{"missing route", `{"error":{"code":"not_found","message":"model not found"}}`, 404, ""},
		{"invalid JSON", `{"error":{"code":"unsupported_route"`, 400, ""},
		{"non-string code", `{"error":{"code":400,"type":"unsupported_feature","message":"unsupported endpoint"}}`, 400, ""},
		{"auth status", `{"error":{"code":"unsupported_route"}}`, 401, ""},
		{"forbidden status", `{"error":{"code":"unsupported_route"}}`, 403, ""},
		{"rate limited", `{"error":{"code":"unsupported_route"}}`, 429, ""},
		{"upstream status", `{"error":{"code":"unsupported_route"}}`, 502, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := &APIError{Provider: "custom", Status: tc.status, Body: tc.body}
			err := fmt.Errorf("request: %w", cause)
			got := ClassifyEndpointError(err, "anthropic", "https://relay.example/proxy%2Ftenant/v1/messages")
			if tc.wantCode == "" {
				if got != nil {
					t.Fatalf("unconfirmed route failure reclassified: %v", got)
				}
				return
			}
			if got == nil || got.Code != tc.wantCode || got.Status != tc.status || got.Path != "/proxy%2Ftenant/v1/messages" || got.Protocol != "anthropic" || got.Provider != "custom" {
				t.Fatalf("endpoint diagnostic=%+v", got)
			}
			var apiErr *APIError
			if !errors.Is(got, err) || !errors.As(got, &apiErr) || apiErr != cause {
				t.Fatal("original diagnostic was not preserved")
			}
		})
	}
	if ClassifyEndpointError(&AuthError{Status: http.StatusUnauthorized}, "anthropic", "https://relay.example/v1/messages") != nil {
		t.Fatal("authentication error classified as an endpoint error")
	}
}
