package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/openai"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type terminalStreamProvider struct {
	terminal       provider.Chunk
	requestContext context.Context
	finished       chan struct{}
}

type completionContextRecorder struct {
	provider.Provider
	contexts []context.Context
}

func (p *completionContextRecorder) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.contexts = append(p.contexts, ctx)
	return p.Provider.Stream(ctx, req)
}

func TestOpenAIStreamThreeTurnsWithSyntheticToolsAndSlowSink(t *testing.T) {
	const toolBytes = 68 * 1024
	var requests, toolResults atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		var req struct {
			Messages []provider.Message `json:"messages"`
			Model    string             `json:"model"`
			Tools    []json.RawMessage  `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.Model != "synthetic-local-model" || len(req.Tools) != 1 {
			t.Error("model/tool schema changed between requests")
		}
		if n%2 == 0 {
			last := req.Messages[len(req.Messages)-1]
			if last.Role != provider.RoleTool || len(last.Content) < maxToolOutputBytes {
				t.Error("synthetic tool result missing/truncated")
			}
			toolResults.Add(1)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeDelta := func(delta any, finish any) {
			encoded, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta, "finish_reason": finish}}})
			if err != nil {
				t.Error(err)
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", encoded)
			w.(http.Flusher).Flush()
		}
		writeDelta(map[string]any{"reasoning_content": "synthetic reasoning"}, nil)
		if n%2 == 1 {
			args, _ := json.Marshal(map[string]string{"text": strings.Repeat("s", toolBytes)})
			writeDelta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprint(n), "function": map[string]any{"name": "echo", "arguments": string(args[:128])}}}}, nil)
			writeDelta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": string(args[128:])}}}}, "tool_calls")
		} else {
			writeDelta(map[string]any{"content": "synthetic "}, nil)
			writeDelta(map[string]any{"content": "answer"}, "stop")
		}
		io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":5,\"total_tokens\":25}}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	p, err := openai.New(provider.Config{Name: "synthetic", BaseURL: srv.URL, Model: "synthetic-local-model", APIKey: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	recorded := &completionContextRecorder{Provider: p}
	var text strings.Builder
	sink := event.Lifecycle(event.FuncSink(func(e event.Event) {
		if e.Kind == event.Text {
			text.WriteString(e.Text)
			time.Sleep(15 * time.Millisecond)
		}
	}))
	a := New(recorded, echoRegistry(), NewSession("synthetic"), Options{}, sink)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < 3; i++ {
		if err := a.Run(ctx, "synthetic turn "+strings.Repeat("s", toolBytes)); err != nil {
			t.Fatal(err)
		}
		for _, completed := range recorded.contexts {
			if completed.Err() == nil {
				t.Fatal("completed request still live between turns")
			}
		}
	}
	if requests.Load() != 6 || toolResults.Load() != 3 || text.String() != strings.Repeat("synthetic answer", 3) {
		t.Fatalf("requests=%d tool results=%d text bytes=%d", requests.Load(), toolResults.Load(), text.Len())
	}
	if ctx.Err() != nil {
		t.Fatal("completion cancellation escaped to parent")
	}
}

func (p *terminalStreamProvider) Name() string { return "synthetic" }
func (p *terminalStreamProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.requestContext = ctx
	out := make(chan provider.Chunk, 2)
	out <- provider.Chunk{Type: provider.ChunkText, Text: "synthetic"}
	out <- p.terminal
	go func() {
		defer close(p.finished)
		defer close(out)
		<-ctx.Done()
	}()
	return out, nil
}

func TestStreamTerminalReleasesRequestBeforeNextToolOrTurn(t *testing.T) {
	for _, kind := range []provider.ChunkType{provider.ChunkDone, provider.ChunkError} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			terminalErr := errors.New("synthetic terminal error")
			p := &terminalStreamProvider{terminal: provider.Chunk{Type: kind, Err: terminalErr}, finished: make(chan struct{})}
			a := New(p, tool.NewRegistry(), NewSession("synthetic"), Options{}, event.Discard)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			returned := make(chan error, 1)
			go func() {
				_, _, _, _, _, _, _, err := a.stream(ctx, 1, "synthetic-request")
				returned <- err
			}()
			select {
			case err := <-returned:
				if kind == provider.ChunkError && !errors.Is(err, terminalErr) {
					t.Fatalf("got %v", err)
				}
				if kind == provider.ChunkDone && err != nil {
					t.Fatalf("got %v", err)
				}
			case <-time.After(time.Second):
				cancel()
				<-returned
				t.Fatal("terminal chunk did not release agent from provider")
			}
			select {
			case <-p.finished:
			case <-time.After(time.Second):
				cancel()
				<-p.finished
				t.Fatal("return left request context alive until parent turn cancellation")
			}
			if ctx.Err() != nil {
				t.Fatal("completion cancelled the whole parent turn")
			}
		})
	}
}

func TestOpenAIStreamSlowSinkFailsExplicitlyWithoutToolReplay(t *testing.T) {
	var requests atomic.Int32
	disconnected := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 128; i++ {
			io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"synthetic\"}}]}\n\n")
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		disconnected <- struct{}{}
	}))
	defer srv.Close()
	p, err := openai.New(provider.Config{Name: "synthetic", BaseURL: srv.URL, Model: "local", APIKey: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	releaseSink := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releaseSink) })
	a := New(p, echoRegistry(), NewSession("synthetic"), Options{}, event.FuncSink(func(e event.Event) {
		if e.Kind == event.Text {
			<-releaseSink
		}
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	started := time.Now()
	finished := make(chan error, 1)
	go func() { finished <- a.Run(ctx, "synthetic") }()
	select {
	case <-disconnected:
		elapsed := time.Since(started)
		if elapsed < 4*time.Second || elapsed > 8*time.Second {
			t.Errorf("default consumer timeout took %s", elapsed)
		}
	case <-ctx.Done():
		t.Error("blocked sink kept upstream response open")
	}
	releaseOnce.Do(func() { close(releaseSink) })
	select {
	case err := <-finished:
		if !errors.Is(err, provider.ErrStreamConsumerBlocked) {
			t.Fatalf("expected local backpressure failure, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("agent failed to finish after sink resumed")
	}
	if requests.Load() != 1 {
		t.Fatalf("consumer failure replayed %d requests", requests.Load())
	}
}
