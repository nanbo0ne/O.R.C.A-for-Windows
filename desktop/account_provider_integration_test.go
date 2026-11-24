package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
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

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

const integrationSubject = "integration-subject"
const integrationGrant = "integration-grant"
const integrationModel = "Synthetic/Case-V1"
const integrationRequestID = "req-integrated"
const integrationServerID = "request:integrated-private"
const integrationAccess = "synthetic-integration-access-token"
const integrationRefresh = "synthetic-integration-refresh-token"
const integrationKey = "synthetic-integration-restricted-key"
const integrationPrompt = "synthetic-private-prompt-never-persist"
const integrationOutput = "synthetic-private-output-never-persist"
const integrationCost int64 = 9007199254740993

type integrationReceiptStore struct {
	orcaaccount.Store
	fail atomic.Bool
}

func (s *integrationReceiptStore) Save(ctx context.Context, b []byte) error {
	if s.fail.Load() {
		return errors.New("synthetic private storage failure detail")
	}
	return s.Store.Save(ctx, b)
}

type accountStreamFixture struct {
	t                 *testing.T
	app               *App
	protocol          string
	sessionPath       string
	receiptPath       string
	sessionStore      orcaaccount.Store
	receiptStore      *integrationReceiptStore
	callbackHTTP      *http.Client
	mu                sync.Mutex
	proof             url.Values
	catalogHook       func(*http.Request) (*http.Response, error)
	expectedRequestID string
	browsers          atomic.Int32
	exchanges         atomic.Int32
	sessions          atomic.Int32
	catalogs          atomic.Int32
	credentials       atomic.Int32
	inferences        atomic.Int32
	queries           atomic.Int32
	blockedStream     atomic.Bool
	inferenceFailure  atomic.Int32 // 1: HTTP 503, 2: partial EOF, 3: pre-token EOF.
	settled           atomic.Bool
	queryStarted      chan struct{}
	bodyClosed        chan struct{}
}

func newAccountStreamFixture(t *testing.T, protocol string) *accountStreamFixture {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("integrated current-user DPAPI storage requires Windows")
	}
	root := lifecycleProfile(t)
	f := &accountStreamFixture{t: t, protocol: protocol,
		sessionPath: filepath.Join(root, "account", "session.dpapi"), receiptPath: filepath.Join(root, "account", "receipts.dpapi"),
		queryStarted: make(chan struct{}, 8), bodyClosed: make(chan struct{})}
	var err error
	f.sessionStore, err = orcaaccount.NewSystemStore(f.sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := orcaaccount.NewSystemStore(f.receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	f.receiptStore = &integrationReceiptStore{Store: store}
	loopback := &http.Transport{Proxy: nil}
	f.callbackHTTP = &http.Client{Transport: loopback, Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("callback redirect forbidden") }}
	previous := accountDesktopActive.Load()
	t.Cleanup(func() {
		if f.app != nil {
			f.app.stopOrcaAccount()
		}
		accountDesktopActive.Store(previous)
		loopback.CloseIdleConnections()
	})
	f.attach(false)
	return f
}

func (f *accountStreamFixture) attach(restore bool) {
	f.t.Helper()
	c, err := orcaaccount.New(orcaaccount.Options{Store: f.sessionStore, OpenBrowser: f.browser,
		HTTPClient: &http.Client{Transport: accountTestTransport(f.roundTrip)}})
	if err != nil {
		f.t.Fatal(err)
	}
	s := newAccountDesktop(context.Background(), c)
	s.receiptJournal = &accountReceiptJournal{store: f.receiptStore}
	f.app = &App{account: s}
	accountDesktopActive.Store(s)
	if err := f.app.beginAccountLogin(restore); err != nil {
		f.t.Fatal(err)
	}
	lifecycleSignedIn(f.t, f.app, integrationSubject)
}

func (f *accountStreamFixture) browser(ctx context.Context, raw string) error {
	f.browsers.Add(1)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "sub.aichat.diy" || u.Path != orcaaccount.APIPrefix+"/authorize" {
		return errors.New("unexpected synthetic authorization URL")
	}
	q := u.Query()
	if q.Get("client_id") != orcaaccount.ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
		q.Get("state") == "" || q.Get("nonce") == "" || q.Get("code_challenge") == "" {
		return errors.New("public PKCE authorization contract missing")
	}
	callback, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || callback.Scheme != "http" || callback.Hostname() != "127.0.0.1" || callback.Port() == "" || callback.Path != "/callback" || callback.User != nil {
		return errors.New("non-loopback callback rejected")
	}
	f.mu.Lock()
	f.proof = q
	f.mu.Unlock()
	callback.RawQuery = url.Values{"code": {"synthetic-integration-code"}, "state": {q.Get("state")}, "iss": {orcaaccount.Issuer}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, callback.String(), nil)
	if err != nil {
		return err
	}
	// This is the fixture's only real socket: the client's ephemeral loopback callback.
	resp, err := f.callbackHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return errors.New("synthetic OAuth callback was rejected")
	}
	return nil
}

func integrationJSON(r *http.Request, value any) (*http.Response, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(b)), Request: r}, nil
}

func (f *accountStreamFixture) reject(reason string) (*http.Response, error) {
	f.t.Error(reason)
	return nil, errors.New("synthetic transport rejected unexpected request")
}

func (f *accountStreamFixture) roundTrip(r *http.Request) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	if r.URL.Scheme != "https" || r.URL.User != nil || r.URL.RawQuery != "" {
		return f.reject("request escaped the fixed synthetic endpoints")
	}
	if r.URL.Host == "router.aichat.diy" {
		return f.inference(r)
	}
	if r.URL.Host != "sub.aichat.diy" {
		return f.reject("external network request was blocked")
	}
	path := r.URL.Path
	if path == orcaaccount.APIPrefix+"/session" && r.Method == http.MethodGet && r.Header.Get("Authorization") == "" && r.Header.Get("Cookie") == "" {
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}
	if path == orcaaccount.APIPrefix+"/revoke" {
		if r.Method != http.MethodPost || r.ParseForm() != nil || r.Form.Get("token") != integrationRefresh || r.Form.Get("client_id") != orcaaccount.ClientID {
			return f.reject("logout did not revoke the synthetic grant")
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}
	if path == orcaaccount.APIPrefix+"/token" {
		f.exchanges.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.ParseForm() != nil {
			return f.reject("token exchange did not use form POST")
		}
		f.mu.Lock()
		q := f.proof
		f.mu.Unlock()
		hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if r.Form.Get("client_id") != orcaaccount.ClientID || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "synthetic-integration-code" ||
			r.Form.Get("redirect_uri") != q.Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(hash[:]) != q.Get("code_challenge") || r.Form.Has("client_secret") {
			return f.reject("public PKCE token proof did not match authorization")
		}
		return integrationJSON(r, map[string]any{"access_token": integrationAccess, "refresh_token": integrationRefresh, "token_type": "Bearer", "expires_in": 300,
			"subject": integrationSubject, "issuer": orcaaccount.Issuer, "nonce": q.Get("nonce"), "scope": "desktop", "session_id": integrationGrant, "device_id": q.Get("device_id")})
	}
	if r.Header.Get("Authorization") != "Bearer "+integrationAccess {
		return f.reject("issuer request did not use the account access credential")
	}
	switch path {
	case orcaaccount.APIPrefix + "/session":
		f.sessions.Add(1)
		if r.Method != http.MethodGet {
			return f.reject("session lookup was not GET")
		}
		f.mu.Lock()
		device := f.proof.Get("device_id")
		f.mu.Unlock()
		return integrationJSON(r, orcaaccount.Profile{Subject: integrationSubject, DisplayName: "Fixture account", SessionID: integrationGrant,
			DeviceID: device, ExpiresAt: time.Now().Add(time.Hour), Groups: []orcaaccount.Group{{ID: 7, Name: "Fixture group"}}})
	case orcaaccount.APIPrefix + "/models":
		f.catalogs.Add(1)
		if r.Method != http.MethodGet {
			return f.reject("catalog lookup was not GET")
		}
		f.mu.Lock()
		hook := f.catalogHook
		f.mu.Unlock()
		if hook != nil {
			return hook(r)
		}
		endpoint := "/chat/completions"
		if f.protocol == "anthropic" {
			endpoint = "/messages"
		}
		return integrationJSON(r, orcaaccount.Models{Currency: "CNY", Models: []orcaaccount.Model{{ID: integrationModel, GroupID: 7, Protocol: f.protocol, Endpoint: endpoint}}})
	case orcaaccount.APIPrefix + "/credentials":
		f.credentials.Add(1)
		var scope struct {
			GroupID int64 `json:"group_id"`
		}
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if r.Method != http.MethodPost || d.Decode(&scope) != nil || scope.GroupID != 7 {
			return f.reject("restricted credential was not scoped to the catalog group")
		}
		return integrationJSON(r, map[string]any{"api_key": integrationKey, "credential_id": "fixture-restricted-credential", "base_url": orcaaccount.Router + "/v1",
			"restricted": true, "group_id": 7, "expires_at": time.Now().Add(time.Hour)})
	case orcaaccount.APIPrefix + "/requests/" + integrationServerID:
		f.queries.Add(1)
		select {
		case f.queryStarted <- struct{}{}:
		default:
		}
		if r.Method != http.MethodGet {
			return f.reject("recovery attempted something other than GET")
		}
		status := orcaaccount.Settlement{RequestID: integrationServerID, Status: orcaaccount.SettlementPending, Currency: "CNY"}
		if f.settled.Load() {
			cost := integrationCost
			status.Status, status.SettledMicros = orcaaccount.SettlementSettled, &cost
		}
		return integrationJSON(r, status)
	default:
		return f.reject("unexpected issuer endpoint; model replay and refresh exchange are not fixture operations")
	}
}

func (f *accountStreamFixture) inference(r *http.Request) (*http.Response, error) {
	f.inferences.Add(1)
	f.mu.Lock()
	requestID := f.expectedRequestID
	f.mu.Unlock()
	if requestID == "" {
		requestID = integrationRequestID
	}
	path := "/v1/chat/completions"
	if f.protocol == "anthropic" {
		path = "/v1/messages"
	}
	if r.Method != http.MethodPost || r.URL.Path != path || r.Header.Get("Authorization") != "Bearer "+integrationKey ||
		r.Header.Get("Idempotency-Key") != requestID || r.Header.Get("X-Api-Key") != "" || r.Header.Get("Cookie") != "" {
		return f.reject("adapter request escaped its protocol, scoped credential, or idempotency binding")
	}
	var body struct {
		Model    string          `json:"model"`
		Stream   bool            `json:"stream"`
		Messages json.RawMessage `json:"messages"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != integrationModel || !body.Stream || !bytes.Contains(body.Messages, []byte(integrationPrompt)) {
		return f.reject("adapter did not send the exact synthetic model request")
	}
	doc, err := f.document()
	if err != nil || len(doc.Records) != 1 || doc.Subject != integrationSubject || doc.Grant != integrationGrant ||
		doc.Records[0].RequestID != requestID || doc.Records[0].Status != "unknown" || doc.Records[0].CostMicros != nil || doc.Records[0].ServerID != "" {
		return f.reject("inference reached transport before its unknown receipt was durable")
	}
	if f.inferenceFailure.Load() == 1 {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"X-Request-Id": {integrationServerID}},
			Body: io.NopCloser(strings.NewReader("synthetic admission failure")), Request: r}, nil
	}
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"" + integrationOutput + "\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	if f.protocol == "anthropic" {
		stream = "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + integrationOutput + "\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	}
	if f.inferenceFailure.Load() == 2 {
		stream = "data: {\"choices\":[{\"delta\":{\"content\":\"" + integrationOutput + "\"}}]}\n\n"
		if f.protocol == "anthropic" {
			stream = "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + integrationOutput + "\"}}\n\n"
		}
	} else if f.inferenceFailure.Load() == 3 {
		stream = ""
	}
	var bodyStream io.ReadCloser = io.NopCloser(strings.NewReader(stream))
	if f.blockedStream.Load() {
		bodyStream = &integrationPendingBody{ctx: r.Context(), closed: f.bodyClosed}
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {integrationServerID}}, Body: bodyStream, Request: r}, nil
}

type integrationPendingBody struct {
	ctx    context.Context
	closed chan struct{}
	once   sync.Once
}

func (b *integrationPendingBody) Read([]byte) (int, error) {
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	case <-b.closed:
		return 0, io.ErrClosedPipe
	}
}
func (b *integrationPendingBody) Close() error { b.once.Do(func() { close(b.closed) }); return nil }

func (f *accountStreamFixture) document() (accountReceiptDocument, error) {
	// Production journal readers share this lock. An unsynchronized os.Open
	// in the fixture can itself deny atomic replacement on Windows.
	j := f.app.account.receiptJournal
	j.mu.Lock()
	defer j.mu.Unlock()
	b, err := f.receiptStore.Load(context.Background())
	if err != nil {
		return accountReceiptDocument{}, err
	}
	defer clear(b)
	return decodeAccountReceipts(b)
}

func (f *accountStreamFixture) registeredProvider() provider.Provider {
	f.t.Helper()
	s := f.app.account
	s.mu.Lock()
	var name string
	for key, m := range s.catalog {
		if m.ID == integrationModel {
			name = key
		}
	}
	s.mu.Unlock()
	if name == "" {
		f.t.Fatal("OAuth login did not publish the catalog")
	}
	p, err := provider.New(accountProviderKind, provider.Config{Name: name, Model: integrationModel, Extra: map[string]any{"reasoning_protocol": "none"}})
	if err != nil {
		f.t.Fatal(err)
	}
	if _, ok := p.(*accountProvider); !ok {
		f.t.Fatal("fixture bypassed the registered account provider")
	}
	return p
}

func integrationRequest() provider.Request {
	return provider.Request{RequestID: integrationRequestID, Purpose: provider.RequestPurposeBrowser,
		Messages: []provider.Message{{Role: provider.RoleUser, Content: integrationPrompt}}, MaxTokens: 32}
}

func drainIntegrationStream(t *testing.T, stream <-chan provider.Chunk, canceled bool) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case chunk, ok := <-stream:
			if !ok {
				return
			}
			if chunk.Type == provider.ChunkError && !canceled {
				t.Fatalf("integrated adapter failed: %v", chunk.Err)
			}
		case <-deadline.C:
			t.Fatal("integrated stream did not finish")
		}
	}
}

func (f *accountStreamFixture) assertSettled() {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	settled := false
	var lastStatus string
	for time.Now().Before(deadline) {
		doc, err := f.document()
		if err == nil && len(doc.Records) == 1 {
			lastStatus = doc.Records[0].Status
			if lastStatus == "settled" && doc.Records[0].CostMicros != nil && *doc.Records[0].CostMicros == integrationCost {
				settled = true
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	if !settled {
		f.t.Fatalf("settlement persistence did not finish: queries=%d disk_status=%s persistence_warning=%t", f.queries.Load(), lastStatus, f.app.OrcaAccountStatus().ReceiptError != "")
	}
	if f.inferences.Load() != 1 || f.credentials.Load() != 1 || f.browsers.Load() != 1 || f.exchanges.Load() != 1 {
		f.t.Fatal("integration replayed inference, minted another credential, or reauthorized")
	}
}

func (f *accountStreamFixture) assertPrivateReadOnlyDTO(turnID string) {
	f.t.Helper()
	before, err := f.receiptStore.Load(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	rows := f.app.OrcaAccountReceipts()
	if len(rows) != 1 || rows[0].RequestID != integrationRequestID || rows[0].TurnID != turnID || rows[0].Purpose != "browser" || rows[0].Cost != "9007199254.740993" || rows[0].Status != "settled" {
		f.t.Fatal("settled UI receipt lost identity, purpose, or exact currency")
	}
	public, err := json.Marshal(struct {
		Account  OrcaAccountView
		Receipts []OrcaReceiptView
	}{f.app.OrcaAccountStatus(), rows})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, forbidden := range []string{integrationAccess, integrationRefresh, integrationKey, integrationServerID, integrationGrant, integrationPrompt, integrationOutput, "serverID", "server_id", "generation", "access_token", "api_key"} {
		if bytes.Contains(public, []byte(forbidden)) {
			f.t.Fatal("private account data entered a public DTO")
		}
	}
	rows[0].Status, rows[0].Cost = "unknown", "0.000000"
	view := f.app.OrcaAccountStatus()
	view.DisplayName = "mutated DTO"
	if got := f.app.OrcaAccountReceipts(); got[0].Status != "settled" || got[0].Cost != "9007199254.740993" || f.app.OrcaAccountStatus().DisplayName == view.DisplayName {
		f.t.Fatal("caller mutated backend state through a read-only DTO")
	}
	after, err := f.receiptStore.Load(context.Background())
	if err != nil || !bytes.Equal(before, after) {
		f.t.Fatal("DTO reads changed the encrypted receipt document")
	}
	for _, forbidden := range []string{integrationAccess, integrationRefresh, integrationKey, integrationPrompt, integrationOutput} {
		if bytes.Contains(after, []byte(forbidden)) {
			f.t.Fatal("receipt document persisted a secret or model content")
		}
	}
	for _, path := range []string{f.sessionPath, f.receiptPath, config.UserConfigPath()} {
		raw, err := os.ReadFile(path)
		if err != nil {
			f.t.Fatal(err)
		}
		for _, forbidden := range []string{integrationAccess, integrationRefresh, integrationKey, integrationPrompt, integrationOutput, integrationServerID} {
			if bytes.Contains(raw, []byte(forbidden)) {
				f.t.Fatal("plaintext secret or receipt fallback found on disk")
			}
		}
	}
}

func TestAccountProviderIntegratedStreamPersistsSettlement(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			f := newAccountStreamFixture(t, protocol)
			f.settled.Store(true)
			ctx, cancel := agent.WithParentTurn(context.Background())
			defer cancel()
			turnID, _ := agent.ParentTurn(ctx)
			stream, err := f.registeredProvider().Stream(ctx, integrationRequest())
			if err != nil {
				t.Fatal(err)
			}
			drainIntegrationStream(t, stream, false)
			f.assertSettled()
			if f.sessions.Load() != 1 || f.catalogs.Load() != 1 || f.queries.Load() != 1 {
				t.Fatal("expected session/catalog/settlement steps were bypassed or repeated")
			}
			f.assertPrivateReadOnlyDTO(turnID)
		})
	}
}

func TestAccountProviderIntegratedErrorsForbidReplay(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, mode := range []struct {
			name    string
			failure int32
		}{{"http503", 1}, {"partialEOF", 2}, {"beforeTokenEOF", 3}} {
			t.Run(protocol+"/"+mode.name, func(t *testing.T) {
				f := newAccountStreamFixture(t, protocol)
				f.inferenceFailure.Store(mode.failure)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				stream, err := f.registeredProvider().Stream(ctx, integrationRequest())
				if mode.failure == 1 {
					if stream != nil || !provider.ReplayForbidden(err) {
						t.Fatal("post-dispatch HTTP error lost no-replay marker")
					}
				} else {
					if err != nil {
						t.Fatal("synthetic stream did not reach adapter")
					}
					var output strings.Builder
					var streamErr error
					closed := false
					for !closed {
						select {
						case chunk, ok := <-stream:
							if !ok {
								closed = true
								break
							}
							if chunk.Type == provider.ChunkText {
								output.WriteString(chunk.Text)
							}
							if chunk.Type == provider.ChunkError {
								streamErr = chunk.Err
							}
						case <-ctx.Done():
							t.Fatal("integrated error stream did not finish")
						}
					}
					if !provider.ReplayForbidden(streamErr) {
						t.Fatal("adapter ChunkError lost no-replay marker")
					}
					if mode.failure == 2 && output.String() != integrationOutput {
						t.Fatal("no-replay wrapper discarded received text")
					}
					if mode.failure == 3 && output.Len() != 0 {
						t.Fatal("pre-token error invented output")
					}
				}
				if f.inferences.Load() != 1 || f.credentials.Load() != 1 {
					t.Fatal("integrated failure replayed inference")
				}
				doc, e := f.document()
				if e != nil || len(doc.Records) != 1 || doc.Records[0].ServerID != integrationServerID || doc.Records[0].CostMicros != nil || doc.Records[0].Status == "settled" {
					t.Fatal("failed stream lost pending receipt or invented a charge")
				}
			})
		}
	}
}

func TestAccountProviderIntegratedJournalFailureBlocksInference(t *testing.T) {
	f := newAccountStreamFixture(t, "openai")
	p := f.registeredProvider()
	f.receiptStore.fail.Store(true)
	stream, err := p.Stream(context.Background(), integrationRequest())
	if !errors.Is(err, errAccountReceiptStore) || stream != nil || f.inferences.Load() != 0 {
		t.Fatal("journal failure did not stop the actual account provider before inference")
	}
	doc, err := f.document()
	if err != nil || len(doc.Records) != 0 {
		t.Fatal("failed initial save created a durable admitted receipt")
	}
	if f.app.OrcaAccountStatus().ReceiptError == "" {
		t.Fatal("journal failure was hidden from status")
	}
	f.receiptStore.fail.Store(false)
	if _, err := p.Stream(context.Background(), integrationRequest()); err == nil || f.inferences.Load() != 0 {
		t.Fatal("same uncertain request identity was silently replayed after storage recovered")
	}
}

func TestAccountProviderIntegratedCanceledPendingRestoresQueryOnly(t *testing.T) {
	f := newAccountStreamFixture(t, "openai")
	f.blockedStream.Store(true)
	ctx, cancel := agent.WithParentTurn(context.Background())
	defer cancel()
	turnID, _ := agent.ParentTurn(ctx)
	stream, err := f.registeredProvider().Stream(ctx, integrationRequest())
	if err != nil {
		t.Fatal(err)
	}
	doc, err := f.document()
	if err != nil || len(doc.Records) != 1 || doc.Records[0].ServerID != integrationServerID || doc.Records[0].Status != "pending" || doc.Records[0].CostMicros != nil {
		t.Fatal("response header did not durably capture a pending receipt before body completion")
	}
	cancel()
	drainIntegrationStream(t, stream, true)
	lifecycleWait(t, f.bodyClosed)
	lifecycleWait(t, f.queryStarted)
	rows := f.app.OrcaAccountReceipts()
	if len(rows) != 1 || rows[0].Status != "pending" || rows[0].Cost != "" {
		t.Fatal("cancellation invented a settled zero cost")
	}
	f.app.stopOrcaAccount()
	beforeRestore := f.queries.Load()
	f.settled.Store(true)
	f.attach(true)
	f.assertSettled()
	if f.sessions.Load() != 2 || f.catalogs.Load() != 2 || f.queries.Load() != beforeRestore+1 {
		t.Fatal("restart did not perform exactly one same-grant recovery query")
	}
	f.assertPrivateReadOnlyDTO(turnID)
}
