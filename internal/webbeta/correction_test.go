package webbeta

import (
	"context"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type correctionBrowser struct {
	fakeBrowser
	fixes int
	bad   bool
	urls  []string
}

func (b *correctionBrowser) Run(ctx context.Context, c Command, emit func(Event) error) error {
	if c.URL == "" {
		return b.fakeBrowser.Run(ctx, c, emit)
	}
	b.urls = append(b.urls, c.URL)
	text := `{"id":"one","name":"echo","arguments":{}}`
	if strings.Contains(c.Text, "未通过校验") {
		b.fixes++
		if !b.bad {
			text = toolMarker(0) + " " + text
		}
	}
	if err := emit(Event{ID: c.ID, Kind: "text", Text: text}); err != nil {
		return err
	}
	return emit(Event{ID: c.ID, Kind: "done", URL: c.URL})
}
func TestToolFormatCorrectionIsBoundedAndUsesSameChat(t *testing.T) {
	for _, bad := range []bool{false, true} {
		t.Run(map[bool]string{false: "corrected", true: "still_invalid"}[bad], func(t *testing.T) {
			b := &correctionBrowser{bad: bad}
			e := testEngine(t, b)
			old := CurrentEngine()
			SetEngine(e)
			defer SetEngine(old)
			r := provider.Request{ParentTaskID: "synthetic-parent", ConversationID: "a", RequestID: "r", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic task"}}, Tools: []provider.ToolSchema{{Name: "echo"}}}
			ch, err := (&Adapter{}).Stream(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			tools, errs, done := 0, 0, 0
			for c := range ch {
				switch c.Type {
				case provider.ChunkToolCall:
					tools++
				case provider.ChunkError:
					errs++
					if !provider.ReplayForbidden(c.Err) {
						t.Fatal("format failure can trigger an outer replay", c.Err)
					}
				case provider.ChunkDone:
					done++
				case provider.ChunkText:
					if c.Text != "" {
						t.Fatal("malformed tool leaked as final answer")
					}
				}
			}
			if b.fixes != 1 || len(b.urls) != 2 || b.urls[0] != b.urls[1] {
				t.Fatalf("unsafe correction: %+v", b.urls)
			}
			if bad {
				if tools != 0 || errs != 1 || done != 0 {
					t.Fatal("invalid corrected call accepted")
				}
			} else if tools != 1 || errs != 0 || done != 1 {
				t.Fatal("valid correction lost")
			}
			c := e.Status().Conversations[0]
			if c.Phase != "bound" {
				t.Fatal("confirmed format error must not be a transport uncertainty")
			}
		})
	}
}
