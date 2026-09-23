package control

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type compactBlockedProvider struct{ started chan struct{} }

func (*compactBlockedProvider) Name() string { return "synthetic" }
func (p *compactBlockedProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	close(p.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestManualCompactCanCancelAndDoesNotRaceNewTurn(t *testing.T) {
	s := agent.NewSession("system")
	for i := 0; i < 4; i++ {
		s.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	}
	before := s.DisplaySnapshot()
	p := &compactBlockedProvider{started: make(chan struct{})}
	ex := agent.New(p, tool.NewRegistry(), s, agent.Options{RecentKeep: 2}, event.Discard)
	c := New(Options{Executor: ex, SessionPath: filepath.Join(t.TempDir(), "s.jsonl")})
	defer c.Close()
	done := make(chan error, 1)
	go func() { done <- c.Compact(context.Background(), "") }()
	select {
	case <-p.started:
	case <-time.After(3 * time.Second):
		t.Fatal("compaction never started")
	}
	if !c.Running() {
		t.Fatal("compaction not guarded")
	}
	if err := c.Compact(context.Background(), ""); !errors.Is(err, ErrTurnRunning) {
		t.Fatalf("concurrent compaction accepted: %v", err)
	}
	c.Cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled compaction: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("compaction cancellation stuck")
	}
	if c.Running() || !reflect.DeepEqual(before, s.DisplaySnapshot()) {
		t.Fatal("cancel did not preserve idle history")
	}
}

func TestNoopCompactPreservesLiveCheckpointBoundaries(t *testing.T) {
	s := agent.NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "short"})
	c := New(Options{Executor: agent.New(nil, tool.NewRegistry(), s, agent.Options{}, event.Discard)})
	defer c.Close()
	c.cpBound = map[int]int{0: 1}
	if err := c.Compact(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if !c.CheckpointHasBoundary(0) {
		t.Fatal("no-op compact erased live checkpoint")
	}
}

func TestCompactionEpochRejectsStaleCheckpointAfterReload(t *testing.T) {
	s := agent.NewSession("system")
	path := filepath.Join(t.TempDir(), "s.jsonl")
	ex := agent.New(nil, tool.NewRegistry(), s, agent.Options{}, event.Discard)
	c := New(Options{Executor: ex, SessionPath: path})
	defer c.Close()
	c.beginCheckpoint("first")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "original"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	if !c.CheckpointHasBoundary(0) {
		t.Fatal("initial boundary missing")
	}
	s.RewriteContext([]provider.Message{{Role: provider.RoleSystem, Content: "system"}, {Role: provider.RoleUser, Content: "summary"}}, &agent.DisplayEntry{Summary: "summary"})
	if c.CheckpointHasBoundary(0) {
		t.Fatal("old in-range index incorrectly remains usable after compression")
	}
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	c.Resume(loaded, path)
	if c.CheckpointHasBoundary(0) {
		t.Fatal("reload resurrected stale compacted index")
	}
	c.beginCheckpoint("next")
	if !c.CheckpointHasBoundary(1) {
		t.Fatal("new live context boundary unavailable")
	}
}
