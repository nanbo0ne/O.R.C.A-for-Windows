package orcaaccount

import (
	"bytes"
	"context"
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
)

type memoryStore struct {
	mu                   sync.Mutex
	data                 []byte
	saveErr, errorDelete error
}

func (s *memoryStore) Load(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(s.data) == 0 {
		return nil, ErrNotFound
	}
	return bytes.Clone(s.data), nil
}
func (s *memoryStore) Save(ctx context.Context, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.saveErr != nil {
		return s.saveErr
	}
	s.data = bytes.Clone(b)
	return nil
}
func (s *memoryStore) Delete(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.errorDelete != nil {
		return s.errorDelete
	}
	clear(s.data)
	s.data = nil
	return nil
}

type fakeProof struct{ nonce, challenge, redirect, device string }
type fakeIssuer struct {
	t            *testing.T
	server       *httptest.Server
	mu           sync.Mutex
	proofs       map[string]fakeProof
	tokens       map[string]tokenResponse
	refreshes    map[string]tokenResponse
	next         int
	refreshCount int
	revocations  int
	// Hooks are configured before requests start, never mutated concurrently.
	hook        func(http.ResponseWriter, *http.Request) bool
	mutateToken func(*tokenResponse)
}

func newIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	f := &fakeIssuer{t: t, proofs: map[string]fakeProof{}, tokens: map[string]tokenResponse{}, refreshes: map[string]tokenResponse{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeIssuer) serve(w http.ResponseWriter, r *http.Request) {
	if f.hook != nil && f.hook(w, r) {
		return
	}
	if r.Header.Get("Cookie") != "" {
		f.t.Error("account request carried cookies")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, APIPrefix)
	switch path {
	case "/authorize":
		q := r.URL.Query()
		if q.Get("client_id") != ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
			len(q.Get("code_challenge")) != 43 || len(q.Get("state")) != 43 || len(q.Get("nonce")) != 43 || !devicePattern.MatchString(q.Get("device_id")) || q.Get("scope") != "desktop" {
			f.t.Error("invalid public authorization parameters")
			http.Error(w, "invalid", 400)
			return
		}
		u, _ := url.Parse(q.Get("redirect_uri"))
		if u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Scheme != "http" || u.Path != "/callback" {
			f.t.Error("invalid loopback")
		}
		f.next++
		code := fmt.Sprintf("code-%d", f.next)
		f.proofs[code] = fakeProof{q.Get("nonce"), q.Get("code_challenge"), q.Get("redirect_uri"), q.Get("device_id")}
		v := url.Values{"code": {code}, "state": {q.Get("state")}, "iss": {Issuer}}
		http.Redirect(w, r, q.Get("redirect_uri")+"?"+v.Encode(), 302)
	case "/token":
		_ = r.ParseForm()
		if r.Method != "POST" || r.Form.Get("client_id") != ClientID || r.Form.Has("client_secret") || r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			f.t.Error("invalid public token request")
			http.Error(w, "invalid", 400)
			return
		}
		var out tokenResponse
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			proof, ok := f.proofs[r.Form.Get("code")]
			delete(f.proofs, r.Form.Get("code"))
			if !ok || challenge(r.Form.Get("code_verifier")) != proof.challenge || len(r.Form.Get("code_verifier")) != 43 || r.Form.Get("redirect_uri") != proof.redirect {
				w.WriteHeader(400)
				writeJSON(w, map[string]string{"error": "invalid_grant"})
				return
			}
			out = tokenResponse{TokenType: "Bearer", ExpiresIn: 300, Scope: "desktop", Subject: "synthetic-account", Issuer: Issuer,
				SessionID: fmt.Sprintf("session-%d", f.next), DeviceID: proof.device, Nonce: proof.nonce}
		case "refresh_token":
			f.refreshCount++
			var ok bool
			out, ok = f.refreshes[r.Form.Get("refresh_token")]
			delete(f.refreshes, r.Form.Get("refresh_token"))
			if !ok {
				w.WriteHeader(400)
				writeJSON(w, map[string]string{"error": "invalid_grant"})
				return
			}
		default:
			http.Error(w, "invalid", 400)
			return
		}
		f.next++
		out.AccessToken = fmt.Sprintf("synthetic-access-%d", f.next)
		out.RefreshToken = fmt.Sprintf("synthetic-refresh-%d", f.next)
		if f.mutateToken != nil {
			f.mutateToken(&out)
		}
		f.tokens[out.AccessToken] = out
		f.refreshes[out.RefreshToken] = out
		writeJSON(w, out)
	case "/revoke":
		_ = r.ParseForm()
		if r.Form.Get("client_id") != ClientID || r.Form.Get("token") == "" {
			f.t.Error("invalid revoke")
		}
		f.revocations++
		w.WriteHeader(204)
	default:
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		s, ok := f.tokens[token]
		if !ok {
			w.WriteHeader(401)
			return
		}
		switch {
		case path == "/session":
			writeJSON(w, Profile{Subject: s.Subject, DisplayName: "Synthetic user", SessionID: s.SessionID, DeviceID: s.DeviceID, ExpiresAt: time.Now().Add(time.Hour), Groups: []Group{{ID: 7, Name: "Test group"}}})
		case path == "/models":
			writeJSON(w, Models{Currency: "CNY", Models: []Model{{ID: "synthetic-model", Name: "Synthetic", GroupID: 7, Protocol: "openai"}}})
		case path == "/wallet":
			writeJSON(w, Wallet{Currency: "CNY", CashMicros: 9007199254740993, HeldMicros: 2, AvailableMicros: 9007199254740991,
				ScopedGifts: []ScopedGift{{ID: "gift-1", GroupID: 7, GroupIDs: []int64{7}, BalanceMicros: 100, HeldMicros: 10, AvailableMicros: 90}}})
		case strings.HasPrefix(path, "/requests/"):
			writeJSON(w, Settlement{RequestID: strings.TrimPrefix(path, "/requests/"), Status: SettlementUnknown, Currency: "CNY"})
		case path == "/credentials":
			var opts CredentialOptions
			if json.NewDecoder(r.Body).Decode(&opts) != nil {
				f.t.Error("invalid credential JSON")
			}
			group := int64(7)
			if opts.GroupID != nil {
				group = *opts.GroupID
			}
			writeJSON(w, credentialResponse{APIKey: "synthetic-runtime-key", CredentialID: "credential-1", BaseURL: Router + "/v1", Restricted: true, GroupID: &group, ExpiresAt: time.Now().Add(time.Hour)})
		default:
			w.WriteHeader(404)
		}
	}
}

func (f *fakeIssuer) client(t *testing.T, store *memoryStore) *Client {
	t.Helper()
	c, err := New(Options{Store: store, HTTPClient: f.server.Client(), OpenBrowser: func(ctx context.Context, u string) error {
		r, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
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
			return errors.New("synthetic browser failed")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	c.issuer = f.server.URL
	t.Cleanup(func() { _ = c.CancelContexts() })
	return c
}

func login(t *testing.T, c *Client) Profile {
	t.Helper()
	p, err := c.Login(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func TestLoginPKCESessionAndSecretFreeSnapshots(t *testing.T) {
	f := newIssuer(t)
	c := f.client(t, &memoryStore{})
	p := login(t, c)
	if p.Subject != "synthetic-account" || !c.Snapshot().Authenticated {
		t.Fatal("not authenticated")
	}
	if _, err := c.Session(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap := c.Snapshot()
	snap.Profile.Groups[0].Name = "mutation"
	if c.Snapshot().Profile.Groups[0].Name == "mutation" {
		t.Fatal("snapshot aliases internal profile")
	}
	b, _ := json.Marshal(c.Snapshot())
	for _, out := range []string{string(b), fmt.Sprintf("%v %+v %#v", c, c, c)} {
		if strings.Contains(out, "synthetic-access") || strings.Contains(out, "synthetic-refresh") {
			t.Fatal("secret exposed")
		}
	}
}

func TestCallbackValidationAndReplay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cb, err := listenCallback(ctx, "correct-state")
	if err != nil {
		t.Fatal(err)
	}
	defer cb.close()
	for _, query := range []string{
		"state=wrong&code=a&iss=" + url.QueryEscape(Issuer),
		"state=correct-state&code=a&iss=https%3A%2F%2Fevil.invalid",
		"state=correct-state&state=correct-state&code=a&iss=" + url.QueryEscape(Issuer),
		"state=correct-state&code=a&error=denied&iss=" + url.QueryEscape(Issuer),
		"state=correct-state&code=a",
	} {
		resp, e := http.Get(cb.uri + "?" + query)
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatal("bad callback accepted")
		}
		select {
		case <-cb.result:
			t.Fatal("bad callback consumed state")
		default:
		}
	}
	good := cb.uri + "?state=correct-state&code=good&iss=" + url.QueryEscape(Issuer)
	for i, want := range []int{200, 409} {
		resp, e := http.Get(good)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("callback %d: %d", i, resp.StatusCode)
		}
		if bytes.Contains(body, []byte("code=good")) || resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("unsafe callback response")
		}
	}
	if got := <-cb.result; got.code != "good" {
		t.Fatal("wrong callback code")
	}
}

func TestLoginCancellationClosesListener(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "logout", "close", "browser-error"} {
		t.Run(mode, func(t *testing.T) {
			f := newIssuer(t)
			c := f.client(t, &memoryStore{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var callbackURL string
			c.openBrowser = func(_ context.Context, u string) error {
				parsed, _ := url.Parse(u)
				callbackURL = parsed.Query().Get("redirect_uri")
				switch mode {
				case "close":
					if e := c.Close(); e != nil {
						t.Error(e)
					}
				case "cancel":
					cancel()
				case "logout":
					if e := c.Logout(context.Background()); e != nil {
						t.Error(e)
					}
				case "browser-error":
					return errors.New("SECRET failure")
				}
				return nil
			}
			if mode == "timeout" {
				c.loginTimeout = 20 * time.Millisecond
			}
			_, err := c.Login(ctx)
			if err == nil || strings.Contains(err.Error(), "SECRET") || c.Snapshot().Authenticated {
				t.Fatal("cancellation failure")
			}
			resp, e := http.Get(callbackURL)
			if e == nil {
				resp.Body.Close()
				t.Fatal("listener remains open")
			}
		})
	}
}

func TestRejectNonceIssuerAndSessionMismatch(t *testing.T) {
	for _, field := range []string{"nonce", "issuer", "device", "subject", "type", "expiry"} {
		t.Run(field, func(t *testing.T) {
			f := newIssuer(t)
			f.mutateToken = func(r *tokenResponse) {
				switch field {
				case "nonce":
					r.Nonce = "wrong"
				case "issuer":
					r.Issuer = "https://evil.invalid"
				case "device":
					r.DeviceID = "other-device-123456"
				case "subject":
					r.Subject = ""
				case "type":
					r.TokenType = "Basic"
				case "expiry":
					r.ExpiresIn = 301
				}
			}
			c := f.client(t, &memoryStore{})
			if _, err := c.Login(context.Background()); !errors.Is(err, ErrProtocol) {
				t.Fatalf("want protocol rejection, got %v", err)
			}
			if c.Snapshot().Authenticated {
				t.Fatal("invalid response installed")
			}
		})
	}
}

func TestRefreshSingleflightAndCanceledWaiter(t *testing.T) {
	f := newIssuer(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == APIPrefix+"/token" {
			_ = r.ParseForm()
			if r.Form.Get("grant_type") == "refresh_token" {
				select {
				case <-entered:
				default:
					close(entered)
				}
				<-release
			}
		}
		return false
	}
	c := f.client(t, &memoryStore{})
	login(t, c)
	c.mu.Lock()
	c.tokens.ExpiresAt = time.Now().Add(-time.Second)
	c.mu.Unlock()
	const count = 24
	errs := make(chan error, count)
	var started sync.WaitGroup
	started.Add(count)
	for i := 0; i < count; i++ {
		go func() { started.Done(); _, err := c.Models(context.Background()); errs <- err }()
	}
	started.Wait()
	await(t, entered)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Refresh(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter not canceled: %v", err)
	}
	close(release)
	for i := 0; i < count; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refreshCount != 1 {
		t.Fatalf("refresh requests: %d", f.refreshCount)
	}
}

func TestOldUnauthorizedResponseDoesNotRotateNewTokenAgain(t *testing.T) {
	f := newIssuer(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == APIPrefix+"/models" && first.CompareAndSwap(false, true) {
			close(entered)
			<-release
			w.WriteHeader(401)
			return true
		}
		return false
	}
	c := f.client(t, &memoryStore{})
	login(t, c)
	done := make(chan error, 1)
	go func() { _, e := c.Models(context.Background()); done <- e }()
	await(t, entered)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refreshCount != 1 {
		t.Fatal("stale 401 repeated refresh")
	}
}

func TestAccountSwitchDiscardsOldRefresh(t *testing.T) {
	f := newIssuer(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == APIPrefix+"/token" {
			_ = r.ParseForm()
			if r.Form.Get("grant_type") == "refresh_token" {
				close(entered)
				<-release
			}
		}
		return false
	}
	c := f.client(t, &memoryStore{})
	first := login(t, c)
	done := make(chan error, 1)
	go func() { done <- c.Refresh(context.Background()) }()
	await(t, entered)
	second := login(t, c)
	close(release)
	if err := <-done; !errors.Is(err, ErrStale) {
		t.Fatalf("old refresh: %v", err)
	}
	if second.SessionID == first.SessionID || c.Snapshot().Profile.SessionID != second.SessionID {
		t.Fatal("old refresh replaced new session")
	}
}

func TestAuthFailureAndLogoutPreserveHistory(t *testing.T) {
	f := newIssuer(t)
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == APIPrefix+"/models" {
			w.WriteHeader(401)
			writeJSON(w, map[string]string{"error": "synthetic-access-LEAK"})
			return true
		}
		return false
	}
	store := &memoryStore{}
	c := f.client(t, store)
	login(t, c)
	history := filepath.Join(t.TempDir(), "history.json")
	fixture := []byte(`{"messages":["synthetic history"]}`)
	if err := os.WriteFile(history, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := c.Models(context.Background())
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected auth failure, got %v", err)
	}
	if c.Snapshot().Authenticated {
		t.Fatal("auth failure retained active account")
	}
	if e := c.Logout(context.Background()); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(history)
	if e != nil || !bytes.Equal(got, fixture) {
		t.Fatal("history changed")
	}
	if _, e = store.Load(context.Background()); !errors.Is(e, ErrNotFound) {
		t.Fatal("auth failure retained credentials")
	}
}

func TestRestoreAndStorageFailureFailClosed(t *testing.T) {
	f := newIssuer(t)
	store := &memoryStore{}
	c := f.client(t, store)
	p := login(t, c)
	restored := f.client(t, store)
	if got, err := restored.Restore(context.Background()); err != nil || got.SessionID != p.SessionID {
		t.Fatalf("restore: %v", err)
	}
	store.mu.Lock()
	store.saveErr = errors.New("SECRET disk failure")
	store.mu.Unlock()
	if err := restored.Refresh(context.Background()); !errors.Is(err, ErrStorage) {
		t.Fatalf("save failure: %v", err)
	}
	if restored.Snapshot().Authenticated {
		t.Fatal("failed rotation was installed")
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatal("rotated stale token survived")
	}
	if _, err := restored.Login(context.Background()); !errors.Is(err, ErrStorage) {
		t.Fatalf("failed login save: %v", err)
	}
}

func TestExactMoneyScopedGiftsAndUnknownSettlement(t *testing.T) {
	f := newIssuer(t)
	c := f.client(t, &memoryStore{})
	login(t, c)
	w, err := c.Wallet(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if w.CashMicros != 9007199254740993 || w.AvailableMicros != 9007199254740991 || w.ScopedGifts[0].AvailableMicros != 90 {
		t.Fatal("money rounded or gifts mixed")
	}
	s, err := c.Request(context.Background(), "synthetic-id")
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != SettlementUnknown || s.SettledMicros != nil || s.EstimatedMicros != nil {
		t.Fatal("unknown amount fabricated")
	}
	m, err := c.Models(context.Background())
	if err != nil || m.Models[0].Capabilities != nil {
		t.Fatal("unknown capabilities fabricated")
	}
	for _, id := range []string{"", "..", "a/b", "a\\b", "a\n"} {
		if _, e := c.Request(context.Background(), id); !errors.Is(e, ErrInvalid) {
			t.Fatal("unsafe request id accepted")
		}
	}
}

func TestCredentialHTTPClientAndLogoutCancelStreams(t *testing.T) {
	f := newIssuer(t)
	c := f.client(t, &memoryStore{})
	login(t, c)
	cr, err := c.Credentials(context.Background(), CredentialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	started, canceled := make(chan struct{}), make(chan struct{})
	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-runtime-key" || r.Header.Get("Idempotency-Key") != "synthetic-id" || r.Header.Get("Cookie") != "" {
			t.Error("bad runtime headers")
		}
		w.Header().Set("X-Request-Id", "synthetic-id")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "first")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer router.Close()
	c.router = router.URL
	req, _ := http.NewRequest(http.MethodPost, router.URL+"/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Idempotency-Key", "synthetic-id")
	req.Header.Set("Cookie", "should-not-leave-client")
	resp, err := cr.HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	await(t, started)
	if resp.Header.Get("X-Request-Id") != "synthetic-id" || resp.Request.Header.Get("Authorization") != "" || req.Header.Get("Authorization") != "" {
		t.Fatal("runtime credential leaked or metadata lost")
	}
	b, _ := json.Marshal(cr)
	if strings.Contains(string(b), "synthetic-runtime-key") || strings.Contains(fmt.Sprintf("%+v %#v", cr, cr), "synthetic-runtime-key") {
		t.Fatal("credential formatted into UI")
	}
	gen := c.CurrentSessionGeneration()
	genCtx, stop, e := c.GenerationContext(context.Background(), gen)
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	if e = c.Logout(context.Background()); e != nil {
		t.Fatal(e)
	}
	await(t, genCtx.Done())
	await(t, canceled)
	if _, e = resp.Body.Read(make([]byte, 64)); !errors.Is(e, context.Canceled) {
		t.Fatalf("stream was not canceled: %v", e)
	}
	if _, e = cr.Do(req); !errors.Is(e, ErrStale) {
		t.Fatal("old provider survived logout")
	}
	if _, _, e = c.GenerationContext(context.Background(), gen); !errors.Is(e, ErrStale) {
		t.Fatal("old generation accepted")
	}
}

func TestCredentialRejectsUntrustedDestinationsAndRedirects(t *testing.T) {
	f := newIssuer(t)
	c := f.client(t, &memoryStore{})
	login(t, c)
	cr, err := c.Credentials(context.Background(), CredentialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"http://router.aichat.diy/v1/models", "https://evil.invalid/v1/models", Router + ".evil.invalid/v1/models", Router + "/v1/../admin", Router + "/admin", Router + "/v1/models#x", "https://user:pass@router.aichat.diy/v1/models"} {
		r, _ := http.NewRequest(http.MethodGet, target, nil)
		if _, e := cr.Do(r); !errors.Is(e, ErrInvalid) {
			t.Fatalf("unsafe destination %s: %v", target, e)
		}
	}
	var reached atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Store(true) }))
	defer target.Close()
	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer router.Close()
	c.router = router.URL
	r, _ := http.NewRequest(http.MethodGet, router.URL+"/v1/models", nil)
	if _, e := cr.Do(r); !errors.Is(e, ErrNetwork) {
		t.Fatalf("redirect not rejected: %v", e)
	}
	if reached.Load() {
		t.Fatal("credential followed redirect")
	}
}
