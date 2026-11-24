package provider

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// --- SanitizeToolPairing ---

// toolIDsAnswered reports whether every assistant tool_call id has a following
// tool message answering it — the contract the OpenAI/DeepSeek API enforces.
func toolIDsAnswered(msgs []Message) bool {
	answered := map[string]bool{}
	for _, m := range msgs {
		if m.Role == RoleTool {
			answered[m.ToolCallID] = true
		}
	}
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if !answered[tc.ID] {
				return false
			}
		}
	}
	return true
}

func TestSanitizeToolPairingBackfillsDanglingCall(t *testing.T) {
	in := []Message{
		{Role: RoleUser, Content: "list files"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "ls"}}},
		{Role: RoleUser, Content: "never mind"},
	}
	out := SanitizeToolPairing(in)
	if !toolIDsAnswered(out) {
		t.Fatalf("dangling tool_call left unanswered: %+v", out)
	}
	// The backfilled result sits right after the assistant turn, keyed to its id.
	if out[2].Role != RoleTool || out[2].ToolCallID != "c1" {
		t.Fatalf("expected a backfilled tool result for c1 at index 2, got %+v", out[2])
	}
}

func TestSanitizeToolPairingKeepsCallOrderAndMultiple(t *testing.T) {
	in := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "a"}, {ID: "b"}, {ID: "c"}}},
		{Role: RoleTool, ToolCallID: "b", Content: "B"}, // out of order, c missing
		{Role: RoleTool, ToolCallID: "a", Content: "A"},
	}
	out := SanitizeToolPairing(in)
	if !toolIDsAnswered(out) {
		t.Fatalf("not all calls answered: %+v", out)
	}
	gotOrder := []string{out[1].ToolCallID, out[2].ToolCallID, out[3].ToolCallID}
	want := []string{"a", "b", "c"}
	for i := range want {
		if gotOrder[i] != want[i] {
			t.Fatalf("tool results out of call order: got %v want %v", gotOrder, want)
		}
	}
}

func TestSanitizeToolPairingDropsOrphanToolMessage(t *testing.T) {
	in := []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleTool, ToolCallID: "ghost", Content: "leftover"}, // no preceding call
		{Role: RoleAssistant, Content: "hello"},
	}
	out := SanitizeToolPairing(in)
	for _, m := range out {
		if m.Role == RoleTool {
			t.Fatalf("orphan tool message survived: %+v", out)
		}
	}
	if len(out) != 2 {
		t.Fatalf("want 2 messages after dropping the orphan, got %d: %+v", len(out), out)
	}
}

func TestSanitizeToolPairingLeavesWellFormedUnchanged(t *testing.T) {
	in := []Message{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "q"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "ls"}}},
		{Role: RoleTool, ToolCallID: "c1", Name: "ls", Content: "main.go"},
		{Role: RoleAssistant, Content: "done"},
	}
	out := SanitizeToolPairing(in)
	if len(out) != len(in) {
		t.Fatalf("well-formed history changed length: %d -> %d", len(in), len(out))
	}
	for i := range in {
		if out[i].Role != in[i].Role || out[i].Content != in[i].Content || out[i].ToolCallID != in[i].ToolCallID {
			t.Fatalf("well-formed message %d mutated: %+v -> %+v", i, in[i], out[i])
		}
	}
}

func TestSanitizeToolPairingClosesTruncatedArgs(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{`, `{}`},
		{`{"time": 2`, `{"time": 2}`},
		{`{"command": "ls -la`, `{"command": "ls -la"}`},
		{`{"a": 1,`, `{"a": 1}`},
		{`{"a":`, `{"a":null}`},
		{`{"path": "C:\\tmp\`, `{"path": "C:\\tmp"}`},
		{`{"items": [1, 2`, `{"items": [1, 2]}`},
		{`total garbage`, `{}`},
		{`{"ok": true}`, `{"ok": true}`},
		{``, ``},
	}
	for _, c := range cases {
		in := []Message{
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "bash", Arguments: c.in}}},
			{Role: RoleTool, ToolCallID: "c1", Content: "r"},
		}
		out := SanitizeToolPairing(in)
		if got := out[0].ToolCalls[0].Arguments; got != c.want {
			t.Errorf("args %q repaired to %q, want %q", c.in, got, c.want)
		}
		if in[0].ToolCalls[0].Arguments != c.in {
			t.Errorf("stored history mutated for %q: %q", c.in, in[0].ToolCalls[0].Arguments)
		}
	}
}

// --- Pricing.Cost ---

func TestPricingCostNil(t *testing.T) {
	var p *Pricing
	if got := p.Cost(&Usage{PromptTokens: 100}); got != 0 {
		t.Errorf("nil Pricing.Cost = %f, want 0", got)
	}
}

func TestPricingCostNilUsage(t *testing.T) {
	p := &Pricing{Input: 2.0, Output: 10.0}
	if got := p.Cost(nil); got != 0 {
		t.Errorf("nil Usage.Cost = %f, want 0", got)
	}
}

func TestPricingCostBothNil(t *testing.T) {
	var p *Pricing
	if got := p.Cost(nil); got != 0 {
		t.Errorf("both nil.Cost = %f, want 0", got)
	}
}

func TestPricingCostCalculation(t *testing.T) {
	p := &Pricing{
		CacheHit: 0.5,  // ¥0.5 per 1M cached tokens
		Input:    2.0,  // ¥2.0 per 1M uncached tokens
		Output:   10.0, // ¥10.0 per 1M completion tokens
	}
	u := &Usage{
		CacheHitTokens:   1_000_000,
		CacheMissTokens:  500_000,
		CompletionTokens: 200_000,
	}
	// Expected: (1M * 0.5 + 500K * 2.0 + 200K * 10.0) / 1M
	//         = (0.5 + 1.0 + 2.0) = 3.5
	got := p.Cost(u)
	if got != 3.5 {
		t.Errorf("Cost = %f, want 3.5", got)
	}
}

func TestPricingCostDerivesCacheMissTokens(t *testing.T) {
	p := &Pricing{CacheHit: 0.02, Input: 1, Output: 2}
	u := &Usage{
		PromptTokens:     1_000,
		CacheHitTokens:   900,
		CacheMissTokens:  0,
		CompletionTokens: 200,
	}
	want := (900*0.02 + 100*1.0 + 200*2.0) / 1_000_000
	if got := p.Cost(u); got != want {
		t.Errorf("Cost = %.8f, want %.8f", got, want)
	}
}

func TestPricingCostZeroTokens(t *testing.T) {
	p := &Pricing{Input: 2.0, Output: 10.0}
	u := &Usage{}
	if got := p.Cost(u); got != 0 {
		t.Errorf("zero tokens Cost = %f, want 0", got)
	}
}

func TestPricingSnapshotAtPeakBoundaries(t *testing.T) {
	offPeak := PricingRates{CacheHit: 0.05, Input: 1.5, Output: 4.5}
	peak := PricingRates{CacheHit: 0.10, Input: 3, Output: 9}
	p := &Pricing{
		CacheHit: offPeak.CacheHit,
		Input:    offPeak.Input,
		Output:   offPeak.Output,
		Currency: "¥",
		Schedule: &PricingSchedule{
			UTCOffsetMinutes: 8 * 60,
			PeakWeekdaysOnly: true,
			PeakWindows: []PricingWindow{
				{StartMinute: 9 * 60, EndMinute: 12 * 60},
				{StartMinute: 14 * 60, EndMinute: 18 * 60},
			},
			Peak: peak, OffPeak: offPeak,
		},
	}
	beijing := time.FixedZone("test-beijing", 8*60*60)
	tests := []struct {
		name string
		hour int
		min  int
		want PricingRates
	}{
		{name: "before morning peak", hour: 8, min: 59, want: offPeak},
		{name: "morning peak starts", hour: 9, min: 0, want: peak},
		{name: "morning peak ends", hour: 12, min: 0, want: offPeak},
		{name: "afternoon peak starts", hour: 14, min: 0, want: peak},
		{name: "afternoon peak ends", hour: 18, min: 0, want: offPeak},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// An arbitrary date proves there is no future effective-date gate.
			at := time.Date(2020, time.January, 2, tt.hour, tt.min, 0, 0, beijing)
			got := p.SnapshotAt(at)
			if got == nil || got.CacheHit != tt.want.CacheHit || got.Input != tt.want.Input || got.Output != tt.want.Output {
				t.Fatalf("SnapshotAt(%s) = %+v, want %+v", at, got, tt.want)
			}
			if got.Schedule != nil {
				t.Fatal("snapshot retained schedule; request pricing could change after start")
			}
		})
	}
	utcPeak := p.SnapshotAt(time.Date(2026, time.August, 17, 1, 0, 0, 0, time.UTC))
	if utcPeak.CacheHit != peak.CacheHit || utcPeak.Input != peak.Input || utcPeak.Output != peak.Output {
		t.Fatalf("01:00 UTC = 09:00 Beijing pricing = %+v, want peak %+v", utcPeak, peak)
	}
}

func TestPricingSnapshotFreezesRequestCostAcrossBoundary(t *testing.T) {
	p := &Pricing{
		Currency: "¥",
		Schedule: &PricingSchedule{
			UTCOffsetMinutes: 8 * 60,
			PeakWeekdaysOnly: true,
			PeakWindows:      []PricingWindow{{StartMinute: 9 * 60, EndMinute: 12 * 60}},
			OffPeak:          PricingRates{CacheHit: 0.05, Input: 1.5, Output: 4.5},
			Peak:             PricingRates{CacheHit: 0.10, Input: 3, Output: 9},
		},
	}
	beijing := time.FixedZone("test-beijing", 8*60*60)
	started := p.SnapshotAt(time.Date(2026, time.August, 17, 8, 59, 59, 0, beijing))
	usage := &Usage{CacheMissTokens: 1_000_000}
	if got := started.Cost(usage); got != 1.5 {
		t.Fatalf("request snapshot cost after crossing boundary = %v, want 1.5", got)
	}
	if got := p.SnapshotAt(time.Date(2026, time.August, 17, 9, 0, 0, 0, beijing)).Cost(usage); got != 3 {
		t.Fatalf("new request at peak cost = %v, want 3", got)
	}
	if got := p.SnapshotAt(time.Date(2026, time.August, 22, 10, 0, 0, 0, beijing)).Cost(usage); got != 1.5 {
		t.Fatalf("Saturday request cost = %v, want off-peak 1.5", got)
	}
}

// --- Pricing.Symbol ---

func TestPricingSymbolDefault(t *testing.T) {
	p := &Pricing{}
	if got := p.Symbol(); got != "¥" {
		t.Errorf("empty Currency.Symbol() = %q, want ¥", got)
	}
}

func TestPricingSymbolNil(t *testing.T) {
	var p *Pricing
	if got := p.Symbol(); got != "¥" {
		t.Errorf("nil.Symbol() = %q, want ¥", got)
	}
}

func TestPricingSymbolCustom(t *testing.T) {
	p := &Pricing{Currency: "$"}
	if got := p.Symbol(); got != "$" {
		t.Errorf("Symbol() = %q, want $", got)
	}
}

// --- AuthError ---

func TestAuthErrorWithKeyEnv(t *testing.T) {
	e := &AuthError{Provider: "deepseek", KeyEnv: "DEEPSEEK_API_KEY", Status: 401}
	msg := e.Error()
	for _, want := range []string{"deepseek", "DEEPSEEK_API_KEY", "401", "invalid or expired"} {
		if !contains(msg, want) {
			t.Errorf("AuthError.Error() missing %q: %s", want, msg)
		}
	}
}

func TestAuthErrorWithoutKeyEnv(t *testing.T) {
	e := &AuthError{Provider: "openai", Status: 403}
	msg := e.Error()
	if !contains(msg, "the API key") {
		t.Errorf("AuthError without KeyEnv should say 'the API key': %s", msg)
	}
	if !contains(msg, "403") {
		t.Errorf("AuthError should include status code 403: %s", msg)
	}
}

func TestAuthErrorImplementsError(t *testing.T) {
	var err error = &AuthError{Provider: "test", Status: 401}
	if err.Error() == "" {
		t.Error("AuthError.Error() should not be empty")
	}
}

// --- Registry ---

func TestRegistryKindsSorted(t *testing.T) {
	// The openai package self-registers via init(); we can't control that here
	// but we can verify Kinds() returns a sorted list.
	kinds := Kinds()
	for i := 1; i < len(kinds); i++ {
		if kinds[i-1] >= kinds[i] {
			t.Errorf("Kinds() not sorted: %v", kinds)
			break
		}
	}
}

func TestNewUnknownKind(t *testing.T) {
	_, err := New("nonexistent-kind-xyzzy", Config{})
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
	if !contains(err.Error(), "unknown kind") {
		t.Errorf("error should mention 'unknown kind': %v", err)
	}
}

func TestNewWithRegisteredKind(t *testing.T) {
	// Register a mock factory.
	kind := "test-mock-__" + t.Name()
	Register(kind, func(cfg Config) (Provider, error) {
		return nil, nil
	})
	t.Cleanup(func() { delete(registry, kind) })
}

func TestNewRejectsTypedNilProvider(t *testing.T) {
	kind := "test-typed-nil-__" + t.Name()
	Register(kind, func(cfg Config) (Provider, error) {
		var p *mockProvider
		return p, nil
	})
	t.Cleanup(func() { delete(registry, kind) })

	_, err := New(kind, Config{})
	if err == nil {
		t.Fatal("New should reject typed nil provider")
	}
	if !contains(err.Error(), "returned nil provider") {
		t.Fatalf("New error = %v, want returned nil provider", err)
	}
}

// --- Role constants ---

func TestRoleConstants(t *testing.T) {
	if RoleSystem != "system" {
		t.Errorf("RoleSystem = %q", RoleSystem)
	}
	if RoleUser != "user" {
		t.Errorf("RoleUser = %q", RoleUser)
	}
	if RoleAssistant != "assistant" {
		t.Errorf("RoleAssistant = %q", RoleAssistant)
	}
	if RoleTool != "tool" {
		t.Errorf("RoleTool = %q", RoleTool)
	}
}

// --- ChunkType constants ---

func TestChunkTypeConstants(t *testing.T) {
	types := []ChunkType{ChunkText, ChunkReasoning, ChunkToolCallStart, ChunkToolCall, ChunkUsage, ChunkDone, ChunkError}
	for i, ct := range types {
		if int(ct) != i {
			t.Errorf("ChunkType %d: got %d", i, int(ct))
		}
	}
}

// --- ToolSchema ---

func TestToolSchemaJSON(t *testing.T) {
	ts := ToolSchema{
		Name:        "bash",
		Description: "Run a shell command",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
	b, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !contains(string(b), "bash") {
		t.Errorf("JSON missing name: %s", b)
	}
}

// helper
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Ensure the Provider interface is satisfied by a minimal mock (compile-time check).
var _ Provider = (*mockProvider)(nil)

type mockProvider struct{}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Stream(ctx context.Context, req Request) (<-chan Chunk, error) {
	ch := make(chan Chunk, 1)
	ch <- Chunk{Type: ChunkDone}
	close(ch)
	return ch, nil
}

func TestMockProviderImplementsInterface(t *testing.T) {
	p := &mockProvider{}
	if p.Name() != "mock" {
		t.Errorf("Name = %q", p.Name())
	}
	ch, err := p.Stream(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := <-ch
	if got.Type != ChunkDone {
		t.Errorf("Chunk.Type = %d, want ChunkDone", got.Type)
	}
}
