//go:build windows

package main

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/webbetahost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/visioncap"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

var betaRuntime struct {
	sync.Mutex
	browser   webBetaBrowser
	engine    *webbeta.Engine
	closed    bool
	resetting bool
	initError string
}

var webBetaLogoutMu sync.Mutex

const webBetaDrainTimeout = 10 * time.Second

type webBetaBrowser interface {
	webbeta.Browser
	SetBounds(webbetahost.Bounds) error
	Recheck(context.Context) error
	CloseAndWait(context.Context) error
	GuardIdentity(bool)
}

var newWebBetaBrowser = func(profile, windowClass string) (webBetaBrowser, error) {
	b, err := webbetahost.NewWithWindowClass(profile, windowClass)
	if b == nil {
		return nil, err
	}
	return b, err
}

func startWebBetaBackground(a *App) {
	if !betaEnabled() {
		return
	}
	// Restoring login/consent does not run openings, refill or replay requests.
	complete := webbeta.BeginEngineStartup()
	go func() {
		_, err := a.WebBetaOpen()
		complete(err)
	}()
}

func (a *App) openWebBetaBackground() error { _, err := a.WebBetaOpen(); return err }

func webBetaProfile() (*webbetahost.Profile, error) {
	if !betaEnabled() {
		return nil, errors.New("Web Beta is disabled in this build")
	}
	root, err := betaDataRoot()
	if err != nil {
		return nil, err
	}
	return webbetahost.OpenProfile(root)
}

func releaseWebBetaBrowser() error {
	if betaRuntime.browser == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := betaRuntime.browser.CloseAndWait(ctx); err != nil {
		return err
	}
	betaRuntime.browser = nil
	return nil
}

func openWebBetaLocked() (webbeta.Status, error) {
	if betaRuntime.resetting {
		return webbeta.Status{}, webbeta.ErrIdentityClosed
	}
	if betaRuntime.closed {
		return webbeta.Status{}, errors.New("Web Beta is shutting down")
	}
	if betaRuntime.engine != nil {
		return betaRuntime.engine.Status(), nil
	}
	if err := releaseWebBetaBrowser(); err != nil {
		return webbeta.Status{}, err
	}
	p, err := webBetaProfile()
	if err != nil {
		return webbeta.Status{}, err
	}
	pending, err := webbeta.IdentityResetPending(p.StatePath())
	if err != nil {
		return webbeta.Status{}, err
	}
	if pending {
		if err := p.Clear(); err != nil {
			return webbeta.Status{}, err
		}
		if err := webbeta.CompleteIdentityReset(p.StatePath()); err != nil {
			return webbeta.Status{}, err
		}
	}
	if err := p.Ensure(); err != nil {
		return webbeta.Status{}, err
	}
	b, err := newWebBetaBrowser(p.BrowserPath(), browserWindowClass())
	// Retain failed initializers until their thread/process releases.
	betaRuntime.browser = b
	if err != nil {
		return webbeta.Status{}, err
	}
	e, err := webbeta.NewEngineWithRequestLimit(p.StatePath(), b, 0)
	if err != nil {
		_ = releaseWebBetaBrowser()
		return webbeta.Status{}, err
	}
	betaRuntime.engine = e
	s := e.Status()
	b.GuardIdentity(s.Settings.Enabled || len(s.Conversations) != 0)
	webbeta.SetEngine(e)
	return e.Status(), nil
}

func (a *App) WebBetaOpen() (webbeta.Status, error) {
	if !betaEnabled() {
		return webbeta.Status{}, errors.New("Web Beta is disabled in this build")
	}
	betaRuntime.Lock()
	defer betaRuntime.Unlock()
	s, err := openWebBetaLocked()
	if err != nil {
		betaRuntime.initError = err.Error()
	} else {
		betaRuntime.initError = ""
	}
	return s, err
}

func (a *App) WebBetaStatus() webbeta.Status {
	betaRuntime.Lock()
	defer betaRuntime.Unlock()
	if !betaEnabled() || betaRuntime.engine == nil {
		return webbeta.Status{Settings: webbeta.DefaultSettings(), Error: betaRuntime.initError}
	}
	return betaRuntime.engine.Status()
}

func (a *App) WebBetaConfigure(s webbeta.Settings) error {
	if !betaEnabled() {
		return errors.New("Web Beta is disabled in this build")
	}
	if err := s.Validate(); err != nil {
		return err
	}
	betaRuntime.Lock()
	defer betaRuntime.Unlock()
	if betaRuntime.resetting {
		return webbeta.ErrIdentityClosed
	}
	if betaRuntime.engine == nil {
		return errors.New("open Web Beta first")
	}
	if s.Enabled && !betaRuntime.browser.State().Ready {
		return errors.New("网页适配尚未核验，暂不能启用自动发送")
	}
	if s.Enabled {
		if err := a.persistWebBetaProvider(); err != nil {
			return err
		}
		entry := webBetaProviderEntry()
		store := visioncap.Load(webBetaVisionPath())
		capability := store.Stored(&entry)
		capability.ModelRef, capability.Key = visioncap.ModelRef(&entry), visioncap.Key(&entry)
		capability.Status, capability.Source = visioncap.Supported, visioncap.SourceMetadata
		capability.AutomaticStatus = visioncap.Supported
		capability.Reason = "Adapter-declared image upload support; not live-verified in this build"
		capability.CheckedAt, capability.Attempts, capability.ProbeVersion = 0, 0, 0
		// Stored retains the user's explicit override, including Unsupported.
		if err := store.Put(capability); err != nil {
			return err
		}
	}
	if err := betaRuntime.engine.Configure(s); err != nil {
		return err
	}
	if a.ctx != nil {
		a.emitReady(a.ctx)
	}
	return nil
}

func webBetaProviderEntry() config.ProviderEntry {
	return config.ProviderEntry{Name: "deepseek-web-beta", Kind: "deepseekweb-beta", BaseURL: webbeta.Origin, Model: "DeepSeek官方（网页 Beta）", ReasoningProtocol: "none"}
}

func registerWebBetaProvider(cfg *config.Config) error {
	return cfg.UpsertProvider(webBetaProviderEntry())
}

func webBetaVisionPath() string {
	return filepath.Join(filepath.Dir(config.UserConfigPath()), "vision-capabilities.json")
}

func (a *App) persistWebBetaProvider() error {
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()
	path := config.UserConfigPath()
	if path == "" {
		return errors.New("beta configuration directory is unavailable")
	}
	cfg := config.LoadForEdit(path)
	if err := registerWebBetaProvider(cfg); err != nil {
		return err
	}
	return cfg.SaveTo(path)
}

func ensureWebBetaModelEntry() error {
	if !betaEnabled() {
		return nil
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	entry, exists := cfg.Provider("deepseek-web-beta")
	if exists && entry.Kind == "deepseekweb-beta" {
		return nil
	}
	if err := registerWebBetaProvider(cfg); err != nil {
		return err
	}
	return cfg.SaveTo(config.UserConfigPath())
}

func (a *App) WebBetaRecover() error {
	if !betaEnabled() {
		return errors.New("Web Beta is disabled in this build")
	}
	betaRuntime.Lock()
	defer betaRuntime.Unlock()
	if betaRuntime.resetting {
		return webbeta.ErrIdentityClosed
	}
	if betaRuntime.engine == nil {
		return errors.New("请先打开网页")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := betaRuntime.browser.Recheck(ctx); err != nil {
		return err
	}
	return betaRuntime.engine.ConfirmRecovery()
}

func (a *App) WebBetaBounds(bounds webbetahost.Bounds) error {
	if !betaEnabled() {
		return errors.New("Web Beta is disabled in this build")
	}
	betaRuntime.Lock()
	defer betaRuntime.Unlock()
	if betaRuntime.browser == nil {
		return nil
	}
	return betaRuntime.browser.SetBounds(bounds)
}

func (a *App) WebBetaLogout() (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), webBetaDrainTimeout)
	defer cancel()
	return a.webBetaLogout(ctx)
}

func (a *App) webBetaLogout(ctx context.Context) (err error) {
	if !betaEnabled() {
		return errors.New("Web Beta is disabled in this build")
	}
	webBetaLogoutMu.Lock()
	defer webBetaLogoutMu.Unlock()
	defer func() {
		betaRuntime.Lock()
		defer betaRuntime.Unlock()
		if err != nil {
			betaRuntime.initError = err.Error()
		} else {
			betaRuntime.initError = ""
		}
	}()
	webBetaAdmission.Lock()
	webBetaAdmission.blocked = true
	controllers, retireErr := a.retireWebBetaControllers(ctx)
	betaRuntime.Lock()
	betaRuntime.resetting = true
	e := betaRuntime.engine
	webbeta.SetEngine(nil)
	betaRuntime.Unlock()
	webBetaAdmission.Unlock()
	var stopErr error
	if e != nil {
		stopErr = e.StopForIdentityChange()
	}
	if retireErr != nil {
		return errors.New("Web Beta logout blocked: queued submissions have not drained: " + retireErr.Error())
	}
	// Retire cancels all controllers first. Wait for real execution, not the
	// cancellation acknowledgement; an uncooperative tool keeps its profile.
	for _, ctrl := range controllers {
		if err := ctrl.WaitIdle(ctx); err != nil {
			return errors.New("Web Beta logout blocked: agent tools have not stopped; retry logout after they finish: " + err.Error())
		}
	}
	if e != nil {
		if err := e.CloseContext(ctx); err != nil {
			return errors.New("Web Beta logout blocked: web requests have not drained: " + err.Error())
		}
	}
	if stopErr != nil {
		return stopErr
	}
	p, err := webBetaProfile()
	if err != nil {
		return err
	}
	betaRuntime.Lock()
	defer betaRuntime.Unlock()
	if e != nil {
		// Persist reset before profile removal; cancel, drain and revoke snapshots.
		if err := e.BeginIdentityReset(); err != nil {
			return err
		}
		betaRuntime.engine = nil
	} else if err := webbeta.BeginStoredIdentityReset(p.StatePath()); err != nil {
		return err
	}
	if err := releaseWebBetaBrowser(); err != nil {
		return err
	}
	if err := p.Clear(); err != nil {
		return err
	}
	if err := webbeta.CompleteIdentityReset(p.StatePath()); err != nil {
		return err
	}
	betaRuntime.resetting = false
	// No betaRuntime lock is acquired by blocked admission, so reopening this
	// gate cannot admit a submission before the reset is complete.
	webBetaAdmission.Lock()
	webBetaAdmission.blocked = false
	webBetaAdmission.controllers = nil
	webBetaAdmission.tabs = nil
	webBetaAdmission.Unlock()
	return nil
}

func closeWebBeta() {
	betaRuntime.Lock()
	defer betaRuntime.Unlock()
	betaRuntime.closed = true
	webbeta.SetEngine(nil)
	if betaRuntime.engine != nil {
		betaRuntime.engine.Close()
		betaRuntime.engine = nil
	}
	if err := releaseWebBetaBrowser(); err != nil {
		betaRuntime.initError = err.Error()
	}
}
