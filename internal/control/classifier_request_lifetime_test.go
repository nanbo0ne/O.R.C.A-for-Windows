package control

import (
	"context"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type planRequestLifetimeProvider struct {
	ctx context.Context
	req provider.Request
}

func (*planRequestLifetimeProvider) Name() string { return "synthetic-plan-lifetime" }
func (p *planRequestLifetimeProvider) Stream(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	p.ctx, p.req = ctx, r
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: `{"needs_plan":true,"reason":"synthetic"}`}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	// A terminal marker is sufficient; the consumer must cancel its own request
	// even if a provider keeps the channel open waiting for that cancellation.
	return ch, nil
}

func TestAutoPlanClassifierClosesOnlyItsNonThinkingRequest(t *testing.T) {
	p := &planRequestLifetimeProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ok, _, err := NewProviderAutoPlanClassifier(p).NeedsPlanWithParentTurn(ctx, "synthetic", 1, "parent")
	if err != nil || !ok {
		t.Fatalf("classification: %v", err)
	}
	if !p.req.DisableThinking {
		t.Error("internal classification did not disable thinking")
	}
	select {
	case <-p.ctx.Done():
	default:
		t.Error("completed classifier request context remains live")
	}
	if ctx.Err() != nil {
		t.Fatal("classifier cancelled its parent task")
	}
	if p.req.ParentTaskID != "parent" || len(p.req.Tools) != 0 {
		t.Fatal("classification lost its isolated request shape")
	}
}
