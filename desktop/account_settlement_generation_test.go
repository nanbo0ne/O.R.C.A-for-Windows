package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
)

func TestAccountLateSettlementCannotOverwriteSwitchedIdentity(t *testing.T) {
	for _, reuseLocalID := range []bool{false, true} {
		name := "distinct_local_ids"
		if reuseLocalID {
			name = "reused_local_id"
		}
		t.Run(name, func(t *testing.T) {
			f := newLifecycleFixture(t, nil, nil)
			entered := make(chan struct{})
			gate, release := lifecycleGate(t)
			defer release()
			type query struct{ id, authorization string }
			queries := make(chan query, 4)
			client, err := orcaaccount.New(orcaaccount.Options{Store: &lifecycleStore{}, OpenBrowser: f.browser,
				HTTPClient: &http.Client{Transport: lifecycleTransport(func(r *http.Request) (*http.Response, error) {
					if r.URL.Scheme != "https" || r.URL.Host != "sub.aichat.diy" {
						t.Error("synthetic transport rejected an external destination")
						return nil, orcaaccount.ErrInvalid
					}
					if id, ok := strings.CutPrefix(r.URL.Path, orcaaccount.APIPrefix+"/requests/"); ok {
						queries <- query{id, r.Header.Get("Authorization")}
						if id == "old-server-id" {
							close(entered)
							<-gate // Deliberately ignore cancellation to reproduce late transport completion.
						}
						amount := int64(18)
						return integrationJSON(r, orcaaccount.Settlement{RequestID: id, Status: orcaaccount.SettlementSettled,
							Currency: "CNY", SettledMicros: &amount})
					}
					recorder := httptest.NewRecorder()
					f.serve(recorder, r)
					return recorder.Result(), nil
				})}})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			if p, err := client.Login(context.Background()); err != nil || p.Subject != "subject-1" {
				t.Fatal("first synthetic identity failed", err)
			}
			s := settlementTestWorker(t, context.Background(), client)
			oldGen := client.CurrentSessionGeneration()
			settlementTestReceipt(s, "old-local-id", "old-server-id")
			lifecycleWait(t, entered)
			if err := client.Logout(context.Background()); err != nil {
				t.Fatal(err)
			}
			if p, err := client.Login(context.Background()); err != nil || p.Subject != "subject-2" {
				t.Fatal("second synthetic identity failed", err)
			}
			id := "new-local-id"
			if reuseLocalID {
				id = "old-local-id"
			}
			s.mu.Lock()
			s.receipts = map[string]accountReceipt{}
			s.mu.Unlock()
			settlementTestReceipt(s, id, "new-server-id")
			late := int64(999)
			s.applySettlement(id, oldGen, orcaaccount.Settlement{RequestID: "new-server-id", Status: orcaaccount.SettlementSettled,
				Currency: "CNY", SettledMicros: &late})
			s.mu.Lock()
			untouched := s.receipts[id].Status == "pending" && s.receipts[id].Cost == ""
			s.mu.Unlock()
			if !untouched {
				t.Fatal("stale result replaced the new account's still-pending receipt")
			}
			release()
			receiptEventually(t, func() bool {
				s.mu.Lock()
				defer s.mu.Unlock()
				return s.receipts[id].Status == "settled"
			})
			if len(queries) != 2 {
				t.Fatalf("expected one query per identity and no replay, got %d", len(queries))
			}
			for i, want := range []query{{"old-server-id", "Bearer synthetic-access-subject-1"},
				{"new-server-id", "Bearer synthetic-access-subject-2"}} {
				if got := <-queries; got != want {
					t.Fatalf("query %d used another identity: %+v", i, got)
				}
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if len(s.receipts) != 1 || s.receipts[id].Cost != "0.000018" || s.receipts[id].generation == oldGen {
				t.Fatal("late completion crossed identity or reintroduced another account's receipt")
			}
		})
	}
}

// WithTimeout consults Deadline after the worker's generation check. Pausing
// here reproduces an account switch before Request captures authorization.
type settlementDispatchBarrier struct {
	context.Context
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (c *settlementDispatchBarrier) Deadline() (time.Time, bool) {
	c.once.Do(func() {
		close(c.entered)
		<-c.release
	})
	return c.Context.Deadline()
}

func settlementTestWorker(t *testing.T, ctx context.Context, client *orcaaccount.Client) *accountDesktop {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	s := &accountDesktop{ctx: ctx, client: client, receipts: map[string]accountReceipt{}, settlements: make(chan string, 4)}
	done := make(chan struct{})
	go func() { defer close(done); s.settlementWorker() }()
	t.Cleanup(func() {
		cancel()
		lifecycleWait(t, done)
	})
	return s
}

func settlementTestReceipt(s *accountDesktop, requestID, serverID string) {
	s.mu.Lock()
	s.receipts[requestID] = accountReceipt{OrcaReceiptView: OrcaReceiptView{RequestID: requestID, Status: "pending", Currency: "CNY"},
		serverID: serverID, generation: s.client.CurrentSessionGeneration()}
	s.mu.Unlock()
	s.queueReceipt(requestID)
}

func TestAccountSettlementSwitchBeforeDispatchCannotBorrowNewAuthorization(t *testing.T) {
	type query struct{ id, authorization string }
	queries := make(chan query, 4)
	f := newLifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.Contains(r.URL.Path, "/requests/") {
			return false
		}
		id := strings.TrimPrefix(r.URL.Path, orcaaccount.APIPrefix+"/requests/")
		queries <- query{id, r.Header.Get("Authorization")}
		amount := int64(18)
		lifecycleJSON(w, orcaaccount.Settlement{RequestID: id, Status: orcaaccount.SettlementSettled, Currency: "CNY", SettledMicros: &amount})
		return true
	}, nil)
	client := f.app.account.client
	if _, err := client.Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	barrier := &settlementDispatchBarrier{Context: context.Background(), entered: make(chan struct{}), release: make(chan struct{})}
	s := settlementTestWorker(t, barrier, client)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(barrier.release) }) })
	settlementTestReceipt(s, "request-old-account", "server-old-account")
	lifecycleWait(t, barrier.entered)
	if err := client.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if profile, err := client.Login(context.Background()); err != nil || profile.Subject != "subject-2" {
		t.Fatalf("second synthetic login failed: %v", err)
	}
	settlementTestReceipt(s, "request-new-account", "server-new-account")
	release.Do(func() { close(barrier.release) })
	receiptEventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.receipts["request-new-account"].Status == "settled"
	})
	if len(queries) != 1 {
		t.Errorf("sent %d queries; only the new account's receipt may be queried", len(queries))
	}
	for len(queries) != 0 {
		q := <-queries
		if q.id != "server-new-account" || q.authorization != "Bearer synthetic-access-subject-2" {
			t.Errorf("receipt queried under wrong account: %+v", q)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.receipts["request-old-account"].Status != "pending" || s.receipts["request-old-account"].Cost != "" ||
		s.receipts["request-new-account"].Cost != "0.000018" {
		t.Error("stale response changed receipt state or new-account settlement was lost")
	}
}

func TestAccountSettlementInFlightQueryCanceledOnLogout(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan struct{})
	f := newLifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.Contains(r.URL.Path, "/requests/") {
			return false
		}
		if r.URL.Path != orcaaccount.APIPrefix+"/requests/server-old-account" || r.Header.Get("Authorization") != "Bearer synthetic-access-subject-1" {
			t.Error("in-flight receipt used unexpected identity")
		}
		close(entered)
		<-r.Context().Done()
		close(canceled)
		return true
	}, nil)
	client := f.app.account.client
	if _, err := client.Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := settlementTestWorker(t, context.Background(), client)
	settlementTestReceipt(s, "request-old-account", "server-old-account")
	lifecycleWait(t, entered)
	if err := client.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	lifecycleWait(t, canceled)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.receipts["request-old-account"].Cost != "" || s.receipts["request-old-account"].Status != "pending" {
		t.Error("canceled query invented a settlement")
	}
}
