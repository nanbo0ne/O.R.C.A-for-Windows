package main

import "testing"

func TestAccountTurnSettlementsExactDedupAndPartial(t *testing.T) {
	r := func(id, turn, state, cost string) accountReceipt {
		return accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: id, TurnID: turn, Status: state, Cost: cost, Currency: "CNY"}, serverID: "request:" + id}
	}
	main := r("main", "a", "settled", "9007199254.740993")
	duplicate := main
	duplicate.RequestID = "alias"
	rows := []accountReceipt{main, duplicate, r("child", "a", "settled", "0.000001"), r("review", "b", "pending", ""), r("other", "b", "settled", "0.000002")}
	got := aggregateAccountTurnSettlements(rows)
	if len(got) != 2 || got[0].Scope != "retained_requests" || got[0].Cost != "9007199254.740994" || got[0].Requests != 2 || got[0].SettledRequests != 2 || got[0].Status != "settled" {
		t.Fatalf("lost exact money or double counted: %+v", got)
	}
	if got[1].Status != "pending" || got[1].Cost != "" || got[1].SettledSubtotal != "0.000002" {
		t.Fatalf("partial value became total: %+v", got[1])
	}
	got = aggregateAccountTurnSettlements(append(rows, r("unknown", "b", "unknown", "")))
	if got[1].Status != "unknown" || got[1].Cost != "" {
		t.Fatalf("unknown was replaced by zero: %+v", got[1])
	}
}

func TestAccountTurnSettlementsRejectConflictAndOverflow(t *testing.T) {
	r := accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: "x", TurnID: "a", Status: "settled", Currency: "CNY", Cost: "9223372036854.775807"}, serverID: "server:x"}
	second := r
	second.RequestID, second.serverID, second.Cost = "y", "server:y", "0.000001"
	got := aggregateAccountTurnSettlements([]accountReceipt{r, second})
	if got[0].Status != "unknown" || got[0].Cost != "" || got[0].SettledSubtotal != "" {
		t.Fatalf("overflow disclosed a false total: %+v", got)
	}
	second = r
	second.TurnID = "b"
	got = aggregateAccountTurnSettlements([]accountReceipt{r, second})
	for _, row := range got {
		if row.Status != "unknown" || row.Cost != "" || row.SettledSubtotal != "" {
			t.Fatalf("conflicting parent accepted: %+v", got)
		}
	}
}
