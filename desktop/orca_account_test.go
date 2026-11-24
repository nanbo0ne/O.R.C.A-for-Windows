package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/anthropic"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/openai"
)

type accountTestStore struct{}

type interruptedAccountBody struct{}

func (interruptedAccountBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (interruptedAccountBody) Close() error             { return nil }

func TestAccountRequestDoesNotReplayBeforeFirstToken(t *testing.T) {
	calls := 0
	transport := &accountReceiptTransport{requestID: "synthetic-single-admission", base: accountTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: interruptedAccountBody{}, Request: r}, nil
	})}
	p, err := openai.New(provider.Config{Name: "account-test", Model: "synthetic", BaseURL: orcaaccount.Router + "/v1", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(provider.WithoutRequestReplay(context.Background()), time.Second)
	defer cancel()
	stream, err := p.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}})
	if err != nil {
		t.Fatal(err)
	}
	var failure error
	for chunk := range stream {
		if chunk.Type == provider.ChunkError {
			failure = chunk.Err
		}
	}
	var interrupted *provider.StreamInterruptedError
	if calls != 1 || !errors.As(failure, &interrupted) {
		t.Fatalf("calls=%d failure=%v", calls, failure)
	}
}

func (accountTestStore) Load(context.Context) ([]byte, error) { return nil, orcaaccount.ErrNotFound }
func (accountTestStore) Save(context.Context, []byte) error   { return nil }
func (accountTestStore) Delete(context.Context) error         { return nil }

func accountTestHost(t *testing.T) *accountDesktop {
	t.Helper()
	c, err := orcaaccount.New(orcaaccount.Options{Store: accountTestStore{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &accountDesktop{client: c, ctx: ctx, cancel: cancel, view: OrcaAccountView{State: "signed_in", Authenticated: true}, receipts: map[string]accountReceipt{}, catalog: map[string]orcaaccount.Model{}}
	t.Cleanup(func() { cancel(); _ = c.Close() })
	return s
}

func TestOrcaAccountExactMoney(t *testing.T) {
	for value, want := range map[int64]string{0: "0.000000", 1: "0.000001", 1_000_001: "1.000001", -1: "-0.000001", math.MinInt64: "-9223372036854.775808", math.MaxInt64: "9223372036854.775807"} {
		if got := cnyMicros(value); got != want {
			t.Fatalf("%d = %s, want %s", value, got, want)
		}
	}
}

func TestOrcaAccountNeverConvertsResponsesIntoChat(t *testing.T) {
	for _, tc := range []struct {
		protocol, endpoint string
		want               bool
	}{
		{"openai", "/chat/completions", true}, {"anthropic", "/messages", true},
		{"openai", "/responses", false}, {"openai", "", false}, {"anthropic", "/chat/completions", false},
		{"openai", "https://other.invalid/chat/completions", false},
	} {
		if got := accountProtocolSupported(tc.protocol, tc.endpoint); got != tc.want {
			t.Fatalf("%+v: %v", tc, got)
		}
	}
}

func TestOrcaAccountCatalogSeparatesUsersGroupsAndProtocols(t *testing.T) {
	models := []orcaaccount.Model{{ID: "Model/Case-V1", GroupID: 1, Protocol: "openai"}, {ID: "Model/Case-V1", GroupID: 2, Protocol: "openai"}, {ID: "Model/Case-V1", GroupID: 1, Protocol: "anthropic"}}
	a := accountCatalog("subject-a", models)
	b := accountCatalog("subject-b", models)
	if len(a) != 3 || len(b) != 3 {
		t.Fatal("catalog merged distinct scopes")
	}
	for name, m := range a {
		if _, collision := b[name]; collision {
			t.Fatal("cross-account model reference")
		}
		p := accountProviderEntry(name, m)
		if p.Model != "Model/Case-V1" || p.APIKey() != "" || p.APIKeyEnv != "" || p.Price != nil || p.ReasoningProtocol != "none" {
			t.Fatalf("unsafe catalog entry: %#v", p)
		}
		if !p.Configured() {
			t.Fatal("OAuth model incorrectly requires API key")
		}
	}
	if len(accountCatalog("", models)) != 0 {
		t.Fatal("empty subject accepted")
	}
}

func TestOrcaAccountSettlementIdentityAndUnknown(t *testing.T) {
	s := accountTestHost(t)
	gen := s.client.CurrentSessionGeneration()
	r := accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: "client-1", TurnID: "turn-1", Status: "unknown", Currency: "CNY"}, generation: gen}
	s.putReceipt(r)
	s.putReceipt(r)
	if len(s.receiptOrder) != 1 || s.receipts[r.RequestID].Cost != "" {
		t.Fatal("duplicate request or invented zero")
	}
	s.captureReceipt(r.RequestID, gen, "request:server-1")
	amount := int64(123)
	s.applySettlement(r.RequestID, gen, orcaaccount.Settlement{RequestID: "wrong", Status: orcaaccount.SettlementSettled, Currency: "CNY", SettledMicros: &amount})
	if s.receipts[r.RequestID].Status != "pending" {
		t.Fatal("wrong receipt applied")
	}
	s.applySettlement(r.RequestID, gen, orcaaccount.Settlement{RequestID: "request:server-1", Status: orcaaccount.SettlementSettled, Currency: "CNY", SettledMicros: &amount})
	if s.receipts[r.RequestID].Cost != "0.000123" {
		t.Fatal("exact server settlement missing")
	}
	amount = 999
	s.applySettlement(r.RequestID, gen, orcaaccount.Settlement{RequestID: "request:server-1", Status: orcaaccount.SettlementSettled, Currency: "CNY", SettledMicros: &amount})
	if s.receipts[r.RequestID].Cost != "0.000123" {
		t.Fatal("duplicate settlement rewrote amount")
	}
	_ = s.client.CancelContexts()
	s.captureReceipt(r.RequestID, gen, "request:new-account")
	if s.receipts[r.RequestID].serverID != "request:server-1" {
		t.Fatal("stale callback crossed account")
	}
}

func TestOrcaAccountReceiptCapacityKeepsUnresolvedRequests(t *testing.T) {
	s := accountTestHost(t)
	gen := s.client.CurrentSessionGeneration()
	for i := 0; i < accountReceiptLimit; i++ {
		r := accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: fmt.Sprintf("req-%d", i), Status: "unknown", Currency: "CNY"}, generation: gen}
		if err := s.putReceipt(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.putReceipt(s.receipts["req-0"]); err == nil {
		t.Fatal("an uncertain request was admitted twice")
	}
	next := accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: "next", Status: "unknown", Currency: "CNY"}, generation: gen}
	if err := s.putReceipt(next); !errors.Is(err, errAccountReceiptFull) {
		t.Fatalf("full unresolved history did not stop admission: %v", err)
	}
	if len(s.receipts) != accountReceiptLimit || s.receiptOrder[0] != "req-0" {
		t.Fatal("pending recovery record was removed")
	}
	settled := s.receipts["req-5"]
	settled.Status = "settled"
	s.receipts["req-5"] = settled
	if err := s.putReceipt(next); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.receipts["req-5"]; ok {
		t.Fatal("bounded history retained settled entry instead of unresolved entries")
	}
	if _, ok := s.receipts["req-0"]; !ok {
		t.Fatal("old unresolved request was lost")
	}
}

type accountTestTransport func(*http.Request) (*http.Response, error)

func (f accountTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOrcaAccountAdaptersUseScopedTransportAndExactProtocol(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			receipt := ""
			base := accountTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Idempotency-Key") != "synthetic-request" {
					t.Fatal("missing idempotency identity")
				}
				if r.URL.Host != "router.aichat.diy" {
					t.Fatal("escaped fixed router")
				}
				wantPath := "/v1/chat/completions"
				if kind == "anthropic" {
					wantPath = "/v1/messages"
				}
				if r.URL.Path != wantPath {
					t.Fatalf("protocol %s: %s", kind, r.URL.Path)
				}
				var payload map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				var model string
				_ = json.Unmarshal(payload["model"], &model)
				if model != "Synthetic/Case-V1" {
					t.Fatal("model was normalized")
				}
				if _, ok := payload["reasoning_effort"]; ok {
					t.Fatal("invented effort capability")
				}
				body := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
				if kind == "anthropic" {
					body = "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"request:test"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			transport := &accountReceiptTransport{base: base, requestID: "synthetic-request", capture: func(id string) { receipt = id }}
			cfg := provider.Config{Name: "test-account", Model: "Synthetic/Case-V1", BaseURL: orcaaccount.Router + "/v1", Extra: map[string]any{"reasoning_protocol": "none"}, HTTPClient: &http.Client{Transport: transport}}
			var p provider.Provider
			var err error
			if kind == "openai" {
				p, err = openai.New(cfg)
			} else {
				p, err = anthropic.New(cfg)
			}
			if err != nil {
				t.Fatal(err)
			}
			stream, err := p.Stream(context.Background(), provider.Request{RequestID: "synthetic-request", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}, MaxTokens: 20})
			if err != nil {
				t.Fatal(err)
			}
			for chunk := range stream {
				if chunk.Type == provider.ChunkError {
					t.Fatalf("stream: %v", chunk.Err)
				}
			}
			if calls != 1 || receipt != "request:test" {
				t.Fatalf("calls=%d receipt=%s", calls, receipt)
			}
		})
	}
}
