package orcaaccount

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
)

func randomValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", ErrAuthorization
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

type callbackResult struct {
	code string
	err  error
}

type callback struct {
	uri    string
	result chan callbackResult
	server *http.Server
	done   chan struct{}
	stop   func() bool
}

func listenCallback(ctx context.Context, state string) (*callback, error) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, ErrAuthorization
	}
	cb := &callback{uri: "http://" + l.Addr().String() + "/callback", result: make(chan callbackResult, 1), done: make(chan struct{})}
	var used atomic.Bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		host, _, e := net.SplitHostPort(r.RemoteAddr)
		if e != nil || host != "127.0.0.1" || r.Host != l.Addr().String() || r.URL.Path != "/callback" || r.Method != http.MethodGet {
			http.Error(w, "Invalid callback", http.StatusBadRequest)
			return
		}
		q, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil || len(r.URL.RawQuery) > 8192 {
			http.Error(w, "Invalid callback", 400)
			return
		}
		for _, v := range q {
			if len(v) != 1 {
				http.Error(w, "Invalid callback", 400)
				return
			}
		}
		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 || q.Get("iss") != Issuer {
			http.Error(w, "Invalid callback", 400)
			return
		}
		code, oauthErr := q.Get("code"), q.Get("error")
		if (code == "") == (oauthErr == "") || (code != "" && (!validSecret(code) || len(code) > 4096)) {
			http.Error(w, "Invalid callback", 400)
			return
		}
		if !used.CompareAndSwap(false, true) {
			http.Error(w, "Callback already consumed", 409)
			return
		}
		result := callbackResult{code: code}
		if oauthErr != "" {
			result.err = ErrAuthorization
		}
		cb.result <- result
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "Return to ORCA. This window may be closed.")
	})
	cb.server = &http.Server{Handler: handler, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 3 * time.Second, MaxHeaderBytes: 16 << 10, ErrorLog: log.New(io.Discard, "", 0)}
	cb.stop = context.AfterFunc(ctx, func() { _ = cb.server.Close() })
	go func() { defer close(cb.done); _ = cb.server.Serve(l) }()
	return cb, nil
}

func (cb *callback) close() {
	cb.stop()
	_ = cb.server.Close()
	<-cb.done
}

// Login starts a new account generation and a one-use, short-lived loopback
// callback. The system browser is the only surface that sees the authorize URL.
func (c *Client) Login(ctx context.Context) (Profile, error) {
	gen, life, err := c.begin(ctx, true)
	if err != nil {
		return Profile{}, err
	}
	ctx, cancel := linkContext(ctx, life)
	defer cancel()
	ctx, deadline := context.WithTimeout(ctx, c.loginTimeout)
	defer deadline()
	state, err := randomValue()
	if err != nil {
		return Profile{}, err
	}
	nonce, err := randomValue()
	if err != nil {
		return Profile{}, err
	}
	verifier, err := randomValue()
	if err != nil {
		return Profile{}, err
	}
	cb, err := listenCallback(ctx, state)
	if err != nil {
		return Profile{}, err
	}
	defer cb.close()
	q := url.Values{"response_type": {"code"}, "client_id": {ClientID}, "redirect_uri": {cb.uri}, "state": {state},
		"nonce": {nonce}, "code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"},
		"scope": {"desktop"}, "device_id": {c.deviceID}}
	if c.deviceName != "" {
		q.Set("device_name", c.deviceName)
	}
	if err = c.openBrowser(ctx, c.issuer+APIPrefix+"/authorize?"+q.Encode()); err != nil {
		if ctx.Err() != nil {
			return Profile{}, ctx.Err()
		}
		return Profile{}, ErrBrowser
	}
	var result callbackResult
	select {
	case <-ctx.Done():
		return Profile{}, ctx.Err()
	case result = <-cb.result:
	}
	// Stop accepting connections before exchanging the one-use code.
	_ = cb.server.Close()
	if result.err != nil {
		return Profile{}, result.err
	}
	var response tokenResponse
	err = c.formRequest(ctx, "/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {ClientID},
		"code": {result.code}, "redirect_uri": {cb.uri}, "code_verifier": {verifier}}, &response)
	if err != nil {
		return Profile{}, err
	}
	if subtle.ConstantTimeCompare([]byte(response.Nonce), []byte(nonce)) != 1 {
		return Profile{}, ErrProtocol
	}
	s, err := c.validateTokens(response)
	if err != nil {
		return Profile{}, err
	}
	if s.DeviceID != c.deviceID {
		return Profile{}, ErrProtocol
	}
	p, err := c.fetchProfile(ctx, s)
	if err != nil {
		return Profile{}, err
	}
	if err = c.install(ctx, gen, s, p); err != nil {
		return Profile{}, err
	}
	return p, nil
}
