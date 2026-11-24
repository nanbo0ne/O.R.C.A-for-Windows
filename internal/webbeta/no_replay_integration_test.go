package webbeta

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type interruptedWebBrowser struct {
	fakeBrowser
	cause      error
	partial    bool
	opening    bool
	empty      bool
	dispatches int
	afterText  func()
}

func (b *interruptedWebBrowser) Run(ctx context.Context, c Command, emit func(Event) error) error {
	b.dispatches++
	if c.URL == "" && !b.opening {
		return b.fakeBrowser.Run(ctx, c, emit)
	}
	if b.partial {
		// Even a complete tool block cannot execute before upstream completion.
		text := "ORCA_PROGRESS_V2 {\"text\":\"synthetic progress\"}\n" + `ORCA_TURN_V2 {"type":"tools","calls":[{"id":"unconfirmed","name":"synthetic_echo","arguments":{"value":7}}]}`
		if err := emit(Event{ID: c.ID, Kind: "text", Text: text}); err != nil {
			return err
		}
		if b.afterText != nil {
			b.afterText()
		}
	}
	if b.empty {
		return emit(Event{ID: c.ID, Kind: "done", URL: c.URL})
	}
	// nil deliberately means a dropped completion, not success.
	return b.cause
}

type countedWebAdapter struct {
	*Adapter
	requests []provider.RequestPurpose
}

func (p *countedWebAdapter) Stream(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, r.Purpose)
	return p.Adapter.Stream(ctx, r)
}

func TestActualAgentDoesNotResumeUncertainWebDispatch(t *testing.T) {
	for _, test := range []struct {
		name             string
		cause            error
		partial, opening bool
	}{
		{"eof", io.EOF, false, false},
		{"interrupted-partial-tool", &provider.StreamInterruptedError{Err: io.ErrUnexpectedEOF}, true, false},
		{"missing-completion", nil, true, false},
		{"opening-interrupted", &provider.StreamInterruptedError{Err: io.EOF}, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := &interruptedWebBrowser{cause: test.cause, partial: test.partial, opening: test.opening}
			e := testEngine(t, b)
			old := CurrentEngine()
			SetEngine(e)
			defer SetEngine(old)
			p := &countedWebAdapter{Adapter: &Adapter{name: "synthetic-web"}}
			registry := tool.NewRegistry()
			capability := &probeTool{}
			registry.Add(capability)
			retries := 0
			sink := event.Lifecycle(event.FuncSink(func(ev event.Event) {
				if ev.Kind == event.Retrying {
					retries++
				}
			}))
			a := agent.New(p, registry, agent.NewSession("synthetic no replay"), agent.Options{}, sink)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := a.Run(ctx, "synthetic task")
			if err == nil || !provider.ReplayForbidden(err) {
				t.Errorf("dispatched failure lost no-replay marker: %v", err)
			}
			if test.cause != nil && !errors.Is(err, test.cause) {
				t.Error("original error lost", err)
			}
			wantDispatches := 2
			if test.opening {
				wantDispatches = 1
			}
			if len(p.requests) != 1 || b.dispatches != wantDispatches || retries != 0 || capability.calls != 0 {
				t.Fatalf("automatic continuation: requests=%d dispatches=%d retries=%d tools=%d", len(p.requests), b.dispatches, retries, capability.calls)
			}
			for _, m := range a.Session().Messages {
				if len(m.ToolCalls) > 0 || m.Role == provider.RoleTool {
					t.Fatal("unconfirmed tool persisted or executed")
				}
			}
		})
	}
}

func TestActualCompactionDoesNotFallbackAfterWebDispatch(t *testing.T) {
	for _, test := range []struct {
		name           string
		cause          error
		partial, empty bool
	}{
		{"eof", io.EOF, false, false},
		{"interrupted-partial", &provider.StreamInterruptedError{Err: io.ErrUnexpectedEOF}, true, false},
		{"parameter-shaped-error", &provider.APIError{Status: 422, Body: "reasoning_effort unsupported"}, false, false},
		{"transient-shaped-error", &provider.APIError{Status: 503, Body: "synthetic unavailable"}, false, false},
		{"missing-completion", nil, false, false},
		{"empty-confirmed-completion", nil, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := &interruptedWebBrowser{cause: test.cause, partial: test.partial, empty: test.empty}
			e := testEngine(t, b)
			old := CurrentEngine()
			SetEngine(e)
			defer SetEngine(old)
			p := &countedWebAdapter{Adapter: &Adapter{name: "synthetic-web"}}
			session := agent.NewSession("synthetic compression")
			for i := 0; i < 8; i++ {
				session.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("synthetic context ", 160)})
				session.Add(provider.Message{Role: provider.RoleAssistant, Content: "synthetic answer"})
			}
			before := session.Snapshot()
			a := agent.New(p, tool.NewRegistry(), session, agent.Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)
			ctx, end := agent.WithParentTurn(context.Background())
			defer end()
			ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			err := a.CompactNow(ctx, "")
			if err == nil || !provider.ReplayForbidden(err) {
				t.Errorf("compaction lost no-replay policy: %v", err)
			}
			if test.cause != nil && !errors.Is(err, test.cause) {
				t.Error("original failure lost", err)
			}
			if len(p.requests) != 1 || p.requests[0] != provider.RequestPurposeCompaction || b.dispatches != 2 {
				t.Fatalf("fallback created a new request/owner: requests=%v dispatches=%d", p.requests, b.dispatches)
			}
			if !reflect.DeepEqual(before, session.Snapshot()) {
				t.Fatal("failed compression changed original history")
			}
		})
	}
}

func TestActualCompactionCannotCommitPartialWhenWebIdentityCloses(t *testing.T) {
	b := &interruptedWebBrowser{partial: true, cause: io.ErrUnexpectedEOF}
	e := testEngine(t, b)
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	b.afterText = e.startClose
	p := &countedWebAdapter{Adapter: &Adapter{name: "synthetic-web"}}
	session := agent.NewSession("synthetic compression")
	for i := 0; i < 8; i++ {
		session.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("synthetic context ", 160)})
		session.Add(provider.Message{Role: provider.RoleAssistant, Content: "synthetic answer"})
	}
	before := session.Snapshot()
	a := agent.New(p, tool.NewRegistry(), session, agent.Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)
	ctx, end := agent.WithParentTurn(context.Background())
	defer end()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := a.CompactNow(ctx, "")
	if err == nil || !provider.ReplayForbidden(err) {
		t.Errorf("identity close reported partial summary as valid: %v", err)
	}
	if !reflect.DeepEqual(before, session.Snapshot()) {
		t.Error("partial summary replaced original history")
	}
	if len(p.requests) != 1 || b.dispatches != 2 {
		t.Error("identity close triggered replay")
	}
	deadline, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := e.CloseContext(deadline); err != nil {
		t.Fatal("identity drain stalled", err)
	}
}
