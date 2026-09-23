package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func displayFixture() *agent.Session {
	s := agent.NewSession("old system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first question"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "old answer"})
	s.RewriteContext([]provider.Message{{Role: provider.RoleSystem, Content: "old system"}, {Role: provider.RoleUser, Content: "checkpoint"}}, &agent.DisplayEntry{Trigger: "manual", Summary: "summary", Messages: 2})
	s.Add(provider.Message{Role: provider.RoleUser, Content: "second question"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "new answer"})
	return s
}

func TestDisplayHistoryCompactionPreviewRetainsOriginals(t *testing.T) {
	s := displayFixture()
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := previewSessionMessages(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 || got[1].Content != "first question" || got[2].Content != "old answer" || got[3].Role != "compaction" || got[3].Summary != "summary" || got[5].Content != "new answer" {
		t.Fatalf("unexpected history: %+v", got)
	}
	for _, row := range got {
		if row.MessageID == "" {
			t.Fatalf("missing stable row ID: %+v", row)
		}
	}
}

func TestControllerRebuildPreservesDisplayHistory(t *testing.T) {
	s := displayFixture()
	old := control.New(control.Options{Executor: agent.New(nil, nil, s, agent.Options{}, event.Discard)})
	defer old.Close()
	fresh := control.New(control.Options{Executor: agent.New(nil, nil, agent.NewSession("fresh system"), agent.Options{}, event.Discard)})
	defer fresh.Close()
	resumeWithControllerSystem(fresh, old.History(), "", old)
	if len(fresh.DisplayHistory()) != 6 {
		t.Fatalf("display lost on rebuild: %+v", fresh.DisplayHistory())
	}
	if fresh.History()[0].Content != "fresh system" {
		t.Fatal("old system retained in request")
	}
	if fresh.History()[1].Content != "checkpoint" {
		t.Fatal("display history leaked into model context")
	}
}

func TestDisplayHistorySteerDoesNotAdvanceTurnTelemetry(t *testing.T) {
	const steer = "[用户已排队一条中途引导。不要把它当作新任务；请在完成当前步骤后，仅将其作为当前任务的补充指导。]\nagain"
	s := agent.NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "start"})
	s.Add(provider.Message{Role: provider.RoleUser, Content: steer})
	s.Add(provider.Message{Role: provider.RoleUser, Content: steer})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	turns := []turnTelemetryRecord{{TurnID: "one", Outcome: event.TurnOutcomeSuccess}}
	rows := displayHistoryMessages(s.DisplaySnapshot(), func(s string) string { return s }, turns, nil)
	if len(rows) != 5 || rows[1].Role != "steer" || rows[2].Role != "steer" || rows[1].Content != "again" || rows[1].MessageID == rows[2].MessageID {
		t.Fatalf("guidance not preserved independently: %+v", rows)
	}
	for _, row := range rows {
		if row.TurnID != "one" {
			t.Fatalf("guidance advanced turn: %+v", row)
		}
	}
}

func TestGuidanceImageDisplayDoesNotExposeModelManifest(t *testing.T) {
	const prefix = "[用户已排队一条中途引导。不要把它当作新任务；请在完成当前步骤后，仅将其作为当前任务的补充指导。]\n"
	const display = "Review @[chart.png](.orca/attachments/chart.png)"
	s := agent.NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "start"})
	s.AddWithDisplay(provider.Message{Role: provider.RoleUser, Content: prefix + "<attachments>internal manifest</attachments>"}, display)
	path := filepath.Join(t.TempDir(), "synthetic.jsonl")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	rows := displayHistoryMessages(loaded.DisplaySnapshot(), func(s string) string { return s }, nil, nil)
	if len(rows) != 2 || rows[1].Role != "steer" || rows[1].Content != display {
		t.Fatalf("guidance display lost: %+v", rows)
	}
}

func TestGuidanceForkCopiesDisplayedImagesAndFiles(t *testing.T) {
	for _, direct := range []bool{true, false} {
		source, destination := t.TempDir(), t.TempDir()
		if err := os.MkdirAll(filepath.Join(source, ".orca", "attachments"), 0700); err != nil {
			t.Fatal(err)
		}
		for _, file := range []string{"picture.png", "notes.txt"} {
			if err := os.WriteFile(filepath.Join(source, ".orca", "attachments", file), []byte("synthetic-"+file), 0600); err != nil {
				t.Fatal(err)
			}
		}
		s := agent.NewSession("")
		m := provider.Message{Role: provider.RoleUser, Content: "[用户已排队一条中途引导。不要把它当作新任务；请在完成当前步骤后，仅将其作为当前任务的补充指导。]\n@.orca/attachments/picture.png @.orca/attachments/notes.txt"}
		if direct {
			m.Images = []provider.ImageContent{{Path: ".orca/attachments/picture.png"}}
		}
		s.AddWithDisplay(m, "@[picture.png](.orca/attachments/picture.png) @[notes.txt](.orca/attachments/notes.txt)")
		path := filepath.Join(source, "fork.jsonl")
		if err := s.Save(path); err != nil {
			t.Fatal(err)
		}
		fork, err := migrateForkSessionToWorkspace(path, source, destination)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := agent.LoadSession(fork)
		if err != nil {
			t.Fatal(err)
		}
		display := loaded.DisplaySnapshot()[0].DisplayText
		refs := regexp.MustCompile(`\.orca/attachments/[^)\s]+`).FindAllString(display, -1)
		if len(refs) != 2 {
			t.Fatalf("missing display references: %s", display)
		}
		for i, ref := range refs {
			data, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(ref)))
			if err != nil || string(data) != []string{"synthetic-picture.png", "synthetic-notes.txt"}[i] {
				t.Fatalf("fork attachment unavailable: %s %v", ref, err)
			}
		}
		if s.DisplaySnapshot()[0].DisplayText != "@[picture.png](.orca/attachments/picture.png) @[notes.txt](.orca/attachments/notes.txt)" {
			t.Fatal("source history mutated")
		}
	}
}

func TestDisplayHistoryModeSwitchAnchorsSurviveCompaction(t *testing.T) {
	s := displayFixture()
	entries := s.DisplaySnapshot()
	switches := []RuntimeSwitchRecord{{ID: "switch", MessageIndex: len(s.Snapshot()), DisplayAfterID: entries[len(entries)-1].ID}}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "after switch"})
	rows := displayHistoryMessages(s.DisplaySnapshot(), func(s string) string { return s }, nil, switches)
	if len(rows) != 8 || rows[5].Content != "new answer" || rows[6].Role != "mode_switch" || rows[7].Content != "after switch" {
		t.Fatalf("mode switch moved into pre-compression history: %+v", rows)
	}
	if switches[0].MessageIndex != 4 {
		t.Fatal("display conversion mutated persisted legacy context index")
	}
	rows = displayHistoryMessages(entries[:3], func(s string) string { return s }, nil, switches)
	for _, row := range rows {
		if row.Role == "mode_switch" {
			t.Fatal("removed anchor attached to unrelated branch")
		}
	}
}
