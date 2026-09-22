package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func TestFinalReadinessDoesNotFailForRecoveredActionOrChecklist(t *testing.T) {
	for _, kind := range []string{"recovered action", "rejected checklist", "rejected checklist after earlier failure", "rejected sign-off", "rejected sign-off after earlier failure"} {
		t.Run(kind, func(t *testing.T) {
			todo, ok := tool.LookupBuiltin("todo_write")
			if !ok {
				t.Fatal("missing todo tool")
			}
			reg := tool.NewRegistry()
			reg.Add(todo)
			completeStep, ok := tool.LookupBuiltin("complete_step")
			if !ok {
				t.Fatal("missing complete_step tool")
			}
			reg.Add(completeStep)
			attempts := 0
			reg.Add(batchCancellationTool{fakeTool: fakeTool{name: "bash"}, run: func(context.Context) (string, error) {
				attempts++
				if attempts == 1 {
					return "", errors.New("synthetic temporary failure")
				}
				return "synthetic check passed", nil
			}})
			first := []provider.Chunk{toolCallChunk("todo", "todo_write", `{"todos":[{"content":"Synthetic deferred work","status":"in_progress"}]}`)}
			if strings.HasSuffix(kind, "after earlier failure") {
				first = append(first, toolCallChunk("before", "bash", `{"command":"synthetic check"}`))
			}
			turns := [][]provider.Chunk{first, {{Type: provider.ChunkText, Text: "Synthetic status update."}}}
			if kind == "recovered action" {
				turns = append(turns,
					[]provider.Chunk{toolCallChunk("failed", "bash", `{"command":"synthetic check"}`)},
					[]provider.Chunk{toolCallChunk("recovered", "bash", `{"command":"synthetic check"}`)})
			} else if strings.HasPrefix(kind, "rejected sign-off") {
				turns = append(turns, []provider.Chunk{toolCallChunk("rejected", "complete_step", `{"step":"Synthetic deferred work","result":"unverified","evidence":[]}`)})
			} else {
				turns = append(turns, []provider.Chunk{toolCallChunk("rejected", "todo_write", `{"todos":[{"content":"Synthetic deferred work","status":"completed"}]}`)})
			}
			const final = "Synthetic work remains pending; no completion is claimed."
			turns = append(turns, []provider.Chunk{{Type: provider.ChunkText, Text: final}})
			prov := &scriptedProvider{name: "synthetic", turns: turns}
			var committed []string
			a := New(prov, reg, NewSession(""), Options{MaxSteps: 8}, event.FuncSink(func(e event.Event) {
				if e.Kind == event.AnswerCommitted {
					committed = append(committed, e.Text)
				}
			}))
			if err := a.Run(context.Background(), "synthetic task only"); err != nil {
				t.Fatalf("stale checklist escalated to terminal error: %v", err)
			}
			if prov.call != len(turns) || len(committed) != 1 || committed[0] != final {
				t.Fatal("final response lost or extra remediation requests issued")
			}
			pending, _ := a.evidence.IncompleteLatestTodos()
			if len(pending) != 1 || pending[0].Status != "in_progress" {
				t.Fatal("unfinished work was silently marked complete")
			}
			if strings.HasPrefix(kind, "rejected checklist") && !strings.Contains(lastToolResult(a.session, "todo_write"), "no matching successful complete_step") {
				t.Fatal("unchecked checklist completion was accepted")
			}
			if strings.HasPrefix(kind, "rejected sign-off") && !strings.HasPrefix(lastToolResult(a.session, "complete_step"), "error:") {
				t.Fatal("unverified sign-off was accepted")
			}
		})
	}
}
