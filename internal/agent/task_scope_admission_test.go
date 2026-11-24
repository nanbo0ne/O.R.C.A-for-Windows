package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type taskScopeWaitingAdmission struct {
	*taskScopeProvider
	entered  chan taskScopeRecord
	decision chan error
}

func (p *taskScopeWaitingAdmission) BeginTask(ctx context.Context, conversationID, parentTaskID string) (context.Context, func(), error) {
	p.entered <- taskScopeRecord{ctx, conversationID, parentTaskID}
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case err := <-p.decision:
		if err != nil {
			return nil, nil, err
		}
		return p.taskScopeProvider.BeginTask(ctx, conversationID, parentTaskID)
	}
}

func TestTaskScopeAdmissionFailureKeepsInputAndClosesGuidance(t *testing.T) {
	for _, failure := range []string{"rejected", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			base := newTaskScopeProvider(t, taskScopeAnswer())
			p := &taskScopeWaitingAdmission{taskScopeProvider: base, entered: make(chan taskScopeRecord, 1), decision: make(chan error, 1)}
			session := NewSession("synthetic system")
			session.Add(provider.Message{Role: provider.RoleUser, Content: "synthetic previous task"})
			session.Add(provider.Message{Role: provider.RoleAssistant, Content: "synthetic previous answer"})
			before := session.Snapshot()
			input := RichInput{
				Text:   "synthetic accepted input awaiting initialization",
				Images: []provider.ImageContent{{Name: "synthetic.png", Path: ".orca/attachments/synthetic.png", MediaType: "image/png"}},
				Files:  []provider.FileContent{{Name: "synthetic.txt", MediaType: "text/plain", Digest: "synthetic-digest", Data: []byte("synthetic file snapshot")}},
			}
			wantHistory := append(before[:len(before):len(before)], provider.Message{Role: provider.RoleUser, Content: input.Text, Images: input.Images, Files: input.Files})
			reg := tool.NewRegistry()
			toolCalls := 0
			reg.Add(taskScopeTool{"forbidden", func(context.Context) (string, error) {
				toolCalls++
				return "unexpected", nil
			}})
			var a *Agent
			var startAck <-chan error
			var turns []string
			steers := 0
			a = New(p, reg, session, Options{}, event.FuncSink(func(e event.Event) {
				switch e.Kind {
				case event.TurnStarted:
					turns = append(turns, e.TurnID)
					if len(turns) == 1 {
						var accepted bool
						startAck, accepted = a.TrySteerRich(RichInput{Text: "synthetic start guidance"}, "synthetic start guidance", nil, "at-turn-start")
						if !accepted {
							t.Error("TurnStarted did not open guidance admission")
						}
					}
				case event.Steer:
					steers++
				}
			}))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- a.RunRich(ctx, input) }()
			var admitted taskScopeRecord
			select {
			case admitted = <-p.entered:
			case <-ctx.Done():
				t.Fatal("run did not reach task admission")
			}
			if !reflect.DeepEqual(session.Snapshot(), wantHistory) {
				t.Error("accepted input and attachment snapshots were not recorded before task admission waited")
			}
			if len(turns) != 1 || turns[0] == "" || turns[0] != admitted.taskID || admitted.conversationID != session.ConversationID() {
				t.Error("task admission ran before normal turn initialization")
			}
			waitAck, accepted := a.TrySteerRich(RichInput{Text: "synthetic waiting guidance"}, "synthetic waiting guidance", nil, "during-admission")
			if !accepted {
				t.Error("initialization wait did not accept guidance")
			}
			wantErr := errors.New("synthetic initialization rejected")
			if failure == "canceled" {
				wantErr = context.Canceled
				cancel()
			} else {
				p.decision <- wantErr
			}
			if err := taskScopeResult(t, done); !errors.Is(err, wantErr) {
				t.Errorf("RunRich = %v, want admission failure %v", err, wantErr)
			}
			if !reflect.DeepEqual(session.Snapshot(), wantHistory) {
				t.Error("admission failure changed prior history or lost accepted input/attachments")
			}
			for id, ack := range map[string]<-chan error{"at-turn-start": startAck, "during-admission": waitAck} {
				if ack == nil {
					t.Errorf("missing accepted guidance receipt %q", id)
					continue
				}
				if err := guidanceReceipt(t, ack); !errors.Is(err, wantErr) || !strings.Contains(err.Error(), id) || !strings.Contains(err.Error(), "draft is preserved") {
					t.Errorf("guidance %q did not receive its correlated failure/preserved-draft receipt: %v", id, err)
				}
			}
			if steers != 0 || len(base.requests) != 0 || toolCalls != 0 {
				t.Errorf("failed admission reached execution: steers=%d streams=%d tools=%d", steers, len(base.requests), toolCalls)
			}
			if active, released := base.counts(); active != 0 || released != 0 {
				t.Errorf("failed admission acquired/released a nonexistent scope: active=%d released=%d", active, released)
			}
			a.steerMu.Lock()
			closed, pending, consumed := a.steerClosed, len(a.steerQueue), a.steerConsumed
			a.steerMu.Unlock()
			if !closed || pending != 0 || consumed {
				t.Errorf("failed run left guidance state open: closed=%v pending=%d consumed=%v", closed, pending, consumed)
			}
			if ack, accepted := a.TrySteerRich(RichInput{Text: "late synthetic guidance"}, "", nil, "late"); accepted || ack != nil {
				t.Error("failed run accepted late guidance")
			}
			// Recovered initialization must preserve history without replaying the
			// failed turn's queued guidance into this independent run.
			a.prov = base
			if err := a.Run(context.Background(), "synthetic recovery turn"); err != nil {
				t.Fatal(err)
			}
			if len(turns) != 2 || turns[0] == turns[1] || len(base.requests) != 1 || steers != 0 {
				t.Errorf("recovery reused the failed turn or its guidance: turns=%v requests=%d steers=%d", turns, len(base.requests), steers)
			}
			history := session.Snapshot()
			if len(history) != len(wantHistory)+2 || !reflect.DeepEqual(history[:len(wantHistory)], wantHistory) {
				t.Error("recovery lost or rewrote the accepted failed turn")
			}
		})
	}
}
