package orcaaccount

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestSettlementAmountsRejectNegativeAndPreserveUnknown(t *testing.T) {
	zero, negative, exact := int64(0), int64(-1), int64(9007199254740993)
	cases := []struct {
		id        string
		status    SettlementStatus
		estimated *int64
		settled   *int64
		invalid   bool
	}{
		{"unknown", SettlementUnknown, nil, nil, false},
		{"pending", SettlementPending, nil, nil, false},
		{"estimated-zero", SettlementEstimated, &zero, nil, false},
		{"settled-zero", SettlementSettled, nil, &zero, false},
		{"settled-exact", SettlementSettled, nil, &exact, false},
		{"settled-negative", SettlementSettled, nil, &negative, true},
		{"estimated-negative", SettlementEstimated, &negative, nil, true},
		{"pending-negative-estimate", SettlementPending, &negative, nil, true},
		{"unknown-negative-estimate", SettlementUnknown, &negative, nil, true},
		{"settled-negative-estimate", SettlementSettled, &negative, &zero, true},
	}
	f := newIssuer(t)
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		id, ok := strings.CutPrefix(r.URL.Path, APIPrefix+"/requests/")
		if !ok {
			return false
		}
		for _, tc := range cases {
			if tc.id == id {
				writeJSON(w, Settlement{RequestID: id, Currency: "CNY", Status: tc.status,
					EstimatedMicros: tc.estimated, SettledMicros: tc.settled})
				return true
			}
		}
		t.Error("unexpected synthetic receipt query")
		w.WriteHeader(http.StatusNotFound)
		return true
	}
	c := f.client(t, &memoryStore{})
	login(t, c)
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			got, err := c.Request(context.Background(), tc.id)
			if tc.invalid {
				if !errors.Is(err, ErrProtocol) {
					t.Fatalf("invalid negative cost accepted: status=%s error=%v", got.Status, err)
				}
				return
			}
			if err != nil || got.Status != tc.status {
				t.Fatalf("valid settlement rejected: %v", err)
			}
			same := func(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
			if !same(got.EstimatedMicros, tc.estimated) || !same(got.SettledMicros, tc.settled) {
				t.Fatal("missing amount became zero or exact integer changed")
			}
		})
	}
}
