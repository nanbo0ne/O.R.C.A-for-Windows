package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func TestCompactionRetainsDisplayAcrossSaveAndRepeatedCompaction(t *testing.T) {
	s := compactionFailureSession()
	before := s.DisplaySnapshot()
	a := New(&fakeProvider{reply: "Keep the original goal."}, tool.NewRegistry(), s, Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)
	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot()) >= len(before) {
		t.Fatal("model context was not reduced")
	}
	got := s.DisplaySnapshot()
	if len(got) != len(before)+1 || !reflect.DeepEqual(got[:len(before)], before) || got[len(before)].Kind != "compaction" {
		t.Fatalf("visible history changed: %#v", got)
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	for run := 0; run < 2; run++ {
		if err := s.Save(path); err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadSession(path)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.DisplaySnapshot(), loaded.DisplaySnapshot()) {
			t.Fatal("display history lost on reload")
		}
		s = loaded
		s.Add(provider.Message{Role: provider.RoleUser, Content: "repeat"})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
		a.SetSession(s)
		if err := a.compact(context.Background(), "manual", "", true); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDisplayPreservedByPruneAndControllerContextReplacement(t *testing.T) {
	s := compactionFailureSession()
	before := s.DisplaySnapshot()
	msgs := s.Snapshot()
	msgs[0].Content = "new system"
	msgs[2].Content = "elided"
	s.RewriteContext(msgs, nil)
	copy := s.WithContext(msgs)
	if !reflect.DeepEqual(copy.DisplaySnapshot(), before) {
		t.Fatal("context maintenance mutated display")
	}
	copy.Add(provider.Message{Role: provider.RoleUser, Content: "branch"})
	if len(s.DisplaySnapshot()) != len(before) {
		t.Fatal("clone shares mutable display slice")
	}
}

func TestTimelineAtomicFileRemainsLegacyReadable(t *testing.T) {
	s := compactionFailureSession()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for _, want := range s.Snapshot() {
		var got provider.Message
		if err := dec.Decode(&got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal("legacy context reader changed")
		}
	}
}

func TestDisplayCloneAtLiveBoundaryAfterCompaction(t *testing.T) {
	s := compactionFailureSession()
	a := New(&fakeProvider{reply: "summary"}, tool.NewRegistry(), s, Options{RecentKeep: 2}, event.Discard)
	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatal(err)
	}
	boundary := len(s.Snapshot())
	before := s.DisplaySnapshot()
	s.Add(provider.Message{Role: provider.RoleUser, Content: "later"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "later answer"})
	branch := s.ClonePrefix(boundary)
	if !reflect.DeepEqual(before, branch.DisplaySnapshot()) {
		t.Fatal("fork lost archived display or retained later turn")
	}
}

func TestTimelineConcurrentSaveKeepsMatchingContextAndDisplay(t *testing.T) {
	s := NewSession("system")
	path := filepath.Join(t.TempDir(), "live.jsonl")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				s.Add(provider.Message{Role: provider.RoleUser, Content: "synthetic"})
				if err := s.Save(path); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Snapshot()) != 41 || len(loaded.DisplaySnapshot()) != 41 {
		t.Fatal("concurrent save lost messages")
	}
}

func TestTimelineSummaryOperationsAndFailurePreserveOriginals(t *testing.T) {
	for _, from := range []bool{true, false} {
		s := compactionFailureSession()
		before := s.DisplaySnapshot()
		a := New(&fakeProvider{reply: "summary"}, tool.NewRegistry(), s, Options{}, event.Discard)
		var err error
		if from {
			err = a.SummarizeFrom(context.Background(), 3)
		} else {
			err = a.SummarizeUpTo(context.Background(), 3)
		}
		if err != nil {
			t.Fatal(err)
		}
		after := s.DisplaySnapshot()
		if len(after) != len(before)+1 || !reflect.DeepEqual(before, after[:len(before)]) {
			t.Fatal("summary changed visible original messages")
		}
	}
	s := compactionFailureSession()
	before := s.DisplaySnapshot()
	a := New(&fakeProvider{reply: "summary"}, tool.NewRegistry(), s, Options{RecentKeep: 2}, event.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.compact(ctx, "manual", "", true); err == nil {
		t.Fatal("cancelled compression succeeded")
	}
	if !reflect.DeepEqual(before, s.DisplaySnapshot()) {
		t.Fatal("cancelled compression changed history")
	}
}

func TestTimelinePreviewKeepsOriginalTitleAndCount(t *testing.T) {
	s := compactionFailureSession()
	a := New(&fakeProvider{reply: "summary"}, tool.NewRegistry(), s, Options{RecentKeep: 2}, event.Discard)
	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	preview, count := previewSession(path)
	if !strings.HasPrefix(preview, "important context") || count != 3 {
		t.Fatalf("preview %q/%d lost display history", preview, count)
	}
}

func TestTimelineForkImageRelocationDoesNotChangeOriginal(t *testing.T) {
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "image", Images: []provider.ImageContent{{Path: "before.png"}}})
	clone := s.ClonePrefix(1)
	clone.RelocateImages(func(string) string { return "after.png" })
	if s.Snapshot()[0].Images[0].Path != "before.png" || s.DisplaySnapshot()[0].Message.Images[0].Path != "before.png" {
		t.Fatal("branch image relocation mutated original")
	}
	if clone.Snapshot()[0].Images[0].Path != "after.png" || clone.DisplaySnapshot()[0].Message.Images[0].Path != "after.png" {
		t.Fatal("relocation only changed one history")
	}
}

func TestTimelineFailedReplaceKeepsCompleteRecoveryCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "occupied.jsonl")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	s := compactionFailureSession()
	err := s.Save(path)
	if err == nil || !strings.Contains(err.Error(), "recovery copy") {
		t.Fatalf("missing actionable save error: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, ".session.*.tmp"))
	if err != nil || len(files) != 1 {
		t.Fatalf("recovery copy missing: %v %v", files, err)
	}
	copy, err := LoadSession(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(copy.DisplaySnapshot(), s.DisplaySnapshot()) {
		t.Fatal("recovery copy incomplete")
	}
}

func TestCompressionArchivesNeverOverwriteEachOther(t *testing.T) {
	dir := t.TempDir()
	paths := map[string]bool{}
	for i := 0; i < 20; i++ {
		p, err := archiveMessages(dir, []provider.Message{{Role: provider.RoleUser, Content: "original"}})
		if err != nil {
			t.Fatal(err)
		}
		if paths[p] {
			t.Fatal("archive name reused")
		}
		paths[p] = true
	}
}

func TestMentioningCheckpointTagIsNotACompactionMarker(t *testing.T) {
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "Explain the <context-checkpoint> tag."})
	if s.DisplaySnapshot()[0].Kind != "message" {
		t.Fatal("ordinary text hidden as a checkpoint")
	}
}
