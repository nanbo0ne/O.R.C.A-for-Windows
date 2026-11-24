package orcaaccount_test

// This entire transport and consent driver is test-only. It cannot be linked
// into the desktop binary and has no route to public DNS or system proxy trust.
import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
	"golang.org/x/net/html"
)

const candidateSHA = "2c7c4252f1794655aac4a0aa6ca1362bae1c87962a8611655f6984b2734108f8"
const candidateInputPath = "/run/orca-stage/client-input.json"
const candidateCAPath = "/run/orca-stage/ca.pem"
const candidateDialAddr = "127.0.0.1:8443"
const candidateText = "orca-stage-synthetic"

var candidateRunPattern = regexp.MustCompile(`^orca-startup-[a-f0-9]{32}$`)
var candidateControlPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var errCandidate = errors.New("candidate fixture boundary or contract rejected")

type candidateModel struct {
	GroupID       int64  `json:"group_id"`
	ID            string `json:"id"`
	Protocol      string `json:"protocol"`
	Endpoint      string `json:"endpoint"`
	SettledMicros int64  `json:"settled_micros"`
}

type candidateManifest struct {
	Version              int              `json:"version"`
	RunID                string           `json:"run_id"`
	CandidateSHA256      string           `json:"candidate_sha256"`
	DialAddr             string           `json:"dial_addr"`
	CAFile               string           `json:"ca_file"`
	SessionCookie        string           `json:"session_cookie"`
	Subject              string           `json:"subject"`
	Models               []candidateModel `json:"models"`
	ControlToken         string           `json:"control_token"`
	MaxInferenceAttempts int              `json:"max_inference_attempts"`
	MaxUpstreamCalls     int              `json:"max_upstream_calls"`
	SyntheticText        string           `json:"synthetic_text"`
	MaxOutputTokens      int              `json:"max_output_tokens"`
}

func (candidateManifest) String() string     { return "[private candidate manifest]" }
func (m candidateManifest) GoString() string { return m.String() }

func candidateDecode(r io.Reader, dst any) error {
	b, err := io.ReadAll(io.LimitReader(r, 32769))
	if err != nil || len(b) > 32768 {
		return errCandidate
	}
	defer clear(b)
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil || d.Decode(new(any)) != io.EOF {
		return errCandidate
	}
	return nil
}

func (m candidateManifest) validate(run string) error {
	if m.Version != 1 || !candidateRunPattern.MatchString(run) || m.RunID != run || m.CandidateSHA256 != candidateSHA ||
		m.DialAddr != candidateDialAddr || m.CAFile != candidateCAPath || m.SyntheticText != candidateText ||
		m.MaxOutputTokens != 16 || m.MaxInferenceAttempts != 8 || m.MaxUpstreamCalls != 4 ||
		!candidateControlPattern.MatchString(m.ControlToken) || len(m.Subject) == 0 || len(m.Subject) > 128 ||
		len(m.SessionCookie) < 16 || len(m.SessionCookie) > 8192 || len(m.Models) != 2 {
		return errCandidate
	}
	for _, c := range m.SessionCookie {
		if c < 0x21 || c > 0x7e || strings.ContainsRune(";\"\\,", c) {
			return errCandidate
		}
	}
	seen := map[string]bool{}
	for _, model := range m.Models {
		endpoint := map[string]string{"openai": "/chat/completions", "anthropic": "/messages"}[model.Protocol]
		if endpoint == "" || model.Endpoint != endpoint || model.ID != "orca-stage-"+model.Protocol ||
			model.GroupID <= 0 || model.SettledMicros != 18 || seen[model.Protocol] {
			return errCandidate
		}
		seen[model.Protocol] = true
	}
	return nil
}

func candidatePrivateRead(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() <= 0 || info.Size() > 32768 {
		return nil, errCandidate
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errCandidate
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errCandidate
	}
	b, err := io.ReadAll(io.LimitReader(f, 32769))
	if err != nil || len(b) > 32768 {
		return nil, errCandidate
	}
	return b, nil
}

// Linux production storage remains unsupported. These synthetic session bytes
// live only in this test process, including its Close/Restore recovery exercise.
type candidateMemoryStore struct {
	mu   sync.Mutex
	data []byte
}

func (s *candidateMemoryStore) Load(ctx context.Context) ([]byte, error) {
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
func (s *candidateMemoryStore) Save(ctx context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	clear(s.data)
	s.data = bytes.Clone(data)
	return nil
}
func (s *candidateMemoryStore) Delete(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	clear(s.data)
	s.data = nil
	return nil
}

type candidateRoundTrip func(*http.Request) (*http.Response, error)

func (f candidateRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type candidateGuard struct {
	next     http.RoundTripper
	attempts atomic.Int64
	requests atomic.Int64
	denied   atomic.Int64
}

func candidateRoute(r *http.Request) (inference bool, allowed bool) {
	if r == nil || r.URL == nil {
		return false, false
	}
	u := r.URL
	if u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.RawPath != "" ||
		(r.Host != "" && r.Host != u.Host) {
		return false, false
	}
	if u.Host == "router.aichat.diy" {
		return true, r.Method == http.MethodPost && u.RawQuery == "" &&
			(u.Path == "/v1/chat/completions" || u.Path == "/v1/messages")
	}
	if u.Host != "sub.aichat.diy" {
		return false, false
	}
	if u.Path == orcaaccount.APIPrefix+"/authorize" {
		return false, r.Method == http.MethodGet || (r.Method == http.MethodPost && u.RawQuery == "")
	}
	if u.Path == "/__orca_fixture/stats" {
		q, err := url.ParseQuery(u.RawQuery)
		return false, err == nil && r.Method == http.MethodGet && len(q) == 1 && len(q["request_id"]) == 1 && candidateRequestID(q.Get("request_id"))
	}
	if u.RawQuery != "" {
		return false, false
	}
	switch u.Path {
	case orcaaccount.APIPrefix + "/token", orcaaccount.APIPrefix + "/revoke", orcaaccount.APIPrefix + "/credentials", "/__orca_fixture/control":
		return false, r.Method == http.MethodPost
	case orcaaccount.APIPrefix + "/session", orcaaccount.APIPrefix + "/models", orcaaccount.APIPrefix + "/wallet":
		return false, r.Method == http.MethodGet
	}
	if strings.HasPrefix(u.Path, orcaaccount.APIPrefix+"/requests/") {
		id := strings.TrimPrefix(u.Path, orcaaccount.APIPrefix+"/requests/")
		return false, r.Method == http.MethodGet && candidateRequestID(id)
	}
	return false, false
}

func candidateRequestID(id string) bool {
	if id == "" || len(id) > 512 || id == "." || id == ".." {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_:.", c)) {
			return false
		}
	}
	return true
}

func (g *candidateGuard) RoundTrip(r *http.Request) (*http.Response, error) {
	inference, allowed := candidateRoute(r)
	if !allowed || g.requests.Add(1) > 256 {
		g.denied.Add(1)
		return nil, errCandidate
	}
	if inference && (g.attempts.Add(1) > 8 || !candidateRequestID(r.Header.Get("Idempotency-Key"))) {
		g.denied.Add(1)
		return nil, errCandidate
	}
	// Disable net/http's implicit replay on a reused connection. Even a request
	// with an idempotency key must cross the fixture ingress at most once.
	copy := r.Clone(r.Context())
	copy.GetBody = nil
	return g.next.RoundTrip(copy)
}

func candidateDial(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" || (addr != "sub.aichat.diy:443" && addr != "router.aichat.diy:443") {
		return nil, errCandidate
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	return dialer.DialContext(ctx, "tcp4", candidateDialAddr)
}

func candidateHTTP(ca []byte) (*http.Client, *candidateGuard, *http.Transport, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, nil, nil, errCandidate
	}
	transport := &http.Transport{
		Proxy: nil, DialContext: candidateDial,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool},
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second,
		DisableKeepAlives: true, MaxResponseHeaderBytes: 32768,
	}
	guard := &candidateGuard{next: transport}
	return &http.Client{Transport: guard, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, guard, transport, nil
}

func candidateConsent(body io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(body, 32769))
	if err != nil || len(data) > 32768 {
		return "", errCandidate
	}
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return "", errCandidate
	}
	attr := func(n *html.Node, name string) string {
		for _, a := range n.Attr {
			if a.Key == name {
				return a.Val
			}
		}
		return ""
	}
	forms, tokens := 0, 0
	var token string
	var visit func(*html.Node, bool)
	visit = func(n *html.Node, inForm bool) {
		if n.Type == html.ElementNode && n.Data == "form" {
			forms++
			inForm = strings.EqualFold(attr(n, "method"), "post") && attr(n, "action") == orcaaccount.APIPrefix+"/authorize"
		}
		if n.Type == html.ElementNode && n.Data == "input" && attr(n, "name") == "consent" {
			tokens++
			if inForm && attr(n, "type") == "hidden" {
				token = attr(n, "value")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c, inForm)
		}
	}
	visit(doc, false)
	if forms != 1 || tokens != 1 || len(token) < 16 || len(token) > 8192 {
		return "", errCandidate
	}
	return token, nil
}

func candidateCallback(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Path != "/callback" ||
		u.User != nil || u.Opaque != "" || u.Fragment != "" || u.RawPath != "" || u.RawQuery != "" {
		return nil, errCandidate
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil || p < 1024 || p > 65535 || u.Host != "127.0.0.1:"+strconv.Itoa(p) {
		return nil, errCandidate
	}
	return u, nil
}

func candidateRedirect(raw string, callback *url.URL, state, decision string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != callback.Scheme || u.Host != callback.Host || u.Path != callback.Path ||
		u.User != nil || u.Opaque != "" || u.Fragment != "" || u.RawPath != "" {
		return nil, errCandidate
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || q.Get("state") != state || q.Get("iss") != orcaaccount.Issuer {
		return nil, errCandidate
	}
	for k, v := range q {
		if len(v) != 1 || (k != "code" && k != "state" && k != "iss" && k != "error") {
			return nil, errCandidate
		}
	}
	if decision == "allow" {
		if q.Get("code") == "" || q.Get("error") != "" {
			return nil, errCandidate
		}
	} else if decision != "deny" || q.Get("error") != "access_denied" || q.Get("code") != "" {
		return nil, errCandidate
	}
	return u, nil
}

type candidateBrowser struct {
	http     *http.Client
	cookie   string
	decision string
}

func (b *candidateBrowser) open(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "sub.aichat.diy" || u.Path != orcaaccount.APIPrefix+"/authorize" {
		return errCandidate
	}
	q := u.Query()
	if q.Get("client_id") != orcaaccount.ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
		len(q.Get("code_challenge")) != 43 || len(q.Get("state")) < 16 || len(q.Get("nonce")) < 16 {
		return errCandidate
	}
	callback, err := candidateCallback(q.Get("redirect_uri"))
	if err != nil {
		return errCandidate
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return errCandidate
	}
	req.AddCookie(&http.Cookie{Name: "__Host-sub_sso", Value: b.cookie})
	resp, err := b.http.Do(req)
	if err != nil {
		return errCandidate
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return errCandidate
	}
	consent, err := candidateConsent(resp.Body)
	resp.Body.Close()
	if err != nil {
		return errCandidate
	}
	form := url.Values{"consent": {consent}, "decision": {b.decision}}
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, orcaaccount.Issuer+orcaaccount.APIPrefix+"/authorize", strings.NewReader(form.Encode()))
	if err != nil {
		return errCandidate
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", orcaaccount.Issuer)
	req.AddCookie(&http.Cookie{Name: "__Host-sub_sso", Value: b.cookie})
	resp, err = b.http.Do(req)
	if err != nil {
		return errCandidate
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		return errCandidate
	}
	target, err := candidateRedirect(resp.Header.Get("Location"), callback, q.Get("state"), b.decision)
	if err != nil {
		return errCandidate
	}
	// The only plain HTTP exception is this login's exact ephemeral callback.
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network != "tcp" || addr != callback.Host {
			return nil, errCandidate
		}
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp4", callback.Host)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return errCandidate
	}
	resp, err = client.Do(req)
	if err != nil {
		return errCandidate
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errCandidate
	}
	return nil
}

type candidateStats struct {
	RequestID                string `json:"request_id"`
	IngressAttempts          int64  `json:"ingress_attempts"`
	UpstreamCalls            int64  `json:"upstream_calls"`
	ResponseHeadersSent      bool   `json:"response_headers_sent"`
	FirstDeltaSent           bool   `json:"first_delta_sent"`
	UpstreamContextCancelled bool   `json:"upstream_context_cancelled"`
	IngressContextCancelled  bool   `json:"ingress_context_cancelled"`
	UpstreamClosed           bool   `json:"upstream_closed"`
	TerminalUsageSent        bool   `json:"terminal_usage_sent"`
	TotalIngressAttempts     int64  `json:"total_ingress_attempts"`
	TotalUpstreamCalls       int64  `json:"total_upstream_calls"`
}

func candidateControl(ctx context.Context, client *http.Client, token, id string) error {
	body, _ := json.Marshal(struct {
		RequestID string `json:"request_id"`
		Mode      string `json:"mode"`
	}{id, "stall_after_first_chunk"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, orcaaccount.Issuer+"/__orca_fixture/control", bytes.NewReader(body))
	if err != nil {
		return errCandidate
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return errCandidate
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return errCandidate
	}
	return nil
}

func candidateReadStats(ctx context.Context, client *http.Client, token, id string) (candidateStats, error) {
	var out candidateStats
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, orcaaccount.Issuer+"/__orca_fixture/stats?"+url.Values{"request_id": {id}}.Encode(), nil)
	if err != nil {
		return out, errCandidate
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return out, errCandidate
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || candidateDecode(resp.Body, &out) != nil || out.RequestID != id {
		return candidateStats{}, errCandidate
	}
	return out, nil
}
