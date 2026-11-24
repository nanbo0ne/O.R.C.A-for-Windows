package orcaaccount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGenerationContextCannotBorrowNewSessionAuthorization(t *testing.T) {
	f := newIssuer(t)
	c := f.client(t, &memoryStore{})
	login(t, c)
	ctx, cancel, err := c.GenerationContext(context.Background(), c.CurrentSessionGeneration())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	// Remove cancellation to reproduce delayed AfterFunc delivery deterministically.
	// The identity binding must remain effective independently of cancellation.
	ctx = context.WithoutCancel(ctx)
	if err := c.CancelContexts(); err != nil {
		t.Fatal(err)
	}
	login(t, c)
	var requests atomic.Int32
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		requests.Add(1)
		w.WriteHeader(500)
		return true
	}
	if _, err := c.Request(ctx, "previous-grant-request"); !errors.Is(err, ErrStale) {
		t.Fatalf("old context borrowed new authorization: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatal("stale identity reached the network")
	}
}

func TestClosePreservesStoreAndCancelsWorkWithoutRevoke(t *testing.T) {
	f := newIssuer(t)
	entered, canceled := make(chan struct{}), make(chan struct{})
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == APIPrefix+"/models" {
			close(entered)
			<-r.Context().Done()
			close(canceled)
			return true
		}
		return false
	}
	store := &memoryStore{}
	c := f.client(t, store)
	p := login(t, c)
	before, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gen := c.CurrentSessionGeneration()
	life, stop, err := c.GenerationContext(context.Background(), gen)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	done := make(chan error, 1)
	go func() { _, e := c.Models(context.Background()); done <- e }()
	await(t, entered)
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	if err = c.Close(); err != nil {
		t.Fatal("close is not idempotent")
	}
	await(t, life.Done())
	await(t, canceled)
	if e := <-done; !errors.Is(e, ErrClosed) {
		t.Fatalf("inflight request: %v", e)
	}
	if s := c.Snapshot(); !s.Closed || s.Authenticated || s.Profile.Subject != "" || s.Generation == gen {
		t.Fatal("closed state not cleared")
	}
	after, err := store.Load(context.Background())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("Close changed encrypted-store payload")
	}
	f.mu.Lock()
	revocations := f.revocations
	f.mu.Unlock()
	if revocations != 0 {
		t.Fatal("Close revoked remote grant")
	}
	for _, op := range []func() error{
		func() error { _, e := c.Login(context.Background()); return e }, func() error { _, e := c.Restore(context.Background()); return e },
		func() error { return c.Refresh(context.Background()) }, func() error { return c.Logout(context.Background()) }, c.CancelContexts,
	} {
		if e := op(); !errors.Is(e, ErrClosed) {
			t.Fatalf("closed client reused: %v", e)
		}
	}
	if _, _, e := c.GenerationContext(context.Background(), gen); !errors.Is(e, ErrClosed) {
		t.Fatal("closed context allowed")
	}
	next := f.client(t, store)
	if got, e := next.Restore(context.Background()); e != nil || got.SessionID != p.SessionID {
		t.Fatalf("next-launch restore: %v", e)
	}
}

func TestCloseDuringRefreshCannotDeleteOrOverwriteSavedSession(t *testing.T) {
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
	store := &memoryStore{}
	c := f.client(t, store)
	login(t, c)
	before, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Refresh(context.Background()) }()
	await(t, entered)
	if e := c.Close(); e != nil {
		t.Fatal(e)
	}
	close(release)
	if e := <-done; !errors.Is(e, ErrStale) && !errors.Is(e, ErrClosed) {
		t.Fatalf("late rotation: %v", e)
	}
	after, e := store.Load(context.Background())
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("late refresh changed saved session")
	}
}

func TestLocalCancelAndOfflineLogout(t *testing.T) {
	for _, mode := range []string{"local", "offline", "canceled", "delete-failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newIssuer(t)
			f.hook = func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path == APIPrefix+"/revoke" && mode == "offline" {
					w.WriteHeader(503)
					return true
				}
				return false
			}
			store := &memoryStore{}
			c := f.client(t, store)
			login(t, c)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			if mode == "delete-failure" {
				store.mu.Lock()
				store.errorDelete = errors.New("synthetic DELETE SECRET")
				store.mu.Unlock()
			}
			var err error
			if mode == "local" {
				err = c.CancelContexts()
			} else {
				err = c.Logout(ctx)
			}
			switch mode {
			case "local":
				if err != nil {
					t.Fatal(err)
				}
			case "offline":
				if !errors.Is(err, ErrNetwork) {
					t.Fatal(err)
				}
			case "canceled":
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case "delete-failure":
				if !errors.Is(err, ErrStorage) {
					t.Fatal(err)
				}
			}
			if c.Snapshot().Authenticated {
				t.Fatal("logout failed to clear memory")
			}
			if mode != "delete-failure" {
				if _, err = store.Load(context.Background()); !errors.Is(err, ErrNotFound) {
					t.Fatal("local store not erased")
				}
			}
			f.mu.Lock()
			revocations := f.revocations
			f.mu.Unlock()
			if mode == "local" && revocations != 0 {
				t.Fatal("CancelContexts made a request")
			}
		})
	}
}

func TestTamperedPKCEVerifierIsRejected(t *testing.T) {
	f := newIssuer(t)
	c := f.client(t, &memoryStore{})
	base := c.http.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == APIPrefix+"/token" {
			b, _ := io.ReadAll(r.Body)
			r.Body.Close()
			v, _ := url.ParseQuery(string(b))
			v.Set("code_verifier", strings.Repeat("x", 43))
			r = r.Clone(r.Context())
			body := v.Encode()
			r.Body = io.NopCloser(strings.NewReader(body))
			r.ContentLength = int64(len(body))
		}
		return base.RoundTrip(r)
	})
	if _, err := c.Login(context.Background()); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("PKCE mismatch accepted: %v", err)
	}
	if c.Snapshot().Authenticated {
		t.Fatal("bad proof installed")
	}
}

func TestCallbackDenialMalformedHostAndMethod(t *testing.T) {
	cb, err := listenCallback(context.Background(), "state")
	if err != nil {
		t.Fatal(err)
	}
	defer cb.close()
	u := cb.uri + "?state=state&iss=" + url.QueryEscape(Issuer) + "&code=code"
	for _, mode := range []string{"host", "method", "path", "encoding"} {
		r, _ := http.NewRequest(http.MethodGet, u, nil)
		switch mode {
		case "host":
			r.Host = "attacker.invalid"
		case "method":
			r.Method = "POST"
		case "path":
			r.URL.Path = "/wrong"
		case "encoding":
			r.URL.RawQuery += "&x=%ZZ"
		}
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatal("malformed callback accepted")
		}
	}
	resp, err := http.Get(cb.uri + "?state=state&iss=" + url.QueryEscape(Issuer) + "&error=access_denied&error_description=SECRET")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	result := <-cb.result
	if !errors.Is(result.err, ErrAuthorization) || bytes.Contains(b, []byte("SECRET")) {
		t.Fatal("denial not sanitized")
	}
}

func TestHTTPFailuresNeverExposeSecretsOrFollowRedirects(t *testing.T) {
	var reached atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Store(true) }))
	defer target.Close()
	for _, mode := range []string{"redirect", "oversized", "json", "forbidden", "bad-request", "transport"} {
		t.Run(mode, func(t *testing.T) {
			f := newIssuer(t)
			f.hook = func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path != APIPrefix+"/models" {
					return false
				}
				switch mode {
				case "redirect":
					http.Redirect(w, r, target.URL, 302)
				case "oversized":
					_, _ = io.WriteString(w, strings.Repeat("S", maxResponse+1))
				case "json":
					_, _ = io.WriteString(w, `{"currency":"CNY","models":SECRET}`)
				case "forbidden":
					w.WriteHeader(403)
					_, _ = io.WriteString(w, "SECRET")
				case "bad-request":
					w.WriteHeader(400)
					_, _ = io.WriteString(w, "SECRET")
				default:
					return false
				}
				return true
			}
			c := f.client(t, &memoryStore{})
			login(t, c)
			if mode == "transport" {
				c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("SECRET transport error") })
			}
			_, err := c.Models(context.Background())
			if err == nil || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
	if reached.Load() {
		t.Fatal("redirect destination reached")
	}
}

func TestSettlementStatesAndExactServerModelDTO(t *testing.T) {
	f := newIssuer(t)
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == APIPrefix+"/models" {
			_, _ = io.WriteString(w, `{"currency":"CNY","models":[{"id":"synthetic","name":"Synthetic","publication_id":"publication-1","group_id":7,"protocol":"openai","endpoint":"/responses","capabilities":{"reasoning":null,"vision":false,"context_window":null},"prices":{"input":9007199254740993,"_fractional":{"input":"123"}},"pricing_version":{"version":9007199254740993}}]}`)
			return true
		}
		if strings.HasPrefix(r.URL.Path, APIPrefix+"/requests/") {
			status := strings.TrimPrefix(r.URL.Path, APIPrefix+"/requests/")
			out := Settlement{RequestID: status, Currency: "CNY", Status: SettlementStatus(status)}
			amount := int64(0)
			switch status {
			case "estimated":
				out.EstimatedMicros = &amount
			case "settled":
				out.SettledMicros = &amount
			case "missing":
				out.Status = SettlementSettled
			case "fabricated":
				out.Status = SettlementPending
				out.SettledMicros = &amount
			}
			writeJSON(w, out)
			return true
		}
		return false
	}
	c := f.client(t, &memoryStore{})
	login(t, c)
	m, err := c.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	model := m.Models[0]
	if model.PublicationID != "publication-1" || model.Endpoint != "/responses" || model.Capabilities.Reasoning != nil || model.Capabilities.Vision == nil || *model.Capabilities.Vision || model.Capabilities.ContextWindow != nil || *model.Prices.Input != 9007199254740993 || string(model.PricingVersion["version"]) != "9007199254740993" {
		t.Fatal("model contract drift")
	}
	for _, status := range []string{"estimated", "pending", "settled", "unknown", "missing", "fabricated", "invalid"} {
		s, e := c.Request(context.Background(), status)
		if status == "missing" || status == "fabricated" || status == "invalid" {
			if !errors.Is(e, ErrProtocol) {
				t.Fatalf("invalid settlement accepted: %s", status)
			}
		} else if e != nil || string(s.Status) != status {
			t.Fatalf("status %s: %v", status, e)
		}
	}
}

func TestCredentialScopeAndLifetimeValidation(t *testing.T) {
	for _, mode := range []string{"group", "unrestricted", "origin", "expired", "id", "key"} {
		t.Run(mode, func(t *testing.T) {
			f := newIssuer(t)
			f.hook = func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path != APIPrefix+"/credentials" {
					return false
				}
				group := int64(7)
				out := credentialResponse{APIKey: "synthetic-key", CredentialID: "cred-1", BaseURL: Router + "/v1", GroupID: &group, Restricted: true, ExpiresAt: time.Now().Add(time.Minute)}
				switch mode {
				case "group":
					group = 8
				case "unrestricted":
					out.Restricted = false
				case "origin":
					out.BaseURL = "https://evil.invalid/v1"
				case "expired":
					out.ExpiresAt = time.Now().Add(-time.Second)
				case "id":
					out.CredentialID = ""
				case "key":
					out.APIKey = "bad\nkey"
				}
				writeJSON(w, out)
				return true
			}
			c := f.client(t, &memoryStore{})
			login(t, c)
			group := int64(7)
			if _, e := c.Credentials(context.Background(), CredentialOptions{GroupID: &group}); !errors.Is(e, ErrProtocol) {
				t.Fatalf("invalid credential: %v", e)
			}
		})
	}
}

func TestRuntimeHTTPFailureKeepsStatusAndReceiptButStripsBody(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newIssuer(t)
			c := f.client(t, &memoryStore{})
			login(t, c)
			cr, e := c.Credentials(context.Background(), CredentialOptions{})
			if e != nil {
				t.Fatal(e)
			}
			router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Request-Id", "request:synthetic")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "reflected secret: "+r.Header.Get("Authorization"))
			}))
			defer router.Close()
			c.router = router.URL
			req, _ := http.NewRequest(http.MethodPost, router.URL+"/v1/chat/completions", nil)
			resp, e := cr.HTTPClient().Do(req)
			if e != nil {
				t.Fatalf("HTTP status became transient transport error: %v", e)
			}
			b, e := io.ReadAll(resp.Body)
			resp.Body.Close()
			if e != nil || len(b) != 0 || resp.StatusCode != status || resp.Header.Get("X-Request-Id") != "request:synthetic" || resp.Header.Get("Retry-After") != "1" || resp.Request.Header.Get("Authorization") != "" {
				t.Fatal("unsafe or unclassifiable failure")
			}
		})
	}
}

func TestRestoreRejectsWrongBindingAndRotatesExpiredSession(t *testing.T) {
	f := newIssuer(t)
	store := &memoryStore{}
	c := f.client(t, store)
	login(t, c)
	data, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"issuer", "client_id", "subject"} {
		var saved storedSession
		_ = json.Unmarshal(data, &saved)
		switch field {
		case "issuer":
			saved.Issuer = "https://wrong.invalid"
		case "client_id":
			saved.ClientID = "wrong"
		case "subject":
			saved.Subject = ""
		}
		b, _ := json.Marshal(saved)
		_ = store.Save(context.Background(), b)
		if _, e := c.Restore(context.Background()); !errors.Is(e, ErrStorage) {
			t.Fatalf("wrong binding %s: %v", field, e)
		}
	}
	var saved storedSession
	_ = json.Unmarshal(data, &saved)
	saved.ExpiresAt = time.Now().Add(-time.Minute)
	b, _ := json.Marshal(saved)
	_ = store.Save(context.Background(), b)
	if _, e := c.Restore(context.Background()); e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	count := f.refreshCount
	f.mu.Unlock()
	if count != 1 {
		t.Fatal("expired restore did not rotate")
	}
}

func TestRefreshBindingMismatchFailsClosed(t *testing.T) {
	for _, mode := range []string{"nonce", "subject", "session", "device", "reused-refresh"} {
		t.Run(mode, func(t *testing.T) {
			f := newIssuer(t)
			var first bool
			var oldRefresh string
			f.mutateToken = func(r *tokenResponse) {
				if !first {
					first = true
					oldRefresh = r.RefreshToken
					return
				}
				switch mode {
				case "nonce":
					r.Nonce = "different_nonce_123456789"
				case "subject":
					r.Subject = "different-subject"
				case "session":
					r.SessionID = "different-session"
				case "device":
					r.DeviceID = "different_device_123456"
				case "reused-refresh":
					r.RefreshToken = oldRefresh
				}
			}
			store := &memoryStore{}
			c := f.client(t, store)
			login(t, c)
			if e := c.Refresh(context.Background()); !errors.Is(e, ErrProtocol) {
				t.Fatalf("binding mismatch %s: %v", mode, e)
			}
			if c.Snapshot().Authenticated {
				t.Fatal("mismatched rotation installed")
			}
			if _, e := store.Load(context.Background()); !errors.Is(e, ErrNotFound) {
				t.Fatal("stale refresh token retained")
			}
		})
	}
}

func TestRejectedRefreshReportsAuthenticationFailure(t *testing.T) {
	f := newIssuer(t)
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == APIPrefix+"/models" {
			w.WriteHeader(401)
			return true
		}
		if r.URL.Path == APIPrefix+"/token" {
			_ = r.ParseForm()
			if r.Form.Get("grant_type") == "refresh_token" {
				w.WriteHeader(400)
				writeJSON(w, map[string]string{"error": "invalid_grant"})
				return true
			}
		}
		return false
	}
	c := f.client(t, &memoryStore{})
	login(t, c)
	if _, err := c.Models(context.Background()); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("refresh auth error lost: %v", err)
	}
	if c.Snapshot().Authenticated {
		t.Fatal("failed auth retained session")
	}
}

func TestRestoreAfterRotationFailureDoesNotRetainConsumedToken(t *testing.T) {
	for _, mode := range []string{"profile-failure", "save-failure", "invalid-grant"} {
		t.Run(mode, func(t *testing.T) {
			f := newIssuer(t)
			var restoring atomic.Bool
			f.hook = func(w http.ResponseWriter, r *http.Request) bool {
				if !restoring.Load() {
					return false
				}
				if mode == "profile-failure" && r.URL.Path == APIPrefix+"/session" {
					w.WriteHeader(503)
					return true
				}
				if mode == "invalid-grant" && r.URL.Path == APIPrefix+"/token" {
					w.WriteHeader(400)
					writeJSON(w, map[string]string{"error": "invalid_grant"})
					return true
				}
				return false
			}
			store := &memoryStore{}
			c := f.client(t, store)
			login(t, c)
			data, e := store.Load(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			var saved storedSession
			_ = json.Unmarshal(data, &saved)
			saved.ExpiresAt = time.Now().Add(-time.Minute)
			b, _ := json.Marshal(saved)
			_ = store.Save(context.Background(), b)
			_ = c.Close()
			if mode == "save-failure" {
				store.mu.Lock()
				store.saveErr = errors.New("SYNTHETIC storage error")
				store.mu.Unlock()
			}
			restoring.Store(true)
			next := f.client(t, store)
			if _, e = next.Restore(context.Background()); e == nil {
				t.Fatal("failed restore succeeded")
			}
			if _, e = store.Load(context.Background()); !errors.Is(e, ErrNotFound) {
				t.Fatal("consumed refresh token retained")
			}
			if next.Snapshot().Authenticated {
				t.Fatal("failed restore authenticated")
			}
		})
	}
}

func TestNewAndBrowserValidation(t *testing.T) {
	for _, opts := range []Options{{}, {Store: &memoryStore{}, DeviceID: "short"}, {Store: &memoryStore{}, LoginTimeout: 6 * time.Minute}, {Store: &memoryStore{}, DeviceName: strings.Repeat("x", 129)}} {
		if _, e := New(opts); !errors.Is(e, ErrInvalid) {
			t.Fatalf("invalid options: %v", e)
		}
	}
	for _, u := range []string{"http://sub.aichat.diy/api/v1/desktop/authorize", "https://evil.invalid/api/v1/desktop/authorize", Issuer + "/wrong", Issuer + APIPrefix + "/authorize#fragment"} {
		if e := OpenSystemBrowser(context.Background(), u); !errors.Is(e, ErrInvalid) {
			t.Fatalf("invalid browser URL: %v", e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := OpenSystemBrowser(ctx, Issuer+APIPrefix+"/authorize"); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
