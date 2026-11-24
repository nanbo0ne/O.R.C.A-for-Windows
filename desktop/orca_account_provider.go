package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/anthropic"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/openai"
)

func init() {
	provider.Register(accountProviderKind, func(cfg provider.Config) (provider.Provider, error) {
		if !betaEnabled() || !strings.HasPrefix(cfg.Name, accountProviderPrefix) {
			return nil, orcaaccount.ErrUnsupported
		}
		return &accountProvider{cfg: cfg}, nil
	})
}

type accountProvider struct{ cfg provider.Config }

func (p *accountProvider) Name() string { return p.cfg.Name }

func (s *accountDesktop) credential(ctx context.Context, gen uint64, group int64) (*orcaaccount.Credential, error) {
	s.credentialMu.Lock()
	defer s.credentialMu.Unlock()
	if s.client.CurrentSessionGeneration() != gen {
		return nil, orcaaccount.ErrStale
	}
	if s.credentialGeneration != gen {
		s.credentials = map[int64]*orcaaccount.Credential{}
		s.credentialGeneration = gen
	}
	if cr := s.credentials[group]; cr != nil && cr.Info().ExpiresAt.After(time.Now().Add(time.Minute)) {
		return cr, nil
	}
	cr, err := s.client.Credentials(ctx, orcaaccount.CredentialOptions{GroupID: &group})
	if err != nil {
		return nil, err
	}
	if s.client.CurrentSessionGeneration() != gen {
		return nil, orcaaccount.ErrStale
	}
	s.credentials[group] = cr
	return cr, nil
}

func (p *accountProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	s := accountDesktopActive.Load()
	if s == nil {
		return nil, orcaaccount.ErrUnauthenticated
	}
	m, gen, err := s.readyAccountModel(ctx, p.cfg.Name, p.cfg.Model)
	if err != nil {
		return nil, err
	}
	if !accountProtocolSupported(m.Protocol, m.Endpoint) {
		return nil, errors.New("this ORCA account model requires a protocol not supported by this beta; select a Chat Completions or Anthropic Messages model")
	}
	if c := m.Capabilities; c != nil {
		if c.Vision != nil && !*c.Vision {
			for _, msg := range req.Messages {
				if len(msg.Images) > 0 {
					return nil, errors.New("this ORCA account model does not support images")
				}
			}
		}
		if c.Tools != nil && !*c.Tools && len(req.Tools) > 0 {
			return nil, errors.New("this ORCA account model does not support agent tools")
		}
	}
	cfg, err := accountReasoningConfig(p.cfg, m, req.DisableThinking)
	if err != nil {
		return nil, err
	}
	ctx, cancel, err := s.client.GenerationContext(ctx, gen)
	if err != nil {
		return nil, err
	}
	ctx = provider.WithoutRequestReplay(ctx)
	cr, err := s.credential(ctx, gen, m.GroupID)
	if err != nil {
		cancel()
		return nil, err
	}
	if req.RequestID == "" {
		req.RequestID = event.NewRequestID()
	}
	turnID := req.ParentTaskID
	if turnID == "" {
		turnID, _ = agent.ParentTurn(ctx)
	}
	r := accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: req.RequestID, TurnID: turnID, Purpose: string(req.Purpose), Status: "unknown", Currency: "CNY"}, generation: gen}
	client := cr.HTTPClient()
	client.Transport = &accountReceiptTransport{base: client.Transport, requestID: req.RequestID, capture: func(id string) { s.captureReceipt(req.RequestID, gen, id) }}
	cfg.BaseURL = orcaaccount.Router + "/v1"
	cfg.APIKey = "" // credential transport, never a persisted or frontend secret
	cfg.HTTPClient = client
	var next provider.Provider
	switch m.Protocol {
	case "openai":
		next, err = openai.New(cfg)
	case "anthropic":
		next, err = anthropic.New(cfg)
	default:
		err = errors.New("ORCA account model protocol is not supported")
	}
	if err != nil {
		cancel()
		return nil, err
	}
	if err = s.putReceipt(r); err == nil {
		err = s.persistAccountReceipt(ctx, req.RequestID, gen)
	}
	if err != nil {
		cancel()
		return nil, err
	}
	source, err := next.Stream(ctx, req)
	if err != nil {
		cancel()
		s.queueReceipt(req.RequestID)
		return nil, provider.NoRequestReplay(err)
	}
	out := make(chan provider.Chunk, 1)
	go func() {
		defer close(out)
		defer cancel()
		defer s.queueReceipt(req.RequestID)
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, open := <-source:
				if !open {
					return
				}
				if ctx.Err() != nil {
					return
				}
				if chunk.Type == provider.ChunkError {
					chunk.Err = provider.NoRequestReplay(chunk.Err)
				}
				select {
				case out <- chunk:
				case <-ctx.Done():
					return
				}
				if chunk.Type == provider.ChunkDone || chunk.Type == provider.ChunkError {
					return
				}
			}
		}
	}()
	return out, nil
}

type accountReceiptTransport struct {
	base      http.RoundTripper
	requestID string
	capture   func(string)
}

func accountProtocolSupported(protocol, endpoint string) bool {
	return protocol == "openai" && endpoint == "/chat/completions" || protocol == "anthropic" && endpoint == "/messages"
}

func (t *accountReceiptTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Idempotency-Key", t.requestID)
	resp, err := t.base.RoundTrip(r)
	if resp != nil && t.capture != nil {
		id := resp.Header.Get("X-Request-Id")
		if len(id) > 0 && len(id) <= 512 && !strings.ContainsAny(id, "/\\\x00\r\n") {
			t.capture(id)
		}
	}
	return resp, err
}

func (s *accountDesktop) putReceipt(r accountReceipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.generation != s.client.CurrentSessionGeneration() || !s.view.Authenticated {
		return orcaaccount.ErrStale
	}
	if _, ok := s.receipts[r.RequestID]; ok {
		return errors.New("ORCA account request already admitted; inspect its receipt before sending again")
	} else {
		if len(s.receiptOrder) >= accountReceiptLimit {
			evict := -1
			for i, id := range s.receiptOrder {
				if s.receipts[id].Status == "settled" {
					evict = i
					break
				}
			}
			if evict < 0 {
				return errAccountReceiptFull
			}
			delete(s.receipts, s.receiptOrder[evict])
			s.receiptOrder = append(s.receiptOrder[:evict], s.receiptOrder[evict+1:]...)
		}
		s.receiptOrder = append(s.receiptOrder, r.RequestID)
	}
	s.receipts[r.RequestID] = r
	return nil
}

func (s *accountDesktop) captureReceipt(requestID string, gen uint64, id string) {
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		s.flushAccountReceipt(requestID, gen)
	}()
	r, ok := s.receipts[requestID]
	if !ok || r.generation != gen || gen != s.client.CurrentSessionGeneration() {
		return
	}
	if r.serverID != "" && r.serverID != id {
		return
	}
	r.serverID = id
	if r.Status != "settled" {
		r.Status = "pending"
	}
	s.receipts[requestID] = r
}

func (s *accountDesktop) queueReceipt(id string) {
	select {
	case s.settlements <- id:
	default:
	} // A full queue remains visibly pending, never zero-cost.
}

func (s *accountDesktop) settlementWorker() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case id := <-s.settlements:
			for attempt := 0; attempt < 3; attempt++ {
				s.mu.Lock()
				r, ok := s.receipts[id]
				s.mu.Unlock()
				if !ok || r.serverID == "" || r.Status == "settled" || r.generation != s.client.CurrentSessionGeneration() {
					break
				}
				bound, cancelGeneration, err := s.client.GenerationContext(s.ctx, r.generation)
				if err != nil {
					break
				}
				ctx, cancel := context.WithTimeout(bound, 8*time.Second)
				settlement, err := s.client.Request(ctx, r.serverID)
				cancel()
				cancelGeneration()
				if err == nil {
					s.applySettlement(id, r.generation, settlement)
					if settlement.Status == orcaaccount.SettlementSettled {
						break
					}
				}
				if errors.Is(err, orcaaccount.ErrStale) || errors.Is(err, orcaaccount.ErrUnauthenticated) {
					break
				}
				timer := time.NewTimer(time.Duration(attempt+1) * time.Second)
				select {
				case <-s.ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}
	}
}

func (s *accountDesktop) applySettlement(id string, gen uint64, settlement orcaaccount.Settlement) {
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		s.flushAccountReceipt(id, gen)
	}()
	r, ok := s.receipts[id]
	if !ok || r.generation != gen || gen != s.client.CurrentSessionGeneration() || r.serverID != settlement.RequestID || settlement.Currency != "CNY" || r.Status == "settled" {
		return
	}
	r.Status = string(settlement.Status)
	r.Cost = ""
	if settlement.Status == orcaaccount.SettlementSettled && settlement.SettledMicros != nil {
		r.Cost = cnyMicros(*settlement.SettledMicros)
	}
	if settlement.Status == orcaaccount.SettlementEstimated && settlement.EstimatedMicros != nil {
		r.Cost = cnyMicros(*settlement.EstimatedMicros)
	}
	s.receipts[id] = r
	s.walletAt = time.Time{}
}

func (s *accountDesktop) flushAccountReceipt(id string, gen uint64) {
	if s.receiptJournal == nil {
		return
	}
	// Cancellation ends model input immediately, but not the receipt query/write.
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	_ = s.persistAccountReceipt(ctx, id, gen)
}
