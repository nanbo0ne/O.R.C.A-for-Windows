package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestAuxiliaryCollectorStopsAtDoneWithoutEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream := make(chan provider.Chunk, 2)
	stream <- provider.Chunk{Type: provider.ChunkText, Text: "synthetic answer"}
	stream <- provider.Chunk{Type: provider.ChunkDone}
	defer close(stream)
	text, err := collectAuxiliaryText(ctx, stream)
	if err != nil || text != "synthetic answer" {
		t.Fatalf("Done waited for EOF: text=%q err=%v", text, err)
	}
}

func TestAuxiliaryCollectorRejectsUnconfirmedAndToolResults(t *testing.T) {
	original := provider.NoRequestReplay(errors.New("synthetic unknown outcome"))
	for _, tc := range []struct {
		name   string
		chunks []provider.Chunk
	}{
		{"eof", nil},
		{"partial-eof", []provider.Chunk{{Type: provider.ChunkText, Text: "partial"}}},
		{"tool-start", []provider.Chunk{{Type: provider.ChunkToolCallStart}, {Type: provider.ChunkDone}}},
		{"tool-call", []provider.Chunk{{Type: provider.ChunkToolCall}, {Type: provider.ChunkDone}}},
		{"empty-error", []provider.Chunk{{Type: provider.ChunkError}}},
		{"no-replay-error", []provider.Chunk{{Type: provider.ChunkError, Err: original}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := make(chan provider.Chunk, len(tc.chunks))
			for _, chunk := range tc.chunks {
				stream <- chunk
			}
			close(stream)
			text, err := collectAuxiliaryText(context.Background(), stream)
			if err == nil || text != "" || !provider.ReplayForbidden(err) {
				t.Fatalf("unconfirmed/tool result accepted or replayable: text=%q err=%v", text, err)
			}
			if tc.name == "no-replay-error" && !errors.Is(err, original) {
				t.Fatal("provider no-replay error lost")
			}
		})
	}
}

func TestAuxiliaryCollectorCanceledDoneCannotSucceed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 32; i++ {
		stream := make(chan provider.Chunk, 1)
		stream <- provider.Chunk{Type: provider.ChunkDone}
		close(stream)
		text, err := collectAuxiliaryText(ctx, stream)
		if text != "" || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled Done accepted: text=%q err=%v", text, err)
		}
	}
}

type auxiliaryTaskFixture struct {
	*auxiliaryFixtureProvider
	conversation, parent string
	taskCtx              context.Context
	cancel               context.CancelFunc
	beginErr             error
	beginCount, endCount int
	atEnd                func()
}

func (p *auxiliaryTaskFixture) BeginTask(ctx context.Context, conversation, parent string) (context.Context, func(), error) {
	p.beginCount++
	p.conversation, p.parent = conversation, parent
	if p.beginErr != nil {
		return nil, nil, p.beginErr
	}
	p.taskCtx, p.cancel = context.WithCancel(ctx)
	return p.taskCtx, func() {
		p.endCount++
		if p.atEnd != nil {
			p.atEnd()
		}
		p.cancel()
	}, nil
}

func TestSideChatTaskScopeIncludesCommitAndUncertainResults(t *testing.T) {
	for _, mode := range []string{"success", "empty", "incomplete", "tool", "stream-error", "begin-error"} {
		t.Run(mode, func(t *testing.T) {
			app, cfg, inner := auxiliaryFixture(t)
			p := &auxiliaryTaskFixture{auxiliaryFixtureProvider: inner}
			cfg.Providers[0].Kind += "-task"
			provider.Register(cfg.Providers[0].Kind, func(provider.Config) (provider.Provider, error) { return p, nil })
			if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
				t.Fatal(err)
			}
			if mode == "begin-error" {
				p.beginErr = errors.New("synthetic identity unavailable")
			}
			inner.hook = func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
				if p.beginCount != 1 || p.taskCtx != ctx || p.conversation != req.ConversationID || p.parent != req.ParentTaskID || p.parent == "" {
					return nil, errors.New("sidebar did not bind its host task scope before Stream")
				}
				stream := make(chan provider.Chunk, 2)
				switch mode {
				case "success":
					stream <- provider.Chunk{Type: provider.ChunkText, Text: "synthetic answer"}
				case "tool":
					stream <- provider.Chunk{Type: provider.ChunkToolCall}
				case "stream-error":
					return nil, provider.NoRequestReplay(errors.New("synthetic uncertain dispatch"))
				}
				if mode != "incomplete" {
					stream <- provider.Chunk{Type: provider.ChunkDone}
				}
				close(stream)
				return stream, nil
			}
			p.atEnd = func() {
				f, err := readSideChatFile()
				if err != nil {
					t.Error(err)
					return
				}
				if mode == "success" {
					if f.Bindings["side"].Pending || len(f.Sessions["side"]) != 2 {
						t.Error("task scope released before completed sidebar save")
					}
				} else if !f.Bindings["side"].Pending || len(f.Sessions["side"]) != 0 {
					t.Error("unknown/empty result committed as completed history")
				}
			}
			err := app.SendSideChat("side", "synthetic")
			if (mode == "success") != (err == nil) {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			wantEnd := 1
			if mode == "begin-error" {
				wantEnd = 0
				if len(inner.requests) != 0 {
					t.Fatal("failed task scope still dispatched")
				}
			}
			if p.beginCount != 1 || p.endCount != wantEnd {
				t.Fatalf("scope begin=%d end=%d", p.beginCount, p.endCount)
			}
		})
	}
}

func TestSideChatTaskCancellationAfterDoneKeepsPending(t *testing.T) {
	for _, mode := range []string{"identity-cancel", "source-change"} {
		t.Run(mode, func(t *testing.T) {
			app, cfg, inner := auxiliaryFixture(t)
			p := &auxiliaryTaskFixture{auxiliaryFixtureProvider: inner}
			cfg.Providers[0].Kind += "-task"
			provider.Register(cfg.Providers[0].Kind, func(provider.Config) (provider.Provider, error) { return p, nil })
			if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
				t.Fatal(err)
			}
			entered, resume, completed := make(chan struct{}), make(chan struct{}), make(chan struct{})
			inner.hook = func(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
				if p.taskCtx != ctx || p.cancel == nil {
					return nil, errors.New("sidebar omitted task scope")
				}
				close(entered)
				<-resume
				stream := make(chan provider.Chunk)
				go func() {
					stream <- provider.Chunk{Type: provider.ChunkText, Text: "synthetic answer"}
					stream <- provider.Chunk{Type: provider.ChunkDone}
					close(stream)
					close(completed)
				}()
				return stream, nil
			}
			result := make(chan error, 1)
			go func() { result <- app.SendSideChat("side", "synthetic") }()
			select {
			case <-entered:
			case err := <-result:
				t.Fatalf("failed before scoped stream: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("stream did not start")
			}
			// Hold the post-stream source check so identity changes precede commit.
			app.mu.Lock()
			close(resume)
			<-completed
			if mode == "identity-cancel" {
				p.cancel()
			} else {
				app.tabs["side"].model = "changed-source/model"
			}
			app.mu.Unlock()
			err := <-result
			if err == nil || mode == "identity-cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("late invalidation accepted: %v", err)
			}
			f := loadSideChatFile()
			if !f.Bindings["side"].Pending || len(f.Sessions["side"]) != 0 || p.endCount != 1 || len(inner.requests) != 1 {
				t.Fatal("late invalidation lost pending state, committed history, leaked scope or replayed")
			}
		})
	}
}
