package agent

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestToolOutcomesPersistOutsideModelHistory(t *testing.T) {
	s := NewSession("")
	for _, status := range []string{"done", "error", "stopped"} {
		s.AddToolResult(provider.Message{Role: provider.RoleTool, ToolCallID: status, Content: "synthetic result"}, status, map[string]string{"error": "failed", "stopped": "cancelled"}[status])
	}
	before := s.Snapshot()
	path := filepath.Join(t.TempDir(), "synthetic.jsonl")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	restored, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, restored.Snapshot()) || !reflect.DeepEqual(s.DisplaySnapshot(), restored.DisplaySnapshot()) {
		t.Fatal("tool display outcome was lost or changed model history")
	}
	entries := restored.DisplaySnapshot()
	if entries[0].ToolStatus != "done" || entries[1].ToolError != "failed" || entries[2].ToolStatus != "stopped" {
		t.Fatal("missing real outcomes", entries)
	}
}
