package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func TestWebBetaRequestCarriesStableIdentityAndImmutableFiles(t *testing.T) {
	p := &mockProvider{name: "synthetic", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "synthetic answer"}, {Type: provider.ChunkDone}}}
	s := NewSession("synthetic system")
	a := New(p, tool.NewRegistry(), s, Options{}, event.Discard)
	files := []provider.FileContent{{Name: "synthetic.txt", MediaType: "text/plain", Digest: "synthetic-digest", Data: []byte("private-snapshot-fixture")}}
	if err := a.RunRich(context.Background(), RichInput{Text: "inspect attachment", Files: files}); err != nil {
		t.Fatal(err)
	}
	r := p.lastReq
	if r.ConversationID == "" || r.ConversationID != s.ConversationID() || r.ContextEpoch != s.ContextEpoch() || r.RequestID == "" {
		t.Fatal("missing request routing identity")
	}
	last := r.Messages[len(r.Messages)-1]
	if len(last.Files) != 1 || string(last.Files[0].Data) != "private-snapshot-fixture" {
		t.Fatal("file dropped or downgraded")
	}
	files[0].Data[0] = 'X'
	last.Files[0].Data[0] = 'Y'
	if string(s.Snapshot()[1].Files[0].Data) != "private-snapshot-fixture" {
		t.Fatal("snapshot aliases input/provider memory")
	}
	snapshot := s.Snapshot()
	snapshot[1].Files[0].Data[0] = 'Z'
	display := s.DisplaySnapshot()
	display[1].Message.Files[0].Data[0] = 'Z'
	if string(s.Snapshot()[1].Files[0].Data) != "private-snapshot-fixture" {
		t.Fatal("history readers mutated an authorized snapshot")
	}
	path := filepath.Join(t.TempDir(), "synthetic.jsonl")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-snapshot-fixture") {
		t.Fatal("file bytes persisted")
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ConversationID() != r.ConversationID || len(loaded.Snapshot()[1].Files[0].Data) != 0 {
		t.Fatal("identity lost or file bytes restored")
	}
	rebuilt := loaded.WithContext(loaded.Snapshot())
	if rebuilt.ConversationID() != r.ConversationID {
		t.Fatal("controller rebuild changed owner")
	}
	fork := rebuilt.ClonePrefix(len(rebuilt.Snapshot()))
	if fork.ConversationID() == rebuilt.ConversationID() {
		t.Fatal("fork reused owner")
	}
	epoch := rebuilt.ContextEpoch()
	rebuilt.RewriteContext([]provider.Message{{Role: provider.RoleUser, Content: "synthetic summary"}}, &DisplayEntry{Kind: "compaction"})
	if rebuilt.ConversationID() != r.ConversationID || rebuilt.ContextEpoch() <= epoch {
		t.Fatal("compaction lost binding epoch")
	}
}
