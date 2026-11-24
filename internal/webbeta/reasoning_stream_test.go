package webbeta

import (
	"context"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type liveReasoningBrowser struct {
	fakeBrowser
	release chan struct{}
}

func (b *liveReasoningBrowser) Run(ctx context.Context, c Command, emit func(Event) error) error {
	if c.Operation == "opening" {
		return b.fakeBrowser.Run(ctx, c, emit)
	}
	if err := emit(Event{ID: c.ID, Kind: "reasoning", Text: "synthetic visible reasoning"}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.release:
	}
	if err := emit(Event{ID: c.ID, Kind: "text", Text: `ORCA_TURN_V3 {"type":"final","text":"done"}`}); err != nil {
		return err
	}
	return emit(Event{ID: c.ID, Kind: "done", URL: c.URL})
}

func TestV3ReasoningArrivesBeforeFinalResponse(t *testing.T) {
	b := &liveReasoningBrowser{release: make(chan struct{})}
	e := testEngine(t, b)
	e.newGrammar = latestGrammar
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch, err := (&Adapter{}).Stream(ctx, provider.Request{ParentTaskID: "parent", ConversationID: "owner", RequestID: "r", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case chunk := <-ch:
		if chunk.Type != provider.ChunkReasoning || chunk.Text != "synthetic visible reasoning" {
			t.Fatalf("not streamed reasoning: %+v", chunk)
		}
	case <-ctx.Done():
		t.Fatal("reasoning waited for final answer")
	}
	close(b.release)
	final := false
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if chunk.Type == provider.ChunkToolCall {
			t.Fatal("reasoning became tool")
		}
		final = final || chunk.Type == provider.ChunkDone
	}
	if !final {
		t.Fatal("missing completion")
	}
}
