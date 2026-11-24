//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/webbetahost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

type lifecycleWebBrowser struct {
	releaseErr error
	released   bool
	sends      int
	loading    bool
}

func (b *lifecycleWebBrowser) State() webbeta.BrowserState {
	return webbeta.BrowserState{Ready: !b.loading}
}
func (b *lifecycleWebBrowser) Run(context.Context, webbeta.Command, func(webbeta.Event) error) error {
	b.sends++
	return errors.New("no live requests in fixture")
}
func (b *lifecycleWebBrowser) Logout(ctx context.Context) error { return b.CloseAndWait(ctx) }
func (b *lifecycleWebBrowser) CloseAndWait(context.Context) error {
	if b.releaseErr != nil {
		return b.releaseErr
	}
	b.released = true
	return nil
}
func (*lifecycleWebBrowser) Recheck(context.Context) error      { return nil }
func (*lifecycleWebBrowser) SetBounds(webbetahost.Bounds) error { return nil }
func (*lifecycleWebBrowser) GuardIdentity(bool)                 {}

func isolatedWebRuntime(t *testing.T) string {
	t.Helper()
	oldChannel, oldRoot := channel, initializedBetaRoot
	oldFactory, oldCurrent := newWebBetaBrowser, webbeta.CurrentEngine()
	oldBrowser, oldEngine, oldClosed, oldError := betaRuntime.browser, betaRuntime.engine, betaRuntime.closed, betaRuntime.initError
	oldResetting := betaRuntime.resetting
	oldBlocked, oldControllers := webBetaAdmission.blocked, webBetaAdmission.controllers
	oldTabs := webBetaAdmission.tabs
	oldIsolated := config.IsolatedProfileEnabled()
	oldProfile, _ := config.ProfileHomeDir()
	root := t.TempDir()
	if err := config.SetIsolatedProfileHome(root); err != nil {
		t.Fatal(err)
	}
	channel, initializedBetaRoot = betaChannel, root
	betaRuntime.browser, betaRuntime.engine, betaRuntime.closed, betaRuntime.initError = nil, nil, false, ""
	betaRuntime.resetting = false
	webBetaAdmission.blocked, webBetaAdmission.controllers = false, nil
	webBetaAdmission.tabs = nil
	webbeta.SetEngine(nil)
	t.Cleanup(func() {
		if betaRuntime.engine != nil {
			betaRuntime.engine.Close()
		}
		betaRuntime.browser, betaRuntime.engine, betaRuntime.closed, betaRuntime.initError = oldBrowser, oldEngine, oldClosed, oldError
		betaRuntime.resetting = oldResetting
		webBetaAdmission.blocked, webBetaAdmission.controllers = oldBlocked, oldControllers
		webBetaAdmission.tabs = oldTabs
		if !oldIsolated {
			oldProfile = ""
		}
		_ = config.SetIsolatedProfileHome(oldProfile)
		channel, initializedBetaRoot = oldChannel, oldRoot
		newWebBetaBrowser = oldFactory
		webbeta.SetEngine(oldCurrent)
	})
	return root
}

func TestWebBetaBackgroundAnnouncesInitializationWithoutSending(t *testing.T) {
	isolatedWebRuntime(t)
	entered, release := make(chan struct{}), make(chan struct{})
	b := &lifecycleWebBrowser{}
	newWebBetaBrowser = func(string, string) (webBetaBrowser, error) {
		close(entered)
		<-release
		return b, nil
	}
	startWebBetaBackground(&App{})
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := webbeta.AwaitEngine(ctx)
		result <- err
	}()
	select {
	case err := <-result:
		close(release)
		// Join initialization before the fixture restores process globals.
		betaRuntime.Lock()
		betaRuntime.Unlock()
		t.Fatalf("request did not wait for the announced startup: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	betaRuntime.Lock()
	defer betaRuntime.Unlock()
	if betaRuntime.engine == nil || betaRuntime.engine.Status().Settings.Enabled || b.sends != 0 {
		t.Fatal("startup implicitly enabled or sent a request")
	}
}

func TestWebBetaLogoutReleaseFailureKeepsProfileAndBlocksReopen(t *testing.T) {
	root := isolatedWebRuntime(t)
	p, err := webbetahost.OpenProfile(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(p.BrowserPath(), "synthetic-cookies")
	if err := os.WriteFile(sentinel, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	b := &lifecycleWebBrowser{releaseErr: errors.New("synthetic close failure")}
	e, err := webbeta.NewEngineWithRequestLimit(p.StatePath(), b, 0)
	if err != nil {
		t.Fatal(err)
	}
	betaRuntime.browser, betaRuntime.engine = b, e
	webbeta.SetEngine(e)
	app := &App{}
	created := 0
	newWebBetaBrowser = func(string, string) (webBetaBrowser, error) { created++; return &lifecycleWebBrowser{}, nil }
	if err := app.WebBetaLogout(); err == nil {
		t.Fatal("release failure hidden")
	}
	if webbeta.CurrentEngine() != nil {
		t.Fatal("old engine still exposed")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("profile deleted before release", err)
	}
	if _, err := app.WebBetaOpen(); err == nil || created != 0 {
		t.Fatal("new identity created before release")
	}
	b.releaseErr = nil
	if _, err := app.WebBetaOpen(); err == nil {
		t.Fatal("failed logout reopened without explicit retry")
	}
	if err := app.WebBetaLogout(); err != nil {
		t.Fatal(err)
	}
	s, err := app.WebBetaOpen()
	if err != nil {
		t.Fatal(err)
	}
	if !b.released || created != 1 || s.Settings.Enabled || !s.Settings.ReservePaused {
		t.Fatal("unsafe recovery after release")
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("profile not cleared after release", err)
	}
	if b.sends != 0 {
		t.Fatal("logout sent a request")
	}
}

func TestWebBetaLogoutCreatesNewGenerationWithoutSending(t *testing.T) {
	root := isolatedWebRuntime(t)
	newWebBetaBrowser = func(string, string) (webBetaBrowser, error) { return &lifecycleWebBrowser{}, nil }
	app := &App{}
	before, err := app.WebBetaOpen()
	if err != nil {
		t.Fatal(err)
	}
	old := webbeta.CurrentEngine()
	// Register a synthetic old owner without sending (consent remains disabled).
	_ = old.Send(context.Background(), webbeta.Request{Owner: "old-task", ID: "r", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}}, func(webbeta.Event) error { return nil })
	for _, name := range []string{"browser", "orca-account"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, "keep"), []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.WebBetaLogout(); err != nil {
		t.Fatal(err)
	}
	after, err := app.WebBetaOpen()
	if err != nil {
		t.Fatal(err)
	}
	if before.Generation == after.Generation || after.Settings.Enabled || len(after.Conversations) != 0 || after.Sent != 0 {
		t.Fatal("generation reused or automatically sent")
	}
	for _, name := range []string{"browser", "orca-account"} {
		if _, err := os.Stat(filepath.Join(root, name, "keep")); err != nil {
			t.Fatal("unrelated data cleared", err)
		}
	}
	err = webbeta.CurrentEngine().Send(context.Background(), webbeta.Request{Owner: "old-task", ID: "r2", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}}, func(webbeta.Event) error { return nil })
	if !errors.Is(err, webbeta.ErrIdentityChanged) {
		t.Fatal("old owner accepted", err)
	}
}

func TestWebBetaStatusDoesNotMutateReadinessEnvironment(t *testing.T) {
	isolatedWebRuntime(t)
	t.Setenv("ORCA_WEB_BETA_READY", "unrelated-runtime-value")
	_ = (&App{}).WebBetaStatus()
	if os.Getenv("ORCA_WEB_BETA_READY") != "unrelated-runtime-value" {
		t.Fatal("status mutated global readiness")
	}
	channel = "stable"
	if _, err := (&App{}).WebBetaOpen(); err == nil {
		t.Fatal("stable build opened Web Beta")
	}
	if webbeta.CurrentEngine() != nil {
		t.Fatal("feature gate created runtime")
	}
}

func TestWebBetaRegistrationPreservesAPIDefaults(t *testing.T) {
	cfg := config.Default()
	if err := registerWebBetaProvider(cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Desktop.ProviderAccess) != 0 {
		t.Fatal("web provider narrowed existing access")
	}
	if _, ok := cfg.ResolveModel("deepseek/deepseek-flash"); !ok {
		t.Fatal("API default excluded")
	}
	cfg.Desktop.ProviderAccess = []string{"existing"}
	if err := registerWebBetaProvider(cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Desktop.ProviderAccess) != 1 || cfg.Desktop.ProviderAccess[0] != "existing" {
		t.Fatal("existing access changed")
	}
	cfg.Desktop.ProviderAccess = []string{"deepseek-web-beta"}
	if err := registerWebBetaProvider(cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Desktop.ProviderAccess) != 1 || cfg.Desktop.ProviderAccess[0] != "deepseek-web-beta" {
		t.Fatal("explicit web-only allow-list was broadened")
	}
}
