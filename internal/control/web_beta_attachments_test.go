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
)

type webSnapshotRunner struct {
	received agent.RichInput
	calls    int
}

func (*webSnapshotRunner) Run(context.Context, string) error { return nil }
func (r *webSnapshotRunner) RunRich(_ context.Context, input agent.RichInput) error {
	r.received = input
	r.calls++
	return nil
}

func TestWebBetaFileSnapshotsRequireReadAndExportApproval(t *testing.T) {
	for _, deny := range []string{"", "read_file", "read_file(note.txt)", "file_send"} {
		t.Run("deny="+deny, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "note.txt")
			if err := os.WriteFile(path, []byte("approved fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			var denied []string
			if deny != "" {
				denied = []string{deny}
			}
			c := New(Options{WorkspaceRoot: root, ModelRef: "deepseek-web-beta/synthetic", ProviderKind: "deepseekweb-beta", Policy: permission.New("ask", nil, nil, denied), VisionMode: "auto"})
			defer c.Close()
			var approvals []string
			c.sink = event.FuncSink(func(ev event.Event) {
				if ev.Kind == event.ApprovalRequest {
					approvals = append(approvals, ev.Approval.Tool)
					if ev.Approval.Tool == "file_send" {
						if err := os.WriteFile(path, []byte("replaced after snapshot"), 0600); err != nil {
							t.Error(err)
						}
					}
					c.Approve(ev.Approval.ID, true, false, false)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			rich, err := c.resolveWebBetaInput(ctx, "read @note.txt", "read @note.txt")
			if deny != "" {
				if err == nil || len(rich.Files) != 0 {
					t.Fatal("denied snapshot escaped", err)
				}
				return
			}
			if err != nil || len(rich.Files) != 1 || string(rich.Files[0].Data) != "approved fixture" {
				t.Fatal("snapshot changed after approval", err)
			}
			if strings.Join(approvals, ",") != "read_file,file_send" || strings.Contains(rich.Text, "@note.txt") {
				t.Fatal("attachment was not explicitly authorized and substituted")
			}
		})
	}
}

func TestWebBetaControllerActuallyPassesFileOnlyRichInput(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("synthetic bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &webSnapshotRunner{}
	done := make(chan error, 1)
	c := New(Options{Runner: runner, WorkspaceRoot: root, ModelRef: "deepseek-web-beta/synthetic", ProviderKind: "deepseekweb-beta", Policy: permission.New("allow", nil, nil, nil), Sink: event.FuncSink(func(ev event.Event) {
		if ev.Kind == event.TurnDone {
			done <- ev.Err
		}
	})})
	defer c.Close()
	c.SetToolApprovalMode(ToolApprovalAuto)
	c.runRefTurnWithRefs("inspect @note.txt", "@note.txt", "")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not finish")
	}
	if runner.calls != 1 || len(runner.received.Files) != 1 || string(runner.received.Files[0].Data) != "synthetic bytes" {
		t.Fatal("file-only turn fell back to string runner")
	}
}

func TestWebBetaAttachmentsRejectVisionOffAndMissingSnapshots(t *testing.T) {
	root := t.TempDir()
	putTaskImage(t, root, "fixture.png", taskImagePNG(t, 20))
	c := New(Options{WorkspaceRoot: root, ModelRef: "deepseek-web-beta/synthetic", ProviderKind: "deepseekweb-beta", Policy: permission.New("allow", nil, nil, nil), VisionMode: "off"})
	defer c.Close()
	c.SetToolApprovalMode(ToolApprovalAuto)
	for _, input := range []string{"@fixture.png", "@.orca/attachments/missing.txt"} {
		if rich, err := c.resolveWebBetaInput(context.Background(), input, input); err == nil || len(rich.Files) != 0 {
			t.Fatal("unauthorized attachment accepted", input, err)
		}
	}
}
