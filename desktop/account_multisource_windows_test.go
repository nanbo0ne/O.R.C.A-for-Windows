//go:build windows

package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type accountAttributionObserver struct {
	provider.Provider
	fixture *accountStreamFixture
	seen    chan provider.Request
}

func (p *accountAttributionObserver) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.seen <- req
	if p.fixture != nil {
		p.fixture.mu.Lock()
		p.fixture.expectedRequestID = req.RequestID
		p.fixture.mu.Unlock()
	}
	return p.Provider.Stream(ctx, req)
}

func TestAccountMixedMainAndSubagentPersistOnlyAccountUsage(t *testing.T) {
	for _, accountMain := range []bool{false, true} {
		name := "api_main_account_subagent"
		if accountMain {
			name = "account_main_api_subagent"
		}
		t.Run(name, func(t *testing.T) {
			isolatedWebRuntime(t)
			f := newAccountStreamFixture(t, "openai")
			f.settled.Store(true)
			browser := prepareAccountWebState(t, f.app, "logged_out")
			var apiCalls atomic.Int32
			api := &accountAttributionObserver{Provider: syntheticOrdinaryAPI(t, &apiCalls), seen: make(chan provider.Request, 4)}
			account := &accountAttributionObserver{Provider: f.registeredProvider(), fixture: f, seen: make(chan provider.Request, 4)}
			main, child := api, account
			if accountMain {
				main, child = account, api
			}
			ctx, cancel := agent.WithParentTurn(agent.WithParentSession(context.Background(), "synthetic-session"))
			defer cancel()
			root, _ := agent.ParentTurn(ctx)
			reg := tool.NewRegistry()
			executor := agent.New(main, reg, agent.NewSession("synthetic system"), agent.Options{MaxSteps: 2}, event.Discard)
			if err := executor.Run(ctx, integrationPrompt); err != nil {
				t.Fatal("synthetic main agent failed", err)
			}
			profile := filepath.Dir(filepath.Dir(f.sessionPath))
			task := agent.NewTaskTool(child, nil, reg, 2, 20000, 0.7, 0.85, 0.95, 0, "", "synthetic system", nil, "", "", nil).
				WithTranscripts(agent.NewSubagentStore(filepath.Join(profile, "synthetic-subagents")), profile, child.Name(), "")
			if _, err := task.Execute(ctx, []byte("{\"prompt\":\""+integrationPrompt+"\"}")); err != nil {
				t.Fatal("synthetic subagent failed", err)
			}
			if len(api.seen) != 1 || len(account.seen) != 1 {
				t.Fatal("mixed-source agent requests were replayed or silently substituted")
			}
			apiReq, accountReq := <-api.seen, <-account.seen
			if apiReq.ParentTaskID != root || accountReq.ParentTaskID != root || accountReq.RequestID == "" ||
				apiReq.RequestID == "" || apiReq.RequestID == accountReq.RequestID ||
				apiReq.Purpose != provider.RequestPurposeTurn || accountReq.Purpose != provider.RequestPurposeTurn {
				t.Fatal("main/subagent requests lost common attribution or distinct request identity")
			}
			f.assertSettled()
			doc, err := f.document()
			if err != nil || len(doc.Records) != 1 || doc.Records[0].TurnID != root ||
				doc.Records[0].RequestID != accountReq.RequestID || doc.Records[0].RequestID == apiReq.RequestID {
				t.Fatal("account journal included API usage or lost the subagent's root", err)
			}
			totals := f.app.OrcaAccountTurnSettlements()
			if len(totals) != 1 || totals[0].TurnID != root || totals[0].Requests != 1 ||
				totals[0].Cost != "9007199254.740993" || totals[0].Scope != "retained_requests" ||
				apiCalls.Load() != 1 || browser.sends != 0 {
				t.Fatal("mixed-source total or transport isolation changed")
			}
		})
	}
}

func TestAccountAdmissionNeverFallsBackToConfiguredAPI(t *testing.T) {
	for _, mode := range []string{"signed_out", "removed_model", "responses_only", "another_subject_catalog"} {
		t.Run(mode, func(t *testing.T) {
			f := newAccountStreamFixture(t, "openai")
			p := f.registeredProvider().(*accountProvider)
			var fallbackCalls atomic.Int32
			p.cfg.APIKey, p.cfg.BaseURL = "synthetic-wrong-source-key", "https://wrong-source.invalid/v1"
			p.cfg.HTTPClient = &http.Client{Transport: accountTestTransport(func(*http.Request) (*http.Response, error) {
				fallbackCalls.Add(1)
				return nil, errors.New("another source must never be contacted")
			})}
			s := f.app.account
			switch mode {
			case "signed_out":
				if err := s.client.CancelContexts(); err != nil {
					t.Fatal(err)
				}
			case "removed_model":
				s.mu.Lock()
				delete(s.catalog, p.cfg.Name)
				s.mu.Unlock()
			case "responses_only":
				s.mu.Lock()
				model := s.catalog[p.cfg.Name]
				model.Endpoint = "/responses"
				s.catalog[p.cfg.Name] = model
				s.mu.Unlock()
			case "another_subject_catalog":
				p.cfg.Name = accountProviderPrefix + "another-subject"
			}
			if stream, err := p.Stream(context.Background(), integrationRequest()); err == nil || stream != nil {
				t.Fatal("unavailable selected account model was silently substituted")
			}
			if fallbackCalls.Load() != 0 || f.inferences.Load() != 0 || f.credentials.Load() != 0 || len(f.app.OrcaAccountReceipts()) != 0 {
				t.Fatal("rejected account request dispatched a paid request or changed its source")
			}
			var apiCalls atomic.Int32
			stream, err := syntheticOrdinaryAPI(t, &apiCalls).Stream(context.Background(), integrationRequest())
			if err != nil {
				t.Fatal("independent ordinary API became unavailable", err)
			}
			drainIntegrationStream(t, stream, false)
			if apiCalls.Load() != 1 {
				t.Fatal("independent API request replayed or was blocked")
			}
		})
	}
}

func TestAccountReceiptUsesHostParentTaskID(t *testing.T) {
	for _, purpose := range []provider.RequestPurpose{provider.RequestPurposeTurn, provider.RequestPurposeClassifier,
		provider.RequestPurposeBrowser, provider.RequestPurposeCompaction, provider.RequestPurposeCompactionFallback} {
		t.Run(string(purpose), func(t *testing.T) {
			f := newAccountStreamFixture(t, "openai")
			f.settled.Store(true)
			req := integrationRequest()
			req.ParentTaskID, req.Purpose = "synthetic-host-parent", purpose
			stream, err := f.registeredProvider().Stream(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			drainIntegrationStream(t, stream, false)
			f.assertSettled()
			doc, err := f.document()
			rows := f.app.OrcaAccountReceipts()
			if err != nil || len(doc.Records) != 1 || doc.Records[0].TurnID != req.ParentTaskID ||
				doc.Records[0].Purpose != string(purpose) || len(rows) != 1 || rows[0].TurnID != req.ParentTaskID {
				t.Fatal("host attribution was dropped from the persisted receipt or public view", err)
			}
		})
	}
	t.Run("explicit_host_binding_wins", func(t *testing.T) {
		f := newAccountStreamFixture(t, "anthropic")
		f.settled.Store(true)
		ctx, cancel := agent.WithParentTurn(context.Background())
		defer cancel()
		req := integrationRequest()
		req.ParentTaskID = "synthetic-original-root"
		stream, err := f.registeredProvider().Stream(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		drainIntegrationStream(t, stream, false)
		f.assertSettled()
		if got := f.app.OrcaAccountReceipts(); len(got) != 1 || got[0].TurnID != req.ParentTaskID {
			t.Fatal("a nested context replaced the explicit root task attribution")
		}
	})
}

func syntheticOrdinaryAPI(t *testing.T, calls *atomic.Int32) provider.Provider {
	t.Helper()
	p, err := provider.New("openai", provider.Config{Name: "synthetic-api", Model: "synthetic-api-model",
		APIKey: "synthetic-ordinary-api-key", BaseURL: "https://ordinary-api.invalid/v1",
		Extra: map[string]any{"reasoning_protocol": "none"},
		HTTPClient: &http.Client{Transport: accountTestTransport(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			if r.URL.String() != "https://ordinary-api.invalid/v1/chat/completions" ||
				r.Header.Get("Authorization") != "Bearer synthetic-ordinary-api-key" ||
				r.Header.Get("Cookie") != "" {
				t.Error("ordinary API borrowed another source's endpoint or credentials")
			}
			body := "data: {\"choices\":[{\"delta\":{\"content\":\"synthetic API result\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"ordinary-api-server-id"}},
				Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func prepareAccountWebState(t *testing.T, app *App, state string) *lifecycleWebBrowser {
	t.Helper()
	b := &lifecycleWebBrowser{}
	newWebBetaBrowser = func(string, string) (webBetaBrowser, error) { return b, nil }
	if state == "absent" {
		return b
	}
	opened, err := app.WebBetaOpen()
	if err != nil {
		t.Fatal(err)
	}
	if state == "signed_out" {
		settings := opened.Settings
		settings.Enabled, settings.ReservePaused = true, false
		if err := app.WebBetaConfigure(settings); err != nil {
			t.Fatal(err)
		}
		b.loading = true
	}
	if state == "logged_out" {
		if err := app.WebBetaLogout(); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

func TestAccountAndAPITransportsIgnoreWebLifecycle(t *testing.T) {
	for _, state := range []string{"absent", "disabled", "signed_out", "logged_out"} {
		for _, accountFirst := range []bool{false, true} {
			role := "api_parent_account_child"
			if accountFirst {
				role = "account_parent_api_child"
			}
			t.Run(state+"/"+role, func(t *testing.T) {
				isolatedWebRuntime(t)
				f := newAccountStreamFixture(t, "openai")
				f.settled.Store(true)
				gen := f.app.account.client.CurrentSessionGeneration()
				browser := prepareAccountWebState(t, f.app, state)
				var apiCalls atomic.Int32
				api, account := syntheticOrdinaryAPI(t, &apiCalls), f.registeredProvider()
				root, cancel := agent.WithParentTurn(context.Background())
				defer cancel()
				turnID, _ := agent.ParentTurn(root)
				child, cancelChild := context.WithCancel(root)
				defer cancelChild()
				accountCtx, apiCtx := child, root
				if accountFirst {
					accountCtx, apiCtx = root, child
				}
				runAPI := func() {
					req := integrationRequest()
					req.RequestID, req.Purpose = "ordinary-api-request", provider.RequestPurposeTurn
					stream, err := api.Stream(apiCtx, req)
					if err != nil {
						t.Fatal(err)
					}
					drainIntegrationStream(t, stream, false)
				}
				if !accountFirst {
					runAPI()
				}
				stream, err := account.Stream(accountCtx, integrationRequest())
				if err != nil {
					t.Fatal(err)
				}
				drainIntegrationStream(t, stream, false)
				if accountFirst {
					runAPI()
				}
				f.assertSettled()
				f.assertPrivateReadOnlyDTO(turnID)
				doc, err := f.document()
				if err != nil || len(doc.Records) != 1 || doc.Records[0].TurnID != turnID ||
					doc.Records[0].RequestID != integrationRequestID {
					t.Fatal("mixed provider requests lost attribution or polluted the account journal", err)
				}
				totals := f.app.OrcaAccountTurnSettlements()
				if len(totals) != 1 || totals[0].TurnID != turnID || totals[0].Requests != 1 ||
					totals[0].SettledRequests != 1 || totals[0].Cost != "9007199254.740993" ||
					totals[0].Scope != "retained_requests" {
					t.Fatal("account total included another source or claimed a complete turn bill")
				}
				if apiCalls.Load() != 1 || browser.sends != 0 || !f.app.OrcaAccountStatus().Authenticated ||
					f.app.account.client.CurrentSessionGeneration() != gen {
					t.Fatal("web lifecycle affected an independent source or caused fallback")
				}
			})
		}
	}
}

func TestAccountPendingStreamSurvivesWebLogout(t *testing.T) {
	isolatedWebRuntime(t)
	f := newAccountStreamFixture(t, "anthropic")
	browser := prepareAccountWebState(t, f.app, "disabled")
	f.blockedStream.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gen := f.app.account.client.CurrentSessionGeneration()
	stream, err := f.registeredProvider().Stream(ctx, integrationRequest())
	if err != nil {
		t.Fatal(err)
	}
	receiptEventually(t, func() bool {
		rows := f.app.OrcaAccountReceipts()
		return len(rows) == 1 && rows[0].Status == "pending"
	})
	if err := f.app.WebBetaLogout(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.bodyClosed:
		t.Fatal("web logout canceled the independent account stream")
	default:
	}
	if f.app.account.client.CurrentSessionGeneration() != gen || !f.app.OrcaAccountStatus().Authenticated || browser.sends != 0 {
		t.Fatal("web logout invalidated account identity or sent a web request")
	}
	cancel()
	drainIntegrationStream(t, stream, true)
	lifecycleWait(t, f.bodyClosed)
	rows := f.app.OrcaAccountReceipts()
	if len(rows) != 1 || rows[0].Status != "pending" || rows[0].Cost != "" || f.inferences.Load() != 1 {
		t.Fatal("canceled pending request replayed or became a false zero settlement")
	}
}
