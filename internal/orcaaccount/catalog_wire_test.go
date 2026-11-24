package orcaaccount

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCatalogPreservesServerRequestPricingAndCapabilities(t *testing.T) {
	raw := []byte(`{"id":"synthetic/case-v1","group_id":7,"protocol":"openai","endpoint":"/chat/completions","capabilities":{"reasoning":null,"reasoning_status":"confirmed","reasoning_efforts":["none","high","xhigh"],"default_effort":"high","reasoning_source":"declared","reasoning_revision":4},"prices":{"_request":{"micros":125000,"emptyOutputFree":true},"_long":{"threshold":200000,"prices":{"_request":{"micros":375000,"emptyOutputFree":true}}}}}`)
	var m Model
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Prices == nil || m.Prices.Request == nil || m.Prices.Request.Micros != 125000 || !m.Prices.Request.EmptyOutputFree || m.Prices.Input != nil {
		t.Fatal("per-request quote was lost or converted to token pricing")
	}
	if m.Prices.Long == nil || m.Prices.Long.Threshold != 200000 || m.Prices.Long.Prices.Request == nil || m.Prices.Long.Prices.Request.Micros != 375000 {
		t.Fatal("long-context request tier lost")
	}
	c := m.Capabilities
	if c == nil || c.Reasoning != nil || c.ReasoningStatus != "confirmed" || c.DefaultEffort != "high" || c.ReasoningRevision != 4 || !reflect.DeepEqual(c.ReasoningEfforts, []string{"none", "high", "xhigh"}) {
		t.Fatal("server capability lost or unknown capability invented")
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var restored Model
	if err = json.Unmarshal(encoded, &restored); err != nil || !reflect.DeepEqual(m, restored) {
		t.Fatal("catalog serialization changed the server quote", err)
	}
}

func TestOldCatalogDoesNotInventNewPricesOrCapabilities(t *testing.T) {
	var m Model
	if err := json.Unmarshal([]byte(`{"id":"legacy","prices":{"input":1000000,"output":2000000,"cache":0},"capabilities":{"text":true}}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.Prices.Request != nil || m.Prices.Input == nil || *m.Prices.Input != 1000000 || m.Capabilities.ReasoningStatus != "" || len(m.Capabilities.ReasoningEfforts) != 0 {
		t.Fatal("legacy model semantics changed")
	}
}
