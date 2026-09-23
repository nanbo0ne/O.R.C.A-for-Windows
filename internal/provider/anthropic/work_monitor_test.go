package anthropic

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

func TestWorkMonitorActualAnthropicRequest(t *testing.T) {
	p, err := New(provider.Config{Name: "fixture", BaseURL: "https://fixture.invalid", Model: "fixture", APIKey: "SYNTHETIC_SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	c := p.(*client)
	var wire []byte
	c.http = &http.Client{Transport: monitorTransport(func(r *http.Request) (*http.Response, error) {
		wire, _ = io.ReadAll(r.Body)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))}, nil
	})}
	s := monitor.New("tab")
	defer s.Close()
	ctx := monitor.WithBinding(context.Background(), monitor.Binding{Sink: func() *monitor.Store { return s }, Turn: "turn"})
	ch, err := p.Stream(ctx, provider.Request{RequestID: "req", Messages: []provider.Message{{Role: provider.RoleSystem, Content: "sent system"}, {Role: provider.RoleUser, Content: "sent user"}}})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	v := s.Snapshot(0)
	var captured struct {
		Body json.RawMessage `json:"body"`
	}
	_ = json.Unmarshal(v.Entries[0].Data, &captured)
	want, _ := monitor.ScanJSON(strings.NewReader(string(wire)))
	if string(want) != string(captured.Body) || !strings.Contains(string(captured.Body), "sent system") {
		t.Fatal("actual Anthropic mapping not observed")
	}
	all, _ := json.Marshal(v)
	if strings.Contains(string(all), "SYNTHETIC_SECRET") {
		t.Fatal("auth leaked")
	}
}
