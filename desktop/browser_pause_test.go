package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func browserPauseApp(s *browserService) (*App, *control.Controller) {
	ctrl := &control.Controller{}
	return &App{browser: s, activeTabID: "owner", tabs: map[string]*WorkspaceTab{
		"owner": {ID: "owner", Ctrl: ctrl},
	}}, ctrl
}

func TestBrowserParentPauseCancelsPendingModelAndRequiresNewTurn(t *testing.T) {
	s, b, _ := browserFixture(t)
	a, ctrl := browserPauseApp(s)
	autoBrowserConsent(s)
	entered := make(chan struct{})
	reply := make(chan provider.Chunk, 2)
	p := &controlTestProvider{stream: func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		if ch, ok := browserProbeReply(r); ok {
			return ch, nil
		}
		close(entered)
		return reply, nil
	}}
	ctx := computerParentTestContext(t)
	done := make(chan error, 1)
	go func() { _, err := s.run(ctx, "owner", "first", browserInput(), p, "fake/model"); done <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("model not reached")
	}
	a.PauseTab("")
	if !ctrl.Paused() || s.state("owner").TaskState != "taken_over" {
		t.Fatal("parent pause did not stop browser", s.state("owner"))
	}
	reply <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "late-click", Name: "browser_action", Arguments: `{"kind":"click","elementId":"button"}`}}
	close(reply)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("pending model was not cancelled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("browser task survived pause")
	}
	if b.actions != 0 || s.state("owner").TaskState != "taken_over" {
		t.Fatal("late response dispatched or terminal takeover lost", b.actions)
	}
	good := browserCompleter(func(provider.Request) <-chan provider.Chunk {
		return browserCall("complete", "browser_complete", `{"summary":"done"}`)
	})
	if _, err := s.run(computerParentTestContext(t), "owner", "while-paused", browserInput(), good, "fake/model"); !errors.Is(err, errBrowserStopped) {
		t.Fatal("new task admitted during pause", err)
	}
	a.ResumeTab("owner")
	if ctrl.Paused() || s.state("owner").TaskState != "taken_over" {
		t.Fatal("resume silently restarted browser")
	}
	if _, err := s.run(ctx, "owner", "renamed", browserInput(), good, "fake/model"); !errors.Is(err, errBrowserStopped) {
		t.Fatal("old parent turn resumed browser", err)
	}
	before := b.observations
	if _, err := s.run(computerParentTestContext(t), "owner", "new-turn", browserInput(), good, "fake/model"); err != nil || b.observations <= before {
		t.Fatal("new turn did not observe afresh", err)
	}
}

func TestBrowserParentPauseDrainsDispatchedInputBeforeAcknowledgement(t *testing.T) {
	s, b, _ := browserFixture(t)
	a, ctrl := browserPauseApp(s)
	autoBrowserConsent(s)
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	b.execute = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return ctx.Err()
	}
	p := browserCompleter(func(provider.Request) <-chan provider.Chunk {
		return browserCall("in-flight", "browser_action", `{"kind":"wait"}`)
	})
	done, paused := make(chan error, 1), make(chan struct{})
	go func() {
		_, err := s.run(computerParentTestContext(t), "owner", "outer", browserInput(), p, "fake/model")
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("native not entered")
	}
	go func() { a.PauseTab("owner"); close(paused) }()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("native input not cancelled")
	}
	if ctrl.Paused() || s.state("owner").TaskState != "stopping" {
		t.Fatal("pause acknowledged while native input outstanding")
	}
	select {
	case <-paused:
		t.Fatal("pause returned before native input settled")
	default:
	}
	once.Do(func() { close(release) })
	select {
	case <-paused:
	case <-time.After(2 * time.Second):
		t.Fatal("pause did not settle")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("task did not settle")
	}
	if !ctrl.Paused() || b.actions != 1 || s.state("owner").TaskState != "taken_over" {
		t.Fatal("input repeated or final pause state incorrect")
	}
}

func TestBrowserParentPauseCancelsConsentAndKeepsOwnerIsolation(t *testing.T) {
	s, b, _ := browserFixture(t)
	a, ctrl := browserPauseApp(s)
	approval := make(chan struct{}, 1)
	s.emit = func(_ string, state BrowserState) {
		if state.Approval != nil {
			select {
			case approval <- struct{}{}:
			default:
			}
		}
	}
	p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
		t.Error("unapproved task reached model")
		return nil, errors.New("unexpected provider call")
	}}
	done := make(chan error, 1)
	go func() {
		_, err := s.run(computerParentTestContext(t), "owner", "outer", browserInput(), p, "fake/model")
		done <- err
	}()
	select {
	case <-approval:
	case <-time.After(2 * time.Second):
		t.Fatal("consent not reached")
	}
	a.PauseTab("missing-owner")
	if ctrl.Paused() {
		t.Fatal("unknown owner paused active conversation")
	}
	a.PauseTab("owner")
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approval did not cancel")
	}
	state := s.state("owner")
	if !ctrl.Paused() || state.TaskState != "taken_over" || state.Approval != nil || b.actions != 0 || b.observations != 0 {
		t.Fatal("pending consent survived pause", state)
	}
}
