package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
)

const accountReceiptLimit = 500
const accountRecoveryLimit = 32

var errAccountReceiptStore = errors.New("ORCA receipt storage is unavailable")
var errAccountReceiptFull = errors.New("ORCA receipt history has too many unresolved requests")

// This is the entire disk schema. It deliberately cannot contain model input,
// output, authorization headers, runtime credentials or UI error text.
type storedAccountReceipt struct {
	RequestID  string `json:"request_id"`
	TurnID     string `json:"turn_id,omitempty"`
	Purpose    string `json:"purpose"`
	Status     string `json:"status"`
	Currency   string `json:"currency"`
	CostMicros *int64 `json:"cost_micros,omitempty"`
	ServerID   string `json:"server_id,omitempty"`
	Grant      string `json:"grant"`
}

type accountReceiptDocument struct {
	Version int                    `json:"version"`
	Subject string                 `json:"subject"`
	Grant   string                 `json:"grant"`
	Records []storedAccountReceipt `json:"records"`
}

type accountReceiptJournal struct {
	// Only this narrow lock covers disk IO. Never acquire it while holding the
	// account view, controller admission, or lifecycle transition lock.
	mu         sync.Mutex
	store      orcaaccount.Store
	doc        accountReceiptDocument
	generation uint64
}

func receiptIdentifier(s string, limit int) bool {
	if s == "" || len(s) > limit || s == "." || s == ".." {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_:.", c)) {
			return false
		}
	}
	return true
}

func receiptPurpose(s string) string {
	switch s {
	case "turn", "compaction", "compaction_fallback", "classifier", "browser", "memory", "title", "welcome", "credential_probe":
		return s
	default:
		return "unknown"
	}
}

func receiptMicros(s string) (*int64, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ".")
	if len(parts) != 2 || len(parts[1]) != 6 {
		return nil, errAccountReceiptStore
	}
	v, err := strconv.ParseInt(parts[0]+parts[1], 10, 64)
	if err != nil || cnyMicros(v) != s {
		return nil, errAccountReceiptStore
	}
	return &v, nil
}

func (r storedAccountReceipt) valid() bool {
	if !receiptIdentifier(r.RequestID, 128) || r.TurnID != "" && !receiptIdentifier(r.TurnID, 128) ||
		!receiptIdentifier(r.Grant, 128) || r.ServerID != "" && !receiptIdentifier(r.ServerID, 512) ||
		r.Currency != "CNY" || receiptPurpose(r.Purpose) != r.Purpose ||
		r.CostMicros != nil && *r.CostMicros < 0 {
		return false
	}
	switch r.Status {
	case "settled":
		return r.CostMicros != nil && r.ServerID != ""
	case "estimated":
		return r.ServerID != ""
	case "pending":
		return r.CostMicros == nil && r.ServerID != ""
	case "unknown":
		return r.CostMicros == nil
	default:
		return false
	}
}

func decodeAccountReceipts(data []byte) (accountReceiptDocument, error) {
	var doc accountReceiptDocument
	if len(data) > 1<<20 {
		return doc, errAccountReceiptStore
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&doc); err != nil || doc.Version != 1 || !receiptIdentifier(doc.Subject, 128) ||
		!receiptIdentifier(doc.Grant, 128) || len(doc.Records) > accountReceiptLimit {
		return accountReceiptDocument{}, errAccountReceiptStore
	}
	if d.Decode(new(any)) != io.EOF {
		return accountReceiptDocument{}, errAccountReceiptStore
	}
	seen := map[string]bool{}
	for _, r := range doc.Records {
		if !r.valid() || seen[r.RequestID] {
			return accountReceiptDocument{}, errAccountReceiptStore
		}
		seen[r.RequestID] = true
	}
	return doc, nil
}

func (j *accountReceiptJournal) save(ctx context.Context, doc accountReceiptDocument) error {
	data, err := json.Marshal(doc)
	if err != nil || len(data) > 1<<20 || j.store == nil {
		return errAccountReceiptStore
	}
	defer clear(data)
	if err := j.store.Save(ctx, data); err != nil {
		return errAccountReceiptStore
	}
	j.doc = doc
	return nil
}

// SessionID is the server's desktop grant ID, not a locally invented identity.
// A refresh keeps this ID; a new authorization must not query the old grant.
func (s *accountDesktop) restoreAccountReceipts(ctx context.Context, p orcaaccount.Profile, gen uint64) ([]accountReceipt, error) {
	j := s.receiptJournal
	if j == nil { // Constructors used by tests without persistence.
		return nil, nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.store == nil {
		return nil, errAccountReceiptStore
	}
	if !receiptIdentifier(p.Subject, 128) || !receiptIdentifier(p.SessionID, 128) {
		return nil, errAccountReceiptStore
	}
	data, err := j.store.Load(ctx)
	var doc accountReceiptDocument
	if err == nil {
		doc, err = decodeAccountReceipts(data)
		clear(data)
	}
	if err != nil && !errors.Is(err, orcaaccount.ErrNotFound) {
		return nil, errAccountReceiptStore
	}
	if doc.Subject != p.Subject {
		doc = accountReceiptDocument{Version: 1, Subject: p.Subject}
	}
	doc.Grant = p.SessionID
	for i := range doc.Records {
		r := &doc.Records[i]
		if r.Grant != p.SessionID && r.Status != "settled" {
			r.Status, r.CostMicros = "unknown", nil
		}
	}
	if ctx.Err() != nil || s.client.CurrentSessionGeneration() != gen {
		return nil, orcaaccount.ErrStale
	}
	if err := j.save(ctx, doc); err != nil {
		return nil, err
	}
	j.generation = gen
	var result []accountReceipt
	for _, r := range doc.Records {
		v := accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: r.RequestID, TurnID: r.TurnID, Purpose: r.Purpose, Status: r.Status, Currency: r.Currency}, serverID: r.ServerID}
		if r.CostMicros != nil {
			v.Cost = cnyMicros(*r.CostMicros)
		}
		if r.Grant == p.SessionID {
			v.generation = gen
		}
		result = append(result, v)
	}
	return result, nil
}

// Call after releasing s.mu, before sending a new model request and after each
// server-ID or settlement update. A failed initial write must prevent sending.
func (s *accountDesktop) persistAccountReceipt(ctx context.Context, requestID string, generation uint64) (err error) {
	j := s.receiptJournal
	if j != nil {
		j.mu.Lock()
		defer j.mu.Unlock()
	}
	// Publish each result before releasing the journal lock, in the same order
	// as the writes. A late failure cannot resurrect an already-resolved warning.
	defer func() {
		if errors.Is(err, orcaaccount.ErrStale) {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.view.Authenticated || s.client.CurrentSessionGeneration() != generation {
			return
		}
		if s.receiptFailureGen != generation {
			s.receiptFailureGen = generation
			s.receiptFailures = nil
		}
		if err != nil {
			if s.receiptFailures == nil {
				s.receiptFailures = map[string]struct{}{}
			}
			s.receiptFailures[requestID] = struct{}{}
		} else {
			delete(s.receiptFailures, requestID)
		}
		s.view.ReceiptError = ""
		if len(s.receiptFailures) != 0 {
			s.view.ReceiptError = "Receipt persistence failed; recovery may be incomplete."
		}
	}()
	if j == nil {
		return errAccountReceiptStore
	}
	s.mu.Lock()
	r, ok := s.receipts[requestID]
	ready := s.view.Authenticated
	s.mu.Unlock()
	snapshot := s.client.Snapshot()
	if !ok || !ready || !snapshot.Authenticated || generation != snapshot.Generation || r.generation != generation ||
		j.generation != generation || j.doc.Subject != snapshot.Profile.Subject || j.doc.Grant != snapshot.Profile.SessionID {
		return orcaaccount.ErrStale
	}
	cost, err := receiptMicros(r.Cost)
	if err != nil {
		return err
	}
	v := storedAccountReceipt{RequestID: r.RequestID, TurnID: r.TurnID, Purpose: receiptPurpose(r.Purpose), Status: r.Status, Currency: r.Currency, CostMicros: cost, ServerID: r.serverID, Grant: j.doc.Grant}
	if !v.valid() {
		return errAccountReceiptStore
	}
	doc := j.doc
	doc.Records = append([]storedAccountReceipt(nil), doc.Records...)
	index := -1
	for i, old := range doc.Records {
		if old.RequestID == requestID {
			if old.Grant != v.Grant || old.ServerID != "" && old.ServerID != v.ServerID {
				return orcaaccount.ErrStale
			}
			if old.Status == "settled" {
				return nil
			}
			index = i
			break
		}
	}
	if index >= 0 {
		doc.Records[index] = v
	} else {
		if len(doc.Records) == accountReceiptLimit {
			evict := -1
			for i, old := range doc.Records {
				if old.Status == "settled" {
					evict = i
					break
				}
			}
			if evict < 0 {
				return errAccountReceiptFull
			}
			doc.Records = append(doc.Records[:evict], doc.Records[evict+1:]...)
		}
		doc.Records = append(doc.Records, v)
	}
	if err := j.save(ctx, doc); err != nil {
		return err
	}
	if s.client.CurrentSessionGeneration() != generation || ctx.Err() != nil {
		return orcaaccount.ErrStale
	}
	return nil
}

// A bounded recovery pass makes GET requests only. Missing server IDs remain
// unknown; an idempotency key is not proof that the server accepted a request.
func (s *accountDesktop) recoverAccountReceipts(gen uint64, receipts []accountReceipt) {
	ctx, cancel, err := s.client.GenerationContext(s.ctx, gen)
	if err != nil {
		return
	}
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 2*time.Minute)
	defer timeout()
	count := 0
	for _, r := range receipts {
		if r.generation != gen || r.serverID == "" || r.Status == "settled" {
			continue
		}
		if count == accountRecoveryLimit || ctx.Err() != nil {
			return
		}
		count++
		query, done := context.WithTimeout(ctx, 5*time.Second)
		settlement, err := s.client.Request(query, r.serverID)
		done()
		if errors.Is(err, orcaaccount.ErrUnauthenticated) || errors.Is(err, orcaaccount.ErrStale) {
			return
		}
		if err == nil {
			s.applySettlement(r.RequestID, gen, settlement)
			if s.persistAccountReceipt(ctx, r.RequestID, gen) != nil {
				return
			}
		}
	}
}
