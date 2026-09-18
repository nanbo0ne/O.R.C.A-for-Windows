//go:build windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/installipc"
)

func TestInstallerShutdownListenerSnapshotsBeforeQuit(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	app, tab := appWithTab(t, path)
	defer tab.Ctrl.Close()
	quit := make(chan struct{})
	if err := app.listenForInstallerShutdown(context.Background(), dir, func() {
		b, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(b), "remember this turn") {
			t.Errorf("quit preceded snapshot: %v", err)
		}
		if saved := loadTabsFile(); saved.ActiveTab != tab.ID || len(saved.Tabs) != 1 {
			t.Errorf("quit preceded tab persistence: %+v", saved)
		}
		app.stopInstallerShutdownListener()
		close(quit)
	}); err != nil {
		t.Fatal(err)
	}
	defer app.stopInstallerShutdownListener()
	if sent, err := installipc.RequestShutdown(dir); err != nil || !sent {
		t.Fatalf("request = %v, %v", sent, err)
	}
	select {
	case <-quit:
	case <-time.After(5 * time.Second):
		t.Fatal("installer shutdown did not reach quit")
	}
}

func TestInstallerShutdownHandshakePrecedesSnapshotAndUnlockedQuit(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	app, tab := appWithTab(t, path)
	defer tab.Ctrl.Close()
	events := &installerDraftTestEvents{}
	var flushed atomic.Bool
	events.onEmit = func(name string, data ...interface{}) {
		if name != installerDraftFlushRequest {
			return
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("snapshot ran before draft flush")
		}
		flushed.Store(true)
		events.Emit(installerDraftFlushAck, data[0], true)
	}
	quit := make(chan struct{})
	if err := app.listenForInstallerShutdown(installerDraftTestContext(events), dir, func() {
		defer close(quit)
		if !flushed.Load() || events.listenerCount() != 0 {
			t.Error("quit preceded draft flush or ack listener cleanup")
		}
		if _, err := os.Stat(path); err != nil {
			t.Error("quit preceded controller snapshot")
		}
		if !app.installerShutdown.mu.TryLock() {
			t.Error("quit would deadlock reentering shutdown with listener mutex held")
			return
		}
		app.installerShutdown.mu.Unlock()
		if !app.mu.TryLock() {
			t.Error("beforeClose would deadlock acquiring app mutex")
			return
		}
		app.mu.Unlock()
		// Match quitApp's force flag and Wails' synchronous OnBeforeClose call.
		app.forceQuit.Store(true)
		if app.beforeClose(context.Background()) {
			t.Error("beforeClose prevented installer quit")
		}
		app.shutdown(context.Background())
	}); err != nil {
		t.Fatal(err)
	}
	defer app.stopInstallerShutdownListener()
	if sent, err := installipc.RequestShutdown(dir); err != nil || !sent {
		t.Fatalf("request = %v, %v", sent, err)
	}
	select {
	case <-quit:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown path deadlocked")
	}
}

func TestInstallerShutdownCancelsPendingDraftHandshake(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := &App{}
	dir := t.TempDir()
	events := &installerDraftTestEvents{}
	requested := make(chan struct{})
	events.onEmit = func(name string, _ ...interface{}) {
		if name == installerDraftFlushRequest {
			close(requested)
		}
	}
	var quits atomic.Int32
	if err := app.listenForInstallerShutdown(installerDraftTestContext(events), dir, func() { quits.Add(1) }); err != nil {
		t.Fatal(err)
	}
	defer app.stopInstallerShutdownListener()
	if sent, err := installipc.RequestShutdown(dir); err != nil || !sent {
		t.Fatalf("request = %v, %v", sent, err)
	}
	select {
	case <-requested:
	case <-time.After(time.Second):
		t.Fatal("draft handshake did not start")
	}
	stopped := make(chan struct{})
	go func() { app.shutdown(context.Background()); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("shutdown blocked on draft acknowledgement")
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for events.listenerCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if events.listenerCount() != 0 || quits.Load() != 0 {
		t.Fatal("shutdown leaked ack listener or allowed late quit")
	}
}

func TestInstallerShutdownMissingFrontendAckStillQuitsWithinBudget(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	app := &App{}
	events := &installerDraftTestEvents{}
	quit := make(chan struct{})
	if err := app.listenForInstallerShutdown(installerDraftTestContext(events), dir, func() {
		app.shutdown(context.Background())
		close(quit)
	}); err != nil {
		t.Fatal(err)
	}
	defer app.stopInstallerShutdownListener()
	started := time.Now()
	if sent, err := installipc.RequestShutdown(dir); err != nil || !sent {
		t.Fatalf("request = %v, %v", sent, err)
	}
	select {
	case <-quit:
	case <-time.After(2 * time.Second):
		t.Fatal("missing frontend ack exceeded shutdown budget")
	}
	if elapsed := time.Since(started); elapsed < installerDraftFlushTimeout || elapsed > 2*time.Second {
		t.Fatalf("missing ack shutdown duration = %v", elapsed)
	}
	if events.listenerCount() != 0 {
		t.Fatal("ack timeout leaked listener")
	}
}

func TestInstallerShutdownStopsWithApp(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := &App{}
	dir := t.TempDir()
	if err := app.listenForInstallerShutdown(context.Background(), dir, func() { t.Error("quit after shutdown") }); err != nil {
		t.Fatal(err)
	}
	app.shutdown(context.Background())
	app.stopInstallerShutdownListener()
	if sent, err := installipc.RequestShutdown(dir); sent || err != nil {
		t.Fatalf("listener survived app shutdown: %v, %v", sent, err)
	}
	if err := app.listenForInstallerShutdown(context.Background(), dir, func() { t.Error("listener restarted after shutdown") }); err != nil {
		t.Fatal(err)
	}
	if sent, err := installipc.RequestShutdown(dir); sent || err != nil {
		t.Fatalf("listener restarted after app shutdown: %v, %v", sent, err)
	}
}
