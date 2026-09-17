package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/i18n"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// explainError maps a provider HTTP failure to an actionable, localized message
// so the turn-done error the UI shows is never a bare status code or silent
// failure. Unknown errors (and nil) pass through unchanged.
func explainError(err error) error {
	if err == nil {
		return nil
	}
	var endpointErr *provider.EndpointError
	if errors.As(err, &endpointErr) {
		if i18n.M == i18n.Chinese {
			hint := "请核对服务商的 OpenAI-compatible base_url 和 Chat Completions 路径。"
			if endpointErr.Protocol == "anthropic" {
				hint = "当前选择的是 Anthropic Messages。若服务商仅提供 OpenAI 兼容接口，请在设置中选择 OpenAI-compatible (kind=\"openai\") 并填写对应的 base_url；若应使用 Anthropic，请核对 Messages 接口路径。"
			}
			return fmt.Errorf("服务商 %q 报告当前接口或协议不受支持 (HTTP %d, %s)：已选择 kind=%q，请求 POST %s。\n%s 不会自动切换协议或重发请求。\n服务端原因：%s",
				endpointErr.Provider, endpointErr.Status, endpointErr.Code, endpointErr.Protocol, endpointErr.Path, hint, endpointErr.Reason)
		}
		return errors.New(endpointErr.Error())
	}
	// This wrapper also contains an APIError; handle it before the generic HTTP
	// mapping so the recovery guidance is not replaced by the raw server body.
	var historyErr *provider.ReasoningHistoryError
	if errors.As(err, &historyErr) {
		if i18n.M == i18n.Chinese {
			return errors.New("DeepSeek 无法使用这段对话的推理记录。请带上摘要新建对话，原对话会保留。")
		}
		return errors.New(historyErr.Error())
	}
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		msg := i18n.M.ProviderStatusMessage(apiErr.Status)
		if msg == "" {
			return err
		}
		if reason := requestErrorReason(apiErr); reason != "" {
			if looksLikeVisionError(reason) {
				return fmt.Errorf("%s\n%s\n%s", msg, reason, i18n.M.ProviderErrVisionUnsupported)
			}
			return fmt.Errorf("%s\n%s", msg, reason)
		}
		return errors.New(msg)
	}
	var authErr *provider.AuthError
	if errors.As(err, &authErr) {
		msg := i18n.M.ProviderStatusMessage(authErr.Status)
		if msg == "" {
			return err
		}
		if authErr.KeyEnv != "" {
			return fmt.Errorf("%s (%s)", msg, authErr.KeyEnv)
		}
		return errors.New(msg)
	}
	return err
}

func looksLikeVisionError(reason string) bool {
	lower := strings.ToLower(reason)
	for _, marker := range []string{"image_url", "image input", "image content", "vision", "multimodal", "media_type"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// requestErrorReason returns the provider's verbatim reason for request-shaped
// 4xx (400/422) — the localized line names the category, the body names the
// actual cause (context-length exceeded, unpaired tool_calls). Empty otherwise.
func requestErrorReason(e *provider.APIError) string {
	if e.Status != 400 && e.Status != 422 {
		return ""
	}
	return providerBodyReason(e.Body)
}

// providerBodyReason pulls the human reason from an OpenAI/Anthropic-shaped error
// body ({"error":{"message":…}}), falling back to the trimmed raw body.
func providerBodyReason(body string) string {
	if body == "" {
		return ""
	}
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &parsed) == nil && parsed.Error.Message != "" {
		return clampRunes(parsed.Error.Message, 800)
	}
	return clampRunes(body, 800)
}

func clampRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
