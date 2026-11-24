//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	agenttest "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent/testutil"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/visioncap"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

type webBetaBlockingTool struct{ started, cancelled, release chan struct{} }

func (*webBetaBlockingTool) Name() string            { return "synthetic_wait" }
func (*webBetaBlockingTool) Description() string     { return "Synthetic non-cooperative tool" }
func (*webBetaBlockingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (*webBetaBlockingTool) ReadOnly() bool          { return true }
func (t *webBetaBlockingTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	close(t.started)
	<-ctx.Done()
	close(t.cancelled)
	<-t.release
	return "finished", nil
}

type webBetaTestRunner struct {
	started chan context.Context
	release chan struct{}
}

func (r *webBetaTestRunner) Run(ctx context.Context, _ string) error {
	r.started <- ctx
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.release:
		return nil
	}
}

func webBetaTestApp(ctrl *control.Controller) *App {
	return &App{activeTabID: "web", tabs: map[string]*WorkspaceTab{
		"web": {ID: "web", Ctrl: ctrl, model: "deepseek-web-beta/synthetic", Ready: true},
	}}
}

func awaitWebBetaSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("synthetic execution did not reach boundary")
	}
}

func TestWebBetaLogoutWaitsForActualAgentToolAndPreservesOtherControllers(t *testing.T) {
	testWebBetaIdentityDrain(t, false)
}

func TestWebBetaAccountSwitchWaitsForActualAgentToolAndPreservesOtherControllers(t *testing.T) {
	testWebBetaIdentityDrain(t, true)
}

func testWebBetaIdentityDrain(t *testing.T, switchLogin bool) {
	for _, mode := range []string{"current", "detached", "detached-rebuilt"} {
		t.Run(mode, func(t *testing.T) {
			detached := mode != "current"
			isolatedWebRuntime(t)
			configureWebBetaAdmissionFixture(t, "deepseek-web-beta", "deepseekweb-beta")
			b := &lifecycleWebBrowser{}
			newWebBetaBrowser = func(string, string) (webBetaBrowser, error) { return b, nil }
			capability := &webBetaBlockingTool{make(chan struct{}), make(chan struct{}), make(chan struct{})}
			reg := tool.NewRegistry()
			reg.Add(capability)
			prov := agenttest.NewMock("synthetic", agenttest.Turn{ToolCalls: []provider.ToolCall{{ID: "wait", Name: capability.Name(), Arguments: `{}`}}})
			ag := agent.New(prov, reg, agent.NewSession("synthetic"), agent.Options{}, event.Discard)
			ctrl := control.New(control.Options{Runner: ag, Executor: ag, Sink: event.Discard, ModelRef: "deepseek-web-beta/synthetic", ProviderKind: "deepseekweb-beta"})
			app := webBetaTestApp(ctrl)
			transition := app.webBetaLogout
			if switchLogin {
				transition = app.webBetaUseCurrentLogin
			}
			before, err := app.WebBetaOpen()
			if err != nil {
				t.Fatal(err)
			}
			e := webbeta.CurrentEngine()
			s := before.Settings
			s.Enabled = true
			if err := e.Configure(s); err != nil {
				t.Fatal(err)
			}
			p, err := webBetaProfile()
			if err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(p.BrowserPath(), "synthetic-profile")
			if err := os.WriteFile(sentinel, []byte("keep until actual tool stops"), 0600); err != nil {
				t.Fatal(err)
			}
			var release sync.Once
			t.Cleanup(func() {
				release.Do(func() { close(capability.release) })
				ctrl.Cancel()
				_ = ctrl.WaitIdle(context.Background())
			})
			tab := app.tabs["web"]
			if mode == "detached-rebuilt" {
				tab.Ctrl = control.New(control.Options{Sink: event.Discard})
				tab.runtimeReconfiguring = true
			}
			if err := app.SubmitToTab("web", "synthetic tool"); err != nil {
				t.Fatal(err)
			}
			if mode == "detached-rebuilt" {
				tab.Ctrl = ctrl
				tab.runtimeReconfiguring = false
				ctrl.Send("synthetic queued tool")
			}
			awaitWebBetaSignal(t, capability.started)
			tab.runtimeReconfiguring = true
			tab.pendingSubmitCount = 1
			tab.pendingRuntimeSubmits = []pendingRuntimeSubmit{{input: "must never replay"}}
			if detached {
				delete(app.tabs, "web")
			} else {
				tab.model = "api/new-selection"
			}
			apiRunner := &webBetaTestRunner{make(chan context.Context, 1), make(chan struct{})}
			api := control.New(control.Options{Runner: apiRunner, Sink: event.Discard})
			app.tabs["api"] = &WorkspaceTab{ID: "api", Ctrl: api, model: "api/model"}
			api.Send("unrelated")
			apiContext := <-apiRunner.started
			t.Cleanup(func() { close(apiRunner.release); _ = api.WaitIdle(context.Background()) })
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			if err := transition(ctx); err == nil {
				t.Fatal("logout completed while actual tool was executing")
			}
			awaitWebBetaSignal(t, capability.cancelled)
			if !ctrl.Running() || !ctrl.Retired() || b.released || e.Status().Generation != before.Generation {
				t.Fatal("cancellation was mistaken for drain or reset began too early")
			}
			if apiContext.Err() != nil || api.Retired() {
				t.Fatal("unrelated API controller cancelled")
			}
			if !detached && (tab.pendingSubmitCount != 0 || len(tab.pendingRuntimeSubmits) != 0) {
				t.Fatal("queued submission retained")
			}
			if _, err := os.Stat(sentinel); err != nil {
				t.Fatal("profile removed before actual tool completion", err)
			}
			if status, err := app.WebBetaOpen(); err == nil && (!switchLogin || status.Settings.Enabled || status.Blocked != webbeta.IdentitySwitchPending) {
				t.Fatal("reopen bypassed failed drain")
			}
			if err := app.WebBetaRecover(); err == nil {
				t.Fatal("recover bypassed failed drain")
			}
			if err := app.WebBetaConfigure(s); err == nil {
				t.Fatal("configuration bypassed failed drain")
			}
			if webbeta.CurrentEngine() != nil {
				t.Fatal("failed drain exposed engine")
			}
			release.Do(func() { close(capability.release) })
			if err := ctrl.WaitIdle(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := transition(context.Background()); err != nil {
				t.Fatal(err)
			}
			after, err := app.WebBetaOpen()
			if err != nil {
				t.Fatal(err)
			}
			if before.Generation == after.Generation || after.Settings.Enabled != switchLogin || b.sends != 0 {
				t.Fatal("unsafe new generation")
			}
			if _, err := os.Stat(sentinel); switchLogin && err != nil || !switchLogin && !os.IsNotExist(err) {
				t.Fatal("profile retention disagrees with requested transition", err)
			}
			if err := ctrl.RunTurn(context.Background(), "stale task"); !errors.Is(err, control.ErrControllerRetired) {
				t.Fatal("retired task reused", err)
			}
		})
	}
}

func TestWebBetaSubmitGuardsPrecedeRuntimeQueueAndLeaveAPITabsAlone(t *testing.T) {
	for _, display := range []bool{false, true} {
		t.Run(map[bool]string{false: "submit", true: "display"}[display], func(t *testing.T) {
			isolatedWebRuntime(t)
			configureWebBetaAdmissionFixture(t, "deepseek-web-beta", "deepseekweb-beta")
			ctrl := control.New(control.Options{Sink: event.Discard})
			app := webBetaTestApp(ctrl)
			tab := app.tabs["web"]
			tab.runtimeReconfiguring = true
			submit := func() error {
				if display {
					return app.SubmitDisplayToTab("web", "display", "synthetic")
				}
				return app.SubmitToTab("web", "synthetic")
			}
			t.Setenv("ORCA_WEB_BETA_READY", "1")
			if err := submit(); err == nil || tab.pendingSubmitCount != 0 || ctrl.Running() {
				t.Fatal("unopened web turn admitted")
			}
			b := &lifecycleWebBrowser{}
			newWebBetaBrowser = func(string, string) (webBetaBrowser, error) { return b, nil }
			if _, err := app.WebBetaOpen(); err != nil {
				t.Fatal(err)
			}
			if err := submit(); err == nil || tab.pendingSubmitCount != 0 {
				t.Fatal("disabled web turn admitted")
			}
			s := webbeta.CurrentEngine().Status().Settings
			s.Enabled = true
			if err := webbeta.CurrentEngine().Configure(s); err != nil {
				t.Fatal(err)
			}
			b.loading = true
			if err := submit(); err != nil || tab.pendingSubmitCount != 1 {
				t.Fatal("cold start incorrectly gated on visibility/readiness", err)
			}
			webBetaAdmission.blocked = true
			if err := submit(); err == nil || tab.pendingSubmitCount != 1 {
				t.Fatal("logout boundary admitted work")
			}
			tab.model = "api/model"
			if err := submit(); err != nil || tab.pendingSubmitCount != 2 {
				t.Fatal("web gate blocked unrelated API tab", err)
			}
		})
	}
}

func TestWebBetaModelRegistrationAfterInitializationIsIdempotent(t *testing.T) {
	isolatedWebRuntime(t)
	initial := config.Default()
	initial.Desktop.ProviderAccess = []string{"deepseek-web-beta"}
	initial.Desktop.VisionMode = config.VisionModeOff
	if err := initial.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	if err := ensureWebBetaModelEntry(); err != nil {
		t.Fatal(err)
	}
	path := config.UserConfigPath()
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureWebBetaModelEntry(); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil || string(first) != string(second) {
		t.Fatal("startup registration churned config", err)
	}
	cfg := config.LoadForEdit(path)
	entry, ok := cfg.Provider("deepseek-web-beta")
	if !ok || entry.Kind != "deepseekweb-beta" || !entry.Configured() {
		t.Fatal("web model not discoverable")
	}
	if len(cfg.Desktop.ProviderAccess) != 1 || cfg.Desktop.ProviderAccess[0] != "deepseek-web-beta" || cfg.DesktopVisionMode() != config.VisionModeOff {
		t.Fatal("startup registration changed explicit preferences")
	}
	if webbeta.CurrentEngine() != nil || betaRuntime.browser != nil {
		t.Fatal("model registration started browser")
	}
}

func TestWebBetaConfigureDeclaresMetadataWithoutOverridingDisabledVision(t *testing.T) {
	isolatedWebRuntime(t)
	newWebBetaBrowser = func(string, string) (webBetaBrowser, error) { return &lifecycleWebBrowser{}, nil }
	app := &App{}
	status, err := app.WebBetaOpen()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Desktop.VisionMode = config.VisionModeOff
	cfg.Desktop.ProviderAccess = []string{"deepseek-web-beta"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	entry := webBetaProviderEntry()
	store := visioncap.Load(webBetaVisionPath())
	if err := store.Put(visioncap.Capability{Key: visioncap.Key(&entry), ModelRef: visioncap.ModelRef(&entry), Override: visioncap.Unsupported,
		Status: visioncap.Supported, Source: visioncap.SourceMetadata, Reason: "synthetic PNG recognition verified", CheckedAt: 123}); err != nil {
		t.Fatal(err)
	}
	s := status.Settings
	s.Enabled = true
	if err := app.WebBetaConfigure(s); err != nil {
		t.Fatal(err)
	}
	store = visioncap.Load(webBetaVisionPath())
	metadata, effective := store.Stored(&entry), store.Get(&entry)
	if metadata.Source != visioncap.SourceMetadata || metadata.CheckedAt != 0 || metadata.ProbeVersion != 0 ||
		!strings.Contains(metadata.Reason, "Adapter-declared") || !strings.Contains(metadata.Reason, "not live-verified") || strings.Contains(metadata.Reason, "recognition verified") {
		t.Fatalf("metadata claimed live verification: %+v", metadata)
	}
	if effective.Status != visioncap.Unsupported || effective.Override != visioncap.Unsupported || effective.Source != visioncap.SourceManual {
		t.Fatal("explicit capability disable overwritten")
	}
	cfg = config.LoadForEdit(config.UserConfigPath())
	if cfg.DesktopVisionMode() != config.VisionModeOff || len(cfg.Desktop.ProviderAccess) != 1 || cfg.Desktop.ProviderAccess[0] != "deepseek-web-beta" {
		t.Fatal("enable web sending changed explicit preferences")
	}
}

func TestWebBetaConfigureSerializesSharedCatalogReadModifyWrite(t *testing.T) {
	isolatedWebRuntime(t)
	newWebBetaBrowser = func(string, string) (webBetaBrowser, error) { return &lifecycleWebBrowser{}, nil }
	app := &App{}
	status, err := app.WebBetaOpen()
	if err != nil {
		t.Fatal(err)
	}
	s := status.Settings
	s.Enabled = true
	app.configWriteMu.Lock()
	locked := true
	finished := make(chan struct{})
	t.Cleanup(func() {
		if locked {
			app.configWriteMu.Unlock()
		}
		<-finished
	})
	started, done := make(chan struct{}), make(chan error, 1)
	go func() { defer close(finished); close(started); done <- app.WebBetaConfigure(s) }()
	<-started
	select {
	case err := <-done:
		t.Fatalf("web configure ignored shared config writer lock: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.UpsertProvider(config.ProviderEntry{Name: "synthetic-concurrent-catalog", Kind: "openai", Model: "model", BaseURL: "https://synthetic.invalid/v1", APIKeyEnv: "ORCA_SYNTHETIC_KEY"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	app.configWriteMu.Unlock()
	locked = false
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("web config write did not finish")
	}
	cfg = config.LoadForEdit(config.UserConfigPath())
	for _, name := range []string{"synthetic-concurrent-catalog", "deepseek-web-beta"} {
		if _, ok := cfg.Provider(name); !ok {
			t.Fatal("concurrent catalog entry lost", name)
		}
	}
}

func TestWebBetaLogoutCannotOvertakeQueuedSubmissionAdmission(t *testing.T) {
	isolatedWebRuntime(t)
	newWebBetaBrowser = func(string, string) (webBetaBrowser, error) { return &lifecycleWebBrowser{}, nil }
	ctrl := control.New(control.Options{Sink: event.Discard, ModelRef: "deepseek-web-beta/synthetic", ProviderKind: "deepseekweb-beta"})
	app := webBetaTestApp(ctrl)
	before, err := app.WebBetaOpen()
	if err != nil {
		t.Fatal(err)
	}
	tab := app.tabs["web"]
	tab.submitMu.Lock()
	locked := true
	t.Cleanup(func() {
		if locked {
			tab.submitMu.Unlock()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := app.webBetaLogout(ctx); err == nil {
		t.Fatal("reset overtook an uncompleted queue admission")
	}
	if !ctrl.Retired() || betaRuntime.engine.Status().Generation != before.Generation {
		t.Fatal("failed queue drain did not retain old identity")
	}
	tab.submitMu.Unlock()
	locked = false
	if err := app.WebBetaLogout(); err != nil {
		t.Fatal(err)
	}
}
