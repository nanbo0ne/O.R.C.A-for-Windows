package orcaaccount

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Store is a trusted backend-only secret sink. Save must be atomic. Production
// callers must use NewSystemStore; the in-memory implementation lives in tests.
type Store interface {
	Load(context.Context) ([]byte, error)
	Save(context.Context, []byte) error
	Delete(context.Context) error
}

type BrowserOpener func(context.Context, string) error

type Options struct {
	Store       Store
	OpenBrowser BrowserOpener
	// HTTPClient is a trusted backend dependency, never UI-controlled. Redirects
	// and cookies are disabled on a copy; the supplied client is not modified.
	HTTPClient   *http.Client
	DeviceID     string
	DeviceName   string
	LoginTimeout time.Duration
}

type storedSession struct {
	Issuer       string    `json:"issuer"`
	ClientID     string    `json:"client_id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	SessionID    string    `json:"session_id"`
	DeviceID     string    `json:"device_id"`
	Subject      string    `json:"subject"`
	Nonce        string    `json:"nonce"`
}

func (storedSession) String() string     { return "[redacted stored session]" }
func (s storedSession) GoString() string { return s.String() }

type refreshCall struct {
	done chan struct{}
	err  error
}

type generationContextKey struct{}
type generationBinding struct {
	client *Client
	gen    uint64
}

type Client struct {
	mu           sync.Mutex
	store        Store
	openBrowser  BrowserOpener
	http         *http.Client
	routerHTTP   *http.Client
	issuer       string
	router       string
	deviceID     string
	deviceName   string
	loginTimeout time.Duration
	generation   uint64
	lifetime     context.Context
	cancel       context.CancelFunc
	tokens       storedSession
	profile      Profile
	refresh      *refreshCall
	closed       bool
}

func (*Client) String() string     { return "orcaaccount.Client{secrets:redacted}" }
func (c *Client) GoString() string { return c.String() }

var devicePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

func New(opts Options) (*Client, error) {
	if opts.Store == nil || len(opts.DeviceName) > 128 || strings.ContainsAny(opts.DeviceName, "\x00\r\n") || opts.LoginTimeout < 0 || opts.LoginTimeout > 5*time.Minute {
		return nil, ErrInvalid
	}
	if opts.DeviceID == "" {
		var err error
		opts.DeviceID, err = randomValue()
		if err != nil {
			return nil, err
		}
	}
	if !devicePattern.MatchString(opts.DeviceID) {
		return nil, ErrInvalid
	}
	if opts.LoginTimeout == 0 {
		opts.LoginTimeout = 3 * time.Minute
	}
	if opts.OpenBrowser == nil {
		opts.OpenBrowser = OpenSystemBrowser
	}
	h := http.Client{Timeout: 20 * time.Second}
	if opts.HTTPClient != nil {
		h = *opts.HTTPClient
	}
	if h.Timeout == 0 {
		h.Timeout = 20 * time.Second
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	h.Jar = nil
	rh := h
	rh.Timeout = 0 // Router streams are bounded by the request and account lifetime.
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{store: opts.Store, openBrowser: opts.OpenBrowser, http: &h, routerHTTP: &rh,
		issuer: Issuer, router: Router, deviceID: opts.DeviceID, deviceName: opts.DeviceName,
		loginTimeout: opts.LoginTimeout, generation: 1, lifetime: ctx, cancel: cancel}, nil
}

func (c *Client) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.profile
	p.Groups = append([]Group(nil), p.Groups...)
	return Snapshot{Generation: c.generation, Closed: c.closed, Authenticated: c.tokens.AccessToken != "", Profile: p}
}

func (c *Client) CurrentSessionGeneration() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation
}

func (c *Client) GenerationContext(ctx context.Context, gen uint64) (context.Context, context.CancelFunc, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, nil, ErrClosed
	}
	if gen != c.generation {
		return nil, nil, ErrStale
	}
	if c.tokens.AccessToken == "" {
		return nil, nil, ErrUnauthenticated
	}
	linked, cancel := linkContext(ctx, c.lifetime)
	return context.WithValue(linked, generationContextKey{}, generationBinding{c, gen}), cancel, nil
}

// CancelContexts is local sign-out, with no network access. Logout additionally
// revokes server credentials. A storage error is surfaced, never hidden.
func (c *Client) CancelContexts() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	c.advanceLocked()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.store.Delete(ctx); err != nil {
		return storageError(err)
	}
	return nil
}

// Close is application shutdown, not logout: it cancels work and clears only
// memory. The encrypted store is preserved, no revocation request is sent, and
// this Client cannot be reused. A new Client may Restore on the next launch.
// An interrupted in-flight rotation can require sign-in again; Close never
// retries a potentially consumed refresh token or writes a stale response.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	c.cancel()
	c.generation++
	c.tokens = storedSession{}
	c.profile = Profile{}
	c.refresh = nil
	return nil
}

// advanceLocked is the linearization point for logout, account switching, and
// invalidation. No old-generation request may install credentials after it.
func (c *Client) advanceLocked() {
	c.cancel()
	c.generation++
	c.lifetime, c.cancel = context.WithCancel(context.Background())
	c.tokens = storedSession{}
	c.profile = Profile{}
	c.refresh = nil
}

func (c *Client) begin(ctx context.Context, erase bool) (uint64, context.Context, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, nil, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	c.advanceLocked()
	if erase {
		if err := c.store.Delete(ctx); err != nil {
			return 0, nil, storageError(err)
		}
	}
	return c.generation, c.lifetime, nil
}

func storageError(err error) error {
	if errors.Is(err, ErrUnsupported) {
		return ErrUnsupported
	}
	return ErrStorage
}

func linkContext(ctx, lifetime context.Context) (context.Context, context.CancelFunc) {
	linked, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(lifetime, cancel)
	if lifetime.Err() != nil {
		cancel()
	}
	return linked, func() { stop(); cancel() }
}

func (c *Client) check(gen uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	if c.generation != gen {
		return ErrStale
	}
	return nil
}

func (c *Client) invalidate(gen uint64) error {
	return c.discard(gen, ErrUnauthenticated)
}

func (c *Client) discard(gen uint64, reason error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != gen {
		return ErrStale
	}
	c.advanceLocked()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.store.Delete(ctx); err != nil {
		return storageError(err)
	}
	return reason
}

func (c *Client) persistLocked(ctx context.Context, session storedSession) error {
	b, err := json.Marshal(session)
	if err != nil {
		return ErrStorage
	}
	defer clear(b)
	if err = c.store.Save(ctx, b); err != nil {
		return storageError(err)
	}
	return nil
}

func (c *Client) install(ctx context.Context, gen uint64, session storedSession, profile Profile) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != gen {
		return ErrStale
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.persistLocked(ctx, session); err != nil {
		return err
	}
	c.tokens, c.profile = session, profile
	c.profile.Groups = append([]Group(nil), profile.Groups...)
	return nil
}

func (c *Client) validateTokens(r tokenResponse) (storedSession, error) {
	if r.TokenType != "Bearer" || !validSecret(r.AccessToken) || !validSecret(r.RefreshToken) ||
		r.ExpiresIn <= 0 || r.ExpiresIn > 300 || r.SessionID == "" || !devicePattern.MatchString(r.DeviceID) || !devicePattern.MatchString(r.Nonce) || r.Scope != "desktop" || r.Subject == "" || r.Issuer != Issuer {
		return storedSession{}, ErrProtocol
	}
	return storedSession{Issuer: Issuer, ClientID: ClientID, AccessToken: r.AccessToken, RefreshToken: r.RefreshToken,
		ExpiresAt: time.Now().Add(time.Duration(r.ExpiresIn) * time.Second), SessionID: r.SessionID, DeviceID: r.DeviceID, Subject: r.Subject, Nonce: r.Nonce}, nil
}

func validSecret(s string) bool {
	if len(s) == 0 || len(s) > 16384 {
		return false
	}
	for _, r := range s {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}

func (c *Client) fetchProfile(ctx context.Context, s storedSession) (Profile, error) {
	var p Profile
	if err := c.jsonRequest(ctx, http.MethodGet, "/session", s.AccessToken, nil, &p); err != nil {
		return Profile{}, err
	}
	if p.Subject != s.Subject || p.SessionID != s.SessionID || p.DeviceID != s.DeviceID || !p.ExpiresAt.After(time.Now()) {
		return Profile{}, ErrProtocol
	}
	return p, nil
}

// Restore validates the encrypted saved session against the issuer before it
// becomes visible as authenticated. No offline permission fallback is provided.
func (c *Client) Restore(ctx context.Context) (Profile, error) {
	gen, life, err := c.begin(ctx, false)
	if err != nil {
		return Profile{}, err
	}
	ctx, cancel := linkContext(ctx, life)
	defer cancel()
	c.mu.Lock()
	if c.generation != gen {
		c.mu.Unlock()
		return Profile{}, ErrStale
	}
	b, err := c.store.Load(ctx)
	c.mu.Unlock()
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, storageError(err)
	}
	defer clear(b)
	var s storedSession
	if json.Unmarshal(b, &s) != nil || s.Issuer != Issuer || s.ClientID != ClientID || !validSecret(s.AccessToken) ||
		!validSecret(s.RefreshToken) || s.SessionID == "" || !devicePattern.MatchString(s.DeviceID) || !devicePattern.MatchString(s.Nonce) || s.Subject == "" {
		return Profile{}, ErrStorage
	}
	rotated := false
	if !s.ExpiresAt.After(time.Now().Add(30 * time.Second)) {
		rotated = true
		s, err = c.refreshTokens(ctx, s)
		if err != nil {
			return Profile{}, c.discard(gen, err)
		}
	}
	p, err := c.fetchProfile(ctx, s)
	if err != nil {
		if rotated {
			return Profile{}, c.discard(gen, err)
		}
		if errors.Is(err, ErrUnauthenticated) {
			return Profile{}, c.invalidate(gen)
		}
		return Profile{}, err
	}
	if err = c.install(ctx, gen, s, p); err != nil {
		if rotated {
			return Profile{}, c.discard(gen, err)
		}
		return Profile{}, err
	}
	return p, nil
}

func (c *Client) refreshTokens(ctx context.Context, old storedSession) (storedSession, error) {
	var r tokenResponse
	err := c.formRequest(ctx, "/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {ClientID}, "refresh_token": {old.RefreshToken}}, &r)
	if err != nil {
		return storedSession{}, err
	}
	s, err := c.validateTokens(r)
	if err != nil {
		return storedSession{}, err
	}
	if s.SessionID != old.SessionID || s.DeviceID != old.DeviceID || s.Subject != old.Subject || s.Nonce != old.Nonce || s.RefreshToken == old.RefreshToken {
		return storedSession{}, ErrProtocol
	}
	return s, nil
}

// Refresh coalesces concurrent callers. A waiting caller may cancel without
// canceling another caller's rotation; logout always cancels the shared request.
func (c *Client) Refresh(ctx context.Context) error {
	c.mu.Lock()
	gen := c.generation
	c.mu.Unlock()
	_, err := c.access(ctx, gen, true, "")
	return err
}

func (c *Client) access(ctx context.Context, gen uint64, force bool, rejected string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return "", ErrClosed
	}
	if c.generation != gen {
		c.mu.Unlock()
		return "", ErrStale
	}
	if c.tokens.AccessToken == "" {
		c.mu.Unlock()
		return "", ErrUnauthenticated
	}
	if (rejected != "" && c.tokens.AccessToken != rejected) || (!force && c.tokens.ExpiresAt.After(time.Now().Add(30*time.Second))) {
		token := c.tokens.AccessToken
		c.mu.Unlock()
		return token, nil
	}
	call := c.refresh
	if call == nil {
		call = &refreshCall{done: make(chan struct{})}
		c.refresh = call
		old, life := c.tokens, c.lifetime
		go c.runRefresh(gen, life, old, call)
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		// A failed refresh cancels the account lifetime while holding mu. Wait
		// for that state transition so its auth/storage error wins over the
		// incidental cancellation; independent caller cancellation still wins.
		c.mu.Lock()
		defer c.mu.Unlock()
		select {
		case <-call.done:
			if call.err != nil {
				return "", call.err
			}
		default:
		}
		if c.generation != gen {
			return "", ErrStale
		}
		return "", ctx.Err()
	case <-call.done:
		if call.err != nil {
			return "", call.err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.generation != gen {
			return "", ErrStale
		}
		return c.tokens.AccessToken, nil
	}
}

func (c *Client) runRefresh(gen uint64, life context.Context, old storedSession, call *refreshCall) {
	ctx, cancel := context.WithTimeout(life, 20*time.Second)
	defer cancel()
	s, err := c.refreshTokens(ctx, old)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != gen {
		err = ErrStale
	} else {
		if err == nil {
			err = c.persistLocked(ctx, s)
		}
		if err == nil {
			c.tokens = s
		} else {
			// A lost/invalid refresh response may have rotated the server token.
			// Fail closed rather than retrying the old refresh token and risking replay.
			c.advanceLocked()
			deleteCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
			if e := c.store.Delete(deleteCtx); e != nil {
				err = storageError(e)
			}
			done()
		}
		c.refresh = nil
	}
	call.err = err
	close(call.done)
}

// Logout never touches history. Local clearing happens before remote revocation.
func (c *Client) Logout(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	old := c.tokens
	c.advanceLocked()
	deleteCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
	err := c.store.Delete(deleteCtx)
	done()
	c.mu.Unlock()
	var revokeErr error
	if old.RefreshToken != "" {
		revokeErr = c.formRequest(ctx, "/revoke", url.Values{"client_id": {ClientID}, "token": {old.RefreshToken}, "token_type_hint": {"refresh_token"}}, nil)
	}
	if err != nil {
		return storageError(err)
	}
	return revokeErr
}
