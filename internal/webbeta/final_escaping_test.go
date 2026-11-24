package webbeta

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

const escapingAnswer = `{"nonce":"65db6faa931e1bf43261851c85e2d439ae0c1170d6a17887","sum":123,"count":3}`

func escapingEnvelope(text string) string {
	raw, _ := json.Marshal(map[string]string{"type": "final", "text": text})
	return turnMarker + string(raw)
}

func TestFinalEscapingConfirmedFailureAndLosslessEncoding(t *testing.T) {
	bad := turnMarker + `{"type":"final","text":"` + escapingAnswer + `"}`
	if len(bad) != 119 || contentHash(bad) != "4c225f79a20f871a698147e6f7f0eff47e94685df5e269d92e7e725cbe85b94c" {
		t.Fatal("synthetic fixture differs from confirmed failure")
	}
	if turn, err := parseTurn(bad, nil, nil, currentGrammar); err == nil || len(turn.Calls) != 0 || turn.Final != "" || !repairableTurn(bad, nil, nil) {
		t.Fatal("unescaped final accepted or became executable")
	}
	for _, text := range []string{escapingAnswer, "print(\"hello\")\npath = \"C:\\fixture\\note.txt\"", "第一行\n\"第二行\"\t\\"} {
		turn, err := parseTurn(escapingEnvelope(text), nil, nil, currentGrammar)
		if err != nil || turn.Final != text || len(turn.Calls) != 0 {
			t.Fatal("encoded answer changed", err)
		}
	}
	for _, raw := range []string{turnMarker + `{"type":"final","text":` + escapingAnswer + `}`, turnMarker + `{"type":"final","text":"one","text":"two"}`, escapingEnvelope(escapingAnswer) + "\n" + escapingEnvelope("extra")} {
		if turn, err := parseTurn(raw, nil, nil, currentGrammar); err == nil || len(turn.Calls) != 0 {
			t.Fatal("strict final boundary widened")
		}
	}
}

func TestFinalEscapingInstructionsExplainNestedJSON(t *testing.T) {
	for _, instructions := range []string{protocolInstructions(), transportContract} {
		if !strings.Contains(instructions, "text 始终是 JSON 字符串") || !strings.Contains(instructions, `ORCA_TURN_V2 {"type":"final","text":"{\"ok\":true}"}`) || !strings.Contains(instructions, "反斜线") {
			t.Fatal("missing explicit nested JSON encoding guidance")
		}
	}
}

type escapingTool struct {
	name  string
	calls atomic.Int32
}

func (t *escapingTool) Name() string      { return t.name }
func (*escapingTool) Description() string { return "Synthetic in-memory fixture only" }
func (*escapingTool) ReadOnly() bool      { return true }
func (*escapingTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (t *escapingTool) Execute(context.Context, json.RawMessage) (string, error) {
	t.calls.Add(1)
	return "synthetic receipt", nil
}

type escapingBrowser struct {
	fakeBrowser
	requests                            int
	url                                 string
	ids                                 map[string]bool
	keepInvalid, correctionInstructions bool
}

func (b *escapingBrowser) Run(ctx context.Context, c Command, emit func(Event) error) error {
	if c.URL == "" {
		return b.fakeBrowser.Run(ctx, c, emit)
	}
	b.requests++
	if b.url == "" {
		b.url = c.URL
	}
	if c.URL != b.url || b.ids[c.ID] {
		return errors.New("changed chat or replayed request")
	}
	b.ids[c.ID] = true
	output := turnMarker + `{"type":"tools","calls":[{"id":"lookup","name":"synthetic_lookup","arguments":{}},{"id":"add","name":"synthetic_add","arguments":{}},{"id":"count","name":"synthetic_count","arguments":{}}]}`
	switch b.requests {
	case 1:
	case 2:
		for _, id := range []string{"lookup", "add", "count"} {
			if !strings.Contains(c.Text, `"tool_call_id":"`+id+`"`) {
				return errors.New("missing actual host receipt")
			}
		}
		output = turnMarker + `{"type":"final","text":"` + escapingAnswer + `"}`
	case 3:
		b.correctionInstructions = strings.Contains(c.Text, protocolInstructions()) && strings.Contains(c.Text, `ORCA_TURN_V2 {"type":"final","text":"{\"ok\":true}"}`)
		output = escapingEnvelope(escapingAnswer)
		if b.keepInvalid {
			output = turnMarker + `{"type":"final","text":"` + escapingAnswer + `"}`
		}
	default:
		return errors.New("more than one correction")
	}
	if err := emit(Event{ID: c.ID, Kind: "text", Text: output}); err != nil {
		return err
	}
	return emit(Event{ID: c.ID, Kind: "done", URL: c.URL})
}

func TestFinalEscapingOneCorrectionNeverReplaysCompletedTools(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "corrected", true: "still-invalid"}[invalid], func(t *testing.T) {
			b := &escapingBrowser{ids: map[string]bool{}, keepInvalid: invalid}
			e := testEngine(t, b)
			old := CurrentEngine()
			SetEngine(e)
			defer SetEngine(old)
			reg := tool.NewRegistry()
			var tools []*escapingTool
			for _, name := range []string{"synthetic_lookup", "synthetic_add", "synthetic_count"} {
				capability := &escapingTool{name: name}
				reg.Add(capability)
				tools = append(tools, capability)
			}
			commits := 0
			final := ""
			sink := event.FuncSink(func(ev event.Event) {
				if ev.Kind == event.AnswerCommitted {
					commits++
					final = ev.Text
				}
			})
			a := agent.New(&Adapter{name: "synthetic-web"}, reg, agent.NewSession("Synthetic offline escaping acceptance"), agent.Options{MaxSteps: 4, Gate: &probeGate{allow: true}}, sink)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := a.Run(ctx, "Use the synthetic capabilities, then return the synthetic JSON result.")
			for _, capability := range tools {
				if capability.calls.Load() != 1 {
					t.Fatal("completed tool replayed or skipped")
				}
			}
			if b.requests != 3 || !b.correctionInstructions {
				t.Fatal("correction unbounded or missing escaping guidance")
			}
			if invalid {
				if err == nil || !provider.ReplayForbidden(err) || commits != 0 {
					t.Fatal("invalid correction must fail closed")
				}
			} else if err != nil || commits != 1 || final != escapingAnswer {
				t.Fatal("corrected final changed", err)
			}
			loaded, loadErr := loadState(e.path)
			if loadErr != nil || len(loaded.Tasks) != 1 {
				t.Fatal("missing parent budget", loadErr)
			}
			for _, budget := range loaded.Tasks {
				if budget.Actions != 3 || budget.Corrections != 1 || budget.Generations != 4 {
					t.Fatalf("unexpected budget: %+v", budget)
				}
			}
		})
	}
}
