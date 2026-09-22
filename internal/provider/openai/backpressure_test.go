package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestReadStreamIdleClosesBlockedDelivery(t *testing.T) {
	body := &unreadStreamBody{
		first:     []byte("data: {\"choices\":[{\"delta\":{\"content\":\"synthetic\"}}]}\n\n"),
		firstRead: make(chan struct{}), release: make(chan struct{}),
	}
	c := &client{name: "synthetic", idleTimeout: 40 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := c.readStream(ctx, &http.Response{Body: body}, make(chan provider.Chunk))
		finished <- err
	}()
	select {
	case err := <-finished:
		if err == nil || !strings.Contains(err.Error(), "stalled") {
			t.Fatalf("expected idle error, got %v", err)
		}
	case <-time.After(time.Second):
		cancel()
		<-finished
		t.Fatal("idle watchdog closed body but did not release blocked chunk delivery")
	}
}

func TestStreamReadsAheadWhileConsumerIsDelayed(t *testing.T) {
	const count = 24
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < count; i++ {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"%02d\"}}]}\n\n", i)
		}
		io.WriteString(w, "data: [DONE]\n\n")
		flush(w)
		// A completed SSE response must be closed independently of UI consumption.
		<-r.Context().Done()
	}))
	defer srv.Close()
	p, err := New(provider.Config{Name: "synthetic", BaseURL: srv.URL, Model: "local", APIKey: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	p.(*client).idleTimeout = 80 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	// Longer than the network idle threshold, with all data already available.
	time.Sleep(200 * time.Millisecond)
	var got, want strings.Builder
	var done bool
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			got.WriteString(chunk.Text)
		case provider.ChunkDone:
			done = true
		case provider.ChunkError:
			t.Fatalf("consumer delay misclassified as network failure: %v", chunk.Err)
		}
	}
	for i := 0; i < count; i++ {
		fmt.Fprintf(&want, "%02d", i)
	}
	if got.String() != want.String() || !done || requests.Load() != 1 {
		t.Fatalf("lost/replayed chunks: text bytes=%d done=%v requests=%d", got.Len(), done, requests.Load())
	}
}

func TestStreamBlockedDeliveryCancelStillClosesBody(t *testing.T) {
	body := &unreadStreamBody{
		first:     []byte("data: {\"choices\":[{\"delta\":{\"content\":\"synthetic\"}}]}\n\n"),
		firstRead: make(chan struct{}), release: make(chan struct{}),
	}
	c := &client{name: "synthetic", idleTimeout: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := c.readStream(ctx, &http.Response{Body: body}, make(chan provider.Chunk))
		finished <- err
	}()
	<-body.firstRead
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not unblock delivery")
	}
	select {
	case <-body.release:
	default:
		t.Fatal("response body left open")
	}
}

func TestStreamConsumerTimeoutClosesResponseWithoutReplay(t *testing.T) {
	var requests atomic.Int32
	disconnected := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n > 1 {
			io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"next\"}}]}\n\ndata: [DONE]\n\n")
			return
		}
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"synthetic-tool\",\"function\":{\"name\":\"echo\",\"arguments\":\"{\"}}]}}]}\n\n")
		for i := 0; i < 2*streamChunkBuffer; i++ {
			io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"synthetic\"}}]}\n\n")
		}
		flush(w)
		<-r.Context().Done()
		close(disconnected)
	}))
	defer srv.Close()
	p, err := New(provider.Config{Name: "synthetic", BaseURL: srv.URL, Model: "local", APIKey: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	p.(*client).idleTimeout = time.Hour
	p.(*client).consumerTimeout = 40 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-disconnected:
	case <-ctx.Done():
		t.Fatal("consumer stalled but HTTP response remained open")
	}
	var gotError bool
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkError:
			gotError = true
			if !errors.Is(chunk.Err, provider.ErrStreamConsumerBlocked) || provider.IsStreamInterrupted(chunk.Err) || provider.IsConnReset(chunk.Err) {
				t.Fatalf("consumer failure incorrectly classified: %v", chunk.Err)
			}
		case provider.ChunkDone, provider.ChunkToolCall:
			t.Fatal("failed stream marked complete or dispatched unfinished tool")
		}
	}
	if !gotError || requests.Load() != 1 {
		t.Fatalf("error=%v requests=%d", gotError, requests.Load())
	}
	ch, err = p.Stream(ctx, provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	var next strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if chunk.Type == provider.ChunkText {
			next.WriteString(chunk.Text)
		}
	}
	if next.String() != "next" || requests.Load() != 2 {
		t.Fatal("consumer failure contaminated next completion")
	}
}

func TestStreamIdleTracksBytesWithinFragmentedEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		parts := []string{"data: {", "\"choices\":[", "{\"delta\":{", "\"content\":\"", "synthetic", "\"}}]}", "\n\n", "data: [DONE]\n\n"}
		for _, part := range parts {
			io.WriteString(w, part)
			flush(w)
			select {
			case <-r.Context().Done():
				return
			case <-time.After(30 * time.Millisecond):
			}
		}
	}))
	defer srv.Close()
	p, err := New(provider.Config{Name: "synthetic", BaseURL: srv.URL, Model: "local", APIKey: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	p.(*client).idleTimeout = 140 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("active fragmented event reported idle: %v", chunk.Err)
		}
		if chunk.Type == provider.ChunkText {
			text.WriteString(chunk.Text)
		}
	}
	if text.String() != "synthetic" {
		t.Fatal("fragmented event was lost")
	}
}

func TestStreamCancelWhileReportingConsumerTimeout(t *testing.T) {
	body := &unreadStreamBody{
		first:     []byte("data: {\"choices\":[{\"delta\":{\"content\":\"synthetic\"}}]}\n\n"),
		firstRead: make(chan struct{}), release: make(chan struct{}),
	}
	c := &client{name: "synthetic", idleTimeout: time.Hour, consumerTimeout: 20 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		c.streamWithReconnect(ctx, &http.Response{Body: body}, func(context.Context) (*http.Request, error) {
			return nil, errors.New("consumer errors must not reconnect")
		}, make(chan provider.Chunk))
	}()
	select {
	case <-body.release:
	case <-time.After(time.Second):
		t.Fatal("blocked delivery did not close body")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("terminal error notification leaked after cancellation")
	}
}
