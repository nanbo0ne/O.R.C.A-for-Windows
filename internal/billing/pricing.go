// Package billing contains provider billing rules shared by configuration and
// telemetry. Official prices are kept here so changing a provider's tariff
// cannot accidentally alter unrelated provider pricing.
package billing

import (
	"net/url"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// OfficialDeepSeekPricing returns the current built-in schedule for an
// official DeepSeek V4 model. It returns nil for custom gateways, other
// providers, and unknown models so their configured prices remain untouched.
func OfficialDeepSeekPricing(providerName, endpoint, model string) *provider.Pricing {
	switch OfficialDeepSeekModel(providerName, endpoint, model) {
	case "deepseek-flash":
		return deepSeekSchedule(
			provider.PricingRates{CacheHit: 0.02, Input: 1, Output: 4},
			provider.PricingRates{CacheHit: 0.04, Input: 2, Output: 8},
		)
	case "deepseek-v4-pro":
		return deepSeekSchedule(
			provider.PricingRates{CacheHit: 0.15, Input: 4.5, Output: 13.5},
			provider.PricingRates{CacheHit: 0.30, Input: 9, Output: 27},
		)
	default:
		return nil
	}
}

// OfficialDeepSeekModel returns the canonical model ID only for a known model
// on an official provider and endpoint. Legacy Flash IDs now alias V4.1 Flash.
func OfficialDeepSeekModel(providerName, endpoint, model string) string {
	if !officialProviderName(providerName) || !officialEndpoint(endpoint) {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp":
		return "deepseek-flash"
	case "deepseek-v4-pro":
		return "deepseek-v4-pro"
	default:
		return ""
	}
}

func officialProviderName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "deepseek", "deepseek-flash", "deepseek-pro":
		return true
	default:
		return false
	}
}

func deepSeekSchedule(offPeak, peak provider.PricingRates) *provider.Pricing {
	return &provider.Pricing{
		CacheHit: offPeak.CacheHit,
		Input:    offPeak.Input,
		Output:   offPeak.Output,
		Currency: "¥",
		Schedule: &provider.PricingSchedule{
			UTCOffsetMinutes: 8 * 60,
			PeakWeekdaysOnly: true,
			PeakWindows: []provider.PricingWindow{
				{StartMinute: 9 * 60, EndMinute: 12 * 60},
				{StartMinute: 14 * 60, EndMinute: 18 * 60},
			},
			Peak:    peak,
			OffPeak: offPeak,
		},
	}
}

func officialEndpoint(endpoint string) bool {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "api.deepseek.com") || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(endpoint, "#") {
		return false
	}
	path := parsed.EscapedPath()
	return path == "" || path == "/" || path == "/v1" || path == "/v1/"
}

// IsOfficialDeepSeekPricing reports whether p is one of the known official
// DeepSeek per-request snapshots. Historical CNY and USD snapshots remain
// recognized for persisted/in-flight compatibility; they are never repriced.
func IsOfficialDeepSeekPricing(p *provider.Pricing, endpoint string) bool {
	if p == nil || !officialEndpoint(endpoint) {
		return false
	}
	type knownPricing struct {
		currency      string
		cacheHit      float64
		input, output float64
	}
	known := []knownPricing{
		{currency: "¥", cacheHit: 0.02, input: 1, output: 4},
		{currency: "¥", cacheHit: 0.04, input: 2, output: 8},
		{currency: "¥", cacheHit: 0.05, input: 1.5, output: 4.5},
		{currency: "¥", cacheHit: 0.10, input: 3, output: 9},
		{currency: "¥", cacheHit: 0.15, input: 4.5, output: 13.5},
		{currency: "¥", cacheHit: 0.30, input: 9, output: 27},
		{currency: "$", cacheHit: 0.007, input: 0.22, output: 0.66},
		{currency: "$", cacheHit: 0.014, input: 0.44, output: 1.32},
		{currency: "$", cacheHit: 0.022, input: 0.66, output: 1.98},
		{currency: "$", cacheHit: 0.044, input: 1.32, output: 3.96},
	}
	for _, rates := range known {
		if p.Symbol() == rates.currency && p.CacheHit == rates.cacheHit && p.Input == rates.input && p.Output == rates.output {
			return true
		}
	}
	return false
}
