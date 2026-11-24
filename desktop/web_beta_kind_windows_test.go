//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

func configureWebBetaAdmissionFixture(t *testing.T, name, kind string) {
	t.Helper()
	cfg := config.Default()
	cfg.Providers = []config.ProviderEntry{{Name: name, Kind: kind, Model: "synthetic", BaseURL: webbeta.Origin}}
	cfg.DefaultModel = name + "/synthetic"
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
}

func TestWebBetaAdmissionResolvesKindNotName(t *testing.T) {
	for _, tc := range []struct {
		name, kind, ref string
		web             bool
	}{
		{"alias", "deepseekweb-beta", "alias/synthetic", true},
		{"alias", "deepseekweb-beta", "alias", true},
		{"alias", "deepseekweb-beta", "synthetic", true},
		{"alias", "deepseekweb-beta", "", true},
		{"deepseek-web-beta", "openai", "deepseek-web-beta/synthetic", false},
		{"ordinary", "openai", "deepseek-web-beta/missing", false},
		{"ordinary", "openai", "unknown/synthetic", false},
		{"unknown", "unregistered-kind", "unknown/synthetic", false},
	} {
		t.Run(tc.name+"/"+tc.kind+"/"+tc.ref, func(t *testing.T) {
			isolatedWebRuntime(t)
			configureWebBetaAdmissionFixture(t, tc.name, tc.kind)
			app := webBetaTestApp(nil)
			app.tabs["web"].model = tc.ref
			webBetaAdmission.blocked = true
			release, err := app.beginWebBetaAdmission("web")
			if release != nil {
				release()
			}
			if tc.web && !errors.Is(err, webbeta.ErrIdentityClosed) {
				t.Fatalf("web Kind bypassed identity gate: %v", err)
			}
			if !tc.web && err != nil {
				t.Fatalf("non-web or unknown source treated as web: %v", err)
			}
			if betaRuntime.browser != nil || webbeta.CurrentEngine() != nil {
				t.Fatal("kind resolution started a browser")
			}
		})
	}
}

func TestWebBetaBoundKindSurvivesRemovedConfigAndDetachedTab(t *testing.T) {
	for _, mode := range []string{"untracked", "current", "detached", "replaced-api", "new-api-queue"} {
		t.Run(mode, func(t *testing.T) {
			isolatedWebRuntime(t)
			configureWebBetaAdmissionFixture(t, "alias", "deepseekweb-beta")
			ctrl := control.New(control.Options{ModelRef: "alias/synthetic", ProviderKind: "deepseekweb-beta", Sink: event.Discard})
			app := webBetaTestApp(ctrl)
			tab := app.tabs["web"]
			tab.model, tab.runtimeReconfiguring = "alias/synthetic", true
			if mode != "untracked" {
				newWebBetaBrowser = func(string, string) (webBetaBrowser, error) { return &lifecycleWebBrowser{}, nil }
				status, err := app.WebBetaOpen()
				if err != nil {
					t.Fatal(err)
				}
				status.Settings.Enabled = true
				if err := webbeta.CurrentEngine().Configure(status.Settings); err != nil {
					t.Fatal(err)
				}
				if err := app.SubmitToTab("web", "synthetic old-identity draft"); err != nil || tab.pendingSubmitCount != 1 {
					t.Fatal("queue admission failed", err)
				}
			}
			if err := os.Remove(config.UserConfigPath()); err != nil {
				t.Fatal(err)
			}
			tab.model = "unknown/removed-selection"
			webBetaAdmission.blocked = true
			if release, err := app.beginWebBetaAdmission("web"); !errors.Is(err, webbeta.ErrIdentityClosed) {
				if release != nil {
					release()
				}
				t.Fatal("bound web identity forgotten after config removal", err)
			}
			var replacement *control.Controller
			if mode == "detached" {
				delete(app.tabs, "web")
			} else if mode == "replaced-api" || mode == "new-api-queue" {
				replacement = control.New(control.Options{ModelRef: "api/synthetic", ProviderKind: "openai", Sink: event.Discard})
				tab.Ctrl, tab.model = replacement, "api/synthetic"
				if mode == "new-api-queue" {
					tab.pendingSubmitEpoch++
					tab.pendingRuntimeSubmits = []pendingRuntimeSubmit{{input: "new API batch", epoch: tab.pendingSubmitEpoch}}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			webBetaAdmission.Lock()
			retired, err := app.retireWebBetaControllers(ctx)
			webBetaAdmission.Unlock()
			if err != nil || len(retired) != 1 || retired[0] != ctrl || !ctrl.Retired() {
				t.Fatalf("bound controller escaped retirement: retired=%v err=%v", retired, err)
			}
			wantPending := 0
			if mode == "new-api-queue" {
				wantPending = 1
			}
			if tab.pendingSubmitCount != wantPending || len(tab.pendingRuntimeSubmits) != wantPending {
				t.Fatal("old-identity draft retained")
			}
			if replacement != nil && replacement.Retired() {
				t.Fatal("replacement API controller was retired")
			}
		})
	}
}

func TestWebBetaRetirementRechecksQueueEpochAfterWaiting(t *testing.T) {
	isolatedWebRuntime(t)
	ctrl := control.New(control.Options{ModelRef: "alias/synthetic", ProviderKind: "deepseekweb-beta"})
	app := webBetaTestApp(ctrl)
	tab := app.tabs["web"]
	tab.pendingSubmitEpoch, tab.pendingSubmitCount = 1, 1
	tab.pendingRuntimeSubmits = []pendingRuntimeSubmit{{input: "old web draft", epoch: 1}}
	webBetaAdmission.tabs = map[*WorkspaceTab]uint64{tab: 1}
	tab.submitMu.Lock()
	locked := true
	t.Cleanup(func() {
		if locked {
			tab.submitMu.Unlock()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		webBetaAdmission.Lock()
		_, err := app.retireWebBetaControllers(ctx)
		webBetaAdmission.Unlock()
		done <- err
	}()
	for !ctrl.Retired() && ctx.Err() == nil {
		time.Sleep(time.Millisecond)
	}
	replacement := control.New(control.Options{ModelRef: "api/synthetic", ProviderKind: "openai"})
	app.mu.Lock()
	tab.Ctrl, tab.model = replacement, "api/synthetic"
	tab.pendingSubmitEpoch = 2
	tab.pendingRuntimeSubmits = []pendingRuntimeSubmit{{input: "new API batch", epoch: 2}}
	app.mu.Unlock()
	tab.submitMu.Unlock()
	locked = false
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !ctrl.Retired() || replacement.Retired() || tab.pendingSubmitEpoch != 2 || tab.pendingSubmitCount != 1 || len(tab.pendingRuntimeSubmits) != 1 {
		t.Fatal("late API queue or controller cancelled")
	}
}

func TestWebBetaRetirementPreservesUnknownAndBoundAPI(t *testing.T) {
	for _, kind := range []string{"", "openai", "synthetic-api-kind"} {
		t.Run("kind="+kind, func(t *testing.T) {
			isolatedWebRuntime(t)
			if kind != "" {
				// A config replacement must not rewrite an existing API controller's identity.
				configureWebBetaAdmissionFixture(t, "deepseek-web-beta", "deepseekweb-beta")
			}
			runner := &webBetaTestRunner{make(chan context.Context, 1), make(chan struct{})}
			ctrl := control.New(control.Options{Runner: runner, ModelRef: "deepseek-web-beta/synthetic", ProviderKind: kind, Sink: event.Discard})
			app := webBetaTestApp(ctrl)
			ctrl.Send("synthetic API work")
			active := <-runner.started
			t.Cleanup(func() { close(runner.release); _ = ctrl.WaitIdle(context.Background()) })
			webBetaAdmission.blocked = true
			release, err := app.beginWebBetaAdmission("web")
			if release != nil {
				release()
			}
			if err != nil {
				t.Fatal("name or changed config overrode bound API identity", err)
			}
			app.tabs["web"].pendingSubmitCount = 1
			app.tabs["web"].pendingRuntimeSubmits = []pendingRuntimeSubmit{{input: "unrelated API draft"}}
			webBetaAdmission.Lock()
			retired, err := app.retireWebBetaControllers(context.Background())
			webBetaAdmission.Unlock()
			if err != nil || len(retired) != 0 || ctrl.Retired() || active.Err() != nil || app.tabs["web"].pendingSubmitCount != 1 {
				t.Fatal("unrelated API or unknown controller/queue cancelled", err)
			}
		})
	}
}
