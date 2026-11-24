package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func expireAccountCatalog(s *accountDesktop) {
	s.mu.Lock()
	s.catalogAt = time.Now().Add(-accountModelsMaxAge - time.Second)
	s.mu.Unlock()
}

func TestAccountCatalogRefreshPreservesExactModelSettings(t *testing.T) {
	f := newAccountStreamFixture(t, "openai")
	p := f.registeredProvider()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	old, ok := cfg.Provider(p.Name())
	if !ok {
		t.Fatal("fixture account catalog was not persisted")
	}
	old.Thinking, old.Effort, old.ReasoningProtocol = "adaptive", "high", "openai"
	old.SupportedEfforts, old.DefaultEffort = []string{"low", "high"}, "low"
	old.ContextWindow = 65536
	old.ModelContextWindows = map[string]int{integrationModel: 32768}
	old.Price = &provider.Pricing{Input: 1, Output: 2, CacheHit: 0.25, Currency: "CNY"}
	// These fields must not survive even when the catalog identity matches.
	old.BaseURL, old.ModelsURL, old.BalanceURL = "https://invalid.example/v1", "https://invalid.example/models", "https://invalid.example/wallet"
	old.APIKeyEnv, old.NoProxy = "SYNTHETIC_SECRET_ENV", true
	unrelated := config.ProviderEntry{Name: "synthetic-unrelated", Kind: "openai", Model: "other", BaseURL: "https://synthetic.invalid/v1", Effort: "low"}
	cfg.Providers = append(cfg.Providers, unrelated)
	cfg.DefaultModel = p.Name() + "/" + integrationModel
	cfg.Agent.SubagentModels = map[string]string{"browser": cfg.DefaultModel}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	wantSettings := *old
	baseline := config.LoadForEdit(path)
	wantOther, ok := baseline.Provider(unrelated.Name)
	if !ok {
		t.Fatal("synthetic unrelated provider was not persisted")
	}
	for _, known := range []bool{false, true} {
		f.setCatalogHook(func(r *http.Request) (*http.Response, error) {
			model := orcaaccount.Model{ID: integrationModel, GroupID: 7, Protocol: "openai", Endpoint: "/chat/completions"}
			if known {
				value := int64(131072)
				model.Capabilities = &orcaaccount.ModelCapabilities{ContextWindow: &value}
			}
			return integrationJSON(r, orcaaccount.Models{Currency: "CNY", Models: []orcaaccount.Model{model}})
		})
		expireAccountCatalog(f.app.account)
		if _, _, err := f.app.account.readyAccountModel(context.Background(), p.Name(), integrationModel); err != nil {
			t.Fatal(err)
		}
		after := config.LoadForEdit(path)
		entry, ok := after.Provider(p.Name())
		if !ok {
			t.Fatal("refresh removed exact selection")
		}
		if entry.Thinking != wantSettings.Thinking || entry.Effort != wantSettings.Effort || entry.ReasoningProtocol != wantSettings.ReasoningProtocol ||
			entry.DefaultEffort != wantSettings.DefaultEffort || !reflect.DeepEqual(entry.SupportedEfforts, wantSettings.SupportedEfforts) ||
			!reflect.DeepEqual(entry.ModelContextWindows, wantSettings.ModelContextWindows) || !reflect.DeepEqual(entry.Price, wantSettings.Price) {
			t.Fatalf("refresh discarded settings: thinking=%t effort=%t protocol=%t default=%t supported=%t contextmap=%t price=%t", entry.Thinking != wantSettings.Thinking, entry.Effort != wantSettings.Effort, entry.ReasoningProtocol != wantSettings.ReasoningProtocol, entry.DefaultEffort != wantSettings.DefaultEffort, !reflect.DeepEqual(entry.SupportedEfforts, wantSettings.SupportedEfforts), !reflect.DeepEqual(entry.ModelContextWindows, wantSettings.ModelContextWindows), !reflect.DeepEqual(entry.Price, wantSettings.Price))
		}
		wantContext := 65536
		if known {
			wantContext = 131072
		}
		if entry.ContextWindow != wantContext {
			t.Fatal("server context metadata did not override the fallback")
		}
		if entry.Kind != accountProviderKind || entry.Model != integrationModel || entry.BaseURL != orcaaccount.Router+"/v1" ||
			entry.ModelsURL != "" || entry.BalanceURL != "" || entry.APIKeyEnv != "" || entry.NoProxy || len(entry.Models) != 0 || entry.Default != "" {
			t.Fatal("refresh retained untrusted routing, discovery or credential settings")
		}
		gotOther, ok := after.Provider(unrelated.Name)
		if !ok || !reflect.DeepEqual(gotOther, wantOther) || after.DefaultModel != baseline.DefaultModel || after.Agent.SubagentModels["browser"] != baseline.Agent.SubagentModels["browser"] {
			t.Fatal("refresh changed unrelated config or selected another model")
		}
	}
	if f.credentials.Load() != 0 || f.inferences.Load() != 0 {
		t.Fatal("metadata refresh triggered paid admission")
	}
}

func TestAccountCatalogSettingsNeverCrossIdentity(t *testing.T) {
	for _, change := range []string{"subject", "group", "endpoint", "protocol", "model", "stored-model"} {
		t.Run(change, func(t *testing.T) {
			lifecycleProfile(t)
			app := &App{}
			model := orcaaccount.Model{ID: integrationModel, GroupID: 7, Protocol: "openai", Endpoint: "/chat/completions"}
			initial := accountCatalog("synthetic-original", []orcaaccount.Model{model})
			if err := app.persistAccountCatalog(initial); err != nil {
				t.Fatal(err)
			}
			cfg := config.LoadForEdit(config.UserConfigPath())
			var oldName string
			for name := range initial {
				oldName = name
			}
			old, _ := cfg.Provider(oldName)
			old.Thinking, old.Effort = "adaptive", "high"
			old.Price = &provider.Pricing{Input: 99, Currency: "CNY"}
			subject := "synthetic-original"
			switch change {
			case "subject":
				subject = "synthetic-new"
			case "group":
				model.GroupID = 8
			case "endpoint":
				model.Endpoint = "/responses"
			case "protocol":
				model.Protocol, model.Endpoint = "anthropic", "/messages"
			case "model":
				model.ID = "another-model"
			case "stored-model":
				old.Model = "conflicting-model"
			}
			if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
				t.Fatal(err)
			}
			if err := app.persistAccountCatalog(accountCatalog(subject, []orcaaccount.Model{model})); err != nil {
				t.Fatal(err)
			}
			after := config.LoadForEdit(config.UserConfigPath())
			var accounts []config.ProviderEntry
			for _, entry := range after.Providers {
				if entry.Kind == accountProviderKind {
					accounts = append(accounts, entry)
				}
			}
			if len(accounts) != 1 {
				t.Fatal("obsolete account entry survived refresh")
			}
			entry := accounts[0]
			if entry.Thinking != "" || entry.Effort != "" || entry.Price != nil || entry.Model != model.ID {
				t.Fatal("settings crossed account/model identity")
			}
		})
	}
}

func freshAccountCatalog(r *http.Request) (*http.Response, error) {
	return integrationJSON(r, orcaaccount.Models{Currency: "CNY", Models: []orcaaccount.Model{{ID: integrationModel, GroupID: 7, Protocol: "openai", Endpoint: "/chat/completions"}}})
}

func (f *accountStreamFixture) setCatalogHook(hook func(*http.Request) (*http.Response, error)) {
	f.mu.Lock()
	f.catalogHook = hook
	f.mu.Unlock()
}

// Done is evaluated when the caller has joined the refresh's select. This
// avoids timing sleeps when proving that several callers share one flight.
type accountCatalogWaiter struct {
	context.Context
	joined chan struct{}
	once   sync.Once
}

func (c *accountCatalogWaiter) Done() <-chan struct{} {
	c.once.Do(func() { close(c.joined) })
	return c.Context.Done()
}

func TestAccountProviderStaleCatalogRefreshesExactSelection(t *testing.T) {
	f := newAccountStreamFixture(t, "openai")
	f.settled.Store(true)
	p := f.registeredProvider()
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.DefaultModel = p.Name() + "/" + integrationModel
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	expireAccountCatalog(f.app.account)
	f.setCatalogHook(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > accountModelsRefreshTimeout {
			t.Error("catalog refresh is not bounded to fifteen seconds")
		}
		if f.credentials.Load() != 0 || f.inferences.Load() != 0 {
			t.Error("paid admission started before availability refresh finished")
		}
		return freshAccountCatalog(r)
	})
	stream, err := p.Stream(context.Background(), integrationRequest())
	if err != nil {
		t.Fatal(err)
	}
	drainIntegrationStream(t, stream, false)
	f.assertSettled()
	if f.catalogs.Load() != 2 || f.sessions.Load() != 2 || f.app.OrcaAccountStatus().ModelsStale {
		t.Fatal("expired catalog did not refresh exactly once before inference")
	}
	if config.LoadForEdit(config.UserConfigPath()).DefaultModel != cfg.DefaultModel {
		t.Fatal("availability refresh selected a different default model")
	}
	for i := 0; i < 3; i++ {
		m, _, err := f.app.account.readyAccountModel(context.Background(), p.Name(), integrationModel)
		if err != nil || m.ID != integrationModel || m.GroupID != 7 || m.Endpoint != "/chat/completions" {
			t.Fatal("selection changed")
		}
	}
	if f.catalogs.Load() != 2 || f.sessions.Load() != 2 {
		t.Fatal("fresh metadata caused additional network calls")
	}
}

func TestAccountProviderStaleCatalogNeverSubstitutesRemovedSelection(t *testing.T) {
	for _, replacement := range []orcaaccount.Model{
		{ID: "replacement", GroupID: 7, Protocol: "openai", Endpoint: "/chat/completions"},
		{ID: integrationModel, GroupID: 8, Protocol: "openai", Endpoint: "/chat/completions"},
		{ID: integrationModel, GroupID: 7, Protocol: "openai", Endpoint: "/responses"},
	} {
		t.Run(replacement.ID+replacement.Endpoint, func(t *testing.T) {
			f := newAccountStreamFixture(t, "openai")
			p := f.registeredProvider()
			before := config.LoadForEdit(config.UserConfigPath()).DefaultModel
			f.setCatalogHook(func(r *http.Request) (*http.Response, error) {
				return integrationJSON(r, orcaaccount.Models{Currency: "CNY", Models: []orcaaccount.Model{replacement}})
			})
			expireAccountCatalog(f.app.account)
			stream, err := p.Stream(context.Background(), integrationRequest())
			if err == nil || !strings.Contains(err.Error(), "no longer available") || stream != nil {
				t.Fatal("removed selection was silently replaced")
			}
			if f.credentials.Load() != 0 || f.inferences.Load() != 0 || len(f.app.OrcaAccountReceipts()) != 0 {
				t.Fatal("removed model reached paid admission")
			}
			if config.LoadForEdit(config.UserConfigPath()).DefaultModel != before {
				t.Fatal("default selection was changed")
			}
			if _, err := p.Stream(context.Background(), integrationRequest()); err == nil {
				t.Fatal("removed model was readmitted")
			}
			if f.catalogs.Load() != 2 {
				t.Fatal("fresh removed-model result caused an unnecessary retry")
			}
		})
	}
}

func TestAccountProviderStaleCatalogFailureRequiresExplicitRetry(t *testing.T) {
	f := newAccountStreamFixture(t, "openai")
	f.settled.Store(true)
	p := f.registeredProvider()
	var fail atomic.Bool
	fail.Store(true)
	f.setCatalogHook(func(r *http.Request) (*http.Response, error) {
		if fail.Load() {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("synthetic unavailable")), Request: r}, nil
		}
		return freshAccountCatalog(r)
	})
	expireAccountCatalog(f.app.account)
	if _, err := p.Stream(context.Background(), integrationRequest()); !errors.Is(err, orcaaccount.ErrNetwork) {
		t.Fatalf("refresh failure was not returned: %v", err)
	}
	s := f.app.account
	s.mu.Lock()
	_, retained := s.catalog[p.Name()]
	s.mu.Unlock()
	if !retained || !f.app.OrcaAccountStatus().ModelsStale || f.credentials.Load() != 0 || f.inferences.Load() != 0 {
		t.Fatal("transient failure either discarded display metadata or admitted stale paid work")
	}
	doc, err := f.document()
	if err != nil || len(doc.Records) != 0 {
		t.Fatal("failed refresh reserved a paid request identity")
	}
	fail.Store(false)
	stream, err := p.Stream(context.Background(), integrationRequest())
	if err != nil {
		t.Fatal(err)
	}
	drainIntegrationStream(t, stream, false)
	f.assertSettled()
	if f.catalogs.Load() != 3 || f.sessions.Load() != 3 {
		t.Fatal("explicit retry did not perform one fresh lookup")
	}
}

func TestAccountCatalogAutomaticAndManualRefreshShareFlight(t *testing.T) {
	f := newAccountStreamFixture(t, "openai")
	s := f.app.account
	p := f.registeredProvider()
	entered := make(chan struct{})
	gate, release := lifecycleGate(t)
	f.setCatalogHook(func(r *http.Request) (*http.Response, error) {
		close(entered)
		select {
		case <-gate:
			return freshAccountCatalog(r)
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})
	expireAccountCatalog(s)
	manual := make(chan error, 1)
	go func() { manual <- f.app.OrcaAccountRefreshModels() }()
	lifecycleWait(t, entered)
	results := make(chan error, 12)
	for i := 0; i < cap(results); i++ {
		ctx := &accountCatalogWaiter{Context: context.Background(), joined: make(chan struct{})}
		go func() {
			m, _, err := s.readyAccountModel(ctx, p.Name(), integrationModel)
			if err == nil && (m.ID != integrationModel || m.GroupID != 7) {
				err = errors.New("selection changed")
			}
			results <- err
		}()
		lifecycleWait(t, ctx.joined)
	}
	release()
	if err := lifecycleResult(t, manual); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < cap(results); i++ {
		if err := lifecycleResult(t, results); err != nil {
			t.Fatal(err)
		}
	}
	if f.sessions.Load() != 2 || f.catalogs.Load() != 2 || f.credentials.Load() != 0 || f.inferences.Load() != 0 {
		t.Fatal("concurrent refreshes were not shared metadata-only work")
	}
}

func TestAccountProviderCanceledCatalogWaiterLeavesSharedFetchAlive(t *testing.T) {
	f := newAccountStreamFixture(t, "openai")
	f.settled.Store(true)
	p := f.registeredProvider()
	expireAccountCatalog(f.app.account)
	preCanceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := p.Stream(preCanceled, integrationRequest()); !errors.Is(err, context.Canceled) || f.catalogs.Load() != 1 {
		t.Fatal("pre-canceled caller started a refresh")
	}
	entered := make(chan context.Context, 1)
	gate, release := lifecycleGate(t)
	f.setCatalogHook(func(r *http.Request) (*http.Response, error) {
		entered <- r.Context()
		select {
		case <-gate:
			return freshAccountCatalog(r)
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	canceled := make(chan error, 1)
	go func() { _, err := p.Stream(ctx, integrationRequest()); canceled <- err }()
	var fetchCtx context.Context
	select {
	case fetchCtx = <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not start")
	}
	other := &accountCatalogWaiter{Context: context.Background(), joined: make(chan struct{})}
	survivor := make(chan error, 1)
	go func() {
		_, _, err := f.app.account.readyAccountModel(other, p.Name(), integrationModel)
		survivor <- err
	}()
	lifecycleWait(t, other.joined)
	cancel()
	select {
	case err := <-canceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled waiter: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled waiter blocked on shared catalog IO")
	}
	if fetchCtx.Err() != nil || f.credentials.Load() != 0 || f.inferences.Load() != 0 {
		t.Fatal("canceling one waiter killed shared work or admitted inference")
	}
	release()
	if err := lifecycleResult(t, survivor); err != nil {
		t.Fatal(err)
	}
	stream, err := p.Stream(context.Background(), integrationRequest())
	if err != nil {
		t.Fatal(err)
	}
	drainIntegrationStream(t, stream, false)
	f.assertSettled()
	if f.catalogs.Load() != 2 {
		t.Fatal("surviving refresh was repeated")
	}
}

func TestAccountProviderLateCatalogAfterLogoutCannotAdmitOrCommit(t *testing.T) {
	f := newAccountStreamFixture(t, "openai")
	p := f.registeredProvider()
	s := f.app.account
	entered := make(chan struct{})
	gate, release := lifecycleGate(t)
	f.setCatalogHook(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-gate // Deliberately emulate a transport returning after cancellation.
		return freshAccountCatalog(r)
	})
	expireAccountCatalog(s)
	done := make(chan error, 1)
	go func() { _, err := p.Stream(context.Background(), integrationRequest()); done <- err }()
	lifecycleWait(t, entered)
	s.modelsMu.Lock()
	oldFlight := s.modelsFlight
	s.modelsMu.Unlock()
	if err := f.app.OrcaAccountLogout(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, orcaaccount.ErrStale) {
			t.Fatalf("logout did not invalidate catalog waiter: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("logout waiter was retained by late transport")
	}
	if f.credentials.Load() != 0 || f.inferences.Load() != 0 {
		t.Fatal("logged-out refresh reached inference")
	}
	// A new generation can sign in without the previous generation's response.
	f.setCatalogHook(nil)
	if err := f.app.OrcaAccountLogin(); err != nil {
		t.Fatal(err)
	}
	lifecycleSignedIn(t, f.app, integrationSubject)
	gen := s.client.CurrentSessionGeneration()
	release()
	lifecycleWait(t, oldFlight.done)
	if !f.app.OrcaAccountStatus().Authenticated || f.app.OrcaAccountStatus().ModelsStale || s.client.CurrentSessionGeneration() != gen {
		t.Fatal("old refresh invalidated new account metadata")
	}
	if _, current, err := s.readyAccountModel(context.Background(), p.Name(), integrationModel); err != nil || current != gen {
		t.Fatal("old response replaced the current generation catalog")
	}
}
