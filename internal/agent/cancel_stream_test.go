package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type cancelBlockedProvider struct {
	ch      chan provider.Chunk
	started chan struct{}
}

func (p cancelBlockedProvider) Name() string { return "blocked" }
func (p cancelBlockedProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	if p.started != nil {
		close(p.started)
	}
	return p.ch, nil
}

func TestCancelSilentStreamPreservesPartialWithoutRetry(t *testing.T) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "partial answer"}
	received := make(chan struct{})
	sess := NewSession("test")
	a := New(cancelBlockedProvider{ch: ch}, tool.NewRegistry(), sess, Options{}, event.FuncSink(func(e event.Event) {
		if e.Kind == event.Text {
			close(received)
		}
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, "synthetic cancellation test") }()
	<-received
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		close(ch)
		<-done
		t.Fatal("agent waited for provider channel close instead of cancellation")
	}
	if got := sess.Messages[len(sess.Messages)-1]; got.Role != provider.RoleAssistant || got.Content != "partial answer" {
		t.Fatalf("partial reply lost: %+v", got)
	}
}

func TestCancelSilentStreamBeforeFirstChunk(t *testing.T) {
	ch := make(chan provider.Chunk)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess := NewSession("test")
	started := make(chan struct{})
	a := New(cancelBlockedProvider{ch: ch, started: started}, tool.NewRegistry(), sess, Options{}, event.Discard)
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, "synthetic before-token cancellation") }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		close(ch)
		<-done
		t.Fatal("silent provider blocked cancellation")
	}
}
