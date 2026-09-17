package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// EndpointError identifies a server-confirmed unsupported route or protocol, not
// an arbitrary bad request. Keep the original HTTP diagnostic available to callers.
type EndpointError struct {
	Provider string
	Protocol string
	Path     string
	Status   int
	Code     string
	Reason   string
	Err      error
}

func (e *EndpointError) Error() string {
	protocol := "OpenAI-compatible Chat Completions"
	hint := "Check the provider's OpenAI-compatible base_url and Chat Completions route."
	if e.Protocol == "anthropic" {
		protocol = "Anthropic Messages"
		hint = "If this provider only offers an OpenAI-compatible endpoint, choose kind=\"openai\" and its base_url in settings; otherwise check the Anthropic Messages route."
	}
	return fmt.Sprintf("provider %q reports an unsupported endpoint (HTTP %d, %s): selected %s (kind=%q), POST %s. %s No automatic protocol switch or replay. Server reason: %s",
		e.Provider, e.Status, e.Code, protocol, e.Protocol, e.Path, hint, e.Reason)
}

func (e *EndpointError) Unwrap() error { return e.Err }

// ClassifyEndpointError requires structured, route-specific evidence. A generic
// unsupported feature (tools, images, effort, etc.) must retain its original error.
func ClassifyEndpointError(err error, protocol, endpoint string) *EndpointError {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return nil
	}
	switch apiErr.Status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnprocessableEntity:
	default:
		return nil
	}
	type detail struct {
		Code    string `json:"code"`
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	var body struct {
		detail
		Error *detail `json:"error"`
	}
	if json.Unmarshal([]byte(apiErr.Body), &body) != nil {
		return nil
	}
	d := body.detail
	if body.Error != nil {
		d = *body.Error
	}
	code := strings.ToLower(strings.TrimSpace(d.Code))
	if code == "" {
		code = strings.ToLower(strings.TrimSpace(d.Type))
	}
	switch code {
	case "unsupported_endpoint", "unsupported_route", "unsupported_protocol":
	case "unsupported_feature":
		message := strings.ToLower(d.Message)
		matched := false
		for _, marker := range []string{
			"\u6b64\u63a8\u7406\u63a5\u53e3\u5c1a\u672a\u652f\u6301",
			"unsupported endpoint", "unsupported route", "unsupported protocol",
			"endpoint is not supported", "route is not supported", "protocol is not supported",
			"endpoint not supported", "route not supported", "protocol not supported",
		} {
			if strings.Contains(message, marker) {
				matched = true
				break
			}
		}
		if !matched {
			return nil
		}
	default:
		return nil
	}
	u, parseErr := url.Parse(endpoint)
	if parseErr != nil {
		return nil
	}
	return &EndpointError{
		Provider: apiErr.Provider, Protocol: protocol, Path: u.EscapedPath(),
		Status: apiErr.Status, Code: code, Reason: strings.TrimSpace(d.Message), Err: err,
	}
}
