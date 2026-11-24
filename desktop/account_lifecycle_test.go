package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
)

type lifecycleStore struct {
	mu           sync.Mutex
	data         []byte
	beforeDelete func()
}

func (s *lifecycleStore) Load(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(s.data) == 0 {
		return nil, orcaaccount.ErrNotFound
	}
	return bytes.Clone(s.data), nil
}
func (s *lifecycleStore) Save(ctx context.Context, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.data = bytes.Clone(b)
	return nil
}
func (s *lifecycleStore) Delete(context.Context) error {
	if s.beforeDelete != nil {
		s.beforeDelete()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.data)
	s.data = nil
	return nil
}

type lifecycleTransport func(*http.Request) (*http.Response, error)

func (f lifecycleTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type lifecycleProof struct{ nonce, challenge, device, redirect, subject, session string }

type lifecycleFixture struct {
	t      *testing.T
	app    *App
	store  *lifecycleStore
	server *httptest.Server
	mu     sync.Mutex
	next   int
	proofs map[string]lifecycleProof
	access map[string]lifecycleProof
	hook   func(http.ResponseWriter, *http.Request) bool
}

func lifecycleProfile(t *testing.T) string {
	t.Helper()
	oldChannel := channel
	wasIsolated := config.IsolatedProfileEnabled()
	oldHome, err := config.ProfileHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = config.SetIsolatedProfileHome(root); err != nil {
		t.Fatal(err)
	}
	channel = betaChannel
	t.Cleanup(func() {
		channel = oldChannel
		if wasIsolated {
			_ = config.SetIsolatedProfileHome(oldHome)
		} else {
			_ = config.SetIsolatedProfileHome("")
		}
	})
	return root
}

func newLifecycleFixture(t *testing.T, hook func(http.ResponseWriter, *http.Request) bool, store *lifecycleStore) *lifecycleFixture {
	t.Helper()
	lifecycleProfile(t)
	if store == nil {
		store = &lifecycleStore{}
	}
	f := &lifecycleFixture{t: t, store: store, proofs: map[string]lifecycleProof{}, access: map[string]lifecycleProof{}, hook: hook}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	local, _ := url.Parse(f.server.URL)
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	client, err := orcaaccount.New(orcaaccount.Options{Store: store, OpenBrowser: f.browser, HTTPClient: &http.Client{Transport: lifecycleTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "sub.aichat.diy" {
			return nil, errors.New("test rejected non-issuer request")
		}
		r = r.Clone(r.Context())
		r.URL.Scheme = local.Scheme
		r.URL.Host = local.Host
		r.Host = local.Host
		return transport.RoundTrip(r)
	})}})
	if err != nil {
		t.Fatal(err)
	}
	f.app = &App{account: newAccountDesktop(context.Background(), client)}
	t.Cleanup(f.app.stopOrcaAccount)
	return f
}

func (f *lifecycleFixture) browser(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	q := u.Query()
	f.mu.Lock()
	f.next++
	n := f.next
	code := fmt.Sprintf("synthetic-code-%d", n)
	f.proofs[code] = lifecycleProof{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), device: q.Get("device_id"), redirect: q.Get("redirect_uri"), subject: fmt.Sprintf("subject-%d", n), session: fmt.Sprintf("session-%d", n)}
	f.mu.Unlock()
	callback, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		return err
	}
	if callback.Hostname() != "127.0.0.1" || callback.Path != "/callback" {
		return errors.New("test rejected non-loopback callback")
	}
	callback.RawQuery = url.Values{"state": {q.Get("state")}, "iss": {orcaaccount.Issuer}, "code": {code}}.Encode()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, callback.String(), nil)
	if err != nil {
		return err
	}
	resp, err := f.server.Client().Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != 200 {
		return errors.New("synthetic callback rejected")
	}
	return nil
}

func lifecycleJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *lifecycleFixture) serve(w http.ResponseWriter, r *http.Request) {
	if f.hook != nil && f.hook(w, r) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case orcaaccount.APIPrefix + "/token":
		_ = r.ParseForm()
		proof, ok := f.proofs[r.Form.Get("code")]
		h := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || r.Form.Get("client_id") != orcaaccount.ClientID || base64.RawURLEncoding.EncodeToString(h[:]) != proof.challenge || r.Form.Get("redirect_uri") != proof.redirect {
			w.WriteHeader(400)
			return
		}
		delete(f.proofs, r.Form.Get("code"))
		access := "synthetic-access-" + proof.subject
		f.access[access] = proof
		lifecycleJSON(w, map[string]any{"access_token": access, "refresh_token": "synthetic-refresh-" + proof.subject, "token_type": "Bearer", "expires_in": 300, "subject": proof.subject, "issuer": orcaaccount.Issuer, "nonce": proof.nonce, "scope": "desktop", "session_id": proof.session, "device_id": proof.device})
	case orcaaccount.APIPrefix + "/session":
		proof, ok := f.access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			w.WriteHeader(401)
			return
		}
		lifecycleJSON(w, orcaaccount.Profile{Subject: proof.subject, DisplayName: proof.subject, SessionID: proof.session, DeviceID: proof.device, ExpiresAt: time.Now().Add(time.Hour), Groups: []orcaaccount.Group{{ID: 7, Name: "Synthetic group"}}})
	case orcaaccount.APIPrefix + "/models":
		proof, ok := f.access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			w.WriteHeader(401)
			return
		}
		lifecycleJSON(w, orcaaccount.Models{Currency: "CNY", Models: []orcaaccount.Model{{ID: "model-" + proof.subject, Name: "Synthetic model", GroupID: 7, Protocol: "openai", Endpoint: "/chat/completions"}}})
	case orcaaccount.APIPrefix + "/revoke":
		w.WriteHeader(204)
	default:
		w.WriteHeader(404)
	}
}

func lifecycleWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for lifecycle barrier")
	}
}

func lifecycleResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for lifecycle result")
		return nil
	}
}

func lifecycleSignedIn(t *testing.T, a *App, subject string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		v := a.OrcaAccountStatus()
		if v.Authenticated && a.account.client.Snapshot().Profile.Subject == subject {
			return
		}
		if v.State == "error" {
			t.Fatalf("sign-in failed: %s", v.Error)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("sign-in did not finish")
}

func lifecycleGate(t *testing.T) (chan struct{}, func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	return gate, release
}

func TestAccountLifecycleLogoutSerializesNewLoginAndCatalog(t *testing.T) {
	entered := make(chan struct{})
	var release <-chan struct{}
	f := newLifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == orcaaccount.APIPrefix+"/revoke" {
			close(entered)
			<-release
		}
		return false
	}, nil)
	gate, unblock := lifecycleGate(t)
	release = gate
	if err := f.app.OrcaAccountLogin(); err != nil {
		t.Fatal(err)
	}
	lifecycleSignedIn(t, f.app, "subject-1")
	logout := make(chan error, 1)
	go func() { logout <- f.app.OrcaAccountLogout() }()
	lifecycleWait(t, entered)
	if view := f.app.OrcaAccountStatus(); view.State != "logging_out" || view.Authenticated {
		t.Fatal("logout did not publish the UI logging_out contract before revocation completed")
	}
	started := make(chan struct{})
	login := make(chan error, 1)
	go func() { close(started); login <- f.app.OrcaAccountLogin() }()
	lifecycleWait(t, started)
	select {
	case err := <-login:
		t.Fatalf("login bypassed logout gate: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	unblock()
	if err := lifecycleResult(t, logout); err != nil {
		t.Fatal(err)
	}
	if err := lifecycleResult(t, login); err != nil {
		t.Fatal(err)
	}
	lifecycleSignedIn(t, f.app, "subject-2")
	assertLifecycleCatalog(t, f.app, "subject-2")
}

func TestMissingDesktopServiceDoesNotOpenLoginOrChangeOtherSources(t *testing.T) {
	var probes atomic.Int32
	f := newLifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == orcaaccount.APIPrefix+"/session" {
			probes.Add(1)
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Error("availability probe carried credentials")
			}
			w.WriteHeader(http.StatusNotFound)
			return true
		}
		t.Error("unexpected request after missing desktop route")
		return true
	}, nil)
	if err := f.app.OrcaAccountLogin(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		view := f.app.OrcaAccountStatus()
		if view.State == "error" {
			if view.Error != orcaaccount.ErrServiceUnavailable.Error() || view.Authenticated {
				t.Fatal("wrong unavailable status", view)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("unavailable login remained pending")
		}
		time.Sleep(time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.next != 0 || probes.Load() != 1 {
		t.Fatal("browser login opened or availability was retried")
	}
}

func TestAccountLifecycleCancelErasesBeforeNewLogin(t *testing.T) {
	deleteEntered, modelsEntered := make(chan struct{}), make(chan struct{})
	var blockDelete atomic.Bool
	var deleteRelease, modelsRelease <-chan struct{}
	store := &lifecycleStore{beforeDelete: func() {
		if blockDelete.CompareAndSwap(true, false) {
			close(deleteEntered)
			<-deleteRelease
		}
	}}
	f := newLifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == orcaaccount.APIPrefix+"/models" && r.Header.Get("Authorization") == "Bearer synthetic-access-subject-1" {
			close(modelsEntered)
			<-modelsRelease
		}
		return false
	}, store)
	deleteGate, releaseDelete := lifecycleGate(t)
	deleteRelease = deleteGate
	modelGate, releaseModel := lifecycleGate(t)
	modelsRelease = modelGate
	if err := f.app.OrcaAccountLogin(); err != nil {
		t.Fatal(err)
	}
	lifecycleWait(t, modelsEntered)
	blockDelete.Store(true)
	canceled := make(chan error, 1)
	go func() { canceled <- f.app.OrcaAccountCancelLogin() }()
	lifecycleWait(t, deleteEntered)
	login := make(chan error, 1)
	started := make(chan struct{})
	go func() { close(started); login <- f.app.OrcaAccountLogin() }()
	lifecycleWait(t, started)
	select {
	case err := <-login:
		t.Fatalf("login bypassed cancellation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	releaseDelete()
	if err := lifecycleResult(t, canceled); err != nil {
		t.Fatal(err)
	}
	if err := lifecycleResult(t, login); err != nil {
		t.Fatal(err)
	}
	lifecycleSignedIn(t, f.app, "subject-2")
	releaseModel()
	assertLifecycleCatalog(t, f.app, "subject-2")
	data, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Subject string `json:"subject"`
	}
	if json.Unmarshal(data, &saved) != nil || saved.Subject != "subject-2" {
		t.Fatal("cancel or late login replaced the new saved account")
	}
}

func TestAccountLifecycleInvalidationDuringCatalogSaveRejectsCommit(t *testing.T) {
	f := newLifecycleFixture(t, nil, nil)
	s := f.app.account
	f.app.configWriteMu.Lock()
	locked := true
	defer func() {
		if locked {
			f.app.configWriteMu.Unlock()
		}
	}()
	if err := f.app.OrcaAccountLogin(); err != nil {
		t.Fatal(err)
	}
	// opCancel is cleared immediately before persistence. Holding configWriteMu
	// leaves login between its initial identity check and its final UI commit.
	receiptEventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.opCancel == nil && s.view.State == "signing_in" && s.client.Snapshot().Authenticated
	})
	if err := s.client.CancelContexts(); err != nil {
		t.Fatal(err)
	}
	f.app.configWriteMu.Unlock()
	locked = false
	receiptEventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.view.State == "error" && !s.view.Authenticated
	})
	// Acquire transitionMu so catalog cleanup has completed, not merely the view.
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	s.mu.Lock()
	if len(s.catalog) != 0 {
		t.Error("invalidated login published an active catalog")
	}
	s.mu.Unlock()
	for _, p := range config.LoadForEdit(config.UserConfigPath()).Providers {
		if p.Kind == accountProviderKind {
			t.Fatal("invalidated login left its catalog persisted")
		}
	}
}

func assertLifecycleCatalog(t *testing.T, a *App, subject string) {
	t.Helper()
	s := a.account
	s.mu.Lock()
	if !s.view.Authenticated || len(s.catalog) != 1 {
		s.mu.Unlock()
		t.Fatal("current catalog/view lost")
	}
	var name string
	for key, m := range s.catalog {
		name = key
		if m.ID != "model-"+subject {
			s.mu.Unlock()
			t.Fatal("stale catalog committed")
		}
	}
	s.mu.Unlock()
	cfg := config.LoadForEdit(config.UserConfigPath())
	found := 0
	for _, p := range cfg.Providers {
		if p.Kind == accountProviderKind {
			found++
			if p.Name != name || p.Model != "model-"+subject {
				t.Fatal("old operation overwrote persisted catalog")
			}
		}
	}
	if found != 1 {
		t.Fatal("new catalog was erased or duplicated")
	}
}

func TestAccountLifecycleExpiredAuthReconcilesAndAllowsLogin(t *testing.T) {
	for _, poll := range []bool{true, false} {
		t.Run(fmt.Sprintf("poll=%v", poll), func(t *testing.T) {
			f := newLifecycleFixture(t, nil, nil)
			if err := f.app.OrcaAccountLogin(); err != nil {
				t.Fatal(err)
			}
			lifecycleSignedIn(t, f.app, "subject-1")
			s := f.app.account
			s.mu.Lock()
			s.wallet = OrcaWalletView{Cash: "stale"}
			s.walletAt = time.Now()
			s.receipts["old"] = accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: "old", Status: "pending"}}
			s.receiptOrder = []string{"old"}
			s.mu.Unlock()
			if err := s.client.CancelContexts(); err != nil {
				t.Fatal(err)
			}
			if poll {
				if got := f.app.OrcaAccountStatus(); got.Authenticated || got.State != "error" {
					t.Fatal("stale authentication reported")
				}
				s.mu.Lock()
				stale := s.view.Authenticated || len(s.catalog) != 0 || len(s.receipts) != 0 || !s.walletAt.IsZero()
				s.mu.Unlock()
				if stale {
					t.Fatal("only the returned status was changed")
				}
				if got := f.app.OrcaAccountWallet(); got.Error == "" || got.Cash != "" {
					t.Fatal("expired wallet cache was returned")
				}
				if len(f.app.OrcaAccountReceipts()) != 0 {
					t.Fatal("expired receipts leaked into new account flow")
				}
			}
			if err := f.app.OrcaAccountLogin(); err != nil {
				t.Fatalf("expired auth blocked login: %v", err)
			}
			lifecycleSignedIn(t, f.app, "subject-2")
			assertLifecycleCatalog(t, f.app, "subject-2")
		})
	}
}

func TestAccountLifecycleShutdownRejectsLateLoginCommit(t *testing.T) {
	entered := make(chan struct{})
	var release <-chan struct{}
	f := newLifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == orcaaccount.APIPrefix+"/models" {
			close(entered)
			<-release
		}
		return false
	}, nil)
	gate, unblock := lifecycleGate(t)
	release = gate
	if err := f.app.OrcaAccountLogin(); err != nil {
		t.Fatal(err)
	}
	lifecycleWait(t, entered)
	before, err := f.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.app.stopOrcaAccount()
	unblock()
	if err = f.app.OrcaAccountLogin(); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown allowed another login: %v", err)
	}
	after, err := f.store.Load(context.Background())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("shutdown deleted saved session")
	}
	if !f.app.account.client.Snapshot().Closed || f.app.OrcaAccountStatus().Authenticated {
		t.Fatal("shutdown remained authenticated")
	}
	if _, err = os.Stat(config.UserConfigPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("late login persisted a catalog after shutdown")
	}
}

func TestAccountLifecycleWalletDeduplicatesGrantsNotGroups(t *testing.T) {
	w := orcaaccount.Wallet{Currency: "CNY", CashMicros: 9007199254740993, HeldMicros: 1, AvailableMicros: 9007199254740992, ScopedGifts: []orcaaccount.ScopedGift{
		{ID: "grant-a", GroupID: 7, GroupIDs: []int64{7, 8}, AvailableMicros: 2500000},
		{ID: "grant-a", GroupID: 8, GroupIDs: []int64{7, 8}, AvailableMicros: 2500000},
		{ID: "grant-b", GroupID: 7, AvailableMicros: 3000000},
		{ID: "grant-b", GroupID: 9, AvailableMicros: 3000000},
	}}
	profile := orcaaccount.Profile{Groups: []orcaaccount.Group{{ID: 7, Name: "Seven"}, {ID: 8, Name: "Eight"}}}
	v := accountWalletView(w, profile)
	if len(v.Gifts) != 2 || v.Gifts[0].Available != "2.500000" || v.Gifts[0].Groups != "Seven, Eight" || v.Gifts[1].Available != "3.000000" || v.Gifts[1].Groups != "Seven, group 9" {
		t.Fatalf("wrong scoped grant projection: %+v", v.Gifts)
	}
	if v.Cash != "9007199254.740993" || v.Available != "9007199254.740992" || v.Held != "0.000001" {
		t.Fatal("gift dedup changed or rounded cash")
	}
}

func TestAccountLifecycleCatalogPersistsOnlyGlobalNonsecretMetadata(t *testing.T) {
	root := lifecycleProfile(t)
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)
	user := config.Default()
	user.Language = "en"
	user.Providers = []config.ProviderEntry{{Name: "manual", Kind: "openai", Model: "manual-model", APIKeyEnv: "SYNTHETIC_MANUAL_KEY"}}
	if err := user.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	project := config.Default()
	project.Language = "zh"
	project.Providers = []config.ProviderEntry{{Name: "project-only", Kind: "openai", Model: "project-model", APIKeyEnv: "PROJECT_ONLY_SECRET"}}
	projectPath := config.ProjectConfigPath(workspace)
	if err := project.SaveTo(projectPath); err != nil {
		t.Fatal(err)
	}
	projectBefore, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{}
	models := accountCatalog("synthetic-subject", []orcaaccount.Model{{ID: "exact-model", GroupID: 7, Protocol: "openai", Endpoint: "/chat/completions"}})
	if err = a.persistAccountCatalog(models); err != nil {
		t.Fatal(err)
	}
	got := config.LoadForEdit(config.UserConfigPath())
	if got.Language != "en" || len(got.Providers) != 2 {
		t.Fatal("project config was merged into global config")
	}
	for _, p := range got.Providers {
		if p.Name == "project-only" || (p.Kind == accountProviderKind && (p.APIKey() != "" || p.APIKeyEnv != "")) {
			t.Fatal("project/provider secret persisted")
		}
	}
	data, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("PROJECT_ONLY_SECRET")) || bytes.Contains(data, []byte("access_token")) || bytes.Contains(data, []byte("refresh_token")) {
		t.Fatal("secret fields entered account catalog")
	}
	if err = a.persistAccountCatalog(nil); err != nil {
		t.Fatal(err)
	}
	got = config.LoadForEdit(config.UserConfigPath())
	if len(got.Providers) != 1 || got.Providers[0].Name != "manual" {
		t.Fatal("catalog cleanup touched manual provider")
	}
	projectAfter, err := os.ReadFile(projectPath)
	if err != nil || !bytes.Equal(projectBefore, projectAfter) {
		t.Fatal("project configuration was modified")
	}
	raw, err := json.Marshal(accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: "public", Status: "unknown"}, serverID: "private-server-id", generation: 123})
	if err != nil || bytes.Contains(raw, []byte("private-server-id")) || bytes.Contains(raw, []byte("generation")) {
		t.Fatal("private receipt fields leaked")
	}
}
