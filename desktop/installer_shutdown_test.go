package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

func TestInstallerShutdownSnapshotsAllControllersAndTabDrafts(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	app := &App{tabs: make(map[string]*WorkspaceTab), activeTabID: "draft"}
	for _, id := range []string{"first", "second"} {
		ctrl := controllerWithContent(t, filepath.Join(dir, id+".jsonl"))
		defer ctrl.Close()
		app.tabs[id] = &WorkspaceTab{ID: id, Scope: "global", Ctrl: ctrl}
	}
	app.tabs["draft"] = &WorkspaceTab{ID: "draft", Scope: "global", goal: "unfinished goal", model: "test/model"}
	app.snapshotForInstallerShutdown()
	for _, id := range []string{"first", "second"} {
		b, err := os.ReadFile(filepath.Join(dir, id+".jsonl"))
		if err != nil || !strings.Contains(string(b), "remember this turn") || !strings.Contains(string(b), "acknowledged") {
			t.Fatalf("controller %s snapshot: %s, %v", id, b, err)
		}
	}
	saved := loadTabsFile()
	if saved.ActiveTab != "draft" || len(saved.Tabs) != 3 {
		t.Fatalf("lost tab state: %+v", saved)
	}
	for _, tab := range saved.Tabs {
		if tab.ID == "draft" && tab.Goal == "unfinished goal" && tab.Model == "test/model" {
			return
		}
	}
	t.Fatal("unsent tab goal/model draft was not persisted")
}

type installerDraftTestEvents struct {
	mu        sync.Mutex
	next      int
	listeners map[string]map[int]func(...interface{})
	onEmit    func(string, ...interface{})
}

func (e *installerDraftTestEvents) On(name string, callback func(...interface{})) func() {
	e.mu.Lock()
	if e.listeners == nil {
		e.listeners = make(map[string]map[int]func(...interface{}))
	}
	if e.listeners[name] == nil {
		e.listeners[name] = make(map[int]func(...interface{}))
	}
	e.next++
	id := e.next
	e.listeners[name][id] = callback
	e.mu.Unlock()
	return func() {
		e.mu.Lock()
		delete(e.listeners[name], id)
		e.mu.Unlock()
	}
}

func (e *installerDraftTestEvents) Emit(name string, data ...interface{}) {
	e.mu.Lock()
	var callbacks []func(...interface{})
	for _, callback := range e.listeners[name] {
		callbacks = append(callbacks, callback)
	}
	e.mu.Unlock()
	for _, callback := range callbacks {
		callback(data...)
	}
	if e.onEmit != nil {
		e.onEmit(name, data...)
	}
}

func (e *installerDraftTestEvents) listenerCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.listeners[installerDraftFlushAck])
}

func installerDraftTestContext(events *installerDraftTestEvents) context.Context {
	ctx := context.WithValue(context.Background(), "frontend", struct{}{})
	return context.WithValue(ctx, "events", events)
}

func TestInstallerDraftFlushMatchesAckAndCleansOnlyOwnListener(t *testing.T) {
	events := &installerDraftTestEvents{}
	unrelated := events.On(installerDraftFlushAck, func(...interface{}) {})
	defer unrelated()
	var ids []string
	events.onEmit = func(name string, data ...interface{}) {
		if name != installerDraftFlushRequest {
			return
		}
		id := data[0].(string)
		ids = append(ids, id)
		events.Emit(installerDraftFlushAck, "stale-id", false)
		events.Emit(installerDraftFlushAck, id)
		events.Emit(installerDraftFlushAck, id, "true")
		events.Emit(installerDraftFlushAck, id, true)
		// Duplicate/late deliveries must never block an event callback.
		events.Emit(installerDraftFlushAck, id, false)
	}
	for i := 0; i < 2; i++ {
		if err := requestInstallerDraftFlush(installerDraftTestContext(events), time.Second); err != nil {
			t.Fatal(err)
		}
		if events.listenerCount() != 1 {
			t.Fatal("ack cleanup removed an unrelated listener or leaked its own")
		}
	}
	if ids[0] == "" || ids[0] == ids[1] {
		t.Fatalf("request IDs were not unique: %q", ids)
	}
}

func TestInstallerDraftFlushFailureTimeoutAndCancellation(t *testing.T) {
	for _, mode := range []string{"failure", "timeout", "cancel", "expired"} {
		t.Run(mode, func(t *testing.T) {
			events := &installerDraftTestEvents{}
			ctx, cancel := context.WithCancel(installerDraftTestContext(events))
			defer cancel()
			var late func(...interface{})
			var requestID string
			events.onEmit = func(name string, data ...interface{}) {
				if name != installerDraftFlushRequest {
					return
				}
				requestID = data[0].(string)
				events.mu.Lock()
				for _, callback := range events.listeners[installerDraftFlushAck] {
					late = callback
				}
				events.mu.Unlock()
				switch mode {
				case "failure":
					events.Emit(installerDraftFlushAck, requestID, false)
				case "cancel":
					cancel()
				case "expired":
					t.Error("emitted request after deadline")
				}
			}
			timeout := 30 * time.Millisecond
			if mode == "expired" {
				timeout = 0
			}
			started := time.Now()
			err := requestInstallerDraftFlush(ctx, timeout)
			want := error(context.DeadlineExceeded)
			if mode == "failure" {
				want = errInstallerDraftFlush
			} else if mode == "cancel" {
				want = context.Canceled
			}
			if !errors.Is(err, want) {
				t.Fatalf("flush = %v, want %v", err, want)
			}
			if time.Since(started) > time.Second || events.listenerCount() != 0 {
				t.Fatal("flush was unbounded or leaked ack listener")
			}
			if late != nil {
				finished := make(chan struct{})
				go func() { late(requestID, true); late(requestID, true); close(finished) }()
				select {
				case <-finished:
				case <-time.After(time.Second):
					t.Fatal("late ack blocked after cleanup")
				}
			}
		})
	}
}

func TestInstallerDraftFlushWithoutWails(t *testing.T) {
	events := &installerDraftTestEvents{onEmit: func(string, ...interface{}) { t.Error("emitted without live Wails context") }}
	for _, ctx := range []context.Context{nil, context.Background(), context.WithValue(context.Background(), "events", events), context.WithValue(context.Background(), "frontend", struct{}{})} {
		if err := requestInstallerDraftFlush(ctx, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(installerDraftTestContext(events))
	cancel()
	if err := requestInstallerDraftFlush(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context = %v", err)
	}
	if events.listenerCount() != 0 {
		t.Fatal("registered listener after cancellation")
	}
}

func TestInstallerForceQuitBeforeCloseBypassesBackgroundOnce(t *testing.T) {
	isolateDesktopUserDirs(t)
	consumeSystemQuitRequested()
	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.SetDesktopCloseBehavior("background"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	app.forceQuit.Store(true)
	if app.beforeClose(context.Background()) {
		t.Fatal("force quit was redirected to background")
	}
	if app.forceQuit.Load() {
		t.Fatal("beforeClose did not consume the one-shot force flag")
	}
	app.shutdown(context.Background())
}

func TestInstallerShutdownBeforeRestorePreservesSavedTabs(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := &App{tabs: map[string]*WorkspaceTab{"saved": {ID: "saved", Scope: "global"}}, activeTabID: "saved"}
	app.snapshotForInstallerShutdown()
	app.tabs = nil
	app.activeTabID = ""
	app.snapshotForInstallerShutdown()
	if saved := loadTabsFile(); saved.ActiveTab != "saved" || len(saved.Tabs) != 1 {
		t.Fatalf("early shutdown overwrote saved tabs: %+v", saved)
	}
	app.tabs = map[string]*WorkspaceTab{"partial": {ID: "partial", Scope: "global"}}
	app.snapshotForInstallerShutdown()
	if saved := loadTabsFile(); saved.ActiveTab != "saved" || len(saved.Tabs) != 1 || saved.Tabs[0].ID != "saved" {
		t.Fatalf("partial restore overwrote saved tabs: %+v", saved)
	}
}
