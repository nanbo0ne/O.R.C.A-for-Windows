//go:build windows

package main

import (
	"context"
	"errors"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

// WebBetaUseCurrentLogin is explicit account adoption, not logout or a retry of
// an old request. Native profile data is never cleared by this path.
func (a *App) WebBetaUseCurrentLogin() error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return a.webBetaUseCurrentLogin(ctx)
}

func (a *App) webBetaUseCurrentLogin(ctx context.Context) (result error) {
	if !betaEnabled() {
		return errors.New("Web Beta is disabled in this build")
	}
	webBetaLogoutMu.Lock()
	defer webBetaLogoutMu.Unlock()
	webBetaAdmission.Lock()
	webBetaAdmission.blocked = true
	controllers, retireErr := a.retireWebBetaControllers(ctx)
	betaRuntime.Lock()
	betaRuntime.resetting = true
	old := betaRuntime.engine
	webbeta.SetEngine(nil)
	betaRuntime.Unlock()
	webBetaAdmission.Unlock()
	defer func() {
		betaRuntime.Lock()
		betaRuntime.resetting = false
		if result != nil {
			betaRuntime.initError = result.Error()
		}
		betaRuntime.Unlock()
	}()
	if old == nil {
		return errors.New("web_recheck_unavailable")
	}
	if err := old.StopForAccountSwitch(); err != nil {
		return err
	}
	if retireErr != nil {
		return errors.New("web_recheck_busy")
	}
	for _, ctrl := range controllers {
		if err := ctrl.WaitIdle(ctx); err != nil {
			return errors.New("web_recheck_busy")
		}
	}
	if err := old.CloseContext(ctx); err != nil {
		return errors.New("web_recheck_busy")
	}
	if err := old.PrepareAccountSwitch(); err != nil {
		return err
	}
	p, err := webBetaProfile()
	if err != nil {
		return err
	}
	betaRuntime.Lock()
	if err := releaseWebBetaBrowser(); err != nil {
		betaRuntime.Unlock()
		return err
	}
	if err := p.PrepareLoginAdoption(); err != nil {
		betaRuntime.Unlock()
		return err
	}
	b, err := newWebBetaBrowser(p.BrowserPath(), browserWindowClass())
	betaRuntime.browser = b
	if err != nil {
		betaRuntime.Unlock()
		return errors.New("web_recheck_unavailable")
	}
	next, err := webbeta.NewEngineWithRequestLimit(p.StatePath(), b, 0)
	if err != nil {
		betaRuntime.Unlock()
		return err
	}
	betaRuntime.engine = next
	betaRuntime.initError = ""
	betaRuntime.Unlock()
	// A new native document has no old identity latch, but the durable pending
	// transition and admission gate still prohibit any model dispatch.
	check, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = b.Recheck(check)
	cancel()
	if err != nil {
		return err
	}
	if err := next.ConfirmAccountSwitch(); err != nil {
		return err
	}
	webbeta.SetEngine(next)
	webBetaAdmission.Lock()
	webBetaAdmission.blocked = false
	webBetaAdmission.controllers = nil
	webBetaAdmission.tabs = nil
	webBetaAdmission.Unlock()
	if a.ctx != nil {
		a.emitReady(a.ctx)
	}
	return nil
}
