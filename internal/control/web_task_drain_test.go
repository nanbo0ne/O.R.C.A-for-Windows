package control

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/permission"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

type controllerDrainBrowser struct{ started, stopping, release chan struct{} }

func (*controllerDrainBrowser) State() webbeta.BrowserState {
	return webbeta.BrowserState{Ready: true, URL: webbeta.Origin}
}
func (*controllerDrainBrowser) Logout(context.Context) error { return nil }
func (b *controllerDrainBrowser) Run(ctx context.Context, cmd webbeta.Command, emit func(webbeta.Event) error) error {
	if cmd.Operation == "opening" {
		if err := emit(webbeta.Event{ID: cmd.ID, Kind: "text", Text: "synthetic opening"}); err != nil {
			return err
		}
		url := cmd.URL
		if url == "" {
			url = webbeta.Origin + "/a/chat/s/" + cmd.ID
		}
		return emit(webbeta.Event{ID: cmd.ID, Kind: "done", URL: url})
	}
	if err := emit(webbeta.Event{ID: cmd.ID, Kind: "reasoning", Text: "synthetic progress"}); err != nil {
		return err
	}
	close(b.started)
	<-ctx.Done()
	close(b.stopping)
	<-b.release
	return webbeta.ConfirmStopped(context.Canceled, webbeta.StopConfirmation{RequestID: cmd.ID, URL: cmd.URL, Document: "synthetic", HTTPDone: true, UIIdle: true, TaskExited: true, Revoked: true, OutputDrained: true})
}

type drainImmediateRunner struct{}

func (drainImmediateRunner) Run(context.Context, string) error { return nil }

func TestControllerDoesNotBecomeIdleWhileWebProducerDrains(t *testing.T) {
	for _, mode := range []string{"turn", "compact", "risk", "plan"} {
		t.Run(mode, func(t *testing.T) { testControllerWebProducerDrain(t, mode) })
	}
}

func testControllerWebProducerDrain(t *testing.T, mode string) {
	b := &controllerDrainBrowser{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	e, err := webbeta.NewEngine(filepath.Join(t.TempDir(), "state.json"), b)
	if err != nil {
		t.Fatal(err)
	}
	old := webbeta.CurrentEngine()
	webbeta.SetEngine(e)
	var release sync.Once
	var c *Controller
	defer func() {
		release.Do(func() { close(b.release) })
		if c != nil {
			c.Cancel()
			c.Close()
		}
		e.Close()
		webbeta.SetEngine(old)
	}()
	s := webbeta.DefaultSettings()
	s.Enabled = true
	s.ReservePaused = true
	s.RPM = 10
	if err = e.Configure(s); err != nil {
		t.Fatal(err)
	}
	p, err := provider.New("deepseekweb-beta", provider.Config{Name: "synthetic-drain"})
	if err != nil {
		t.Fatal(err)
	}
	a := agent.New(p, tool.NewRegistry(), agent.NewSession("synthetic"), agent.Options{MaxSteps: 1}, event.Discard)
	c = New(Options{Runner: a, Executor: a, ProviderKind: "deepseekweb-beta", WorkspaceRoot: t.TempDir()})
	var compactDone chan error
	if mode == "compact" {
		for i := 0; i < 8; i++ {
			a.Session().Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("synthetic history ", 2000)})
			a.Session().Add(provider.Message{Role: provider.RoleAssistant, Content: "synthetic answer"})
		}
		compactDone = make(chan error, 1)
		go func() { compactDone <- c.Compact(context.Background(), "") }()
	} else if mode == "turn" {
		c.Send("Synthetic task")
	} else {
		c.runGuarded(func(ctx context.Context) error {
			id, _ := agent.ParentTurn(ctx)
			if mode == "risk" {
				_, err := NewProviderRiskClassifier(p).AssessWithParentTurn(ctx, permission.RiskInput{Tool: "synthetic"}, id)
				return err
			}
			_, _, err := NewProviderAutoPlanClassifier(p).NeedsPlanWithParentTurn(ctx, "synthetic", 1, id)
			return err
		})
	}
	select {
	case <-b.started:
	case <-time.After(15 * time.Second):
		t.Fatal("synthetic request did not start")
	}
	id := c.TurnStatus().TurnID
	c.CancelTurn(id)
	select {
	case <-b.stopping:
	case <-time.After(time.Second):
		t.Fatal("producer did not observe cancel")
	}
	// The source's bounded release returns at eight seconds, but its retained
	// operation is still alive. The Controller must not claim idle at that point.
	time.Sleep(8500 * time.Millisecond)
	status := c.TurnStatus()
	if !c.Running() || !status.Running || !status.CancelRequested || status.TurnID != id {
		t.Fatalf("premature idle: %+v", status)
	}
	c.Send("must not start a successor")
	if c.TurnStatus().TurnID != id {
		t.Fatal("successor admitted during drain")
	}
	other := New(Options{Runner: drainImmediateRunner{}, WorkspaceRoot: t.TempDir()})
	done := make(chan error, 1)
	go func() { done <- other.RunTurn(context.Background(), "unrelated API task") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("unrelated task waited on global drain")
	}
	other.Close()
	release.Do(func() { close(b.release) })
	deadline := time.Now().Add(2 * time.Second)
	for c.Running() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if mode == "compact" {
		select {
		case err := <-compactDone:
			if err == nil || a.Session().RewriteVersion() != 0 {
				t.Fatalf("cancelled compaction changed history or returned success: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("compaction did not finalize after producer drain")
		}
	} else if c.TurnStatus().Outcome != event.TurnOutcomeCancelled {
		t.Fatalf("drain outcome: %+v", c.TurnStatus())
	}
	if c.Running() {
		t.Fatalf("drain did not finalize: %+v", c.TurnStatus())
	}
}
