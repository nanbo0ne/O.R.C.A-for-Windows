package orcaaccount

import "testing"

func TestReasoningCapabilityValidation(t *testing.T) {
	for _, c := range []*ModelCapabilities{nil, {}, {ReasoningStatus: "unknown", ReasoningSource: "unknown"}, {ReasoningStatus: "confirmed", ReasoningSource: "declared", ReasoningEfforts: []string{"none", "xhigh"}, DefaultEffort: "none"}, {ReasoningStatus: "confirmed", ReasoningSource: "manual"}} {
		if !ValidReasoningCapabilities(c) {
			t.Fatal("valid catalog rejected", c)
		}
	}
	for _, c := range []*ModelCapabilities{
		{ReasoningStatus: "confirmed", ReasoningSource: "guess", ReasoningEfforts: []string{"high"}},
		{ReasoningStatus: "confirmed", ReasoningSource: "manual", ReasoningEfforts: []string{"high", "high"}},
		{ReasoningStatus: "confirmed", ReasoningSource: "manual", ReasoningEfforts: []string{"high"}, DefaultEffort: "xhigh"},
		{ReasoningStatus: "confirmed", ReasoningSource: "declared", ReasoningEfforts: []string{"HIGH"}},
		{ReasoningStatus: "confirmed", ReasoningSource: "declared", ReasoningEfforts: []string{"auto"}},
		{ReasoningStatus: "unknown", ReasoningSource: "unknown", ReasoningEfforts: []string{"high"}},
		{ReasoningStatus: "invented", ReasoningSource: "manual"},
	} {
		if ValidReasoningCapabilities(c) {
			t.Fatal("invalid catalog accepted", c)
		}
	}
}
