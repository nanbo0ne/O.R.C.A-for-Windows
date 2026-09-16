package billing

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestOfficialDeepSeekPricing(t *testing.T) {
	tests := []struct {
		model             string
		cacheHit, input   float64
		output            float64
		peakCache, peakIn float64
		peakOutput        float64
	}{
		{"deepseek-flash", 0.02, 1, 4, 0.04, 2, 8},
		{"deepseek-v4-flash", 0.02, 1, 4, 0.04, 2, 8},
		{"deepseek-v4-flash-vision-exp", 0.02, 1, 4, 0.04, 2, 8},
		{" DEEPSEEK-FLASH ", 0.02, 1, 4, 0.04, 2, 8},
		{"deepseek-v4-pro", 0.15, 4.5, 13.5, 0.30, 9, 27},
	}
	beijing := time.FixedZone("test-beijing", 8*60*60)
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := OfficialDeepSeekPricing("deepseek", "https://api.deepseek.com", tt.model)
			if p == nil || p.Currency != "¥" || p.CacheHit != tt.cacheHit || p.Input != tt.input || p.Output != tt.output {
				t.Fatalf("off-peak pricing = %+v, want ¥ %.4f/%.4f/%.4f", p, tt.cacheHit, tt.input, tt.output)
			}
			peak := p.SnapshotAt(time.Date(2026, time.August, 17, 15, 0, 0, 0, beijing))
			if peak.CacheHit != tt.peakCache || peak.Input != tt.peakIn || peak.Output != tt.peakOutput {
				t.Fatalf("peak pricing = %+v, want ¥ %.4f/%.4f/%.4f", peak, tt.peakCache, tt.peakIn, tt.peakOutput)
			}
			if peak.Schedule != nil || !IsOfficialDeepSeekPricing(peak, "https://api.deepseek.com") {
				t.Fatalf("peak request snapshot is not recognized: %+v", peak)
			}
			if p.Schedule == nil || p.Schedule.UTCOffsetMinutes != 480 || !p.Schedule.PeakWeekdaysOnly || !reflect.DeepEqual(p.Schedule.PeakWindows, []provider.PricingWindow{{StartMinute: 540, EndMinute: 720}, {StartMinute: 840, EndMinute: 1080}}) {
				t.Fatalf("official schedule = %+v", p.Schedule)
			}
		})
	}
}

func TestOfficialDeepSeekPricingBoundariesAndScope(t *testing.T) {
	beijing := time.FixedZone("test-beijing", 8*60*60)
	p := OfficialDeepSeekPricing("deepseek-flash", "https://api.deepseek.com/v1", "deepseek-v4-flash")
	if p == nil {
		t.Fatal("official pricing is nil")
	}
	for _, tt := range []struct {
		name string
		at   time.Time
		want float64
	}{
		{"morning start", time.Date(2026, time.August, 17, 9, 0, 0, 0, beijing), 2},
		{"morning end", time.Date(2026, time.August, 17, 12, 0, 0, 0, beijing), 1},
		{"afternoon start", time.Date(2026, time.August, 17, 14, 0, 0, 0, beijing), 2},
		{"afternoon end", time.Date(2026, time.August, 17, 18, 0, 0, 0, beijing), 1},
		{"weekend", time.Date(2026, time.August, 22, 10, 0, 0, 0, beijing), 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.SnapshotAt(tt.at).Input; got != tt.want {
				t.Errorf("input rate = %v, want %v", got, tt.want)
			}
		})
	}
	if got := OfficialDeepSeekPricing("other", "https://relay.example/v1", "deepseek-v4-flash"); got != nil {
		t.Fatalf("other provider received official pricing: %+v", got)
	}
	if got := OfficialDeepSeekPricing("relay", "https://relay.example/v1", "deepseek-v4-flash"); got != nil {
		t.Fatalf("custom gateway received official pricing: %+v", got)
	}
	if got := OfficialDeepSeekPricing("other", "https://api.deepseek.com", "deepseek-v4-flash"); got != nil {
		t.Fatalf("other provider received official pricing: %+v", got)
	}
	if got := OfficialDeepSeekPricing("deepseek", "http://api.deepseek.com", "deepseek-v4-flash"); got != nil {
		t.Fatalf("insecure endpoint received official pricing: %+v", got)
	}
}

func TestOfficialDeepSeekModelAliasesAndIdentity(t *testing.T) {
	for _, name := range []string{"deepseek", "deepseek-flash", "deepseek-pro", " DEEPSEEK "} {
		for _, endpoint := range []string{"https://api.deepseek.com", "https://api.deepseek.com/", "https://api.deepseek.com/v1", "https://api.deepseek.com/v1/", " HTTPS://API.DEEPSEEK.COM/v1 "} {
			for _, model := range []string{"deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp", " DEEPSEEK-FLASH "} {
				if got := OfficialDeepSeekModel(name, endpoint, model); got != "deepseek-flash" {
					t.Errorf("canonical model for %q/%q/%q = %q", name, endpoint, model, got)
				}
				if p := OfficialDeepSeekPricing(name, endpoint, model); p == nil || p.Input != 1 {
					t.Errorf("alias pricing for %q/%q/%q = %+v", name, endpoint, model, p)
				}
			}
		}
	}
	for _, tt := range []struct{ name, endpoint, model string }{
		{"custom", "https://api.deepseek.com", "deepseek-flash"},
		{"deepseek-custom", "https://api.deepseek.com", "deepseek-flash"},
		{"", "https://api.deepseek.com", "deepseek-flash"},
		{"deepseek", "https://api.deepseek.com", "deepseek-flash-custom"},
		{"deepseek", "https://api.deepseek.com", "deepseek/deepseek-flash"},
		{"deepseek", "https://api.deepseek.com", "deepseek-chat"},
		{"deepseek", "https://api.deepseek.com", ""},
	} {
		if got := OfficialDeepSeekModel(tt.name, tt.endpoint, tt.model); got != "" {
			t.Errorf("non-official identity %+v recognized as %q", tt, got)
		}
		if got := OfficialDeepSeekPricing(tt.name, tt.endpoint, tt.model); got != nil {
			t.Errorf("non-official identity %+v received pricing %+v", tt, got)
		}
	}
	known := &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 4, Currency: "¥"}
	for _, endpoint := range []string{
		"", "http://api.deepseek.com", "https://relay.example/v1",
		"https://api.deepseek.com.evil.example", "https://deepseek.com",
		"https://api.deepseek.com:443", "https://api.deepseek.com.",
		"https://user@api.deepseek.com", "https://api.deepseek.com@relay.example",
		"https://api.deepseek.com/v2", "https://api.deepseek.com/v1/chat/completions",
		"https://api.deepseek.com//v1", "https://api.deepseek.com/%76%31",
		"https://api.deepseek.com?", "https://api.deepseek.com?route=custom",
		"https://api.deepseek.com#", "https://api.deepseek.com#custom", "://invalid",
	} {
		t.Run(endpoint, func(t *testing.T) {
			if got := OfficialDeepSeekModel("deepseek", endpoint, "deepseek-flash"); got != "" {
				t.Errorf("non-official endpoint recognized as %q", got)
			}
			if got := OfficialDeepSeekPricing("deepseek", endpoint, "deepseek-flash"); got != nil {
				t.Errorf("non-official endpoint received pricing: %+v", got)
			}
			if IsOfficialDeepSeekPricing(known, endpoint) {
				t.Error("non-official endpoint accepted as official receipt")
			}
		})
	}
}

func TestOfficialDeepSeekScheduleWeekdaysBoundariesAndRequestFreezing(t *testing.T) {
	beijing := time.FixedZone("test-beijing", 8*60*60)
	monday := time.Date(2026, time.September, 14, 0, 0, 0, 0, beijing)
	for _, model := range []string{"deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp", "deepseek-v4-pro"} {
		t.Run(model, func(t *testing.T) {
			p := OfficialDeepSeekPricing("deepseek", "https://api.deepseek.com", model)
			for day := 0; day < 7; day++ {
				for _, hour := range []int{9, 12, 14, 18} {
					boundary := monday.AddDate(0, 0, day).Add(time.Duration(hour) * time.Hour)
					for _, delta := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
						at := boundary.Add(delta)
						localHour := at.Hour()
						peak := day < 5 && (localHour >= 9 && localHour < 12 || localHour >= 14 && localHour < 18)
						want := p.Schedule.OffPeak
						if peak {
							want = p.Schedule.Peak
						}
						// Feed UTC instants to catch weekday and clock conversion regressions.
						got := p.SnapshotAt(at.UTC())
						if got.CacheHit != want.CacheHit || got.Input != want.Input || got.Output != want.Output || got.Schedule != nil {
							t.Fatalf("snapshot at %s = %+v, want %+v", at, got, want)
						}
						before := *got
						if later := got.SnapshotAt(at.Add(24 * time.Hour)); *later != before || *got != before {
							t.Fatalf("request rates changed across boundary: before=%+v after=%+v", before, later)
						}
					}
				}
			}
		})
	}
}

func TestIsOfficialDeepSeekPricingPreservesRecordedReceipts(t *testing.T) {
	for _, tt := range []struct {
		name, currency string
		hit, miss, out float64
		cost           float64
	}{
		{"Flash current off-peak CNY", "¥", 0.02, 1, 4, 5.02},
		{"Flash current peak CNY", "¥", 0.04, 2, 8, 10.04},
		{"Flash old off-peak CNY", "¥", 0.05, 1.5, 4.5, 6.05},
		{"Flash old peak CNY", "¥", 0.10, 3, 9, 12.10},
		{"Flash old default currency", "", 0.05, 1.5, 4.5, 6.05},
		{"Pro off-peak CNY", "¥", 0.15, 4.5, 13.5, 18.15},
		{"Pro peak CNY", "¥", 0.30, 9, 27, 36.30},
		{"Flash old off-peak USD", "$", 0.007, 0.22, 0.66, 0.887},
		{"Flash old peak USD", "$", 0.014, 0.44, 1.32, 1.774},
		{"Pro old off-peak USD", "$", 0.022, 0.66, 1.98, 2.662},
		{"Pro old peak USD", "$", 0.044, 1.32, 3.96, 5.324},
	} {
		t.Run(tt.name, func(t *testing.T) {
			type receipt struct {
				Pricing *provider.Pricing
				Usage   *provider.Usage
				Cost    float64
			}
			original := receipt{
				Pricing: &provider.Pricing{CacheHit: tt.hit, Input: tt.miss, Output: tt.out, Currency: tt.currency},
				Usage:   &provider.Usage{PromptTokens: 2_000_000, CacheHitTokens: 1_000_000, CacheMissTokens: 1_000_000, CompletionTokens: 1_000_000},
				Cost:    tt.cost,
			}
			data, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var saved receipt
			if err := json.Unmarshal(data, &saved); err != nil {
				t.Fatal(err)
			}
			if !IsOfficialDeepSeekPricing(saved.Pricing, "https://api.deepseek.com/v1/") {
				t.Fatal("recorded official pricing was rejected")
			}
			_ = OfficialDeepSeekPricing("deepseek", "https://api.deepseek.com", "deepseek-flash")
			if got := saved.Pricing.Cost(saved.Usage); math.Abs(got-tt.cost) > 1e-10 {
				t.Fatalf("recorded receipt cost = %v, want %v", got, tt.cost)
			}
			after, err := json.Marshal(saved)
			if err != nil || !bytes.Equal(data, after) {
				t.Fatalf("recorded receipt changed: before=%s after=%s err=%v", data, after, err)
			}
		})
	}
	for _, p := range []*provider.Pricing{
		nil,
		{CacheHit: 0.02, Input: 1, Output: 4, Currency: "$"},
		{CacheHit: 0.02, Input: 1, Output: 4.5, Currency: "¥"},
		{CacheHit: 0.05, Input: 1.5, Output: 9, Currency: "¥"},
	} {
		if IsOfficialDeepSeekPricing(p, "https://api.deepseek.com") {
			t.Errorf("unknown rates accepted as official: %+v", p)
		}
	}
}
