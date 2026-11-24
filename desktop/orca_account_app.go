package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
)

const accountProviderKind = "orca-account"
const accountProviderPrefix = "orca-account-"
const accountModelsMaxAge = 5 * time.Minute
const accountModelsRefreshTimeout = 15 * time.Second
const accountLoggingOut = "logging_out"

type accountModelsFlight struct {
	done       chan struct{}
	ctx        context.Context
	generation uint64
	op         uint64
	err        error
}

type OrcaAccountView struct {
	State         string `json:"state"`
	Authenticated bool   `json:"authenticated"`
	DisplayName   string `json:"displayName"`
	Error         string `json:"error"`
	ReceiptError  string `json:"receiptError"`
	ModelsStale   bool   `json:"modelsStale"`
	NeedsSignOut  bool   `json:"needsSignOut"`
}

type OrcaWalletGift struct {
	Groups    string `json:"groups"`
	Available string `json:"available"`
}

type OrcaWalletView struct {
	Currency  string           `json:"currency"`
	Cash      string           `json:"cash"`
	Held      string           `json:"held"`
	Available string           `json:"available"`
	Gifts     []OrcaWalletGift `json:"gifts"`
	Error     string           `json:"error"`
}

type OrcaReceiptView struct {
	RequestID string `json:"requestId"`
	TurnID    string `json:"turnId"`
	Purpose   string `json:"purpose"`
	Status    string `json:"status"`
	Cost      string `json:"cost"`
	Currency  string `json:"currency"`
}

type accountReceipt struct {
	OrcaReceiptView
	serverID   string
	generation uint64
}

type accountDesktop struct {
	// Login, logout, cancel and shutdown take transitionMu before mu. Browser
	// waits in asynchronous login do not hold it; final commits check op again.
	transitionMu         sync.Mutex
	admissionMu          sync.Mutex
	controllers          map[*control.Controller]struct{}
	mu                   sync.Mutex
	client               *orcaaccount.Client
	view                 OrcaAccountView
	op                   uint64
	opCancel             context.CancelFunc
	ctx                  context.Context
	cancel               context.CancelFunc
	catalog              map[string]orcaaccount.Model
	catalogAt            time.Time
	modelsMu             sync.Mutex
	modelsFlight         *accountModelsFlight
	refreshModels        func(context.Context, uint64, uint64) error
	receiptJournal       *accountReceiptJournal
	receiptFailures      map[string]struct{}
	receiptFailureGen    uint64
	receipts             map[string]accountReceipt
	receiptOrder         []string
	settlements          chan string
	wallet               OrcaWalletView
	walletAt             time.Time
	walletMu             sync.Mutex
	credentialMu         sync.Mutex
	credentials          map[int64]*orcaaccount.Credential
	credentialGeneration uint64
}

var accountDesktopActive atomic.Pointer[accountDesktop]

func newAccountDesktop(ctx context.Context, client *orcaaccount.Client) *accountDesktop {
	ctx, cancel := context.WithCancel(ctx)
	s := &accountDesktop{ctx: ctx, cancel: cancel, client: client, view: OrcaAccountView{State: "signed_out"},
		catalog: map[string]orcaaccount.Model{}, receipts: map[string]accountReceipt{},
		settlements: make(chan string, 128), credentials: map[int64]*orcaaccount.Credential{}}
	go s.settlementWorker()
	return s
}

func (a *App) startOrcaAccount() {
	if !betaEnabled() || !config.IsolatedProfileEnabled() {
		return
	}
	root, err := betaDataRoot()
	if err != nil {
		return
	}
	store, err := orcaaccount.NewSystemStore(filepath.Join(root, "account", "session.dpapi"))
	if err != nil {
		return
	}
	client, err := orcaaccount.New(orcaaccount.Options{Store: store, DeviceName: "ORCA beta desktop"})
	if err != nil {
		return
	}
	s := newAccountDesktop(a.bootContext(), client)
	receiptStore, err := orcaaccount.NewSystemStore(filepath.Join(root, "account", "receipts.dpapi"))
	if err != nil {
		s.cancel()
		client.Close()
		return
	}
	s.receiptJournal = &accountReceiptJournal{store: receiptStore}
	a.account = s
	accountDesktopActive.Store(s)
	a.beginAccountLogin(true)
}

func (a *App) stopOrcaAccount() {
	if s := a.account; s != nil {
		s.cancel()
		s.transitionMu.Lock()
		defer s.transitionMu.Unlock()
		s.mu.Lock()
		s.op++
		if s.opCancel != nil {
			s.opCancel()
			s.opCancel = nil
		}
		s.view = OrcaAccountView{State: "signed_out"}
		s.clearAccountDataLocked()
		s.mu.Unlock()
		s.client.Close()
		accountDesktopActive.CompareAndSwap(s, nil)
	}
}

func (a *App) BetaCapabilities() map[string]bool {
	return betaCapabilitiesForOS(runtime.GOOS, betaEnabled(), browserPreviewEnabled(), a.account != nil)
}

func betaCapabilitiesForOS(goos string, beta, browser, account bool) map[string]bool {
	return map[string]bool{"enabled": beta, "account": beta && account, "web": beta && goos == "windows", "browser": browser && goos == "windows"}
}

func (a *App) OrcaAccountStatus() OrcaAccountView {
	s := a.account
	if s == nil {
		return OrcaAccountView{State: "signed_out"}
	}
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconcileAccountLocked()
	v := s.view
	v.ModelsStale = v.Authenticated && time.Since(s.catalogAt) >= accountModelsMaxAge
	v.NeedsSignOut = !v.Authenticated && len(s.controllers) > 0 && v.State != accountLoggingOut
	return v
}

func (s *accountDesktop) clearAccountDataLocked() {
	s.catalog = map[string]orcaaccount.Model{}
	s.catalogAt = time.Time{}
	s.wallet = OrcaWalletView{}
	s.walletAt = time.Time{}
	s.receipts = map[string]accountReceipt{}
	s.receiptOrder = nil
	s.receiptFailures = nil
	s.receiptFailureGen = 0
	s.view.ReceiptError = ""
}

// The client may invalidate itself during refresh without a desktop operation.
// Keep the stored view, not just its returned copy, consistent with that state.
func (s *accountDesktop) reconcileAccountLocked() {
	if s.view.Authenticated && !s.client.Snapshot().Authenticated {
		s.view = OrcaAccountView{State: "error", Error: "ORCA account session expired. Sign in again."}
		s.clearAccountDataLocked()
	}
}

func (a *App) OrcaAccountLogin() error {
	if a.account == nil {
		return orcaaccount.ErrUnsupported
	}
	return a.beginAccountLogin(false)
}

func (a *App) beginAccountLogin(restore bool) error {
	s := a.account
	if s == nil {
		return orcaaccount.ErrUnsupported
	}
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return err
	}
	s.admissionMu.Lock()
	for ctrl := range s.controllers {
		if !ctrl.Retired() || ctrl.Running() {
			s.admissionMu.Unlock()
			return errors.New("sign out and finish previous ORCA account tools before signing in")
		}
	}
	s.admissionMu.Unlock()
	s.mu.Lock()
	s.reconcileAccountLocked()
	if s.view.State == accountLoggingOut {
		s.mu.Unlock()
		return errors.New("ORCA account is signing out; previous tools have not finished")
	}
	if s.view.State == "signing_in" || s.view.State == "restoring" {
		s.mu.Unlock()
		return errors.New("ORCA account authorization is already in progress")
	}
	// Switching requires explicit logout; an old account cannot silently become a new one.
	if s.view.Authenticated {
		s.mu.Unlock()
		return errors.New("sign out of the current ORCA account before switching accounts")
	}
	s.op++
	op := s.op
	if s.refreshModels == nil {
		s.refreshModels = func(ctx context.Context, gen, op uint64) error {
			return a.refreshAccountModels(ctx, s, gen, op)
		}
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.opCancel = cancel
	s.clearAccountDataLocked()
	s.view = OrcaAccountView{State: "signing_in"}
	if restore {
		s.view.State = "restoring"
	}
	s.mu.Unlock()
	go func() {
		defer cancel()
		var profile orcaaccount.Profile
		var err error
		if restore {
			profile, err = s.client.Restore(ctx)
		} else {
			err = s.client.CheckDesktopService(ctx)
			if err == nil {
				profile, err = s.client.Login(ctx)
			}
		}
		var catalog orcaaccount.Models
		if err == nil {
			catalog, err = s.client.Models(ctx)
		}
		gen := s.client.CurrentSessionGeneration()
		var receipts []accountReceipt
		if err == nil {
			receipts, err = s.restoreAccountReceipts(ctx, profile, gen)
		}
		s.transitionMu.Lock()
		defer s.transitionMu.Unlock()
		s.mu.Lock()
		if s.op != op || ctx.Err() != nil {
			s.mu.Unlock()
			return
		}
		if err == nil {
			snapshot := s.client.Snapshot()
			if !snapshot.Authenticated || snapshot.Generation != gen || snapshot.Profile.Subject != profile.Subject {
				err = orcaaccount.ErrStale
			}
		}
		s.opCancel = nil
		if err != nil {
			s.view = OrcaAccountView{State: "error", Error: err.Error()}
			if restore && errors.Is(err, orcaaccount.ErrNotFound) {
				s.view = OrcaAccountView{State: "signed_out"}
			}
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		models := accountCatalog(profile.Subject, catalog.Models)
		// Only non-secret catalog metadata is persisted to the beta user file.
		// Never save a merged project configuration or a runtime credential.
		if err = a.persistAccountCatalog(models); err != nil {
			s.mu.Lock()
			s.view = OrcaAccountView{State: "error", Error: err.Error()}
			s.mu.Unlock()
			return
		}
		s.mu.Lock()
		snapshot := s.client.Snapshot()
		if ctx.Err() != nil || !snapshot.Authenticated || snapshot.Generation != gen || snapshot.Profile.Subject != profile.Subject || snapshot.Profile.SessionID != profile.SessionID {
			s.view = OrcaAccountView{State: "error", Error: orcaaccount.ErrStale.Error()}
			s.mu.Unlock()
			_ = a.persistAccountCatalog(nil)
			return
		}
		s.catalog = models
		s.catalogAt = time.Now()
		for _, r := range receipts {
			s.receipts[r.RequestID] = r
			s.receiptOrder = append(s.receiptOrder, r.RequestID)
		}
		s.view = OrcaAccountView{State: "signed_in", Authenticated: true, DisplayName: profile.DisplayName}
		if s.view.DisplayName == "" {
			s.view.DisplayName = profile.User.Username
		}
		s.mu.Unlock()
		go s.recoverAccountReceipts(gen, receipts)
	}()
	return nil
}

func (a *App) OrcaAccountCancelLogin() error {
	s := a.account
	if s == nil {
		return nil
	}
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	s.mu.Lock()
	if s.view.State != "signing_in" && s.view.State != "restoring" {
		s.mu.Unlock()
		return nil
	}
	s.op++
	if s.opCancel != nil {
		s.opCancel()
		s.opCancel = nil
	}
	s.view = OrcaAccountView{State: "signed_out"}
	s.clearAccountDataLocked()
	s.mu.Unlock()
	return s.client.CancelContexts()
}

func (a *App) OrcaAccountLogout() error {
	ctx, cancel := context.WithTimeout(a.bootContext(), 15*time.Second)
	defer cancel()
	return a.logoutOrcaAccount(ctx)
}

func (a *App) logoutOrcaAccount(ctx context.Context) error {
	s := a.account
	if s == nil {
		return nil
	}
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	s.admissionMu.Lock()
	s.mu.Lock()
	s.op++
	if s.opCancel != nil {
		s.opCancel()
		s.opCancel = nil
	}
	s.view = OrcaAccountView{State: accountLoggingOut}
	op := s.op
	turns := map[string]bool{}
	for _, r := range s.receipts {
		if r.TurnID != "" {
			turns[r.TurnID] = true
		}
	}
	s.clearAccountDataLocked()
	s.mu.Unlock()
	if s.controllers == nil {
		s.controllers = map[*control.Controller]struct{}{}
	}
	// Retain registered controllers even after their tabs have been detached.
	a.mu.Lock()
	for _, tab := range a.tabs {
		usesAccount := strings.HasPrefix(tab.model, accountProviderPrefix)
		if tab.Ctrl != nil {
			usesAccount = usesAccount || turns[tab.Ctrl.TurnStatus().TurnID] || strings.HasPrefix(tab.Ctrl.ContextMetadata().ModelRef, accountProviderPrefix)
		}
		if usesAccount {
			tab.pendingSubmitEpoch++
			tab.pendingSubmitCount = 0
			tab.pendingRuntimeSubmits = nil
			tab.launchedPendingID, tab.launchedPendingTurn = "", ""
			if tab.Ctrl != nil {
				s.controllers[tab.Ctrl] = struct{}{}
			}
		}
	}
	a.mu.Unlock()
	controllers := make([]*control.Controller, 0, len(s.controllers))
	for ctrl := range s.controllers {
		ctrl.Retire()
		controllers = append(controllers, ctrl)
	}
	s.admissionMu.Unlock()
	err := s.client.Logout(ctx)
	s.credentialMu.Lock()
	s.credentials = map[int64]*orcaaccount.Credential{}
	s.credentialMu.Unlock()
	if configErr := a.persistAccountCatalog(nil); err == nil {
		err = configErr
	}
	for _, ctrl := range controllers {
		if drainErr := ctrl.WaitIdle(ctx); drainErr != nil {
			s.mu.Lock()
			s.view.Error = "Previous account tools are still stopping. Sign-in remains blocked."
			s.mu.Unlock()
			go s.finishAccountDrain(op, controllers, err)
			return errors.New("ORCA account is signing out; previous tools have not finished")
		}
	}
	s.admissionMu.Lock()
	s.controllers = nil
	s.admissionMu.Unlock()
	s.mu.Lock()
	s.view = OrcaAccountView{State: "signed_out"}
	if err != nil {
		s.view.Error = err.Error()
	}
	s.mu.Unlock()
	return err
}

func (s *accountDesktop) finishAccountDrain(op uint64, controllers []*control.Controller, logoutErr error) {
	for _, ctrl := range controllers {
		if ctrl.WaitIdle(s.ctx) != nil {
			return
		}
	}
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.op == op && s.ctx.Err() == nil && s.view.State == accountLoggingOut {
		s.controllers = nil
		s.view = OrcaAccountView{State: "signed_out"}
		if logoutErr != nil {
			s.view.Error = logoutErr.Error()
		}
	}
}

// Submission callers hold release until Running is published or input queued.
func (a *App) beginOrcaAccountAdmission(tabID string) (func(), error) {
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	selected := tab != nil && (strings.HasPrefix(tab.model, accountProviderPrefix) || tab.Ctrl != nil && strings.HasPrefix(tab.Ctrl.ContextMetadata().ModelRef, accountProviderPrefix))
	a.mu.RUnlock()
	if !selected {
		return func() {}, nil
	}
	s := a.account
	if s == nil {
		return nil, orcaaccount.ErrUnauthenticated
	}
	s.admissionMu.Lock()
	s.mu.Lock()
	s.reconcileAccountLocked()
	ready := s.view.Authenticated
	s.mu.Unlock()
	if !ready {
		s.admissionMu.Unlock()
		return nil, orcaaccount.ErrUnauthenticated
	}
	a.mu.RLock()
	tab = a.tabByIDLocked(tabID)
	var ctrl *control.Controller
	if tab != nil {
		ctrl = tab.Ctrl
	}
	a.mu.RUnlock()
	if ctrl != nil {
		if ctrl.Retired() {
			s.admissionMu.Unlock()
			return nil, control.ErrControllerRetired
		}
		if s.controllers == nil {
			s.controllers = map[*control.Controller]struct{}{}
		}
		s.controllers[ctrl] = struct{}{}
	}
	return s.admissionMu.Unlock, nil
}

func (s *accountDesktop) modelInfo(name string, current string) (ModelInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.catalog[name]
	if !ok || !s.view.Authenticated || !s.client.Snapshot().Authenticated {
		return ModelInfo{}, false
	}
	ref := name + "/" + m.ID
	i := ModelInfo{Ref: ref, Provider: name, Model: m.ID, Current: ref == current, Vision: "unknown", ToolUse: "unknown", StructuredOutput: "unknown", Currency: "CNY", MetadataSource: "orca-account-server"}
	if c := m.Capabilities; c != nil {
		capability := func(v *bool) string {
			if v == nil {
				return "unknown"
			}
			if *v {
				return "supported"
			}
			return "unsupported"
		}
		i.Vision = capability(c.Vision)
		i.ToolUse = capability(c.Tools)
		if c.ContextWindow != nil && *c.ContextWindow > 0 {
			i.ContextWindow = int(*c.ContextWindow)
			i.ContextWindowConfirmed = true
			i.ContextSource = "orca-account-server"
		}
	}
	return i, true
}

func accountCatalog(subject string, models []orcaaccount.Model) map[string]orcaaccount.Model {
	out := map[string]orcaaccount.Model{}
	for _, m := range models {
		if subject == "" || m.ID == "" || m.GroupID <= 0 || (m.Protocol != "openai" && m.Protocol != "anthropic") {
			continue
		}
		if m.Capabilities != nil && m.Capabilities.Text != nil && !*m.Capabilities.Text {
			continue
		}
		hash := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s\x00%s", subject, m.GroupID, m.Protocol, m.Endpoint, m.ID)))
		name := fmt.Sprintf("%s%x", accountProviderPrefix, hash[:12])
		out[name] = m
	}
	return out
}

// Refresh only availability metadata. Never select a replacement model, replay
// a turn, or save a merged project config. Concurrent callers share one fetch.
func (a *App) OrcaAccountRefreshModels() error {
	s := a.account
	if s == nil {
		return orcaaccount.ErrUnauthenticated
	}
	return s.refreshAccountCatalog(s.ctx, s.client.CurrentSessionGeneration(), false)
}

// The shared fetch belongs to the account generation, not an individual caller.
// A canceled waiter leaves promptly without canceling another caller's refresh.
func (s *accountDesktop) refreshAccountCatalog(ctx context.Context, gen uint64, onlyIfStale bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.modelsMu.Lock()
	s.mu.Lock()
	s.reconcileAccountLocked()
	ready, op, fetch := s.view.Authenticated, s.op, s.refreshModels
	fresh := time.Since(s.catalogAt) < accountModelsMaxAge
	s.mu.Unlock()
	if s.client.CurrentSessionGeneration() != gen {
		s.modelsMu.Unlock()
		return orcaaccount.ErrStale
	}
	if !ready {
		s.modelsMu.Unlock()
		return orcaaccount.ErrUnauthenticated
	}
	if onlyIfStale && fresh {
		s.modelsMu.Unlock()
		return nil
	}
	if fetch == nil {
		s.modelsMu.Unlock()
		return errors.New("ORCA account model refresh is unavailable; sign in again")
	}
	f := s.modelsFlight
	if f == nil || f.generation != gen || f.op != op {
		life, stop, err := s.client.GenerationContext(s.ctx, gen)
		if err != nil {
			s.modelsMu.Unlock()
			return err
		}
		bounded, cancel := context.WithTimeout(life, accountModelsRefreshTimeout)
		f = &accountModelsFlight{done: make(chan struct{}), ctx: bounded, generation: gen, op: op}
		s.modelsFlight = f
		go func() {
			defer stop()
			defer cancel()
			err := fetch(bounded, gen, op)
			if err != nil {
				s.mu.Lock()
				if s.op == op && s.client.CurrentSessionGeneration() == gen && s.view.Authenticated {
					s.catalogAt = time.Time{}
				}
				s.mu.Unlock()
			}
			s.modelsMu.Lock()
			f.err = err
			if s.modelsFlight == f {
				s.modelsFlight = nil
			}
			close(f.done)
			s.modelsMu.Unlock()
		}()
	}
	s.modelsMu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.ctx.Done():
		// Completion closes done before canceling the shared context. Prefer its
		// actual result when both channels are ready.
		select {
		case <-f.done:
		default:
			if s.client.CurrentSessionGeneration() != gen {
				return orcaaccount.ErrStale
			}
			return f.ctx.Err()
		}
	case <-f.done:
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.client.CurrentSessionGeneration() != gen {
		return orcaaccount.ErrStale
	}
	return f.err
}

func (a *App) refreshAccountModels(ctx context.Context, s *accountDesktop, gen, op uint64) error {
	profile, err := s.client.Session(ctx)
	if err != nil {
		return err
	}
	catalog, err := s.client.Models(ctx)
	if err != nil {
		return err
	}
	models := accountCatalog(profile.Subject, catalog.Models)
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	s.mu.Lock()
	current := s.view.Authenticated && s.op == op && s.client.CurrentSessionGeneration() == gen && ctx.Err() == nil
	s.mu.Unlock()
	if !current {
		return orcaaccount.ErrStale
	}
	if err = a.persistAccountCatalog(models); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.client.Snapshot().Authenticated || s.client.CurrentSessionGeneration() != gen || ctx.Err() != nil {
		return orcaaccount.ErrStale
	}
	s.catalog, s.catalogAt = models, time.Now()
	s.walletAt = time.Time{}
	return nil
}

// Preserve the exact subject/group/protocol/endpoint/model selection. Stale
// metadata is refreshed once; failures never fall back to stale paid admission.
func (s *accountDesktop) readyAccountModel(ctx context.Context, name, model string) (orcaaccount.Model, uint64, error) {
	if err := ctx.Err(); err != nil {
		return orcaaccount.Model{}, 0, err
	}
	gen := s.client.CurrentSessionGeneration()
	s.mu.Lock()
	s.reconcileAccountLocked()
	ready, stale := s.view.Authenticated, time.Since(s.catalogAt) >= accountModelsMaxAge
	s.mu.Unlock()
	if !ready {
		return orcaaccount.Model{}, 0, orcaaccount.ErrUnauthenticated
	}
	if stale {
		if err := s.refreshAccountCatalog(ctx, gen, true); err != nil {
			return orcaaccount.Model{}, 0, fmt.Errorf("ORCA account models could not be refreshed; retry before sending: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return orcaaccount.Model{}, 0, err
	}
	m, current, err := s.accountModelForRequest(name, model)
	if current != gen && err == nil {
		return orcaaccount.Model{}, 0, orcaaccount.ErrStale
	}
	return m, current, err
}

// Provider integration must check freshness before acquiring a credential.
func (s *accountDesktop) accountModelForRequest(name, model string) (orcaaccount.Model, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconcileAccountLocked()
	if !s.view.Authenticated {
		return orcaaccount.Model{}, 0, orcaaccount.ErrUnauthenticated
	}
	m, ok := s.catalog[name]
	if !ok || m.ID != model {
		return orcaaccount.Model{}, 0, errors.New("ORCA account model is no longer available for this account; choose an authorized model")
	}
	if time.Since(s.catalogAt) >= accountModelsMaxAge {
		return orcaaccount.Model{}, 0, errors.New("ORCA account model availability expired; retry to refresh models")
	}
	return m, s.client.CurrentSessionGeneration(), nil
}

func accountProviderEntry(name string, m orcaaccount.Model) config.ProviderEntry {
	p := config.ProviderEntry{Name: name, Kind: accountProviderKind, BaseURL: orcaaccount.Router + "/v1", Model: m.ID, ReasoningProtocol: "none"}
	applyAccountReasoning(&p, m)
	if m.Capabilities != nil && m.Capabilities.ContextWindow != nil && *m.Capabilities.ContextWindow > 0 {
		p.ContextWindow = int(*m.Capabilities.ContextWindow)
	}
	return p
}

func (a *App) persistAccountCatalog(models map[string]orcaaccount.Model) error {
	if !betaEnabled() || !config.IsolatedProfileEnabled() {
		return orcaaccount.ErrUnsupported
	}
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()
	path := config.UserConfigPath()
	if path == "" {
		return errors.New("beta configuration directory is unavailable")
	}
	cfg := config.LoadForEdit(path)
	entries := make([]config.ProviderEntry, 0, len(cfg.Providers)+len(models))
	for _, p := range cfg.Providers {
		if p.Kind != accountProviderKind || !strings.HasPrefix(p.Name, accountProviderPrefix) {
			entries = append(entries, p)
		}
	}
	names := make([]string, 0, len(models))
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := accountProviderEntry(name, models[name])
		if old, ok := cfg.Provider(name); ok && old.Kind == accountProviderKind && old.Model == entry.Model {
			// The name binds subject/group/protocol/endpoint. Preserve only local
			// model settings, never stored routing, discovery or credentials.
			entry.Thinking, entry.Effort = old.Thinking, old.Effort
			entry.ReasoningProtocol = old.ReasoningProtocol
			entry.SupportedEfforts = append([]string(nil), old.SupportedEfforts...)
			entry.DefaultEffort = old.DefaultEffort
			entry.ModelContextWindows = maps.Clone(old.ModelContextWindows)
			if entry.ContextWindow == 0 {
				entry.ContextWindow = old.ContextWindow
			}
			if old.Price != nil {
				price := *old.Price
				entry.Price = &price
			}
		}
		applyAccountReasoning(&entry, models[name])
		entries = append(entries, entry)
	}
	cfg.Providers = entries
	return cfg.SaveTo(path)
}

// Decimal conversion never rounds a wallet or a receipt through float64.
func cnyMicros(value int64) string {
	negative := value < 0
	var magnitude uint64
	if negative {
		magnitude = uint64(-(value + 1)) + 1
	} else {
		magnitude = uint64(value)
	}
	s := fmt.Sprintf("%d.%06d", magnitude/1_000_000, magnitude%1_000_000)
	if negative {
		s = "-" + s
	}
	return s
}

func (a *App) OrcaAccountWallet() OrcaWalletView {
	s := a.account
	if s == nil {
		return OrcaWalletView{Error: orcaaccount.ErrUnauthenticated.Error(), Gifts: []OrcaWalletGift{}}
	}
	s.walletMu.Lock()
	defer s.walletMu.Unlock()
	s.mu.Lock()
	s.reconcileAccountLocked()
	if !s.view.Authenticated {
		s.mu.Unlock()
		return OrcaWalletView{Error: orcaaccount.ErrUnauthenticated.Error(), Gifts: []OrcaWalletGift{}}
	}
	if time.Since(s.walletAt) < 10*time.Second {
		result := s.wallet
		s.mu.Unlock()
		return result
	}
	s.mu.Unlock()
	gen := s.client.CurrentSessionGeneration()
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	w, err := s.client.Wallet(ctx)
	result := OrcaWalletView{Currency: "CNY", Gifts: []OrcaWalletGift{}}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	profile := s.client.Snapshot().Profile
	result = accountWalletView(w, profile)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client.CurrentSessionGeneration() != gen || !s.view.Authenticated {
		return OrcaWalletView{Error: orcaaccount.ErrStale.Error(), Gifts: []OrcaWalletGift{}}
	}
	s.wallet = result
	s.walletAt = time.Now()
	return result
}

func accountWalletView(w orcaaccount.Wallet, profile orcaaccount.Profile) OrcaWalletView {
	result := OrcaWalletView{Currency: w.Currency, Cash: cnyMicros(w.CashMicros), Held: cnyMicros(w.HeldMicros), Available: cnyMicros(w.AvailableMicros), Gifts: []OrcaWalletGift{}}
	type grantView struct {
		available int64
		groups    []int64
		seen      map[int64]bool
	}
	grants := map[string]*grantView{}
	var order []*grantView
	for _, g := range w.ScopedGifts {
		grant := grants[g.ID]
		if grant == nil {
			grant = &grantView{available: g.AvailableMicros, seen: map[int64]bool{}}
			grants[g.ID] = grant
			order = append(order, grant)
		}
		// A multi-group grant repeats its amount in each row. Merge only scopes.
		groups := g.GroupIDs
		if len(groups) == 0 {
			groups = []int64{g.GroupID}
		}
		for _, id := range groups {
			if !grant.seen[id] {
				grant.seen[id] = true
				grant.groups = append(grant.groups, id)
			}
		}
	}
	for _, grant := range order {
		var names []string
		for _, id := range grant.groups {
			name := fmt.Sprintf("group %d", id)
			for _, p := range profile.Groups {
				if p.ID == id {
					name = p.Name
					break
				}
			}
			names = append(names, name)
		}
		result.Gifts = append(result.Gifts, OrcaWalletGift{Groups: strings.Join(names, ", "), Available: cnyMicros(grant.available)})
	}
	return result
}

func (a *App) OrcaAccountReceipts() []OrcaReceiptView {
	out := []OrcaReceiptView{}
	if s := a.account; s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.reconcileAccountLocked()
		for _, id := range s.receiptOrder {
			if r, ok := s.receipts[id]; ok {
				out = append(out, r.OrcaReceiptView)
			}
		}
	}
	return out
}
