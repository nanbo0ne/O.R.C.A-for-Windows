package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/permission"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type kindSnapshotRunner struct {
	webSnapshotRunner
	plainCalls int
}

func (r *kindSnapshotRunner) Run(context.Context, string) error {
	r.plainCalls++
	return nil
}

func TestProviderKindControlsWebAttachmentAuthorization(t *testing.T) {
	for _, tc := range []struct {
		ref, kind string
		web       bool
	}{
		{"alias/synthetic", "deepseekweb-beta", true},
		{"deepseek-web-beta/synthetic", "deepseekweb-beta", true},
		{"deepseek-web-beta/synthetic", "openai", false},
		{"deepseek-web-beta/synthetic", "", false},
	} {
		t.Run(tc.ref+"/"+tc.kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("synthetic snapshot"), 0600); err != nil {
				t.Fatal(err)
			}
			runner := &kindSnapshotRunner{}
			done := make(chan error, 1)
			c := New(Options{Runner: runner, WorkspaceRoot: root, ModelRef: tc.ref, ProviderKind: tc.kind,
				Policy: permission.New("allow", nil, nil, []string{"file_send"}), Sink: event.FuncSink(func(e event.Event) {
					if e.Kind == event.TurnDone {
						done <- e.Err
					}
				})})
			defer c.Close()
			c.SetToolApprovalMode(ToolApprovalAuto)
			c.runRefTurnWithRefs("inspect @note.txt", "@note.txt", "")
			select {
			case err := <-done:
				if tc.web && (err == nil || !strings.Contains(err.Error(), "file_send")) {
					t.Fatal("web alias skipped export authorization", err)
				}
				if !tc.web && err != nil {
					t.Fatal("name incorrectly selected web attachment authorization", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("synthetic turn did not finish")
			}
			if tc.web && runner.calls+runner.plainCalls != 0 {
				t.Fatal("denied attachment reached runner")
			}
			if !tc.web && runner.plainCalls != 1 {
				t.Fatal("ordinary API reference flow was changed")
			}
			if got := c.ContextMetadata().ProviderKind; got != tc.kind {
				t.Fatalf("bound Kind = %q, want %q", got, tc.kind)
			}
		})
	}
}

func TestProviderKindControlsWebGuidanceAuthorization(t *testing.T) {
	for _, tc := range []struct {
		ref, kind string
		web       bool
	}{
		{"alias/synthetic", "deepseekweb-beta", true},
		{"deepseek-web-beta/synthetic", "openai", false},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("synthetic guidance"), 0600); err != nil {
				t.Fatal(err)
			}
			p := &guidanceProvider{waitForGuidance: true}
			reg := tool.NewRegistry()
			reg.Add(fakeControlTool{name: "guidance_wait"})
			started := make(chan string, 1)
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.TurnStarted {
					started <- e.TurnID
				}
			})
			executor := agent.New(p, reg, agent.NewSession(""), agent.Options{}, sink)
			c := New(Options{Runner: executor, Executor: executor, WorkspaceRoot: root, ModelRef: tc.ref, ProviderKind: tc.kind,
				Policy: permission.New("allow", nil, nil, []string{"file_send"})})
			c.SetToolApprovalMode(ToolApprovalAuto)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			done := make(chan error, 1)
			go func() { done <- c.RunTurn(ctx, "Original synthetic task") }()
			t.Cleanup(func() { cancel(); c.Close() })
			var turn string
			select {
			case turn = <-started:
			case <-ctx.Done():
				t.Fatal("synthetic task did not start")
			}
			err := c.SteerDisplay("inspect @note.txt", "inspect @note.txt", turn, "synthetic-guidance")
			if tc.web {
				cancel()
				if err == nil || !strings.Contains(err.Error(), "file_send") {
					t.Errorf("web guidance skipped export authorization: %v", err)
				}
			} else if err != nil {
				t.Errorf("API guidance incorrectly selected web export: %v", err)
			}
			select {
			case runErr := <-done:
				if !tc.web && runErr != nil {
					t.Fatal(runErr)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("synthetic guidance task did not drain")
			}
		})
	}
}
