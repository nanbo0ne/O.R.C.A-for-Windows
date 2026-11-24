package main

import (
	"context"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent/testutil"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func TestAccountPublicSubmissionsRetainDetachedController(t *testing.T) {
	for _, path := range []string{"submit", "display", "steer", "runtime_queue"} {
		t.Run(path, func(t *testing.T) {
			f := newLifecycleFixture(t, nil, nil)
			receiptLogin(t, f, &lifecycleStore{})
			gate, release := lifecycleGate(t)
			r := &receiptNonCooperativeRunner{started: make(chan struct{}), release: gate}
			executor := agent.New(testutil.NewMock("synthetic"), tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
			ctrl := control.New(control.Options{Runner: r, Executor: executor, Sink: event.Discard})
			t.Cleanup(ctrl.Close)
			f.app.setTestCtrl(ctrl, accountProviderPrefix+"test/model")
			switch path {
			case "submit":
				if err := f.app.SubmitToTab("test", "synthetic"); err != nil {
					t.Fatal(err)
				}
			case "display":
				if err := f.app.SubmitDisplayToTab("test", "synthetic display", "synthetic input"); err != nil {
					t.Fatal(err)
				}
			case "steer":
				f.app.SteerForTab("test", "synthetic guidance")
			case "runtime_queue":
				f.app.mu.Lock()
				tab := f.app.tabs["test"]
				tab.runtimeReconfiguring = true
				f.app.mu.Unlock()
				if err := f.app.SubmitToTab("test", "synthetic queued input"); err != nil {
					t.Fatal(err)
				}
				// Replace the controller after enqueue; drain must register the new
				// actual recipient, not only the controller seen during enqueue.
				replacement := control.New(control.Options{Runner: r, Sink: event.Discard})
				t.Cleanup(replacement.Close)
				f.app.mu.Lock()
				tab.Ctrl = replacement
				tab.runtimeReconfiguring = false
				queued := append([]pendingRuntimeSubmit(nil), tab.pendingRuntimeSubmits...)
				tab.pendingRuntimeSubmits = nil
				f.app.mu.Unlock()
				ctrl = replacement
				f.app.drainRuntimeSubmits(tab, queued)
			}
			lifecycleWait(t, r.started)
			f.app.mu.Lock()
			delete(f.app.tabs, "test")
			f.app.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if err := f.app.logoutOrcaAccount(ctx); err == nil {
				t.Fatal("logout lost a detached admitted controller")
			}
			if v := f.app.OrcaAccountStatus(); v.State != "logging_out" || v.Authenticated || !ctrl.Running() || !ctrl.Retired() {
				t.Fatal("non-cooperative tool was reported stopped")
			}
			if err := f.app.OrcaAccountLogin(); err == nil {
				t.Fatal("new account admitted while previous tool still runs")
			}
			release()
			receiptEventually(t, func() bool { return f.app.OrcaAccountStatus().State == "signed_out" })
		})
	}
}

func TestAccountDeferredSubmissionRejectsSignedOutIdentity(t *testing.T) {
	f := newLifecycleFixture(t, nil, nil)
	r := &receiptNonCooperativeRunner{started: make(chan struct{}), release: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: r, Sink: event.Discard})
	t.Cleanup(ctrl.Close)
	f.app.setTestCtrl(ctrl, accountProviderPrefix+"test/model")
	tab := f.app.tabByID("test")
	tab.pendingSubmitEpoch, tab.pendingSubmitCount = 1, 1
	f.app.drainRuntimeSubmits(tab, []pendingRuntimeSubmit{{epoch: 1, input: "must not run"}})
	select {
	case <-r.started:
		t.Fatal("signed-out queued submission executed")
	default:
	}
	if tab.pendingSubmitCount != 0 || ctrl.Running() {
		t.Fatal("rejected pending work left a false running state")
	}
}

func TestAccountExpiredIdentityExposesSignOutRecovery(t *testing.T) {
	f := newLifecycleFixture(t, nil, nil)
	receiptLogin(t, f, &lifecycleStore{})
	ctrl := control.New(control.Options{Sink: event.Discard})
	t.Cleanup(ctrl.Close)
	f.app.setTestCtrl(ctrl, accountProviderPrefix+"test/model")
	release, err := f.app.beginIdentityAdmission("test")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := f.app.account.client.CancelContexts(); err != nil {
		t.Fatal(err)
	}
	if v := f.app.OrcaAccountStatus(); v.Authenticated || !v.NeedsSignOut {
		t.Fatal("expired registered controller has no visible sign-out recovery")
	}
	if err := f.app.OrcaAccountLogin(); err == nil {
		t.Fatal("account replacement bypassed previous-controller retirement")
	}
	if err := f.app.OrcaAccountLogout(); err != nil {
		t.Fatal(err)
	}
	if v := f.app.OrcaAccountStatus(); v.NeedsSignOut || v.Authenticated || v.State != "signed_out" {
		t.Fatal("sign-out recovery did not reset account state")
	}
	if err := f.app.OrcaAccountLogin(); err != nil {
		t.Fatal(err)
	}
	lifecycleSignedIn(t, f.app, "subject-2")
}
