package webbeta

import (
	"context"
	"errors"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestToolReceiptIsNotPageDelivery(t *testing.T) {
	for _, outcome := range []string{"success", "unsent", "uncertain"} {
		t.Run(outcome, func(t *testing.T) {
			e, r, turn := ledgerFixture(t)
			r.ID = "result-request"
			r.Messages = append(r.Messages, provider.Message{Role: provider.RoleAssistant, Content: turn.Progress, ToolCalls: turn.Calls},
				provider.Message{Role: provider.RoleTool, ToolCallID: "a", Name: "read_file", Content: "value-a"},
				provider.Message{Role: provider.RoleTool, ToolCallID: "b", Name: "read_file", Content: "value-b"})
			e.mu.Lock()
			if err := e.acknowledgeResultsLocked(r); err != nil {
				t.Fatal(err)
			}
			if e.state.Calls[0].ResultDelivery != "pending" {
				t.Fatal("host receipt claimed page delivery")
			}
			e.mu.Unlock()
			b := e.browser.(*fakeBrowser)
			want := "response_received"
			if outcome == "unsent" {
				b.fail = KnownUnsent(errors.New("not dispatched"))
				want = "pending"
			}
			if outcome == "uncertain" {
				b.fail = &Failure{Code: "connection_lost", Sent: true}
				want = "unconfirmed"
			}
			err := e.Send(context.Background(), r, func(Event) error { return nil })
			if (err == nil) != (outcome == "success") {
				t.Fatal(err)
			}
			saved, err := loadState(e.path)
			if err != nil {
				t.Fatal(err)
			}
			for _, call := range saved.Calls {
				if call.Status != "returned" || call.ResultDelivery != want || call.ResultRequestID != r.ID {
					t.Fatalf("wrong checkpoint: %+v", call)
				}
			}
		})
	}
}

func TestResultDeliveryScopeAndRestart(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	e.state.Calls = []CallRecord{
		{Owner: "a", ID: "c", Name: "read_file", Status: "returned", ResultHash: contentHash("value"), ResultDelivery: "pending"},
		{Owner: "b", ID: "c", Name: "read_file", Status: "returned", ResultHash: contentHash("value"), ResultDelivery: "pending"},
		{Owner: "a", ID: "legacy", Status: "returned"},
	}
	r := Request{Owner: "a", ID: "current"}
	e.sendingResultsLocked(r, []provider.Message{{Role: provider.RoleTool, ToolCallID: "c", Name: "read_file", Content: "value"}})
	e.finishResultDeliveryLocked(Request{Owner: "a", ID: "stale"}, "response_received")
	if e.state.Calls[0].ResultDelivery != "sending" || e.state.Calls[1].ResultDelivery != "pending" {
		t.Fatal("crossed owner or request")
	}
	if err := saveState(e.path, e.state); err != nil {
		t.Fatal(err)
	}
	saved, err := loadState(e.path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Calls[0].ResultDelivery != "unconfirmed" || saved.Calls[2].ResultDelivery != "" {
		t.Fatal("restart fabricated delivery")
	}
}
