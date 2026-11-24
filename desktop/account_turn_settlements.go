package main

import (
	"math"
	"sort"
)

type OrcaTurnSettlementView struct {
	TurnID          string `json:"turnId"`
	Status          string `json:"status"`
	Currency        string `json:"currency"`
	Scope           string `json:"scope"`
	Cost            string `json:"cost"`
	SettledSubtotal string `json:"settledSubtotal"`
	Requests        int    `json:"requests"`
	SettledRequests int    `json:"settledRequests"`
}

// This view is independent of the legacy, locally estimated token-price UI.
// This is a bounded receipt window, not a complete historical bill. A partial
// server settlement is never presented as the total turn cost.
func (a *App) OrcaAccountTurnSettlements() []OrcaTurnSettlementView {
	s := a.account
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconcileAccountLocked()
	if !s.view.Authenticated {
		return nil
	}
	rows := make([]accountReceipt, 0, len(s.receiptOrder))
	for _, id := range s.receiptOrder {
		rows = append(rows, s.receipts[id])
	}
	return aggregateAccountTurnSettlements(rows)
}

func aggregateAccountTurnSettlements(rows []accountReceipt) []OrcaTurnSettlementView {
	type total struct {
		view                                 OrcaTurnSettlementView
		settled, known                       int64
		unknown, pending, estimated, invalid bool
	}
	turns := map[string]*total{}
	get := func(id string) *total {
		if turns[id] == nil {
			turns[id] = &total{view: OrcaTurnSettlementView{TurnID: id, Currency: "CNY", Scope: "retained_requests"}}
		}
		return turns[id]
	}
	seen := map[string]accountReceipt{}
	add := func(sum *int64, amount int64) bool {
		if amount < 0 || *sum > math.MaxInt64-amount {
			return false
		}
		*sum += amount
		return true
	}
	for _, r := range rows {
		if r.TurnID == "" {
			continue
		}
		t := get(r.TurnID)
		key := "local:" + r.RequestID
		if r.serverID != "" {
			key = "server:" + r.serverID
		}
		if old, ok := seen[key]; ok {
			if old.TurnID != r.TurnID || old.Status != r.Status || old.Cost != r.Cost || old.Currency != r.Currency {
				t.invalid = true
				get(old.TurnID).invalid = true
			}
			continue
		}
		seen[key] = r
		t.view.Requests++
		if r.Currency != "CNY" {
			t.invalid = true
			continue
		}
		amount, err := receiptMicros(r.Cost)
		switch r.Status {
		case "settled", "estimated":
			if err != nil || amount == nil || r.serverID == "" || !add(&t.known, *amount) {
				t.invalid = true
				continue
			}
			if r.Status == "settled" {
				t.view.SettledRequests++
				if !add(&t.settled, *amount) {
					t.invalid = true
				}
			} else {
				t.estimated = true
			}
		case "pending":
			t.pending = true
		default:
			t.unknown = true
		}
	}
	ids := make([]string, 0, len(turns))
	for id := range turns {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]OrcaTurnSettlementView, 0, len(ids))
	for _, id := range ids {
		t := turns[id]
		switch {
		case t.invalid || t.unknown:
			t.view.Status = "unknown"
		case t.pending:
			t.view.Status = "pending"
		case t.estimated:
			t.view.Status, t.view.Cost = "estimated", cnyMicros(t.known)
		default:
			t.view.Status, t.view.Cost = "settled", cnyMicros(t.known)
		}
		if !t.invalid && t.view.SettledRequests > 0 {
			t.view.SettledSubtotal = cnyMicros(t.settled)
		}
		out = append(out, t.view)
	}
	return out
}
