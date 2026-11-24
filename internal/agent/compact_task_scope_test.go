package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type compactScopeProvider struct {
	cancel                    context.CancelFunc
	active                    bool
	begins, releases, streams int
	conversation, parent      string
	onRelease                 func()
	chunks                    []provider.Chunk
	hang                      bool
}

func (p *compactScopeProvider) Name() string { return "synthetic-summary" }
func (p *compactScopeProvider) BeginTask(ctx context.Context, conversation, parent string) (context.Context, func(), error) {
	p.begins++
	p.active = true
	p.conversation, p.parent = conversation, parent
	ctx, p.cancel = context.WithCancel(ctx)
	return ctx, func() {
		p.releases++
		if p.onRelease != nil {
			p.onRelease()
		}
		p.active = false
		p.cancel()
	}, nil
}
func (p *compactScopeProvider) Stream(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	p.streams++
	if !p.active || p.parent == "" || p.conversation == "" || r.ParentTaskID != p.parent {
		return nil, errors.New("summary escaped its task identity")
	}
	ch := make(chan provider.Chunk, len(p.chunks))
	for _, c := range p.chunks {
		ch <- c
	}
	if !p.hang {
		close(ch)
	}
	return ch, nil
}

func TestManualSummaryTaskScopeCoversCommit(t *testing.T) {
	for _, mode := range []string{"compact", "from", "up-to"} {
		for _, revoke := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/success", true: "/revoked-after-done"}[revoke], func(t *testing.T) {
				sess := compactionFailureSession()
				before := sess.Snapshot()
				p := &compactScopeProvider{chunks: []provider.Chunk{
					{Type: provider.ChunkText, Text: "complete synthetic summary"},
					{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 4}},
					{Type: provider.ChunkDone},
				}}
				p.onRelease = func() {
					if !revoke && reflect.DeepEqual(before, sess.Snapshot()) {
						t.Error("scope released before checkpoint commit")
					}
				}
				sink := event.FuncSink(func(e event.Event) {
					// Usage is emitted when collection has finished, before history commit.
					if revoke && e.Kind == event.Usage && p.cancel != nil {
						p.cancel()
					}
				})
				a := New(p, tool.NewRegistry(), sess, Options{RecentKeep: 2}, sink)
				var err error
				switch mode {
				case "compact":
					err = a.CompactNow(context.Background(), "")
				case "from":
					err = a.SummarizeFrom(context.Background(), 1)
				case "up-to":
					err = a.SummarizeUpTo(context.Background(), 3)
				}
				if revoke {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("error=%v, want cancellation", err)
					}
					if !reflect.DeepEqual(before, sess.Snapshot()) {
						t.Fatal("revoked summary changed history")
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if p.begins != 1 || p.releases != 1 || p.streams != 1 || p.active {
					t.Fatalf("scope counts: begin=%d release=%d stream=%d active=%v", p.begins, p.releases, p.streams, p.active)
				}
			})
		}
	}
}

func TestSummaryStreamRequiresCompletionAndHonorsCancellation(t *testing.T) {
	for _, mode := range []string{"partial-eof", "tools", "nil-error", "stalled"} {
		t.Run(mode, func(t *testing.T) {
			sess := compactionFailureSession()
			before := sess.Snapshot()
			p := &compactScopeProvider{chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "not a completed summary"}}}
			switch mode {
			case "tools":
				p.chunks = append(p.chunks, provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{Name: "write_file"}}, provider.Chunk{Type: provider.ChunkDone})
			case "nil-error":
				p.chunks = append(p.chunks, provider.Chunk{Type: provider.ChunkError})
			case "stalled":
				p.hang = true
			}
			a := New(p, tool.NewRegistry(), sess, Options{RecentKeep: 2}, event.Discard)
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			err := a.CompactNow(ctx, "")
			if err == nil {
				t.Fatal("invalid summary reported success")
			}
			if !reflect.DeepEqual(before, sess.Snapshot()) {
				t.Fatal("invalid summary changed history")
			}
			if p.streams != 1 {
				t.Fatalf("ambiguous completion was replayed %d times", p.streams)
			}
		})
	}
}

func TestSummaryCommitRechecksCancellationAndHistoryUnderLock(t *testing.T) {
	for _, mode := range []string{"cancelled", "changed"} {
		t.Run(mode, func(t *testing.T) {
			s := compactionFailureSession()
			before, epoch, version := s.Snapshot(), s.ContextEpoch(), s.RewriteVersion()
			display := s.DisplaySnapshot()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s.mu.Lock()
			started, result := make(chan struct{}), make(chan error, 1)
			go func() {
				close(started)
				result <- s.commitCompaction(ctx, before, []provider.Message{{Role: provider.RoleUser, Content: "synthetic summary"}}, &DisplayEntry{Summary: "synthetic summary"})
			}()
			<-started
			if mode == "cancelled" {
				cancel()
			} else {
				s.Messages[1].Content = "newer synthetic context"
			}
			want := cloneMessageFileSnapshots(s.Messages)
			s.mu.Unlock()
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("stale summary was committed")
				}
				if mode == "cancelled" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("commit did not exit")
			}
			if !reflect.DeepEqual(want, s.Snapshot()) || s.ContextEpoch() != epoch || s.RewriteVersion() != version || !reflect.DeepEqual(display, s.DisplaySnapshot()) {
				t.Fatal("rejected summary changed messages, epoch, version or visible history")
			}
		})
	}
}
