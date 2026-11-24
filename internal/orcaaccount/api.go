package orcaaccount

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

func (c *Client) Session(ctx context.Context) (Profile, error) {
	var p Profile
	gen, err := c.authorized(ctx, http.MethodGet, "/session", nil, &p)
	if err != nil {
		return Profile{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != gen {
		return Profile{}, ErrStale
	}
	if p.Subject != c.tokens.Subject || p.SessionID != c.tokens.SessionID || p.DeviceID != c.tokens.DeviceID || !p.ExpiresAt.After(time.Now()) {
		return Profile{}, ErrProtocol
	}
	c.profile = p
	c.profile.Groups = append([]Group(nil), p.Groups...)
	return p, nil
}

func (c *Client) Models(ctx context.Context) (Models, error) {
	var result Models
	_, err := c.authorized(ctx, http.MethodGet, "/models", nil, &result)
	if err != nil {
		return Models{}, err
	}
	if result.Currency != "CNY" {
		return Models{}, ErrProtocol
	}
	for _, m := range result.Models {
		if !ValidReasoningCapabilities(m.Capabilities) {
			return Models{}, ErrProtocol
		}
		if m.ID == "" || m.GroupID <= 0 || (m.Protocol != "openai" && m.Protocol != "anthropic") {
			return Models{}, ErrProtocol
		}
	}
	return result, nil
}

func (c *Client) Wallet(ctx context.Context) (Wallet, error) {
	var result Wallet
	_, err := c.authorized(ctx, http.MethodGet, "/wallet", nil, &result)
	if err != nil {
		return Wallet{}, err
	}
	if result.Currency != "CNY" {
		return Wallet{}, ErrProtocol
	}
	for _, g := range result.ScopedGifts {
		if g.ID == "" || g.GroupID <= 0 {
			return Wallet{}, ErrProtocol
		}
	}
	return result, nil
}

func (c *Client) Request(ctx context.Context, id string) (Settlement, error) {
	if len(id) == 0 || len(id) > 512 || id == "." || id == ".." || strings.ContainsAny(id, "/\\\x00\r\n") {
		return Settlement{}, ErrInvalid
	}
	var result Settlement
	_, err := c.authorized(ctx, http.MethodGet, "/requests/"+url.PathEscape(id), nil, &result)
	if err != nil {
		return Settlement{}, err
	}
	if result.RequestID != id || result.Currency != "CNY" {
		return Settlement{}, ErrProtocol
	}
	if result.SettledMicros != nil && *result.SettledMicros < 0 ||
		result.EstimatedMicros != nil && *result.EstimatedMicros < 0 {
		return Settlement{}, ErrProtocol
	}
	switch result.Status {
	case SettlementSettled:
		if result.SettledMicros == nil {
			return Settlement{}, ErrProtocol
		}
	case SettlementEstimated, SettlementPending, SettlementUnknown:
		if result.SettledMicros != nil {
			return Settlement{}, ErrProtocol
		}
	default:
		return Settlement{}, ErrProtocol
	}
	return result, nil
}

// Credential is an opaque, backend-only capability, not a token DTO. It cannot
// be marshaled into a UI event or config with its API key intact.
type Credential struct {
	client     *Client
	generation uint64
	key        string
	info       CredentialInfo
}

func (*Credential) String() string                 { return "orcaaccount.Credential{secret:redacted}" }
func (c *Credential) GoString() string             { return c.String() }
func (c *Credential) MarshalJSON() ([]byte, error) { return json.Marshal(c.Info()) }
func (c *Credential) Info() CredentialInfo {
	info := c.info
	if info.GroupID != nil {
		v := *info.GroupID
		info.GroupID = &v
	}
	return info
}

type credentialTransport struct{ credential *Credential }

func (t credentialTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.credential.Do(r)
}

// HTTPClient is for provider.Config.HTTPClient, never the frontend bridge. It
// preserves Idempotency-Key and X-Request-Id; account identity stays private.
func (cr *Credential) HTTPClient() *http.Client {
	return &http.Client{Transport: credentialTransport{credential: cr},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (c *Client) Credentials(ctx context.Context, opts CredentialOptions) (*Credential, error) {
	if opts.GroupID != nil && *opts.GroupID <= 0 {
		return nil, ErrInvalid
	}
	var wire credentialResponse
	gen, err := c.authorized(ctx, http.MethodPost, "/credentials", opts, &wire)
	if err != nil {
		return nil, err
	}
	if !validSecret(wire.APIKey) || !wire.Restricted || wire.CredentialID == "" || wire.GroupID == nil || *wire.GroupID <= 0 ||
		wire.BaseURL != Router+"/v1" || !wire.ExpiresAt.After(time.Now()) ||
		(opts.GroupID != nil && *wire.GroupID != *opts.GroupID) {
		return nil, ErrProtocol
	}
	return &Credential{client: c, generation: gen, key: wire.APIKey, info: CredentialInfo{CredentialID: wire.CredentialID,
		BaseURL: wire.BaseURL, GroupID: wire.GroupID, Restricted: true, ExpiresAt: wire.ExpiresAt}}, nil
}

// Do only attaches the runtime API key to the fixed router's /v1 namespace.
// Redirects are rejected, there are no application retries, and Response.Request is
// sanitized. Close the response body to release the account cancellation link.
func (cr *Credential) Do(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || cr.client == nil {
		return nil, ErrInvalid
	}
	c := cr.client
	base, _ := url.Parse(c.router)
	u := req.URL
	if u.Scheme != base.Scheme || u.Host != base.Host || u.User != nil || u.Fragment != "" || u.Opaque != "" ||
		!strings.HasPrefix(u.Path, "/v1/") || strings.Contains(u.Path, "\\") ||
		strings.Contains(u.Path, "/../") || strings.HasSuffix(u.Path, "/..") || strings.Contains(u.Path, "/./") || req.Host != "" && req.Host != base.Host {
		return nil, ErrInvalid
	}
	if !cr.info.ExpiresAt.After(time.Now()) {
		return nil, ErrUnauthenticated
	}
	c.mu.Lock()
	if cr.generation != c.generation {
		c.mu.Unlock()
		return nil, ErrStale
	}
	life := c.lifetime
	c.mu.Unlock()
	ctx, cancel := linkContext(req.Context(), life)
	r := req.Clone(ctx)
	r.Header.Del("Cookie")
	r.Header.Del("Proxy-Authorization")
	r.Header.Del("X-API-Key")
	r.Header.Del("API-Key")
	r.Header.Set("Authorization", "Bearer "+cr.key)
	resp, err := c.routerHTTP.Do(r)
	if err != nil {
		cancel()
		if e := c.check(cr.generation); e != nil {
			return nil, e
		}
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		return nil, ErrNetwork
	}
	if err = c.check(cr.generation); err != nil {
		resp.Body.Close()
		cancel()
		return nil, err
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		resp.Body.Close()
		cancel()
		return nil, ErrNetwork
	}
	safe := req.Clone(req.Context())
	safe.Header.Del("Authorization")
	safe.Header.Del("Cookie")
	safe.Header.Del("Proxy-Authorization")
	safe.Header.Del("X-API-Key")
	safe.Header.Del("API-Key")
	resp.Request = safe
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Keep HTTP status/correlation for provider retry/auth classification,
		// but never allow a reflected key in an error body into UI or logs.
		resp.Body.Close()
		resp.Body = io.NopCloser(strings.NewReader(""))
		resp.ContentLength = 0
		resp.Header = resp.Header.Clone()
		resp.Header.Del("Content-Length")
		resp.Header.Del("Content-Encoding")
		cancel()
		return resp, nil
	}
	resp.Body = &accountBody{ReadCloser: resp.Body, ctx: ctx, cancel: cancel}
	return resp, nil
}

type accountBody struct {
	io.ReadCloser
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func (b *accountBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := b.ReadCloser.Read(p)
	if canceled := b.ctx.Err(); canceled != nil {
		clear(p[:n])
		return 0, canceled
	}
	if err != nil {
		b.cancel()
	}
	return n, err
}

func (b *accountBody) Close() error {
	b.once.Do(b.cancel)
	return b.ReadCloser.Close()
}
