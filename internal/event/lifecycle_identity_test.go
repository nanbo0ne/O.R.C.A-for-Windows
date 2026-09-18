package event

import (
	"errors"
	"reflect"
	"testing"
)

func TestLifecycleDifferentTurnResetsAllItemState(t *testing.T) {
	var got []Event
	s := Lifecycle(FuncSink(func(e Event) { got = append(got, e) }))
	emit := func(e Event) Event {
		s.Emit(e)
		return got[len(got)-1]
	}
	emit(Event{Kind: TurnStarted, TurnID: "old"})
	oldMessage := emit(Event{Kind: Message, Text: "old final"})
	emit(Event{Kind: AnswerCommitted})
	oldReasoning := emit(Event{Kind: Reasoning, Text: "old reasoning"})
	oldTool := emit(Event{Kind: ToolDispatch, Tool: Tool{ID: "reused"}})
	emit(Event{Kind: CompactionStarted})
	if e := emit(Event{Kind: TurnStarted, TurnID: "new"}); e.TurnID != "new" {
		t.Fatalf("new start lost identity: %+v", e)
	}
	newReasoning := emit(Event{Kind: Reasoning, Text: "new reasoning"})
	if newReasoning.TurnID != "new" || newReasoning.ItemID == oldReasoning.ItemID || newReasoning.MessageID == oldReasoning.MessageID {
		t.Fatalf("reasoning state leaked: old=%+v new=%+v", oldReasoning, newReasoning)
	}
	newTool := emit(Event{Kind: ToolDispatch, Tool: Tool{ID: "reused"}})
	if newTool.ItemID == oldTool.ItemID || newTool.TurnID != "new" {
		t.Fatalf("tool state leaked: old=%+v new=%+v", oldTool, newTool)
	}
	before := len(got)
	compaction := emit(Event{Kind: CompactionDone})
	if compaction.ItemID != "" || len(got) != before+1 {
		t.Fatal("new turn completed old compaction")
	}
	done := emit(Event{Kind: TurnDone, TurnID: "new", Outcome: TurnOutcomeSuccess})
	if done.Outcome != TurnOutcomeInterrupted || done.FinalItemID != "" || done.FinalMessageID != "" {
		t.Fatalf("new turn inherited old committed answer %q: %+v", oldMessage.MessageID, done)
	}
}

func TestLifecycleStaleTerminalPreservesAggregateAndActiveAnswer(t *testing.T) {
	for _, outcome := range []TurnOutcome{"", TurnOutcomeSuccess, TurnOutcomeCancelled, TurnOutcomeFailed} {
		t.Run(string(outcome), func(t *testing.T) {
			var got []Event
			s := Lifecycle(FuncSink(func(e Event) { got = append(got, e) }))
			s.Emit(Event{Kind: TurnStarted, TurnID: "old"})
			s.Emit(Event{Kind: TurnStarted, TurnID: "new"})
			s.Emit(Event{Kind: Message, Text: "new final"})
			message := got[len(got)-1]
			s.Emit(Event{Kind: AnswerCommitted})
			old := Event{Kind: TurnDone, TurnID: "old", Outcome: outcome,
				FinalItemID: "old-item", FinalMessageID: "old-message", Text: "aggregate receipt"}
			if outcome == TurnOutcomeFailed {
				old.Err = errors.New("old failure")
			}
			before := len(got)
			s.Emit(old)
			if len(got) != before+1 || !reflect.DeepEqual(got[len(got)-1], old) {
				t.Fatalf("stale aggregate event changed: %+v", got[before:])
			}
			s.Emit(Event{Kind: TurnDone, TurnID: "new", Outcome: TurnOutcomeSuccess})
			done := got[len(got)-1]
			if done.Outcome != TurnOutcomeSuccess || done.FinalItemID != message.ItemID || done.FinalMessageID != message.MessageID {
				t.Fatalf("stale terminal corrupted current final answer: %+v", done)
			}
			// Duplicate old terminals after the current turn closes remain receipts.
			s.Emit(old)
			if !reflect.DeepEqual(got[len(got)-1], old) {
				t.Fatal("duplicate historical terminal was rewritten")
			}
		})
	}
}

func TestLifecycleRepeatedAndLegacyStartsKeepCurrentItems(t *testing.T) {
	for _, repeatID := range []string{"active", ""} {
		t.Run("repeat-"+repeatID, func(t *testing.T) {
			var got []Event
			s := Lifecycle(FuncSink(func(e Event) { got = append(got, e) }))
			s.Emit(Event{Kind: TurnStarted, TurnID: "active"})
			s.Emit(Event{Kind: Text, Text: "first"})
			first := got[len(got)-1]
			s.Emit(Event{Kind: TurnStarted, TurnID: repeatID})
			s.Emit(Event{Kind: Text, Text: "second"})
			second := got[len(got)-1]
			if second.TurnID != "active" || second.ItemID != first.ItemID || second.MessageID != first.MessageID {
				t.Fatal("same-turn or legacy start reset the active stream")
			}
			s.Emit(Event{Kind: Message, Text: "final"})
			s.Emit(Event{Kind: AnswerCommitted})
			s.Emit(Event{Kind: TurnDone, Outcome: TurnOutcomeSuccess})
			if done := got[len(got)-1]; done.TurnID != "active" || done.Outcome != TurnOutcomeSuccess {
				t.Fatalf("legacy terminal no longer completes current turn: %+v", done)
			}
		})
	}
}
