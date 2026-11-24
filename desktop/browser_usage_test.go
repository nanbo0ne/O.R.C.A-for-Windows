package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type usageFixtureProvider struct {
	request provider.Request
	chunks  []provider.Chunk
}

func (p *usageFixtureProvider) Name() string { return "synthetic" }
func (p *usageFixtureProvider) Stream(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	p.request = r
	c := make(chan provider.Chunk, len(p.chunks))
	for _, v := range p.chunks {
		c <- v
	}
	close(c)
	return c, nil
}

func TestBrowserUsageReportsOneReceiptIncludingPartialFailure(t *testing.T) {
	for _, terminal := range []provider.ChunkType{provider.ChunkDone, provider.ChunkError} {
		t.Run(string(rune(terminal+'A')), func(t *testing.T) {
			inner := &usageFixtureProvider{chunks: []provider.Chunk{{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 3}}, {Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 5}}, {Type: terminal}}}
			var mu sync.Mutex
			var events []event.Event
			p := &browserUsageProvider{Provider: inner, parent: "turn-a", child: "browser:call-a", endpoint: "https://example.invalid", sink: event.FuncSink(func(e event.Event) { mu.Lock(); defer mu.Unlock(); events = append(events, e) })}
			stream, err := p.Stream(context.Background(), provider.Request{})
			if err != nil {
				t.Fatal(err)
			}
			for range stream {
			}
			if inner.request.Purpose != provider.RequestPurposeBrowser {
				t.Fatalf("browser request has no purpose attribution: %q", inner.request.Purpose)
			}
			if inner.request.ParentTaskID != "turn-a" {
				t.Error("browser request lost its parent task")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(events) != 1 || events[0].Usage.CompletionTokens != 5 || events[0].RequestID == "" || events[0].RequestID != inner.request.RequestID || events[0].ParentTurnID != "turn-a" || events[0].Pricing != nil {
				t.Fatalf("incorrect receipt: %+v", events)
			}
		})
	}
}

func TestBrowserUsageMissingUsageIsNotZeroAndCancellationReleases(t *testing.T) {
	inner := &usageFixtureProvider{chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "synthetic"}, {Type: provider.ChunkText, Text: "synthetic"}, {Type: provider.ChunkDone}}}
	var count int
	p := &browserUsageProvider{Provider: inner, sink: event.FuncSink(func(event.Event) { count++ })}
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := p.Stream(ctx, provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	done := make(chan struct{})
	go func() {
		for range stream {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked consumer retained request")
	}
	if count != 0 {
		t.Fatal("missing usage became a zero receipt")
	}
}
