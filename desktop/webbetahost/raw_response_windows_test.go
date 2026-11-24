//go:build windows

package webbetahost

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

func TestPageCommandOpeningWire(t *testing.T) {
	fixtures := map[string]json.RawMessage{}
	for _, row := range []struct {
		name, operation, url string
		opening              bool
	}{
		{"wire-opening-first", "opening", webbeta.Origin + "/a/chat/s/synthetic-chat", true},
		{"wire-opening-next", "opening", webbeta.Origin + "/a/chat/s/synthetic-chat", true},
		{"wire-opening-empty-url", "opening", "", true},
		{"wire-task", "task", webbeta.Origin + "/a/chat/s/synthetic-chat", false},
		{"wire-correction", "", webbeta.Origin + "/a/chat/s/synthetic-chat", false},
	} {
		cmd := webbeta.Command{Operation: row.operation, ID: row.name, Action: "send", URL: row.url, Text: "synthetic prompt\n"}
		raw, err := marshalPageCommand(cmd)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err = json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["opening"] != row.opening {
			t.Fatal("opening lost at host serialization", row.name)
		}
		if _, ok := decoded["operation"]; ok {
			t.Fatal("core internal operation exposed")
		}
		fixtures[row.name] = raw
	}
	// Explicit opt-in runs the actual Go-produced payloads in the page adapter.
	if node := os.Getenv("ORCA_RAW_BROWSER_TEST_NODE"); node != "" {
		raw, _ := json.Marshal(fixtures)
		_, file, _, _ := runtime.Caller(0)
		cmd := exec.Command(node, filepath.Join(filepath.Dir(file), "raw-response.browser.test.cjs"))
		cmd.Env = append(os.Environ(), "ORCA_SYNTHETIC_PAGE_COMMANDS="+string(raw))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Go wire -> browser failed: %v\n%s", err, out)
		}
		t.Logf("%s", out)
	}
}
