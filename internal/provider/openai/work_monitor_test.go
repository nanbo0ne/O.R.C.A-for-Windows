package openai

import (
	"context"
	"encoding/json"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/monitor"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"io"
	"net/http"
	"strings"
	"testing"
)

type monitorTransport func(*http.Request) (*http.Response, error)

func (fn monitorTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestWorkMonitorActualOpenAIRequest(t *testing.T) {
	p, err := New(provider.Config{Name: "fixture", BaseURL: "https://fixture.invalid", Model: "fixture-model", APIKey: "SYNTHETIC_HEADER_SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	c := p.(*client)
	var wire []byte
	c.http = &http.Client{Transport: monitorTransport(func(r *http.Request) (*http.Response, error) {
		wire, _ = io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer SYNTHETIC_HEADER_SECRET" {
			t.Fatal("wire auth changed")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"RAW_REASONING\",\"content\":\"answer\"}}]}\n\ndata: [DONE]\n\n"))}, nil
	})}
	s := monitor.New("tab")
	defer s.Close()
	ctx := monitor.WithBinding(context.Background(), monitor.Binding{Sink: func() *monitor.Store { return s }, Turn: "turn"})
	ch, err := p.Stream(ctx, provider.Request{RequestID: "provider-request", Messages: []provider.Message{{Role: provider.RoleUser, Content: "sent prompt"}}, MaxTokens: 99})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	v := s.Snapshot(0)
	if len(v.Entries) < 2 || v.Entries[0].RequestID != "provider-request" || v.Entries[0].AttemptID == "" {
		t.Fatalf("identity: %+v", v)
	}
	var captured struct {
		Body json.RawMessage `json:"body"`
	}
	_ = json.Unmarshal(v.Entries[0].Data, &captured)
	want, _ := monitor.ScanJSON(strings.NewReader(string(wire)))
	if string(want) != string(captured.Body) {
		t.Fatalf("not actual wire: %s != %s", want, captured.Body)
	}
	all, _ := json.Marshal(v)
	if strings.Contains(string(all), "SYNTHETIC_HEADER_SECRET") || strings.Contains(string(all), "RAW_REASONING") {
		t.Fatal("header/raw response leaked before Hook")
	}
}

func TestWorkMonitorRetryAttemptIDsAndErrorPrivacy(t *testing.T) {
	p, err := New(provider.Config{Name: "fixture", BaseURL: "https://fixture.invalid", Model: "fixture", APIKey: "SYNTHETIC_SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	c := p.(*client)
	attempts := 0
	c.http = &http.Client{Transport: monitorTransport(func(r *http.Request) (*http.Response, error) {
		attempts++
		status, body := 503, `{"error":"RAW_ERROR_SECRET"}`
		if attempts > 1 {
			status, body = 200, "data: [DONE]\n\n"
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	s := monitor.New("tab")
	defer s.Close()
	ctx := monitor.WithBinding(context.Background(), monitor.Binding{Sink: func() *monitor.Store { return s }, Turn: "turn"})
	ch, err := p.Stream(ctx, provider.Request{RequestID: "canonical", Messages: []provider.Message{{Role: provider.RoleUser, Content: "fixture"}}})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	v := s.Snapshot(0)
	ids := map[string]bool{}
	for _, entry := range v.Entries {
		if entry.Kind == "request" {
			if entry.RequestID != "canonical" {
				t.Fatal("canonical request join lost")
			}
			ids[entry.AttemptID] = true
		}
	}
	if attempts != 2 || len(ids) != 2 {
		t.Fatalf("attempts: %d %+v", attempts, ids)
	}
	all, _ := json.Marshal(v)
	if strings.Contains(string(all), "RAW_ERROR_SECRET") {
		t.Fatal("raw provider error leaked")
	}
}
