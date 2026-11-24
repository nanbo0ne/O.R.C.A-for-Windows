package webbeta

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type probeTool struct{ calls int }

func (*probeTool) Name() string        { return "synthetic_echo" }
func (*probeTool) Description() string { return "Return the provided integer for an isolated test" }
func (*probeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"],"additionalProperties":false}`)
}
func (*probeTool) ReadOnly() bool { return true }
func (t *probeTool) Execute(context.Context, json.RawMessage) (string, error) {
	t.calls++
	return "echo-result:7", nil
}

type probeGate struct {
	allow bool
	calls int
}

func (g *probeGate) Check(context.Context, string, json.RawMessage, bool) (bool, string, error) {
	g.calls++
	return g.allow, "synthetic gate decision", nil
}

type toolLoopBrowser struct {
	fakeBrowser
	taskCalls int
}

func (b *toolLoopBrowser) Run(ctx context.Context, c Command, emit func(Event) error) error {
	if !strings.Contains(c.Text, "可用工具") {
		return b.fakeBrowser.Run(ctx, c, emit)
	}
	b.taskCalls++
	output := `ORCA_TOOL_V1 {"id":"synthetic-call-1","name":"synthetic_echo","arguments":{"value":7}}`
	if b.taskCalls > 1 {
		output = `ORCA_TURN_V2 {"type":"final","text":"Synthetic final answer"}`
	}
	for _, part := range []string{output[:8], output[8:]} {
		if err := emit(Event{ID: c.ID, Kind: "text", Text: part}); err != nil {
			return err
		}
	}
	return emit(Event{ID: c.ID, Kind: "done", URL: c.URL})
}
func TestRealAgentUsesWebAdapterAndPermissionGate(t *testing.T) {
	for _, allow := range []bool{true, false} {
		t.Run(map[bool]string{true: "allowed", false: "denied"}[allow], func(t *testing.T) {
			b := &toolLoopBrowser{}
			e := testEngine(t, b)
			old := CurrentEngine()
			SetEngine(e)
			defer SetEngine(old)
			registry := tool.NewRegistry()
			capability := &probeTool{}
			registry.Add(capability)
			gate := &probeGate{allow: allow}
			var text strings.Builder
			sink := event.Lifecycle(event.FuncSink(func(ev event.Event) {
				if ev.Kind == event.Text {
					text.WriteString(ev.Text)
				}
			}))
			a := agent.New(&Adapter{name: "synthetic-web"}, registry, agent.NewSession("synthetic web integration"), agent.Options{Gate: gate}, sink)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := a.Run(ctx, "Use synthetic_echo with value 7, then answer."); err != nil {
				t.Fatal(err)
			}
			want := 0
			if allow {
				want = 1
			}
			if capability.calls != want || gate.calls != 1 || b.taskCalls != 2 {
				t.Fatalf("tool=%d gate=%d provider=%d", capability.calls, gate.calls, b.taskCalls)
			}
			if text.String() != "Synthetic final answer" {
				t.Fatalf("tool syntax leaked or answer missing: %q", text.String())
			}
		})
	}
}
