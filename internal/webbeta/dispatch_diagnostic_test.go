package webbeta

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func TestDispatchDiagnosticPrivacyAndCause(t *testing.T) {
	cause := errors.Join(context.DeadlineExceeded, errors.New("SECRET https://private/path user-body"))
	d := &DispatchDiagnosticError{Cause: cause, Operation: "SECRET", Stage: "SECRET", Stop: "SECRET"}
	d.Observe(Event{Kind: "phase", Code: "SECRET"})
	d.Observe(Event{Kind: "phase", Code: "dispatch_ack"})
	d.Observe(Event{Kind: "text", Text: "秘密SECRET"})
	d.Observe(Event{Kind: "reasoning", Text: "SECRET reason"})
	if strings.Contains(d.Error(), "SECRET") || !strings.Contains(d.Error(), "stage=dispatch_ack") || !errors.Is(d, context.DeadlineExceeded) {
		t.Fatal(d)
	}
	if d.TextBytes != len("秘密SECRET") || d.ReasoningChunks != 1 || !d.Ack {
		t.Fatal(d)
	}
}

func TestStopFailureDiagnosticIsNotConfirmation(t *testing.T) {
	d := &DispatchDiagnosticError{Cause: &Failure{Code: "stop_unconfirmed"}, Stop: "unconfirmed"}
	ev := Event{Kind: "stop_failed", ID: "SECRET", URL: "https://private/SECRET", Text: "SECRET", StopDiagnostic: &StopDrainDiagnostic{UIIdle: true, TaskExited: true, Revoked: true, OutputDrained: true}}
	d.Observe(ev)
	if !d.StopFailureObserved || d.StopFailure.HTTPDone || !d.StopFailure.UIIdle || strings.Contains(d.Error(), "SECRET") || !strings.Contains(d.Error(), "stop_http_done=false") {
		t.Fatal(d)
	}
	if confirmedStoppedFor(d, "SECRET", "https://private/SECRET") {
		t.Fatal("diagnostic authorized stop")
	}
	copy := *d
	ev.StopDiagnostic.HTTPDone = true
	d.Observe(ev)
	if copy.StopFailure.HTTPDone {
		t.Fatal("diagnostic snapshot mutated")
	}
}

func TestDispatchDiagnosticKeepsFirstTriggerWhenStopFails(t *testing.T) {
	d := &DispatchDiagnosticError{}
	d.Observe(Event{Kind: "error", Code: "raw_response_rewritten", Text: "SECRET"})
	d.RecordTrigger(&Failure{Code: "stop_unconfirmed"})
	d.Cause = errors.Join(&Failure{Code: "stop_unconfirmed"}, &Failure{Code: "raw_response_rewritten"})
	if !strings.Contains(d.Error(), "trigger=raw_response_rewritten") || !strings.Contains(d.Error(), "code=stop_unconfirmed") || strings.Contains(d.Error(), "SECRET") {
		t.Fatal(d)
	}
	for _, input := range []string{"SECRET", "raw_SECRET", "https://private/SECRET"} {
		d = &DispatchDiagnosticError{}
		d.Observe(Event{Kind: "error", Code: input})
		if d.Trigger != "unknown" || strings.Contains(d.Error(), "SECRET") {
			t.Fatal(d)
		}
	}
}

func TestDispatchDiagnosticPublicFragmentCategories(t *testing.T) {
	for _, suffix := range []string{"missing", "null", "invalid", "empty", "request", "template_response", "file", "tool_search", "tool_open", "tool_find", "tip", "read_link"} {
		code := "raw_fragment_type_" + suffix
		d := &DispatchDiagnosticError{}
		d.Observe(Event{Kind: "error", Code: code, Text: "SECRET content"})
		if d.Trigger != code || strings.Contains(d.Error(), "SECRET") {
			t.Fatal(d)
		}
	}
	if safeDispatchTrigger("raw_fragment_type_SECRET") != "unknown" {
		t.Fatal("arbitrary upstream type leaked into diagnostics")
	}
}

type dispatchDiagnosticBrowser struct {
	fakeBrowser
	opening      bool
	dispatches   int
	diagnostic   *DispatchDiagnosticError
	beforeReturn func()
}

func (b *dispatchDiagnosticBrowser) Run(ctx context.Context, c Command, emit func(Event) error) error {
	b.dispatches++
	if c.Operation == "opening" && !b.opening {
		return b.fakeBrowser.Run(ctx, c, emit)
	}
	b.diagnostic = &DispatchDiagnosticError{Operation: c.Operation, Stage: "dispatch_queued", Cause: &Failure{Code: "dispatch_ack_timeout", Sent: true}}
	provider.RecordCancellationDiagnostic(ctx, *b.diagnostic)
	if b.beforeReturn != nil {
		b.beforeReturn()
	}
	return b.diagnostic
}

type cancelledDiagnosticAdapter struct {
	*countedWebAdapter
	browser *dispatchDiagnosticBrowser
}

func (p *cancelledDiagnosticAdapter) BeginTask(ctx context.Context, conversation, task string) (context.Context, func(), error) {
	ctx, end, err := p.Adapter.BeginTask(ctx, conversation, task)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	p.browser.beforeReturn = cancel
	return ctx, func() { cancel(); end() }, nil
}

func TestActualAgentDispatchDiagnosticSurvivesTaskCancellation(t *testing.T) {
	b := &dispatchDiagnosticBrowser{opening: true}
	e := testEngine(t, b)
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	p := &cancelledDiagnosticAdapter{countedWebAdapter: &countedWebAdapter{Adapter: &Adapter{name: "synthetic-web"}}, browser: b}
	a := agent.New(p, tool.NewRegistry(), agent.NewSession("synthetic cancellation"), agent.Options{}, event.Discard)
	err := a.Run(context.Background(), "synthetic question")
	var diagnostic *DispatchDiagnosticError
	if !errors.As(err, &diagnostic) || !provider.ReplayForbidden(err) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel replaced diagnostic: %v", err)
	}
}

func TestActualAgentPreservesDispatchDiagnostic(t *testing.T) {
	for _, opening := range []bool{true, false} {
		b := &dispatchDiagnosticBrowser{opening: opening}
		e := testEngine(t, b)
		old := CurrentEngine()
		SetEngine(e)
		p := &countedWebAdapter{Adapter: &Adapter{name: "synthetic-web"}}
		a := agent.New(p, tool.NewRegistry(), agent.NewSession("synthetic diagnostic"), agent.Options{}, event.Discard)
		err := a.Run(context.Background(), "synthetic question")
		SetEngine(old)
		var diagnostic *DispatchDiagnosticError
		if !errors.As(err, &diagnostic) || diagnostic != b.diagnostic || !provider.ReplayForbidden(err) {
			t.Fatalf("diagnostic lost: %v", err)
		}
		want := "task"
		dispatches := 2
		if opening {
			want = "opening"
			dispatches = 1
		}
		if diagnostic.Operation != want || len(p.requests) != 1 || b.dispatches != dispatches {
			t.Fatalf("unexpected dispatch state: %s %d %d", diagnostic.Operation, len(p.requests), b.dispatches)
		}
	}
}
