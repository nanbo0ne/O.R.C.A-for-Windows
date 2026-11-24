package main

import (
	"encoding/json"
	"testing"
)

func TestAccountReceiptJournalRejectsNegativeAmounts(t *testing.T) {
	for _, status := range []string{"settled", "estimated"} {
		t.Run(status, func(t *testing.T) {
			negative := int64(-1)
			row := storedAccountReceipt{RequestID: "synthetic-request", TurnID: "synthetic-root",
				Purpose: "turn", Status: status, Currency: "CNY", CostMicros: &negative,
				ServerID: "synthetic-server", Grant: "synthetic-grant"}
			doc := accountReceiptDocument{Version: 1, Subject: "synthetic-subject", Grant: row.Grant,
				Records: []storedAccountReceipt{row}}
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeAccountReceipts(data); err == nil {
				t.Fatal("negative request cost accepted from the receipt journal")
			}
			zero := int64(0)
			doc.Records[0].CostMicros = &zero
			data, err = json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeAccountReceipts(data)
			if err != nil || got.Records[0].CostMicros == nil || *got.Records[0].CostMicros != 0 {
				t.Fatal("explicit zero cost must remain distinguishable from unknown", err)
			}
		})
	}
}
