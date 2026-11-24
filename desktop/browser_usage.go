package main

import (
	"context"
	"errors"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// The browser has its own bounded loop rather than an agent.Agent. Report all
// of its requests, including capability probes and final verification.
type browserUsageProvider struct {
	provider.Provider
	sink                    event.Sink
	parent, child, endpoint string
	pricing                 *provider.Pricing
}

func (p *browserUsageProvider) BeginTask(ctx context.Context, conversationID, parentTaskID string) (context.Context, func(), error) {
	if p.parent != "" && p.parent != parentTaskID {
		return nil, nil, errors.New("browser usage parent identity does not match task scope")
	}
	if scoped, ok := p.Provider.(provider.TaskProvider); ok {
		return scoped.BeginTask(ctx, conversationID, parentTaskID)
	}
	return ctx, func() {}, ctx.Err()
}

func (p *browserUsageProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	req.ParentTaskID = p.parent
	if req.Purpose == "" {
		req.Purpose = provider.RequestPurposeBrowser
	}
	if req.RequestID == "" {
		req.RequestID = event.NewRequestID()
	}
	start := time.Now()
	price := p.pricing.SnapshotAt(start)
	requestCtx, cancel := context.WithCancel(ctx)
	stream, err := p.Provider.Stream(requestCtx, req)
	if err != nil {
		cancel()
		return nil, err
	}
	out := make(chan provider.Chunk, 1)
	go func() {
		defer close(out)
		defer cancel()
		var usage *provider.Usage
		defer func() {
			if usage != nil && p.sink != nil {
				p.sink.Emit(event.Event{Kind: event.Usage, RequestID: req.RequestID, ParentTurnID: p.parent,
					ChildID: p.child, ProviderEndpoint: p.endpoint, Usage: usage, Pricing: price})
			}
		}()
		for {
			select {
			case <-requestCtx.Done():
				return
			case chunk, ok := <-stream:
				if !ok {
					return
				}
				if chunk.Type == provider.ChunkUsage && chunk.Usage != nil {
					copy := *chunk.Usage
					usage = &copy
				}
				select {
				case out <- chunk:
				case <-requestCtx.Done():
					return
				}
				if chunk.Type == provider.ChunkDone || chunk.Type == provider.ChunkError {
					return
				}
			}
		}
	}()
	return out, nil
}

func withBrowserUsage(ctx context.Context, p provider.Provider, endpoint string, price *provider.Pricing) provider.Provider {
	parent, _ := agent.ParentTurn(ctx)
	call, sink, _, ok := agent.CallContext(ctx)
	if !ok || sink == nil {
		return p
	}
	return &browserUsageProvider{Provider: p, sink: sink, parent: parent, child: "browser:" + call, endpoint: endpoint, pricing: price}
}
