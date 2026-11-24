package webbeta

import (
	"context"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type framedBrowser struct {
	fakeBrowser
	output string
}

func (b *framedBrowser) Run(ctx context.Context, c Command, emit func(Event) error) error {
	if c.URL == "" {
		return b.fakeBrowser.Run(ctx, c, emit)
	}
	for _, part := range strings.SplitAfter(b.output, "\n") {
		if err := emit(Event{ID: c.ID, Kind: "text", Text: part}); err != nil {
			return err
		}
	}
	return emit(Event{ID: c.ID, Kind: "done", URL: c.URL})
}

func TestFramedProgressThenMultipleTools(t *testing.T) {
	b := &framedBrowser{output: "ORCA_PROGRESS_V2 {\"text\":\"先读取两个文件。\"}\nORCA_TURN_V2 {\"type\":\"tools\",\"calls\":[{\"id\":\"a\",\"name\":\"read_file\",\"arguments\":{\"path\":\"a.txt\"}},{\"id\":\"b\",\"name\":\"read_file\",\"arguments\":{\"path\":\"b.txt\"}}]}"}
	e := testEngine(t, b)
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	ch, err := (&Adapter{}).Stream(context.Background(), provider.Request{ParentTaskID: "synthetic-parent", ConversationID: "framed", RequestID: "r", Messages: []provider.Message{{Role: provider.RoleUser, Content: "read both"}}, Tools: []provider.ToolSchema{{Name: "read_file"}}})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var calls []string
	for c := range ch {
		if c.Err != nil {
			t.Fatal(c.Err)
		}
		if c.Type == provider.ChunkText {
			text.WriteString(c.Text)
		}
		if c.ToolCall != nil {
			calls = append(calls, c.ToolCall.ID)
		}
	}
	if strings.Join(calls, ",") != "a,b" || text.String() != "先读取两个文件。" {
		t.Fatalf("lost framed tools or leaked protocol: calls=%v text=%q", calls, text.String())
	}
}

func TestRenderToolResultKeepsIdentity(t *testing.T) {
	text := render([]provider.Message{{Role: provider.RoleTool, ToolCallID: "call-a", Name: "read_file", Content: "file missing"}}, nil, 2, false)
	for _, want := range []string{`"tool_call_id":"call-a"`, `"name":"read_file"`, `"status":"returned"`, `file missing`} {
		if !strings.Contains(text, want) {
			t.Fatalf("tool result lost %s: %s", want, text)
		}
	}
}

func TestExplicitFinalDoesNotLeakProtocol(t *testing.T) {
	b := &framedBrowser{output: `ORCA_TURN_V2 {"type":"final","text":"核对完毕。"}`}
	e := testEngine(t, b)
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	ch, err := (&Adapter{}).Stream(context.Background(), provider.Request{ParentTaskID: "synthetic-parent", ConversationID: "final", RequestID: "r", Messages: []provider.Message{{Role: provider.RoleUser, Content: "answer"}}, Tools: []provider.ToolSchema{{Name: "read_file"}}})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for c := range ch {
		if c.Err != nil {
			t.Fatal(c.Err)
		}
		if c.Type == provider.ChunkText {
			text.WriteString(c.Text)
		}
	}
	if text.String() != "核对完毕。" {
		t.Fatalf("final envelope leaked: %s", text.String())
	}
}
