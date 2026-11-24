package orcaaccount_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
)

func candidateTestManifest() candidateManifest {
	return candidateManifest{Version: 1, RunID: "orca-startup-" + strings.Repeat("a", 32), CandidateSHA256: candidateSHA,
		DialAddr: candidateDialAddr, CAFile: candidateCAPath, SessionCookie: strings.Repeat("synthetic", 4), Subject: "synthetic-subject",
		ControlToken: strings.Repeat("b", 64), MaxInferenceAttempts: 8, MaxUpstreamCalls: 4, SyntheticText: candidateText, MaxOutputTokens: 16,
		Models: []candidateModel{{1, "orca-stage-openai", "openai", "/chat/completions", 18}, {2, "orca-stage-anthropic", "anthropic", "/messages", 18}}}
}

func TestCandidateManifestFailClosed(t *testing.T) {
	valid := candidateTestManifest()
	if valid.validate(valid.RunID) != nil {
		t.Fatal("valid fixed contract rejected")
	}
	tests := []struct {
		name   string
		change func(*candidateManifest)
	}{
		{"version", func(m *candidateManifest) { m.Version = 2 }},
		{"run", func(m *candidateManifest) { m.RunID = "unowned" }},
		{"digest", func(m *candidateManifest) { m.CandidateSHA256 = strings.Repeat("0", 64) }},
		{"dns", func(m *candidateManifest) { m.DialAddr = "sub.aichat.diy:443" }},
		{"remote", func(m *candidateManifest) { m.DialAddr = "192.168.1.1:8443" }},
		{"ca", func(m *candidateManifest) { m.CAFile = "/etc/ssl/certs/ca-certificates.crt" }},
		{"cookie", func(m *candidateManifest) { m.SessionCookie += "; extra=1" }},
		{"cookie-newline", func(m *candidateManifest) { m.SessionCookie += "\r\n" }},
		{"control", func(m *candidateManifest) { m.ControlToken = "short" }},
		{"model", func(m *candidateManifest) { m.Models[0].ID = "paid-model" }},
		{"group", func(m *candidateManifest) { m.Models[0].GroupID = 0 }},
		{"responses", func(m *candidateManifest) { m.Models[0].Endpoint = "/responses" }},
		{"protocol", func(m *candidateManifest) { m.Models[0].Protocol = "unknown" }},
		{"duplicate", func(m *candidateManifest) { m.Models[1] = m.Models[0] }},
		{"amount", func(m *candidateManifest) { m.Models[0].SettledMicros = 0 }},
		{"budget", func(m *candidateManifest) { m.MaxUpstreamCalls = 32 }},
		{"attempts", func(m *candidateManifest) { m.MaxInferenceAttempts = 100 }},
		{"text", func(m *candidateManifest) { m.SyntheticText = "non-fixture text" }},
		{"output", func(m *candidateManifest) { m.MaxOutputTokens = 17 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := candidateTestManifest()
			tc.change(&m)
			if m.validate(valid.RunID) == nil {
				t.Fatal("unsafe manifest accepted")
			}
		})
	}
	encoded, _ := json.Marshal(valid)
	for _, data := range [][]byte{append(bytes.Clone(encoded), []byte(` {}`)...), []byte(`{"unexpected":1}`), bytes.Repeat([]byte(" "), 32769)} {
		var out candidateManifest
		if candidateDecode(bytes.NewReader(data), &out) == nil {
			t.Fatal("ambiguous or oversized manifest accepted")
		}
	}
	if strings.Contains(valid.String(), valid.SessionCookie) || strings.Contains(valid.GoString(), valid.ControlToken) {
		t.Fatal("manifest formatting exposed secrets")
	}
}

func TestCandidateRouteAndBudgetBoundary(t *testing.T) {
	tests := []struct {
		method, raw string
		allowed     bool
	}{
		{"GET", orcaaccount.Issuer + orcaaccount.APIPrefix + "/session", true},
		{"GET", orcaaccount.Issuer + orcaaccount.APIPrefix + "/models", true},
		{"POST", orcaaccount.Issuer + orcaaccount.APIPrefix + "/token", true},
		{"GET", orcaaccount.Issuer + orcaaccount.APIPrefix + "/requests/request:abc", true},
		{"GET", orcaaccount.Issuer + "/__orca_fixture/stats?request_id=synthetic-1", true},
		{"POST", orcaaccount.Router + "/v1/messages", true},
		{"POST", orcaaccount.Router + "/v1/chat/completions", true},
		{"POST", orcaaccount.Router + "/v1/responses", false},
		{"POST", orcaaccount.Router + "/v1/messages?beta=true", false},
		{"GET", "https://example.com/api/v1/desktop/session", false},
		{"GET", "http://sub.aichat.diy/api/v1/desktop/session", false},
		{"GET", "https://sub.aichat.diy:443/api/v1/desktop/session", false},
		{"GET", "https://sub.aichat.diy.evil/api/v1/desktop/session", false},
		{"GET", "https://user@sub.aichat.diy/api/v1/desktop/session", false},
		{"GET", orcaaccount.Issuer + "/login", false},
		{"GET", orcaaccount.Issuer + orcaaccount.APIPrefix + "/session#fragment", false},
		{"GET", orcaaccount.Issuer + orcaaccount.APIPrefix + "/requests/../session", false},
		{"GET", orcaaccount.Issuer + orcaaccount.APIPrefix + "/requests/%2e%2e", false},
		{"GET", orcaaccount.Issuer + "/__orca_fixture/stats?request_id=x&request_id=y", false},
	}
	for _, tc := range tests {
		r, err := http.NewRequest(tc.method, tc.raw, nil)
		if err != nil {
			t.Fatal("invalid test fixture")
		}
		_, allowed := candidateRoute(r)
		if allowed != tc.allowed {
			t.Fatalf("boundary mismatch for synthetic URL %s", tc.raw)
		}
	}
	calls := 0
	g := &candidateGuard{next: candidateRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.GetBody != nil {
			t.Fatal("implicit net/http replay retained")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	for n := 0; n < 9; n++ {
		r, _ := http.NewRequest("POST", orcaaccount.Router+"/v1/messages", strings.NewReader(`{}`))
		r.Header.Set("Idempotency-Key", "synthetic-budget")
		resp, err := g.RoundTrip(r)
		if n < 8 {
			if err != nil {
				t.Fatal("bounded request rejected")
			}
			resp.Body.Close()
		} else if !errors.Is(err, errCandidate) {
			t.Fatal("inference budget exceeded")
		}
	}
	if calls != 8 || g.denied.Load() != 1 {
		t.Fatal("budget forwarded denied traffic")
	}
	r, _ := http.NewRequest("POST", orcaaccount.Router+"/v1/messages", nil)
	r.Host = "untrusted-host"
	if _, allowed := candidateRoute(r); allowed {
		t.Fatal("Host override accepted")
	}
	for _, addr := range []string{"example.com:443", "sub.aichat.diy:80", "127.0.0.1:8443", "router.aichat.diy:8443"} {
		if conn, err := candidateDial(context.Background(), "tcp", addr); err == nil || conn != nil {
			t.Fatal("unapproved dial target accepted")
		}
	}
}

func TestCandidateConsentAndRedirectBinding(t *testing.T) {
	token := strings.Repeat("synthetic", 4)
	form := `<form method="post" action="/api/v1/desktop/authorize"><input type="hidden" name="consent" value="` + token + `"></form>`
	if got, err := candidateConsent(strings.NewReader(form)); err != nil || got != token {
		t.Fatal("structured consent parse failed")
	}
	for _, bad := range []string{strings.Replace(form, "/api/v1/desktop/authorize", "https://example.com", 1), form + form,
		strings.Replace(form, `type="hidden"`, `type="text"`, 1), strings.Replace(form, `method="post"`, `method="get"`, 1), strings.Repeat("x", 32769)} {
		if _, err := candidateConsent(strings.NewReader(bad)); err == nil {
			t.Fatal("unbound consent form accepted")
		}
	}
	callback, err := candidateCallback("http://127.0.0.1:43210/callback")
	if err != nil {
		t.Fatal("numeric ephemeral callback rejected")
	}
	for _, bad := range []string{"http://localhost:43210/callback", "https://127.0.0.1:43210/callback", "http://127.0.0.1:80/callback", "http://127.0.0.1:43210/other", "http://user@127.0.0.1:43210/callback", "http://127.0.0.1:43210/callback?x=1", "http://127.0.0.1:043210/callback"} {
		if _, err := candidateCallback(bad); err == nil {
			t.Fatal("unsafe callback accepted")
		}
	}
	q := url.Values{"state": {"synthetic-state"}, "iss": {orcaaccount.Issuer}, "code": {"synthetic-code"}}
	raw := callback.String() + "?" + q.Encode()
	if _, err := candidateRedirect(raw, callback, "synthetic-state", "allow"); err != nil {
		t.Fatal("bound authorization redirect rejected")
	}
	for _, bad := range []string{strings.Replace(raw, "43210", "43211", 1), raw + "&state=extra", strings.Replace(raw, "synthetic-state", "wrong-state", 1), strings.Replace(raw, "sub.aichat.diy", "other.invalid", 1), raw + "#extra", raw + "&error=access_denied"} {
		if _, err := candidateRedirect(bad, callback, "synthetic-state", "allow"); err == nil {
			t.Fatal("unbound authorization redirect accepted")
		}
	}
	q.Del("code")
	q.Set("error", "access_denied")
	if _, err := candidateRedirect(callback.String()+"?"+q.Encode(), callback, "synthetic-state", "deny"); err != nil {
		t.Fatal("bound denial redirect rejected")
	}
}

func TestCandidatePrivateTLSAndMemoryStorage(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic-test-only"}, IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	client, guard, transport, err := candidateHTTP(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	if err != nil {
		t.Fatal("fixture-only root rejected")
	}
	defer transport.CloseIdleConnections()
	if transport.Proxy != nil || transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.MinVersion < tls.VersionTLS12 ||
		!transport.DisableKeepAlives || transport.DialContext == nil || transport.TLSClientConfig.RootCAs == nil ||
		len(transport.TLSClientConfig.RootCAs.Subjects()) != 1 || guard.next != transport || client.Jar != nil {
		t.Fatal("fixture transport inherited unsafe defaults")
	}
	if client.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("redirect following enabled")
	}
	if _, _, _, err := candidateHTTP([]byte("invalid")); err == nil {
		t.Fatal("invalid fixture trust accepted")
	}
	store := &candidateMemoryStore{}
	data := []byte("synthetic-sensitive-session")
	if store.Save(context.Background(), data) != nil {
		t.Fatal("memory save failed")
	}
	data[0] = 'X'
	loaded, err := store.Load(context.Background())
	if err != nil || loaded[0] == 'X' {
		t.Fatal("memory save alias")
	}
	loaded[0] = 'Y'
	again, _ := store.Load(context.Background())
	if again[0] == 'Y' {
		t.Fatal("memory load alias")
	}
	if store.Delete(context.Background()) != nil {
		t.Fatal("memory delete failed")
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, orcaaccount.ErrNotFound) {
		t.Fatal("memory delete retained session")
	}
}
