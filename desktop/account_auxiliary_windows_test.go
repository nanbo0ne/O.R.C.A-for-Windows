//go:build windows

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

type auxiliaryWebBrowser struct {
	mu       sync.Mutex
	commands []webbeta.Command
}

func (*auxiliaryWebBrowser) State() webbeta.BrowserState {
	return webbeta.BrowserState{Ready: true, URL: webbeta.Origin}
}
func (*auxiliaryWebBrowser) Logout(context.Context) error { return nil }
func (b *auxiliaryWebBrowser) Run(_ context.Context, cmd webbeta.Command, emit func(webbeta.Event) error) error {
	b.mu.Lock()
	b.commands = append(b.commands, cmd)
	b.mu.Unlock()
	if err := emit(webbeta.Event{ID: cmd.ID, Kind: "text", Text: `ORCA_TURN_V3 {"type":"final","text":"synthetic web answer"}`}); err != nil {
		return err
	}
	return emit(webbeta.Event{ID: cmd.ID, Kind: "done", URL: cmd.URL})
}

func auxiliaryWebEngine(t *testing.T, b *auxiliaryWebBrowser, generation string) *webbeta.Engine {
	t.Helper()
	settings := webbeta.DefaultSettings()
	settings.Enabled, settings.ReservePaused, settings.RPM = true, true, 10
	state := webbeta.State{Version: 2, Generation: generation, Settings: settings,
		Conversations: []webbeta.Conversation{{ID: "synthetic-prepared", URL: webbeta.Origin + "/a/chat/s/synthetic-prepared", Phase: "ready"}}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(config.UserConfigPath()), generation+".json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	engine, err := webbeta.NewEngineWithRequestLimit(path, b, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.Close)
	webbeta.SetEngine(engine)
	return engine
}

func TestSideChatWebUsesStableConversationAndRejectsContextChanges(t *testing.T) {
	isolatedWebRuntime(t)
	app, cfg, paid := auxiliaryFixture(t)
	web := config.ProviderEntry{Name: "deepseek-web-beta", Kind: "deepseekweb-beta", Model: "synthetic-web"}
	cfg.Providers = append(cfg.Providers, web)
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	app.tabs["side"].model = web.Name + "/" + web.Model
	browser := &auxiliaryWebBrowser{}
	engine := auxiliaryWebEngine(t, browser, "synthetic-generation-one")
	if err := app.SendSideChat("side", "synthetic first"); err != nil {
		t.Fatal(err)
	}
	first := loadSideChatFile().Bindings["side"]
	if first.ConversationID == "" || first.Pending || first.Generation != "synthetic-generation-one" {
		t.Fatal("missing completed stable binding")
	}
	if err := app.SendSideChat("side", "synthetic second"); err != nil {
		t.Fatal(err)
	}
	second := loadSideChatFile().Bindings["side"]
	browser.mu.Lock()
	commands := append([]webbeta.Command(nil), browser.commands...)
	browser.mu.Unlock()
	status := engine.Status()
	if first.ConversationID != second.ConversationID || len(commands) != 2 || len(status.Conversations) != 1 ||
		status.Conversations[0].Owner != first.ConversationID || commands[0].URL != commands[1].URL ||
		strings.Contains(commands[1].Text, "synthetic first") || strings.Contains(commands[1].Text, "synthetic web answer") ||
		!strings.Contains(commands[1].Text, "synthetic second") || len(paid.requests) != 0 {
		t.Fatal("sidebar silently replaced a web conversation, replayed prior messages, or used a paid source")
	}
	ctrl := control.New(control.Options{Sink: event.Discard})
	t.Cleanup(ctrl.Close)
	app.tabs["side"].Ctrl = ctrl
	if err := app.SendSideChat("side", "changed main context"); err == nil {
		t.Fatal("main context changed without explicit sidebar reset")
	}
	app.tabs["side"].Ctrl = nil
	engine.Close()
	auxiliaryWebEngine(t, browser, "synthetic-generation-two")
	if err := app.SendSideChat("side", "changed identity"); err == nil {
		t.Fatal("old sidebar borrowed another web identity")
	}
	browser.mu.Lock()
	defer browser.mu.Unlock()
	if len(browser.commands) != 2 || len(paid.requests) != 0 {
		t.Fatal("rejected sidebar request dispatched or changed source")
	}
}

func TestSideChatWebNeverEnablesOrOpensSession(t *testing.T) {
	for _, mode := range []string{"absent", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			isolatedWebRuntime(t)
			app, cfg, paid := auxiliaryFixture(t)
			web := config.ProviderEntry{Name: "deepseek-web-beta", Kind: "deepseekweb-beta", Model: "synthetic-web"}
			cfg.Providers = append(cfg.Providers, web)
			if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
				t.Fatal(err)
			}
			app.tabs["side"].model = web.Name + "/" + web.Model
			opened := 0
			newWebBetaBrowser = func(string, string) (webBetaBrowser, error) {
				opened++
				return &lifecycleWebBrowser{}, nil
			}
			browser := &auxiliaryWebBrowser{}
			var engine *webbeta.Engine
			if mode == "disabled" {
				engine = auxiliaryWebEngine(t, browser, "synthetic-disabled")
				settings := engine.Status().Settings
				settings.Enabled = false
				if err := engine.Configure(settings); err != nil {
					t.Fatal(err)
				}
			}
			if err := app.SendSideChat("side", "synthetic"); err == nil {
				t.Fatal("unauthorized web request was admitted")
			}
			if opened != 0 || len(paid.requests) != 0 || len(browser.commands) != 0 || engine != nil && engine.Status().Settings.Enabled {
				t.Fatal("sidebar opened/authorized web or substituted an API source")
			}
		})
	}
}
