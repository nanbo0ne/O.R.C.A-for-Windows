package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestMonitorHiddenDoesNotOpenBody(t *testing.T) {
	r, _ := http.NewRequest("POST", "https://fixture.invalid", strings.NewReader(`{"model":"fixture"}`))
	r.GetBody = func() (io.ReadCloser, error) { t.Fatal("hidden body opened"); return nil, nil }
	ObserveHTTP(context.Background(), r)(200, nil)
	s := New("tab")
	s.Close()
	ctx := WithBinding(context.Background(), Binding{Sink: func() *Store { return s }, Turn: "turn"})
	ObserveHTTP(ctx, r)(200, nil)
}

func TestMonitorStreamingImagesPreservesMetadata(t *testing.T) {
	body := `{"model":"fixture","messages":[{"role":"user","content":[{"type":"text","text":"ordinary prompt"},{"type":"image","source":{"type":"base64","data":"` + strings.Repeat("A", 2<<20) + `"}}]}],"max_tokens":99,"tools":[{"name":"test","parameters":{"type":"object"}}]}`
	data, partial := ScanJSON(strings.NewReader(body))
	if !partial || !json.Valid(data) || !strings.Contains(string(data), "ordinary prompt") || !strings.Contains(string(data), `"max_tokens":99`) || !strings.Contains(string(data), "parameters") || len(data) > 2048 {
		t.Fatalf("metadata lost or image copied: %.2048s", data)
	}
}

func TestMonitorPrivacyAndScanLimits(t *testing.T) {
	body := `{"model":"fixture","messages":[{"reasoning_content":"ORIGINAL_PRIVATE","content":"Bearer SYNTHETIC_AUTH data:image/png;base64,SECRET_IMAGE"}],"arguments":"{\"api_key\":\"NESTED_AUTH\",\"query\":\"keep me\"}","headers":{"authorization":"AUTH"}}`
	data, p := ScanJSON(strings.NewReader(body))
	for _, secret := range []string{"ORIGINAL_PRIVATE", "SYNTHETIC_AUTH", "SECRET_IMAGE", "NESTED_AUTH", `"AUTH"`} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("leak %s", secret)
		}
	}
	if !p || !strings.Contains(string(data), "keep me") {
		t.Fatal(string(data))
	}
	data, p = ScanJSON(strings.NewReader(`{"model":"keep","data":"` + strings.Repeat("A", MaxScan) + `","later":true}`))
	if !p || !json.Valid(data) || !strings.Contains(string(data), "keep") || strings.Contains(string(data), "later") {
		t.Fatalf("scan limit: %s", data)
	}
	for _, body := range []string{`{`, `{"a":`, `{"a":[1,`, `{"a":"broken`, `null`, strings.Repeat("[", 32)} {
		data, p = ScanJSON(strings.NewReader(body))
		if !p || !json.Valid(data) {
			t.Fatalf("invalid partial %q: %s", body, data)
		}
	}
}

func TestMonitorBoundsAndPagination(t *testing.T) {
	s := New("tab")
	defer s.Close()
	for i := 0; i < 110; i++ {
		s.Record("turn", fmt.Sprint(i), "request", "wait-first", func() (json.RawMessage, bool) {
			return json.RawMessage(`{"text":"` + strings.Repeat("x", MaxBody-1000) + `"}`), false
		})
	}
	if s.bytes > MaxBytes || len(s.requests) > MaxRequests || s.evicted == 0 {
		t.Fatalf("limits: %d %d %d", s.bytes, len(s.requests), s.evicted)
	}
	last := uint64(0)
	count := 0
	for {
		v := s.Snapshot(last)
		cost := 0
		for _, e := range v.Entries {
			if e.Seq <= last {
				t.Fatal("unordered")
			}
			last = e.Seq
			cost += len(e.Data) + 512
			count++
		}
		if cost > MaxSnapshot {
			t.Fatal("snapshot limit")
		}
		if len(v.Entries) == 0 {
			break
		}
	}
	if count != len(s.entries) {
		t.Fatal("pagination gap")
	}
	for i := 0; i < 110; i++ {
		s.Record("turn", fmt.Sprint(i), "request", "", nil)
	}
	if len(s.requests) != 100 {
		t.Fatalf("request cap %d", len(s.requests))
	}
}

func TestMonitorNonblockingAndClosedProducer(t *testing.T) {
	s := New("tab")
	defer s.Close()
	s.mu.Lock()
	done := make(chan struct{})
	go func() {
		s.Record("t", "r", "request", "", func() (json.RawMessage, bool) { t.Error("copied contended data"); return nil, false })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked producer")
	}
	s.mu.Unlock()
	if s.Snapshot(0).Dropped != 1 {
		t.Fatal("missing drop marker")
	}
	s.Close()
	s.Record("t", "", "response", "", func() (json.RawMessage, bool) { t.Fatal("copied closed data"); return nil, false })
	if len(s.Snapshot(0).Entries) != 0 {
		t.Fatal("closed data retained")
	}
}

func TestMonitorPhaseLifecycleAndLease(t *testing.T) {
	s := New("tab")
	defer s.Close()
	for _, phase := range []string{"input", "wait-first", "reasoning", "reasoning", "decode", "cancelling", "decode", "stopped", "final"} {
		s.Record("t", "", "phase", phase, nil)
	}
	v := s.Snapshot(0)
	want := []string{"input", "wait-first", "reasoning", "decode", "cancelling", "stopped"}
	if len(v.Entries) != len(want) {
		t.Fatalf("phases %+v", v.Entries)
	}
	for i, e := range v.Entries {
		if e.Phase != want[i] {
			t.Fatal(e.Phase)
		}
	}
	s.mu.Lock()
	s.expires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	s.expire()
	if !s.Snapshot(0).Expired || s.bytes != 0 {
		t.Fatal("lease retained capture")
	}
}

func TestMonitorConcurrentSnapshotClose(t *testing.T) {
	s := New("tab")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 500; n++ {
				s.Record("t", "", "response", "", nil)
				s.Snapshot(0)
			}
		}()
	}
	s.Close()
	wg.Wait()
}

func TestMonitorClosedTurnCannotReopenAcrossInterleavedTurn(t *testing.T) {
	s := New("tab")
	defer s.Close()
	s.Record("old", "", "phase", "final", nil)
	s.Record("new", "", "phase", "input", nil)
	s.Record("old", "", "phase", "decode", nil)
	s.Record("new", "", "phase", "wait-first", nil)
	v := s.Snapshot(0)
	if len(v.Entries) != 3 || v.Entries[2].TurnID != "new" {
		t.Fatalf("old turn reopened: %+v", v.Entries)
	}
}

func TestMonitorJSONUnicodeEscapesAndThinkingConfig(t *testing.T) {
	for _, body := range []string{
		`{"text":"\u4e2d\u6587 \ud83d\ude42","quote":"a\"b\\c"}`,
		`{"thinking":{"type":"enabled","budget_tokens":2048},"reasoning_effort":"high"}`,
		`{"a":[true,false,null,-1,1.25e2,{"b":"ok"}]}`,
	} {
		got, partial := ScanJSON(strings.NewReader(body))
		want, _ := SanitizeJSON([]byte(body))
		if partial || string(got) != string(want) {
			t.Fatalf("valid JSON differed: %s => %s (%v)", body, got, partial)
		}
	}
	for _, body := range []string{`{"a":1}garbage`, `{"a":"\x"}`, `{"a":"unterminated}`, `[1,]`, `{"a":true,}`, strings.Repeat("{\"x\":", 30) + `0` + strings.Repeat("}", 30)} {
		got, partial := ScanJSON(strings.NewReader(body))
		if !partial || !json.Valid(got) {
			t.Fatalf("malformed JSON missing partial: %q %s", body, got)
		}
	}
	text, p := Text(strings.Repeat("a", 16383) + "\u4e2d\u6587")
	if !p || !utf8.ValidString(text) || len(text) != 16383 {
		t.Fatal("UTF-8 split")
	}
	text, p = Text("bash --key sk-abcdefghijklmnopqrstuv api_key=fixture-value")
	if !p || strings.Contains(text, "abcdefghijkl") || strings.Contains(text, "fixture-value") {
		t.Fatal("recognizable key leak")
	}
	rng := rand.New(rand.NewSource(17))
	for i := 0; i < 500; i++ {
		body := make([]byte, rng.Intn(512))
		_, _ = rng.Read(body)
		got, _ := ScanJSON(strings.NewReader(string(body)))
		if !json.Valid(got) || len(got) > MaxBody {
			t.Fatal("random input broke bounded JSON")
		}
	}
}

func FuzzMonitorScan(f *testing.F) {
	for _, s := range []string{`{"a":"x"}`, `{"thinking":{"type":"adaptive"}}`, `{"data":"ABC","after":1}`, `"\ud800"`, `{"x":"a\\\"b"}`, `{`, ``} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > MaxScan {
			return
		}
		out, _ := ScanJSON(strings.NewReader(body))
		if !json.Valid(out) || len(out) > MaxBody {
			t.Fatal("invalid bounded output")
		}
	})
}

func TestMonitorTextStructuredAndQuotedCredentials(t *testing.T) {
	const secret = "SYNTHETIC_PRIVATE_VALUE"
	cases := []string{
		`{"token":"` + secret + `","safe":"keep"}`,
		`[{"nested":{"refresh-token":"` + secret + `"}}]`,
		`prefix "token": "` + secret + ` with spaces" suffix`,
		`'password' = '` + secret + ` with spaces'`,
		`token=` + secret,
		`{"output":"{\"token\":\"` + secret + `\"}"}`,
	}
	encoded, _ := json.Marshal(cases[0])
	cases = append(cases, string(encoded))
	for _, input := range cases {
		got, partial := Text(input)
		if !partial || strings.Contains(got, secret) {
			t.Fatalf("structured/quoted credential leaked: %q -> %q", input, got)
		}
	}
}
