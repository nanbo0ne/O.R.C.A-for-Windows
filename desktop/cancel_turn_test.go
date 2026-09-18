package main

import (
	"context"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"testing"
)

func TestCancelQueuedIdentityTracksOnlyItsAdmittedTurn(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	runner := &admissionBlockingRunner{started: make(chan context.Context, 2), release: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: runner, Sink: event.Discard})
	defer ctrl.Close()
	app.setTestCtrl(ctrl, "custom/test")
	tab := app.tabs["test"]
	tab.runtimeReconfiguring = true
	app.queueSubmitDuringRuntimeReconfigure(tab.ID, pendingRuntimeSubmit{input: "queued request"})
	id := app.TurnStatusForTab(tab.ID).TurnID
	app.drainRuntimeSubmits(tab, tab.pendingRuntimeSubmits)
	<-runner.started
	actual := ctrl.TurnStatus().TurnID
	if actual == id || actual == "" {
		t.Fatal("missing admitted identity")
	}
	ack := app.RequestCancelTurnForTab(tab.ID, id)
	if !ack.Accepted || ack.TurnID != actual {
		t.Fatalf("queued transition cancellation rejected: %+v", ack)
	}
	waitNotRunning(t, ctrl)
	ctrl.Send("successor")
	<-runner.started
	if app.RequestCancelTurnForTab(tab.ID, id).Accepted {
		t.Fatal("queued alias cancelled successor")
	}
	ctrl.Cancel()
	waitNotRunning(t, ctrl)
}

func TestCancelRuntimeQueueCannotStartAfterRebuild(t *testing.T) {
	app := NewApp()
	tab := testTab("queued", t.TempDir())
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.activeTabID = tab.ID
	tab.runtimeReconfiguring = true
	if !app.queueSubmitDuringRuntimeReconfigure(tab.ID, pendingRuntimeSubmit{input: "must never execute"}) {
		t.Fatal("not queued")
	}
	queued := append([]pendingRuntimeSubmit(nil), tab.pendingRuntimeSubmits...)
	before := app.TurnStatusForTab(tab.ID)
	if !before.Running || before.TurnID == "" {
		t.Fatalf("invisible pending submit: %+v", before)
	}
	if app.RequestCancelTurnForTab(tab.ID, "old-turn").Accepted {
		t.Fatal("stale cancel accepted")
	}
	ack := app.RequestCancelTurnForTab(tab.ID, before.TurnID)
	if !ack.Accepted || ack.Running || ack.Outcome != event.TurnOutcomeCancelled {
		t.Fatalf("ack=%+v", ack)
	}
	app.drainRuntimeSubmits(tab, queued)
	if len(tab.pendingRuntimeSubmits) != 0 || app.TurnStatusForTab(tab.ID).Running {
		t.Fatal("cancelled queue survived")
	}
	if !app.queueSubmitDuringRuntimeReconfigure(tab.ID, pendingRuntimeSubmit{input: "new request"}) {
		t.Fatal("not requeued")
	}
	if app.RequestCancelTurnForTab(tab.ID, before.TurnID).Accepted {
		t.Fatal("old pending identity cancels new submit")
	}
	app.RequestCancelTab(tab.ID)
}

func TestCancelTurnDoesNotAffectOtherTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := NewApp()
	a.tabs = map[string]*WorkspaceTab{}
	for _, id := range []string{"a", "b"} {
		runner := &admissionBlockingRunner{started: make(chan context.Context, 1), release: make(chan struct{})}
		ctrl := control.New(control.Options{Runner: runner, Sink: event.Discard})
		t.Cleanup(ctrl.Close)
		tab := testTab(id, t.TempDir())
		tab.Ctrl = ctrl
		a.tabs[id] = tab
		ctrl.Send("synthetic concurrent task")
		<-runner.started
	}
	first, second := a.tabs["a"].Ctrl, a.tabs["b"].Ctrl
	firstID, secondID := first.TurnStatus().TurnID, second.TurnStatus().TurnID
	if a.RequestCancelTurnForTab("b", firstID).Accepted {
		t.Fatal("foreign turn ID accepted")
	}
	if !a.RequestCancelTurnForTab("a", firstID).Accepted {
		t.Fatal("owned cancellation rejected")
	}
	waitNotRunning(t, first)
	if status := second.TurnStatus(); !status.Running || status.CancelRequested || status.TurnID != secondID {
		t.Fatalf("other tab changed: %+v", status)
	}
	if !a.RequestCancelTurnForTab("b", secondID).Accepted {
		t.Fatal("other tab cannot stop independently")
	}
	waitNotRunning(t, second)
}
