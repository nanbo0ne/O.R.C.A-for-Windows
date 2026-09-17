package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type modelFetchStatusError struct {
	status int
	body   string
}

func (e modelFetchStatusError) Error() string {
	return fmt.Sprintf("fetch models: status %d: %s", e.status, strings.TrimSpace(e.body))
}

// IsModelFetchEndpointMiss reports whether a model-list request reached a
// plausible endpoint path that the provider does not implement.
func IsModelFetchEndpointMiss(err error) bool {
	var statusErr modelFetchStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.status == http.StatusNotFound || statusErr.status == http.StatusMethodNotAllowed
}

type ModelMetadata struct {
	ID                     string
	ContextWindow          int
	ContextWindowConfirmed bool
	ContextReason          string
	Vision                 *bool
	VisionReason           string
	ToolUse                *bool
	ToolUseReason          string
	StructuredOutput       *bool
	StructuredOutputReason string
	Pricing                *provider.Pricing
	PricingReason          string
}

// FetchModels calls the OpenAI-compatible GET /models endpoint and returns the
// available model IDs.
func FetchModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	items, err := FetchModelMetadata(ctx, baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids, nil
}

// FetchModelMetadata retains optional modality hints exposed by compatible
// gateways while keeping FetchModels backward compatible for existing callers.
func FetchModelMetadata(ctx context.Context, baseURL, apiKey string) ([]ModelMetadata, error) {
	u, err := provider.ParseEndpointURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	endpoint := strings.TrimRight(u.String(), "/")
	if !strings.HasSuffix(strings.TrimRight(u.EscapedPath(), "/"), "/models") {
		endpoint, err = NormalizeBaseURL(baseURL)
		if err != nil {
			return nil, fmt.Errorf("fetch models: %w", err)
		}
		endpoint += "/models"
	}
	return FetchModelMetadataURL(ctx, endpoint, apiKey)
}

// FetchModelMetadataURL requests an explicit catalog URL without changing its
// path or query. Base URL callers should use FetchModelMetadata instead.
func FetchModelMetadataURL(ctx context.Context, endpoint, apiKey string) ([]ModelMetadata, error) {
	u, err := provider.ParseRequestURL(endpoint)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	cli := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("fetch models: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := cli.Do(req)
	if err != nil {
		if requestErr, ok := err.(*url.Error); ok {
			err = requestErr.Err
		}
		return nil, fmt.Errorf("fetch models: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, fmt.Errorf("fetch models: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, modelFetchStatusError{status: resp.StatusCode, body: truncateFetchBody(string(body))}
	}

	var result struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("fetch models: decode response: %w", err)
	}

	items := make([]ModelMetadata, 0, len(result.Data))
	for _, m := range result.Data {
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		contextWindow, contextReason := modelContextMetadata(m)
		vision, visionReason := modelVisionMetadata(m)
		toolUse, toolUseReason := modelBooleanCapability(m, []string{"tool_calling", "tools", "function_calling"})
		structured, structuredReason := modelBooleanCapability(m, []string{"structured_output", "structured_outputs", "json_schema"})
		pricing, pricingReason := modelPricingMetadata(m)
		items = append(items, ModelMetadata{
			ID: id, ContextWindow: contextWindow, ContextWindowConfirmed: contextWindow > 0, ContextReason: contextReason,
			Vision: vision, VisionReason: visionReason, ToolUse: toolUse, ToolUseReason: toolUseReason,
			StructuredOutput: structured, StructuredOutputReason: structuredReason, Pricing: pricing, PricingReason: pricingReason,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func modelContextMetadata(raw map[string]any) (int, string) {
	for _, key := range []string{"context_length", "context_window", "max_context_length", "max_model_len"} {
		if value := positiveInt(raw[key]); value > 0 {
			return value, key
		}
	}
	for _, parent := range []string{"architecture", "capabilities", "limits"} {
		nested, ok := raw[parent].(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"context_length", "context_window", "max_context_length", "max_model_len", "context"} {
			if value := positiveInt(nested[key]); value > 0 {
				return value, parent + "." + key
			}
		}
	}
	return 0, ""
}

func modelBooleanCapability(raw map[string]any, keys []string) (*bool, string) {
	for _, key := range keys {
		if value, ok := raw[key].(bool); ok {
			return boolPtr(value), key
		}
	}
	if capabilities, ok := raw["capabilities"].(map[string]any); ok {
		for _, key := range keys {
			if value, ok := capabilities[key].(bool); ok {
				return boolPtr(value), "capabilities." + key
			}
		}
	}
	if parameters, ok := stringList(raw["supported_parameters"]); ok {
		for _, key := range keys {
			if containsString(parameters, key) {
				return boolPtr(true), "supported_parameters"
			}
		}
	}
	return nil, ""
}

func modelPricingMetadata(raw map[string]any) (*provider.Pricing, string) {
	pricing, ok := raw["pricing"].(map[string]any)
	if !ok {
		return nil, ""
	}
	input, inputOK := perMillionPrice(firstMapValue(pricing, "prompt", "input"))
	output, outputOK := perMillionPrice(firstMapValue(pricing, "completion", "output"))
	cacheHit, cacheOK := perMillionPrice(firstMapValue(pricing, "input_cache_read", "cache_read", "cache_hit"))
	if !inputOK && !outputOK && !cacheOK {
		return nil, ""
	}
	currency, _ := pricing["currency"].(string)
	currency = strings.TrimSpace(currency)
	if currency == "" {
		currency = "$"
	}
	return &provider.Pricing{CacheHit: cacheHit, Input: input, Output: output, Currency: currency}, "pricing"
}

func firstMapValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func perMillionPrice(value any) (float64, bool) {
	var amount float64
	switch typed := value.(type) {
	case float64:
		amount = typed
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		amount = parsed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		amount = parsed
	default:
		return 0, false
	}
	if amount < 0 {
		return 0, false
	}
	return amount * 1_000_000, true
}

func positiveInt(value any) int {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return int(typed)
		}
	case json.Number:
		if parsed, err := typed.Int64(); err == nil && parsed > 0 {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil && parsed > 0 {
			return int(parsed)
		}
	}
	return 0
}

func modelVisionMetadata(raw map[string]any) (*bool, string) {
	for _, key := range []string{"input_modalities", "supported_input_modalities", "modalities"} {
		if values, ok := stringList(raw[key]); ok {
			return boolPtr(containsVisionModality(values)), key
		}
	}
	if architecture, ok := raw["architecture"].(map[string]any); ok {
		if values, ok := stringList(architecture["input_modalities"]); ok {
			return boolPtr(containsVisionModality(values)), "architecture.input_modalities"
		}
	}
	if capabilities, ok := raw["capabilities"].(map[string]any); ok {
		for _, key := range []string{"vision", "image", "multimodal"} {
			if value, ok := capabilities[key].(bool); ok {
				return boolPtr(value), "capabilities." + key
			}
		}
	}
	return nil, ""
}

func stringList(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, strings.ToLower(strings.TrimSpace(text)))
		}
	}
	return out, true
}

func containsVisionModality(values []string) bool {
	for _, value := range values {
		if value == "image" || value == "vision" || value == "image_url" || value == "multimodal" {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func boolPtr(value bool) *bool { return &value }

func truncateFetchBody(body string) string {
	body = strings.TrimSpace(body)
	const max = 512
	if len([]rune(body)) <= max {
		return body
	}
	r := []rune(body)
	return string(r[:max]) + "..."
}
