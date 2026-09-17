package control

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/i18n"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestExplainEndpointErrorBeforeWrappedAPIError(t *testing.T) {
	previous := i18n.M
	t.Cleanup(func() { i18n.M = previous })
	for _, language := range []string{"en", "zh"} {
		for _, protocol := range []string{"openai", "anthropic"} {
			t.Run(language+"/"+protocol, func(t *testing.T) {
				i18n.DetectLanguage(language)
				path := "/proxy/v1/chat/completions"
				if protocol == "anthropic" {
					path = "/proxy/v1/messages"
				}
				cause := &provider.APIError{Provider: "custom", Status: 400, Body: `{"error":{"type":"unsupported_feature","message":"此推理接口尚未支持"}}`}
				endpointErr := provider.ClassifyEndpointError(cause, protocol, "https://relay.example"+path)
				if endpointErr == nil {
					t.Fatal("expected endpoint classification")
				}
				got := explainError(fmt.Errorf("request: %w", endpointErr)).Error()
				for _, text := range []string{"custom", "HTTP 400", "unsupported_feature", protocol, path, "OpenAI-compatible", "base_url", "此推理接口尚未支持"} {
					if !strings.Contains(got, text) {
						t.Errorf("missing %q: %s", text, got)
					}
				}
				if strings.Contains(got, i18n.M.ProviderErrBadRequest) || strings.Contains(got, "程序缺陷") || strings.Contains(got, "bug") {
					t.Fatalf("route diagnosis replaced by generic bug message: %s", got)
				}
				if protocol == "anthropic" && (!strings.Contains(got, "Anthropic") || !strings.Contains(got, `kind="openai"`)) {
					t.Fatalf("missing protocol selection guidance: %s", got)
				}
			})
		}
	}
}

func TestExplainReasoningHistoryErrorBeforeWrappedAPIError(t *testing.T) {
	previous := i18n.M
	t.Cleanup(func() { i18n.M = previous })
	for _, tc := range []struct {
		language string
		want     string
	}{
		{"zh", "DeepSeek 无法使用这段对话的推理记录。请带上摘要新建对话，原对话会保留。"},
		{"en", "DeepSeek cannot use this conversation's reasoning history. Start a new conversation with a summary; the original is kept."},
	} {
		t.Run(tc.language, func(t *testing.T) {
			i18n.DetectLanguage(tc.language)
			for _, cause := range []error{nil, &provider.APIError{Provider: "test", Status: 400, Body: `{"error":{"message":"invalid reasoning_content at message 7"}}`}, errors.New("stream: missing reasoning_content")} {
				err := fmt.Errorf("request: %w", &provider.ReasoningHistoryError{Provider: "test", MessageIndex: -1, Err: cause})
				if got := explainError(err).Error(); got != tc.want {
					t.Fatalf("localized history error=%q, want %q", got, tc.want)
				}
			}
		})
	}
}

func TestExplainError(t *testing.T) {
	if explainError(nil) != nil {
		t.Error("nil should stay nil")
	}

	bal := explainError(&provider.APIError{Provider: "deepseek", Status: 402, Body: "Insufficient Balance"})
	if bal.Error() != i18n.M.ProviderErrInsufficientBalance {
		t.Errorf("402 = %q, want the insufficient-balance message", bal.Error())
	}

	auth := explainError(&provider.AuthError{Provider: "deepseek", KeyEnv: "DEEPSEEK_API_KEY", Status: 401})
	if !strings.Contains(auth.Error(), "DEEPSEEK_API_KEY") {
		t.Errorf("401 should name the key env: %q", auth.Error())
	}

	for _, status := range []int{400, 422, 429, 500, 503} {
		got := explainError(&provider.APIError{Provider: "p", Status: status})
		if got.Error() == "" || got.Error() == (&provider.APIError{Provider: "p", Status: status}).Error() {
			t.Errorf("status %d should map to a localized message, got %q", status, got.Error())
		}
	}

	jsonBody := explainError(&provider.APIError{Provider: "deepseek", Status: 400, Body: `{"error":{"message":"This model's maximum context length is 65536 tokens.","type":"invalid_request_error"}}`})
	if !strings.Contains(jsonBody.Error(), i18n.M.ProviderErrBadRequest) || !strings.Contains(jsonBody.Error(), "maximum context length") {
		t.Errorf("400 should append the provider reason from a JSON body, got %q", jsonBody.Error())
	}

	rawBody := explainError(&provider.APIError{Provider: "deepseek", Status: 422, Body: "some unparseable detail"})
	if !strings.Contains(rawBody.Error(), "some unparseable detail") {
		t.Errorf("422 should fall back to the raw body, got %q", rawBody.Error())
	}

	noLeak := explainError(&provider.APIError{Provider: "deepseek", Status: 429, Body: `{"error":{"message":"slow down"}}`})
	if noLeak.Error() != i18n.M.ProviderErrRateLimited {
		t.Errorf("429 body must not leak into the message, got %q", noLeak.Error())
	}

	plain := errors.New("some other failure")
	if explainError(plain) != plain {
		t.Error("unknown errors should pass through unchanged")
	}
}
