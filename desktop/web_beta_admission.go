package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var webBetaAdmission struct {
	sync.Mutex
	blocked     bool
	controllers map[*control.Controller]struct{}
	tabs        map[*WorkspaceTab]uint64 // pending queue epoch admitted under this identity
}

// Reserved-name guard used by auxiliary configuration validation only. Runtime
// identity must use the resolved Kind, never this spelling check.
func isWebBetaModel(model string) bool {
	name, _, qualified := strings.Cut(model, "/")
	return qualified && name == "deepseek-web-beta"
}

func isWebBetaTab(tab *WorkspaceTab) bool {
	return tab != nil && tab.Ctrl.ContextMetadata().ProviderKind == "deepseekweb-beta"
}

func (a *App) webBetaSelected(tabID string) bool {
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		a.mu.RUnlock()
		return false
	}
	model, root := strings.TrimSpace(tab.model), tab.WorkspaceRoot
	bound := tab.Ctrl.ContextMetadata()
	a.mu.RUnlock()
	// A controller keeps its original identity even after config removal or a
	// pending selection change. A matching bound API never needs a disk lookup.
	if bound.ProviderKind == "deepseekweb-beta" {
		return true
	}
	if bound.ProviderKind != "" && (model == bound.ModelRef || model == "") {
		return false
	}
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return false
	}
	if model == "" {
		model = cfg.DefaultModel
	}
	entry, ok := cfg.ResolveModel(model)
	return ok && entry.Kind == "deepseekweb-beta"
}

// Reject before a turn is admitted. Opening the panel never enables requests
// or retries the draft; the user explicitly confirms both actions.
func (a *App) checkWebBetaAdmission(tabID string) error {
	release, err := a.beginWebBetaAdmission(tabID)
	if err == nil {
		release()
	}
	return err
}

// Hold the admission lock until submission publishes Controller.Running or is
// queued. Logout cannot overlook an accepted but not-yet-started submission.
func (a *App) beginWebBetaAdmission(tabID string) (func(), error) {
	if !a.webBetaSelected(tabID) {
		return func() {}, nil
	}
	webBetaAdmission.Lock()
	if !a.webBetaSelected(tabID) {
		webBetaAdmission.Unlock()
		return func() {}, nil
	}
	if webBetaAdmission.blocked {
		webBetaAdmission.Unlock()
		return nil, webbeta.ErrIdentityClosed
	}
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	var ctrl *control.Controller
	if tab != nil {
		ctrl = tab.Ctrl
	}
	a.mu.RUnlock()
	if ctrl != nil && ctrl.Retired() {
		webBetaAdmission.Unlock()
		return nil, control.ErrControllerRetired
	}
	err := a.prepareWebBetaBackground()
	if err != nil && a.ctx != nil {
		runtime.EventsEmit(a.ctx, "webbeta:open-panel")
	}
	if err != nil {
		webBetaAdmission.Unlock()
		return nil, err
	}
	if ctrl.ContextMetadata().ProviderKind == "deepseekweb-beta" {
		if webBetaAdmission.controllers == nil {
			webBetaAdmission.controllers = make(map[*control.Controller]struct{})
		}
		webBetaAdmission.controllers[ctrl] = struct{}{}
	}
	if tab != nil {
		if webBetaAdmission.tabs == nil {
			webBetaAdmission.tabs = make(map[*WorkspaceTab]uint64)
		}
	}
	return func() {
		// Submission has now either published its controller or queued its draft.
		// Remember the batch, not every later API queue on this same tab.
		a.mu.RLock()
		if tab != nil && tab.pendingSubmitCount > 0 {
			webBetaAdmission.tabs[tab] = tab.pendingSubmitEpoch
		}
		a.mu.RUnlock()
		webBetaAdmission.Unlock()
	}, nil
}

// Caller holds webBetaAdmission. Keep detached accepted controllers across a
// failed drain; replacing/closing their tab must not hide a running tool.
func (a *App) retireWebBetaControllers(ctx context.Context) ([]*control.Controller, error) {
	if webBetaAdmission.controllers == nil {
		webBetaAdmission.controllers = make(map[*control.Controller]struct{})
	}
	a.mu.RLock()
	var tabs []*WorkspaceTab
	seen := make(map[*WorkspaceTab]bool)
	collect := func(tab *WorkspaceTab) {
		if seen[tab] {
			return
		}
		seen[tab] = true
		epoch, accepted := webBetaAdmission.tabs[tab]
		queued := tab != nil && accepted && epoch == tab.pendingSubmitEpoch && tab.pendingSubmitCount > 0
		if !isWebBetaTab(tab) && !queued {
			return
		}
		tabs = append(tabs, tab)
		if isWebBetaTab(tab) {
			webBetaAdmission.controllers[tab.Ctrl] = struct{}{}
		}
	}
	for _, tab := range a.tabs {
		collect(tab)
	}
	for tab := range webBetaAdmission.tabs {
		collect(tab)
	}
	a.mu.RUnlock()
	for ctrl := range webBetaAdmission.controllers {
		ctrl.Retire()
	}
	// The queue checks its epoch and fetches its controller in separate critical
	// sections. Serialize invalidation with that whole admission, including swaps.
	for _, tab := range tabs {
		for !tab.submitMu.TryLock() {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
		a.mu.Lock()
		epoch, accepted := webBetaAdmission.tabs[tab]
		if !isWebBetaTab(tab) && (!accepted || epoch != tab.pendingSubmitEpoch || tab.pendingSubmitCount == 0) {
			a.mu.Unlock()
			tab.submitMu.Unlock()
			continue
		}
		tab.pendingSubmitEpoch++
		tab.pendingSubmitCount = 0
		tab.pendingRuntimeSubmits = nil
		tab.launchedPendingID, tab.launchedPendingTurn = "", ""
		var ctrl *control.Controller
		if isWebBetaTab(tab) {
			ctrl = tab.Ctrl
		}
		a.mu.Unlock()
		if ctrl != nil {
			ctrl.Retire()
			webBetaAdmission.controllers[ctrl] = struct{}{}
		}
		tab.submitMu.Unlock()
	}
	controllers := make([]*control.Controller, 0, len(webBetaAdmission.controllers))
	for ctrl := range webBetaAdmission.controllers {
		controllers = append(controllers, ctrl)
	}
	return controllers, nil
}

func (a *App) prepareWebBetaBackground() error {
	if !betaEnabled() {
		return errors.New("Web Beta is disabled in this build")
	}
	if webbeta.CurrentEngine() == nil && a.ctx != nil {
		if err := a.openWebBetaBackground(); err != nil {
			return err
		}
	}
	e := webbeta.CurrentEngine()
	if e != nil {
		s := e.Status()
		// Wait for readiness inside the cancellable Agent turn, not admission.
		if s.Settings.Enabled && s.Blocked == "" && s.Browser.Blocked == "" {
			return nil
		}
	}
	return webbeta.AvailabilityError(e)
}
