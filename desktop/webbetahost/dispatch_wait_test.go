package webbetahost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

func TestDispatchAckTimeoutRemainsUncertain(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		events := make(chan webbeta.Event, 1)
		if foreign {
			events <- webbeta.Event{ID: "other", Kind: "phase", Code: "dispatch_ack"}
		}
		d := &webbeta.DispatchDiagnosticError{Stage: "dispatch_queued"}
		stops := 0
		err := awaitBrowserResult(context.Background(), "owned", events, func(webbeta.Event) error { t.Fatal("unexpected event"); return nil }, func(err error) error { stops++; return err }, func(string) {}, d, time.Millisecond, time.Second)
		var failure *webbeta.Failure
		if !errors.As(err, &failure) || failure.Code != "dispatch_ack_timeout" || !failure.Sent || stops != 1 || d.Ack {
			t.Fatalf("incorrect timeout: %v %+v", err, d)
		}
	}
}

func TestReuseDocumentRequiresExactReadyConversation(t *testing.T) {
	target := webbeta.Origin + "/a/chat/s/synthetic-owned"
	for _, mode := range []string{"ready", "busy", "blocked", "unready", "different", "missing-generation", "root"} {
		t.Run(mode, func(t *testing.T) {
			st := webbeta.BrowserState{Ready: true, URL: target}
			gen, u := "document-1", target
			switch mode {
			case "busy":
				st.Busy = true
			case "blocked":
				st.Blocked = "manual_intervention"
			case "unready":
				st.Ready = false
			case "different":
				st.URL += "-other"
			case "missing-generation":
				gen = ""
			case "root":
				u = webbeta.Origin + "/"
				st.URL = u
			}
			if got := canReuseDocument(st, gen, u); got != (mode == "ready") {
				t.Fatalf("reused=%t", got)
			}
		})
	}
}

func TestDispatchConversationMismatchOnlyUnsentBeforeSend(t *testing.T) {
	for _, mode := range []string{"before-send", "after-send", "missing-ack"} {
		t.Run(mode, func(t *testing.T) {
			events := make(chan webbeta.Event, 3)
			if mode != "missing-ack" {
				events <- webbeta.Event{ID: "owned", Kind: "phase", Code: "dispatch_ack"}
			}
			if mode == "after-send" {
				events <- webbeta.Event{ID: "owned", Kind: "phase", Code: "send_attempted"}
			}
			events <- webbeta.Event{ID: "owned", Kind: "error", Code: "conversation_mismatch"}
			blocked := false
			err := awaitBrowserResult(context.Background(), "owned", events, func(webbeta.Event) error { return nil }, func(err error) error { return err }, func(string) { blocked = true }, &webbeta.DispatchDiagnosticError{}, time.Second, time.Second)
			var failure *webbeta.Failure
			if !errors.As(err, &failure) || failure.Sent != (mode != "before-send") || blocked != (mode != "before-send") {
				t.Fatalf("failure=%+v blocked=%t", failure, blocked)
			}
		})
	}
}

func TestDispatchAcknowledgedThenCancelledKeepsStage(t *testing.T) {
	events := make(chan webbeta.Event, 3)
	events <- webbeta.Event{ID: "owned", Kind: "phase", Code: "dispatch_ack"}
	events <- webbeta.Event{ID: "owned", Kind: "phase", Code: "send_attempted"}
	events <- webbeta.Event{ID: "owned", Kind: "text", Text: "synthetic"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &webbeta.DispatchDiagnosticError{}
	err := awaitBrowserResult(ctx, "owned", events, func(webbeta.Event) error { cancel(); return nil }, func(err error) error { return err }, func(string) {}, d, time.Second, time.Second)
	if !errors.Is(err, context.Canceled) || !d.Ack || !d.SendAttempted || d.TextBytes != 9 {
		t.Fatalf("lost stage: %v %+v", err, d)
	}
}

func TestDispatchMissingAdapterReturnsImmediately(t *testing.T) {
	events := make(chan webbeta.Event, 1)
	events <- webbeta.Event{ID: "owned", Kind: "error", Code: "adapter_missing"}
	d := &webbeta.DispatchDiagnosticError{}
	err := awaitBrowserResult(context.Background(), "owned", events, func(webbeta.Event) error { t.Fatal("emitted error as content"); return nil }, func(err error) error { t.Fatal("unexpected stop"); return err }, func(string) {}, d, time.Second, time.Second)
	var failure *webbeta.Failure
	if !errors.As(err, &failure) || failure.Code != "adapter_missing" {
		t.Fatal(err)
	}
}

func TestDispatchDoesNotAcceptDoneWithoutAck(t *testing.T) {
	events := make(chan webbeta.Event, 1)
	events <- webbeta.Event{ID: "owned", Kind: "done"}
	d := &webbeta.DispatchDiagnosticError{}
	err := awaitBrowserResult(context.Background(), "owned", events, func(webbeta.Event) error { t.Fatal("unacknowledged completion emitted"); return nil }, func(err error) error { return err }, func(string) {}, d, time.Second, time.Second)
	var failure *webbeta.Failure
	if !errors.As(err, &failure) || failure.Code != "dispatch_ack_missing" || !failure.Sent {
		t.Fatal(err)
	}
}
