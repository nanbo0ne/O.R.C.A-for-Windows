package webbeta

import (
	"context"
	"errors"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type cancelledDeliveryBrowser struct {
	fakeBrowser
	tasks int
}

func (b *cancelledDeliveryBrowser) Run(ctx context.Context, cmd Command, emit func(Event) error) error {
	if cmd.Operation == "opening" {
		return b.fakeBrowser.Run(ctx, cmd, emit)
	}
	b.tasks++
	text := `ORCA_PROGRESS_V2 {"text":"Preparing synthetic calls"}` + "\n" + `ORCA_TURN_V2 {"type":"tools","calls":[{"id":"first","name":"synthetic_echo","arguments":{"value":7}},{"id":"second","name":"synthetic_echo","arguments":{"value":7}}]}`
	if b.tasks > 1 {
		text = `ORCA_TURN_V2 {"type":"final","text":"Fresh task finished"}`
	}
	if err := emit(Event{ID: cmd.ID, Kind: "text", Text: text}); err != nil {
		return err
	}
	return emit(Event{ID: cmd.ID, Kind: "done", URL: cmd.URL})
}

func TestCancelledUnexecutedDeliveryCanContinueWithoutReplay(t *testing.T) {
	b := &cancelledDeliveryBrowser{}
	e := testEngine(t, b)
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg := tool.NewRegistry()
	capability := &probeTool{}
	reg.Add(capability)
	a := agent.New(&Adapter{name: "synthetic"}, reg, agent.NewSession("synthetic"), agent.Options{MaxSteps: 2}, event.FuncSink(func(ev event.Event) {
		if ev.Kind == event.Message {
			cancel()
		}
	}))
	if err := a.Run(ctx, "Synthetic first task"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if capability.calls != 0 {
		t.Fatal("cancelled calls executed")
	}
	if err := a.Run(context.Background(), "Fresh task; do not replay cancelled tools"); err != nil {
		t.Fatal("fresh turn failed", err)
	}
	if capability.calls != 0 || b.tasks != 2 {
		t.Fatalf("tool replay or extra requests: tools=%d tasks=%d", capability.calls, b.tasks)
	}
	if len(e.state.Calls) != 2 {
		t.Fatal("unexpected call ledger")
	}
	for _, call := range e.state.Calls {
		if call.Status != "returned" {
			t.Fatal("cancel receipt was not reconciled")
		}
	}
}
