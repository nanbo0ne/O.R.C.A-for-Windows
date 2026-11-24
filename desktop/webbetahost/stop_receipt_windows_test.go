//go:build windows

package webbetahost

import (
	"context"
	"errors"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

func TestQueuedStopReceiptRevalidatesCurrentHost(t *testing.T) {
	url := webbeta.Origin + "/a/chat/s/synthetic"
	for _, change := range []string{"none", "generation", "url", "active", "blocked", "closed", "identity"} {
		t.Run(change, func(t *testing.T) {
			b := &Browser{active: "owned", generation: "document", state: webbeta.BrowserState{URL: url}}
			queued := make(chan webbeta.Event, 1)
			queued <- webbeta.Event{ID: "owned", Generation: "document", Kind: "stopped", Stop: &webbeta.StopConfirmation{HTTPDone: true, UIIdle: true, TaskExited: true, Revoked: true, OutputDrained: true}}
			b.mu.Lock()
			switch change {
			case "generation":
				b.generation = "new-document"
			case "url":
				b.state.URL = webbeta.Origin + "/a/chat/s/other"
			case "active":
				b.active = "another-request"
			case "blocked":
				b.state.Blocked = "manual_intervention"
			case "closed":
				b.closed = true
			case "identity":
				b.identity.lost = true
			}
			b.mu.Unlock()
			err, confirmed := b.confirmStopReceipt("owned", url, "document", <-queued, context.Canceled)
			if change == "none" {
				if !confirmed || !errors.Is(err, context.Canceled) || !provider.ReplayForbidden(err) {
					t.Fatal("current complete receipt rejected", err)
				}
			} else {
				var failure *webbeta.Failure
				if confirmed || !errors.As(err, &failure) || failure.Code != "stop_unconfirmed" {
					t.Fatal("stale queued receipt accepted", err)
				}
			}
		})
	}
}

func TestStopFailureDiagnosticRevalidatesAndCannotConfirm(t *testing.T) {
	u := webbeta.Origin + "/a/chat/s/synthetic"
	for _, change := range []string{"none", "request", "generation", "url", "closed", "identity"} {
		t.Run(change, func(t *testing.T) {
			b := &Browser{active: "owned", generation: "document", state: webbeta.BrowserState{URL: u}}
			ev := webbeta.Event{ID: "owned", Generation: "document", Kind: "stop_failed", StopDiagnostic: &webbeta.StopDrainDiagnostic{UIIdle: true, TaskExited: true, Revoked: true, OutputDrained: true}}
			switch change {
			case "request":
				ev.ID = "different"
			case "generation":
				b.generation = "new"
			case "url":
				b.state.URL = u + "2"
			case "closed":
				b.closed = true
			case "identity":
				b.identity.lost = true
			}
			d := &webbeta.DispatchDiagnosticError{}
			b.observeStopFailure("owned", u, "document", ev, d)
			if d.StopFailureObserved != (change == "none") {
				t.Fatal("stale diagnostic accepted or valid dropped")
			}
			if _, confirmed := b.confirmStopReceipt("owned", u, "document", ev, context.Canceled); confirmed {
				t.Fatal("diagnostic accepted as stop proof")
			}
		})
	}
}
