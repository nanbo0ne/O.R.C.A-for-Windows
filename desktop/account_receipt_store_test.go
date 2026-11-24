package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
)

func receiptLogin(t *testing.T, f *lifecycleFixture, store orcaaccount.Store) *accountDesktop {
	t.Helper()
	f.app.account.receiptJournal = &accountReceiptJournal{store: store}
	if err := f.app.OrcaAccountLogin(); err != nil {
		t.Fatal(err)
	}
	lifecycleSignedIn(t, f.app, "subject-1")
	return f.app.account
}

func receiptRestart(t *testing.T, f *lifecycleFixture, store orcaaccount.Store) *accountDesktop {
	t.Helper()
	f.app.stopOrcaAccount()
	local, _ := url.Parse(f.server.URL)
	c, err := orcaaccount.New(orcaaccount.Options{Store: f.store, HTTPClient: &http.Client{Transport: lifecycleTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "sub.aichat.diy" {
			return nil, errors.New("non-issuer recovery request rejected")
		}
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host, r.Host = local.Scheme, local.Host, local.Host
		return f.server.Client().Transport.RoundTrip(r)
	})}})
	if err != nil {
		t.Fatal(err)
	}
	f.app = &App{account: newAccountDesktop(context.Background(), c)}
	f.app.account.receiptJournal = &accountReceiptJournal{store: store}
	t.Cleanup(f.app.stopOrcaAccount)
	if err := f.app.beginAccountLogin(true); err != nil {
		t.Fatal(err)
	}
	lifecycleSignedIn(t, f.app, "subject-1")
	return f.app.account
}

func receiptPut(t *testing.T, s *accountDesktop, id, server, status, cost string) {
	t.Helper()
	gen := s.client.CurrentSessionGeneration()
	r := accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: id, TurnID: "turn-test", Purpose: "turn", Status: status, Cost: cost, Currency: "CNY"}, serverID: server, generation: gen}
	if err := s.putReceipt(r); err != nil {
		t.Fatal(err)
	}
	if err := s.persistAccountReceipt(context.Background(), id, gen); err != nil {
		t.Fatal(err)
	}
}

func receiptEventually(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("receipt condition did not complete")
}

func TestAccountReceiptsRestartQueriesSameGrantOnly(t *testing.T) {
	var queries atomic.Int32
	f := newLifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.Path, "/requests/") {
			queries.Add(1)
			if r.Method != "GET" || r.URL.Path != orcaaccount.APIPrefix+"/requests/server-pending" || r.Header.Get("Authorization") != "Bearer synthetic-access-subject-1" {
				t.Error("recovery sent an unexpected request")
			}
			amount := int64(9007199254740993)
			lifecycleJSON(w, orcaaccount.Settlement{RequestID: "server-pending", Status: orcaaccount.SettlementSettled, Currency: "CNY", SettledMicros: &amount})
			return true
		}
		return false
	}, nil)
	store := &lifecycleStore{}
	s := receiptLogin(t, f, store)
	receiptPut(t, s, "req-pending", "server-pending", "pending", "")
	receiptPut(t, s, "req-unknown", "", "unknown", "")
	receiptPut(t, s, "req-settled", "server-settled", "settled", cnyMicros(math.MaxInt64))
	before, _ := store.Load(context.Background())
	for _, forbidden := range []string{"access_token", "refresh_token", "synthetic-access", "synthetic-refresh", "prompt", "messages", "generation"} {
		if bytes.Contains(before, []byte(forbidden)) {
			t.Fatalf("journal persisted forbidden field %s", forbidden)
		}
	}
	s = receiptRestart(t, f, store)
	receiptEventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.receipts["req-pending"].Status == "settled"
	})
	s.mu.Lock()
	if s.receipts["req-pending"].Cost != "9007199254.740993" || s.receipts["req-unknown"].Cost != "" || s.receipts["req-settled"].Cost != cnyMicros(math.MaxInt64) {
		t.Error("restart lost exact currency or invented a known cost")
	}
	s.mu.Unlock()
	if queries.Load() != 1 {
		t.Fatal("recovery replayed or queried an unrecoverable receipt")
	}
}

func TestAccountReceiptsGrantChangeAndAccountSwitch(t *testing.T) {
	var queries atomic.Int32
	f := newLifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.Path, "/requests/") {
			queries.Add(1)
			w.WriteHeader(404)
			return true
		}
		return false
	}, nil)
	store := &lifecycleStore{}
	s := receiptLogin(t, f, store)
	receiptPut(t, s, "req-old", "server-old", "pending", "")
	receiptPut(t, s, "req-done", "server-done", "settled", "0.000000")
	// Simulate a valid saved authorization with the same subject, new grant.
	f.store.mu.Lock()
	var session map[string]json.RawMessage
	if err := json.Unmarshal(f.store.data, &session); err != nil {
		t.Fatal(err)
	}
	session["session_id"] = json.RawMessage(`"session-new"`)
	f.store.data, _ = json.Marshal(session)
	f.store.mu.Unlock()
	f.mu.Lock()
	p := f.access["synthetic-access-subject-1"]
	p.session = "session-new"
	f.access["synthetic-access-subject-1"] = p
	f.mu.Unlock()
	s = receiptRestart(t, f, store)
	s.mu.Lock()
	r := s.receipts["req-old"]
	done := s.receipts["req-done"]
	s.mu.Unlock()
	if r.Status != "unknown" || r.Cost != "" || r.generation != 0 || done.Cost != "0.000000" {
		t.Fatal("old grant was revived or settled history changed")
	}
	s.recoverAccountReceipts(s.client.CurrentSessionGeneration(), []accountReceipt{r, done})
	if queries.Load() != 0 {
		t.Fatal("old grant queried with new grant")
	}
	if err := f.app.OrcaAccountLogout(); err != nil {
		t.Fatal(err)
	}
	// The restarted client uses a deliberately disabled system browser. Login on
	// the original fixture transport creates the second synthetic account.
	f.app.stopOrcaAccount()
	local, _ := url.Parse(f.server.URL)
	c, err := orcaaccount.New(orcaaccount.Options{Store: f.store, OpenBrowser: f.browser, HTTPClient: &http.Client{Transport: lifecycleTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "sub.aichat.diy" {
			return nil, errors.New("non-issuer rejected")
		}
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host, r.Host = local.Scheme, local.Host, local.Host
		return f.server.Client().Transport.RoundTrip(r)
	})}})
	if err != nil {
		t.Fatal(err)
	}
	f.app = &App{account: newAccountDesktop(context.Background(), c)}
	f.app.account.receiptJournal = &accountReceiptJournal{store: store}
	t.Cleanup(f.app.stopOrcaAccount)
	if err := f.app.OrcaAccountLogin(); err != nil {
		t.Fatal(err)
	}
	lifecycleSignedIn(t, f.app, "subject-2")
	if len(f.app.OrcaAccountReceipts()) != 0 {
		t.Fatal("other account receipts leaked into UI")
	}
	raw, _ := store.Load(context.Background())
	if bytes.Contains(raw, []byte("server-old")) || bytes.Contains(raw, []byte("subject-1")) {
		t.Fatal("other account retained in active journal")
	}
}

type receiptFaultStore struct {
	lifecycleStore
	fail   atomic.Bool
	onSave func()
}

func (s *receiptFaultStore) Save(ctx context.Context, data []byte) error {
	if s.onSave != nil {
		s.onSave()
	}
	if s.fail.Load() {
		return errors.New("private filesystem path and secret must not surface")
	}
	return s.lifecycleStore.Save(ctx, data)
}

func TestAccountReceiptStorageFailureAndLockIsolation(t *testing.T) {
	f := newLifecycleFixture(t, nil, nil)
	store := &receiptFaultStore{}
	s := f.app.account
	store.onSave = func() {
		if !s.mu.TryLock() {
			t.Error("journal write held account view lock")
		} else {
			s.mu.Unlock()
		}
		if !s.transitionMu.TryLock() {
			t.Error("journal write held transition lock")
		} else {
			s.transitionMu.Unlock()
		}
	}
	receiptLogin(t, f, store)
	before, _ := store.Load(context.Background())
	store.fail.Store(true)
	gen := s.client.CurrentSessionGeneration()
	s.putReceipt(accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: "req-failed", Purpose: "private prompt text", Status: "unknown", Currency: "CNY"}, generation: gen})
	err := s.persistAccountReceipt(context.Background(), "req-failed", gen)
	if !errors.Is(err, errAccountReceiptStore) || strings.Contains(err.Error(), "private") {
		t.Fatal("storage failure hidden or unsanitized")
	}
	after, _ := store.Load(context.Background())
	if !bytes.Equal(before, after) || f.app.OrcaAccountStatus().ReceiptError == "" {
		t.Fatal("failed write lost previous journal or hid recovery warning")
	}
	store.fail.Store(false)
	if err := s.persistAccountReceipt(context.Background(), "req-failed", gen); err != nil {
		t.Fatal(err)
	}
	if f.app.OrcaAccountStatus().ReceiptError != "" {
		t.Fatal("successful retry retained a resolved persistence warning")
	}
	after, _ = store.Load(context.Background())
	if bytes.Contains(after, []byte("private prompt")) {
		t.Fatal("untrusted purpose text persisted")
	}
}

func TestAccountReceiptWarningsResolvePerIDAndResetPerGeneration(t *testing.T) {
	f := newLifecycleFixture(t, nil, nil)
	store := &receiptFaultStore{}
	s := receiptLogin(t, f, store)
	gen := s.client.CurrentSessionGeneration()
	store.fail.Store(true)
	for _, id := range []string{"req-failed-a", "req-failed-b"} {
		if err := s.putReceipt(accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: id, Purpose: "turn", Status: "unknown", Currency: "CNY"}, generation: gen}); err != nil {
			t.Fatal(err)
		}
		if err := s.persistAccountReceipt(context.Background(), id, gen); !errors.Is(err, errAccountReceiptStore) {
			t.Fatalf("expected synthetic storage failure: %v", err)
		}
	}
	if f.app.OrcaAccountStatus().ReceiptError == "" {
		t.Fatal("failed receipt writes were not reported")
	}
	store.fail.Store(false)
	for i := 0; i < 2; i++ {
		if err := s.persistAccountReceipt(context.Background(), "req-failed-a", gen); err != nil {
			t.Fatal(err)
		}
		if f.app.OrcaAccountStatus().ReceiptError == "" {
			t.Fatal("one successful receipt hid another receipt's failed write")
		}
	}
	s.mu.Lock()
	_, aFailed := s.receiptFailures["req-failed-a"]
	_, bFailed := s.receiptFailures["req-failed-b"]
	correctGeneration := s.receiptFailureGen == gen
	s.mu.Unlock()
	if aFailed || !bFailed || !correctGeneration {
		t.Fatal("persistence failures were not tracked by current-generation request ID")
	}
	if err := s.persistAccountReceipt(context.Background(), "req-failed-b", gen); err != nil {
		t.Fatal(err)
	}
	if f.app.OrcaAccountStatus().ReceiptError != "" {
		t.Fatal("the final successful retry did not clear the warning")
	}
	store.fail.Store(true)
	if err := s.persistAccountReceipt(context.Background(), "req-failed-b", gen); !errors.Is(err, errAccountReceiptStore) {
		t.Fatal("repeated failure was not surfaced")
	}
	if err := f.app.OrcaAccountLogout(); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	reset := len(s.receiptFailures) == 0 && s.receiptFailureGen == 0 && s.view.ReceiptError == ""
	s.mu.Unlock()
	if !reset {
		t.Fatal("account reset retained receipt failure state")
	}
	store.fail.Store(false)
	if err := f.app.OrcaAccountLogin(); err != nil {
		t.Fatal(err)
	}
	lifecycleSignedIn(t, f.app, "subject-2")
	if err := s.persistAccountReceipt(context.Background(), "req-failed-b", gen); !errors.Is(err, orcaaccount.ErrStale) {
		t.Fatal("old-generation persistence attempt was admitted")
	}
	if f.app.OrcaAccountStatus().ReceiptError != "" {
		t.Fatal("stale persistence attempt leaked a warning into the new account")
	}
}

func TestAccountReceiptLateWriteCannotCrossAccount(t *testing.T) {
	f := newLifecycleFixture(t, nil, nil)
	store := &receiptFaultStore{}
	s := receiptLogin(t, f, store)
	entered := make(chan struct{})
	gate, release := lifecycleGate(t)
	var block atomic.Bool
	block.Store(true)
	store.onSave = func() {
		if block.CompareAndSwap(true, false) {
			close(entered)
			<-gate
		}
	}
	gen := s.client.CurrentSessionGeneration()
	s.putReceipt(accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: "req-old-late", Purpose: "turn", Status: "unknown", Currency: "CNY"}, generation: gen})
	done := make(chan error, 1)
	go func() { done <- s.persistAccountReceipt(context.Background(), "req-old-late", gen) }()
	lifecycleWait(t, entered)
	if err := f.app.OrcaAccountLogout(); err != nil {
		t.Fatal(err)
	}
	if err := f.app.OrcaAccountLogin(); err != nil {
		t.Fatal(err)
	}
	release()
	if err := lifecycleResult(t, done); !errors.Is(err, orcaaccount.ErrStale) {
		t.Fatalf("late receipt write admitted: %v", err)
	}
	lifecycleSignedIn(t, f.app, "subject-2")
	raw, _ := store.Load(context.Background())
	if bytes.Contains(raw, []byte("req-old-late")) || len(f.app.OrcaAccountReceipts()) != 0 {
		t.Fatal("late old-account write overwrote new journal")
	}
}

func TestAccountReceiptBoundedRecoveryAndCapacity(t *testing.T) {
	var queries atomic.Int32
	f := newLifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.Path, "/requests/") {
			queries.Add(1)
			lifecycleJSON(w, orcaaccount.Settlement{RequestID: strings.TrimPrefix(r.URL.Path, orcaaccount.APIPrefix+"/requests/"), Status: orcaaccount.SettlementPending, Currency: "CNY"})
			return true
		}
		return false
	}, nil)
	store := &lifecycleStore{}
	s := receiptLogin(t, f, store)
	gen := s.client.CurrentSessionGeneration()
	j := s.receiptJournal
	j.mu.Lock()
	doc := j.doc
	for i := 0; i < accountReceiptLimit; i++ {
		doc.Records = append(doc.Records, storedAccountReceipt{RequestID: fmt.Sprintf("req-%d", i), ServerID: fmt.Sprintf("server-%d", i), Purpose: "turn", Status: "pending", Currency: "CNY", Grant: doc.Grant})
	}
	if err := j.save(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	j.mu.Unlock()
	receipts, err := s.restoreAccountReceipts(context.Background(), s.client.Snapshot().Profile, gen)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	for _, r := range receipts {
		s.receipts[r.RequestID] = r
		s.receiptOrder = append(s.receiptOrder, r.RequestID)
	}
	s.mu.Unlock()
	s.recoverAccountReceipts(gen, receipts)
	if queries.Load() != accountRecoveryLimit {
		t.Fatalf("recovery query bound not enforced: %d", queries.Load())
	}
	overflow := accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: "req-overflow", Purpose: "turn", Status: "unknown", Currency: "CNY"}, generation: gen}
	if err := s.putReceipt(overflow); !errors.Is(err, errAccountReceiptFull) {
		t.Fatalf("RAM admission discarded pending history: %v", err)
	}
	// Independently exercise the journal boundary even if a caller bypasses RAM admission.
	s.mu.Lock()
	s.receipts[overflow.RequestID] = overflow
	s.mu.Unlock()
	if err := s.persistAccountReceipt(context.Background(), "req-overflow", gen); !errors.Is(err, errAccountReceiptFull) {
		t.Fatalf("pending journal was silently evicted: %v", err)
	}
	raw, _ := store.Load(context.Background())
	if !bytes.Contains(raw, []byte(`"req-0"`)) || bytes.Contains(raw, []byte("req-overflow")) {
		t.Fatal("capacity refusal changed durable pending history")
	}
	// Free one settled row only. Exact settled amounts in all retained rows survive.
	amount := int64(9007199254740993)
	s.applySettlement("req-0", gen, orcaaccount.Settlement{RequestID: "server-0", Status: orcaaccount.SettlementSettled, Currency: "CNY", SettledMicros: &amount})
	if err := s.persistAccountReceipt(context.Background(), "req-0", gen); err != nil {
		t.Fatal(err)
	}
	if err := s.persistAccountReceipt(context.Background(), "req-overflow", gen); err != nil {
		t.Fatal(err)
	}
	raw, _ = store.Load(context.Background())
	doc, err = decodeAccountReceipts(raw)
	if err != nil || len(doc.Records) != accountReceiptLimit || bytes.Contains(raw, []byte(`"req-0"`)) || !bytes.Contains(raw, []byte(`"req-1"`)) {
		t.Fatal("bounded journal evicted an unresolved row instead of settled history")
	}
}

func TestAccountReceiptEncryptedStoreAndStrictSchema(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI current-user Windows integration")
	}
	f := newLifecycleFixture(t, nil, nil)
	root, err := config.ProfileHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "account", "receipts.dpapi")
	store, err := orcaaccount.NewSystemStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s := receiptLogin(t, f, store)
	receiptPut(t, s, "req-encrypted-unique", "server-private-unique", "settled", "9007199254.740993")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"req-encrypted-unique", "server-private-unique", "subject-1", "cost_micros"} {
		if bytes.Contains(raw, []byte(text)) {
			t.Fatal("plaintext receipt fallback")
		}
	}
	decoded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeAccountReceipts(decoded); err != nil {
		t.Fatal(err)
	}
	bad := bytes.Replace(decoded, []byte(`"version":1`), []byte(`"version":1,"prompt":"must reject"`), 1)
	if _, err := decodeAccountReceipts(bad); err == nil {
		t.Fatal("unknown journal fields accepted")
	}
	for _, value := range []int64{math.MinInt64, -1, 0, 1, 9007199254740993, math.MaxInt64} {
		got, err := receiptMicros(cnyMicros(value))
		if err != nil || got == nil || *got != value {
			t.Fatal("exact int64 currency roundtrip failed")
		}
	}
}

type receiptNonCooperativeRunner struct {
	started chan struct{}
	release <-chan struct{}
}

func (r *receiptNonCooperativeRunner) Run(context.Context, string) error {
	close(r.started)
	<-r.release
	return nil
}

func TestAccountLogoutDrainsDetachedNonCooperativeTools(t *testing.T) {
	f := newLifecycleFixture(t, nil, nil)
	receiptLogin(t, f, &lifecycleStore{})
	gate, release := lifecycleGate(t)
	r := &receiptNonCooperativeRunner{started: make(chan struct{}), release: gate}
	ctrl := control.New(control.Options{Runner: r, Sink: event.Discard})
	f.app.setTestCtrl(ctrl, accountProviderPrefix+"test/model")
	admitted, err := f.app.beginOrcaAccountAdmission("test")
	if err != nil {
		t.Fatal(err)
	}
	ctrl.Send("synthetic test")
	admitted()
	lifecycleWait(t, r.started)
	f.app.mu.Lock()
	delete(f.app.tabs, "test")
	f.app.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := f.app.logoutOrcaAccount(ctx); err == nil {
		t.Fatal("logout pretended non-cooperative tool was idle")
	}
	if v := f.app.OrcaAccountStatus(); v.State != "logging_out" || v.Authenticated || !ctrl.Running() || !ctrl.Retired() {
		t.Fatal("logout did not retain honest draining state")
	}
	if err := f.app.OrcaAccountLogin(); err == nil {
		t.Fatal("new login admitted before old tool stopped")
	}
	if err := f.app.OrcaAccountCancelLogin(); err != nil || f.app.OrcaAccountStatus().State != "logging_out" {
		t.Fatal("cancel-login reopened admission while old tools were running")
	}
	release()
	receiptEventually(t, func() bool { return f.app.OrcaAccountStatus().State == "signed_out" })
	if ctrl.Running() {
		t.Fatal("drain published idle prematurely")
	}
	if err := f.app.OrcaAccountLogin(); err != nil {
		t.Fatal(err)
	}
	lifecycleSignedIn(t, f.app, "subject-2")
	ctrl.Close()
}

func TestAccountMetadataRefreshDeduplicatesAndNeverSwitches(t *testing.T) {
	var refresh atomic.Bool
	var requests atomic.Int32
	entered := make(chan struct{}, 1)
	gate, release := lifecycleGate(t)
	f := newLifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
		if refresh.Load() && r.URL.Path == orcaaccount.APIPrefix+"/models" {
			requests.Add(1)
			select {
			case entered <- struct{}{}:
			default:
			}
			<-gate
			m := orcaaccount.Model{ID: "new-model", GroupID: 7, Protocol: "openai", Endpoint: "/chat/completions"}
			lifecycleJSON(w, orcaaccount.Models{Currency: "CNY", Models: []orcaaccount.Model{m, m}})
			return true
		}
		return false
	}, nil)
	s := receiptLogin(t, f, &lifecycleStore{})
	s.mu.Lock()
	var oldName string
	for name := range s.catalog {
		oldName = name
	}
	s.catalogAt = time.Now().Add(-accountModelsMaxAge)
	s.mu.Unlock()
	if _, _, err := s.accountModelForRequest(oldName, "model-subject-1"); err == nil || !f.app.OrcaAccountStatus().ModelsStale {
		t.Fatal("expired metadata remained request-ready")
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	beforeDefault := cfg.DefaultModel
	refresh.Store(true)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- f.app.OrcaAccountRefreshModels() }()
	}
	lifecycleWait(t, entered)
	time.Sleep(40 * time.Millisecond)
	release()
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatal("concurrent refreshes were not coalesced")
	}
	s.mu.Lock()
	count := len(s.catalog)
	s.mu.Unlock()
	if count != 1 || f.app.OrcaAccountStatus().ModelsStale || config.LoadForEdit(config.UserConfigPath()).DefaultModel != beforeDefault {
		t.Fatal("refresh did not deduplicate or auto-switched the default model")
	}
	if _, _, err := s.accountModelForRequest(oldName, "model-subject-1"); err == nil {
		t.Fatal("removed model still admitted")
	}
}

func TestAccountCatalogIncludesEndpointAndBetaPlatformCapabilities(t *testing.T) {
	models := accountCatalog("subject", []orcaaccount.Model{
		{ID: "same", GroupID: 7, Protocol: "openai", Endpoint: "/chat/completions"},
		{ID: "same", GroupID: 7, Protocol: "openai", Endpoint: "/responses"},
	})
	if len(models) != 2 {
		t.Fatal("different endpoints collided")
	}
	for _, goos := range []string{"windows", "linux", "darwin"} {
		for _, enabled := range []bool{false, true} {
			v := betaCapabilitiesForOS(goos, enabled, enabled, false)
			if v["enabled"] != enabled || v["web"] != (enabled && goos == "windows") || v["browser"] != (enabled && goos == "windows") || v["account"] {
				t.Fatalf("unsupported platform or channel advertised capability: %s", goos)
			}
		}
	}
	lifecycleProfile(t)
	actual := (&App{}).BetaCapabilities()
	if !actual["enabled"] || actual["account"] || actual["web"] != (runtime.GOOS == "windows") || actual["browser"] != (runtime.GOOS == "windows") {
		t.Fatal("public capability method bypassed the native platform gate")
	}
}

func TestAccountMetadataRefreshCannotCommitAfterLogout(t *testing.T) {
	var refresh atomic.Bool
	entered := make(chan struct{})
	gate, release := lifecycleGate(t)
	f := newLifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
		if refresh.Load() && r.URL.Path == orcaaccount.APIPrefix+"/models" && r.Header.Get("Authorization") == "Bearer synthetic-access-subject-1" {
			close(entered)
			<-gate
			lifecycleJSON(w, orcaaccount.Models{Currency: "CNY", Models: []orcaaccount.Model{{ID: "late-old-model", GroupID: 7, Protocol: "openai", Endpoint: "/chat/completions"}}})
			return true
		}
		return false
	}, nil)
	receiptLogin(t, f, &lifecycleStore{})
	refresh.Store(true)
	done := make(chan error, 1)
	go func() { done <- f.app.OrcaAccountRefreshModels() }()
	lifecycleWait(t, entered)
	if err := f.app.OrcaAccountLogout(); err != nil {
		t.Fatal(err)
	}
	if err := f.app.OrcaAccountLogin(); err != nil {
		t.Fatal(err)
	}
	lifecycleSignedIn(t, f.app, "subject-2")
	release()
	if err := lifecycleResult(t, done); err == nil {
		t.Fatal("late metadata refresh succeeded")
	}
	assertLifecycleCatalog(t, f.app, "subject-2")
}
