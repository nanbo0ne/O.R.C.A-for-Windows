package orcaaccount_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/anthropic"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/openai"
)

// This test never starts a substitute account/gateway server. It requires the
// separately booted, digest-pinned candidate and disposable native data stores.
func TestActualCandidateAccount(t *testing.T) {
	if os.Getenv("ORCA_ACCOUNT_CANDIDATE") != "1" {
		t.Skip("explicit isolated full-candidate execution only")
	}
	if runtime.GOOS != "linux" || os.Getenv("ORCA_STAGE_DISPOSABLE") != "1" {
		t.Fatal("candidate execution requires disposable Linux fixture")
	}
	input, err := candidatePrivateRead(candidateInputPath)
	if err != nil {
		t.Fatal("private fixture manifest unavailable")
	}
	defer clear(input)
	var manifest candidateManifest
	if candidateDecode(bytes.NewReader(input), &manifest) != nil || manifest.validate(os.Getenv("ORCA_STAGE_RUN")) != nil {
		t.Fatal("fixture manifest rejected")
	}
	ca, err := candidatePrivateRead(manifest.CAFile)
	if err != nil {
		t.Fatal("fixture CA unavailable")
	}
	httpClient, guard, transport, err := candidateHTTP(ca)
	if err != nil {
		t.Fatal("fixture trust rejected")
	}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	store := &candidateMemoryStore{}
	defer store.Delete(context.Background())
	browser := &candidateBrowser{http: httpClient, cookie: manifest.SessionCookie, decision: "deny"}
	newClient := func() *orcaaccount.Client {
		client, e := orcaaccount.New(orcaaccount.Options{Store: store, OpenBrowser: browser.open, HTTPClient: httpClient,
			DeviceID: manifest.RunID, DeviceName: "Disposable synthetic acceptance", LoginTimeout: 30 * time.Second})
		if e != nil {
			t.Fatal("public client construction failed")
		}
		return client
	}
	client := newClient()
	defer func() { _ = client.Close() }()
	if _, err = client.Login(ctx); !errors.Is(err, orcaaccount.ErrAuthorization) || client.Snapshot().Authenticated {
		t.Fatal("real consent denial did not reject authorization")
	}
	browser.decision = "allow"
	profile, err := client.Login(ctx)
	if err != nil || profile.Subject != manifest.Subject || profile.SessionID == "" {
		t.Fatal("real PKCE consent/exchange/profile failed")
	}
	t.Log("candidate public-client synthetic consent denial and PKCE login passed")
	if err = client.Refresh(ctx); err != nil {
		t.Fatal("public refresh rotation failed")
	}
	session, err := client.Session(ctx)
	if err != nil || session.Subject != profile.Subject || session.SessionID != profile.SessionID {
		t.Fatal("refresh changed account or grant binding")
	}
	catalog, err := client.Models(ctx)
	if err != nil || catalog.Currency != "CNY" {
		t.Fatal("native catalog read failed")
	}
	for _, expected := range manifest.Models {
		matches := 0
		for _, model := range catalog.Models {
			if model.GroupID == expected.GroupID && model.ID == expected.ID && model.Protocol == expected.Protocol && model.Endpoint == expected.Endpoint {
				matches++
			}
		}
		if matches != 1 {
			t.Fatal("expected exact group/model/protocol/endpoint unavailable")
		}
	}
	wallet, err := client.Wallet(ctx)
	if err != nil || wallet.Currency != "CNY" || wallet.CashMicros < 0 || wallet.HeldMicros < 0 ||
		wallet.AvailableMicros != wallet.CashMicros-wallet.HeldMicros {
		t.Fatal("native integer wallet contract failed")
	}
	seenGifts := map[string]bool{}
	for _, gift := range wallet.ScopedGifts {
		if seenGifts[gift.ID] || gift.AvailableMicros != gift.BalanceMicros-gift.HeldMicros {
			t.Fatal("scoped gift contract failed")
		}
		seenGifts[gift.ID] = true
	}
	// Public snapshots are detached DTOs; no mutable group pointer is a session
	// capability. Never serialize the memory store or transport credentials.
	snapshot := client.Snapshot()
	if len(snapshot.Profile.Groups) > 0 {
		old := snapshot.Profile.Groups[0].ID
		snapshot.Profile.Groups[0].ID = -1
		if client.Snapshot().Profile.Groups[0].ID != old {
			t.Fatal("snapshot aliases session state")
		}
	}
	var ids []string
	for _, canceled := range []bool{false, true} {
		for _, model := range manifest.Models {
			mode := "success"
			if canceled {
				mode = "cancel"
			}
			id := manifest.RunID + "-" + model.Protocol + "-" + mode
			ids = append(ids, id)
			if canceled && candidateControl(ctx, httpClient, manifest.ControlToken, id) != nil {
				t.Fatal("fixture cancellation control failed")
			}
			serverID := candidateStream(t, ctx, client, model, id, canceled)
			candidateAwaitStats(t, ctx, httpClient, manifest.ControlToken, id, canceled)
			if canceled {
				// Shutdown and session recovery use the public API. Query the same
				// server receipt from a fresh context; never resubmit the model.
				if client.Close() != nil {
					t.Fatal("runtime close failed")
				}
				client = newClient()
				restored, e := client.Restore(ctx)
				if e != nil || restored.Subject != profile.Subject || restored.SessionID != profile.SessionID {
					t.Fatal("same-grant RAM session recovery failed")
				}
			}
			candidateAwaitReceipt(t, ctx, client, serverID, canceled, model.SettledMicros)
			// Re-reading after receipt recovery must still show exactly one
			// admission. No model call is made during the query-only recovery.
			candidateAwaitStats(t, ctx, httpClient, manifest.ControlToken, id, canceled)
			t.Log("candidate " + model.Protocol + " " + mode + " adapter/receipt/no-replay checks passed")
		}
	}
	var last candidateStats
	for _, id := range ids {
		last, err = candidateReadStats(ctx, httpClient, manifest.ControlToken, id)
		if err != nil || last.IngressAttempts != 1 || last.UpstreamCalls != 1 {
			t.Fatal("final per-request no-replay evidence failed")
		}
	}
	if guard.attempts.Load() != 4 || guard.denied.Load() != 0 || last.TotalIngressAttempts != 4 || last.TotalUpstreamCalls != 4 {
		t.Fatal("native fixture inference totals differ from four synthetic calls")
	}
	gen := client.CurrentSessionGeneration()
	groupID := manifest.Models[0].GroupID
	staleCredential, err := client.Credentials(ctx, orcaaccount.CredentialOptions{GroupID: &groupID})
	if err != nil {
		t.Fatal("pre-logout credential unavailable")
	}
	if err = client.Logout(ctx); err != nil {
		t.Fatal("real revoke/logout failed")
	}
	if client.Snapshot().Authenticated || client.CurrentSessionGeneration() == gen {
		t.Fatal("logout did not invalidate generation")
	}
	if _, err = store.Load(ctx); !errors.Is(err, orcaaccount.ErrNotFound) {
		t.Fatal("logout retained in-memory tokens")
	}
	if _, err = client.Models(ctx); !errors.Is(err, orcaaccount.ErrUnauthenticated) {
		t.Fatal("logout allowed catalog authentication")
	}
	staleRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, orcaaccount.Router+"/v1/chat/completions", nil)
	if err != nil {
		t.Fatal("stale credential test construction failed")
	}
	if resp, e := staleCredential.HTTPClient().Do(staleRequest); !errors.Is(e, orcaaccount.ErrStale) || resp != nil {
		if resp != nil {
			resp.Body.Close()
		}
		t.Fatal("logout failed to reject the old credential locally")
	}
	if guard.attempts.Load() != 4 || guard.denied.Load() != 0 {
		t.Fatal("stale credential reached fixture transport")
	}
	t.Log("actual candidate acceptance passed: four synthetic inferences, exact integer receipts, query-only canceled recovery, revoke")
}

func candidateStream(t *testing.T, parent context.Context, client *orcaaccount.Client, model candidateModel, id string, cancelAfterDelta bool) string {
	t.Helper()
	ctx, timeout := context.WithTimeout(parent, 30*time.Second)
	defer timeout()
	ctx, stop, err := client.GenerationContext(ctx, client.CurrentSessionGeneration())
	if err != nil {
		t.Fatal("stream generation binding failed")
	}
	defer stop()
	ctx, cancel := context.WithCancel(provider.WithoutRequestReplay(ctx))
	defer cancel()
	credential, err := client.Credentials(ctx, orcaaccount.CredentialOptions{GroupID: &model.GroupID})
	if err != nil {
		t.Fatal("restricted scoped credential failed")
	}
	info := credential.Info()
	if !info.Restricted || info.GroupID == nil || *info.GroupID != model.GroupID || info.BaseURL != orcaaccount.Router+"/v1" {
		t.Fatal("credential scope contract failed")
	}
	*info.GroupID = -1
	if *credential.Info().GroupID != model.GroupID {
		t.Fatal("credential DTO aliases authorization scope")
	}
	encoded, err := json.Marshal(credential)
	if err != nil || bytes.Contains(encoded, []byte("api_key")) || bytes.Contains(encoded, []byte("access_token")) || bytes.Contains(encoded, []byte("refresh_token")) {
		t.Fatal("credential serialization exposed secret fields")
	}
	httpClient := credential.HTTPClient()
	base := httpClient.Transport
	var mu sync.Mutex
	var serverID string
	attempts := 0
	httpClient.Transport = candidateRoundTrip(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		attempts++
		mu.Unlock()
		copy := r.Clone(r.Context())
		copy.Header.Set("Idempotency-Key", id)
		resp, e := base.RoundTrip(copy)
		if resp != nil {
			mu.Lock()
			serverID = resp.Header.Get("X-Request-Id")
			mu.Unlock()
		}
		return resp, e
	})
	cfg := provider.Config{Name: "synthetic-candidate", BaseURL: credential.Info().BaseURL, Model: model.ID, HTTPClient: httpClient}
	var adapter provider.Provider
	if model.Protocol == "openai" {
		adapter, err = openai.New(cfg)
	} else {
		adapter, err = anthropic.New(cfg)
	}
	if err != nil {
		t.Fatal("actual adapter construction failed")
	}
	stream, err := adapter.Stream(ctx, provider.Request{RequestID: id, Purpose: provider.RequestPurposeTurn, MaxTokens: 16,
		Messages: []provider.Message{{Role: provider.RoleUser, Content: candidateText}}})
	if err != nil {
		t.Fatal("actual adapter HTTP stream admission failed")
	}
	var output strings.Builder
	var canceledAt time.Time
	for {
		select {
		case <-ctx.Done():
			if !cancelAfterDelta || canceledAt.IsZero() {
				t.Fatal("stream timed out before synthetic first delta")
			}
			// Cancellation must finish the actual adapter goroutine promptly.
			drain := time.NewTimer(3 * time.Second)
			defer drain.Stop()
			for {
				select {
				case _, ok := <-stream:
					if !ok {
						goto finished
					}
				case <-drain.C:
					t.Fatal("canceled adapter did not drain promptly")
				}
			}
		case chunk, ok := <-stream:
			if !ok {
				goto finished
			}
			if chunk.Type == provider.ChunkError && !cancelAfterDelta {
				t.Fatal("actual adapter reported stream failure")
			}
			if chunk.Type == provider.ChunkText {
				output.WriteString(chunk.Text)
				if output.Len() > 128 {
					t.Fatal("fixture emitted excessive synthetic output")
				}
				if cancelAfterDelta && chunk.Text != "" && canceledAt.IsZero() {
					canceledAt = time.Now()
					cancel()
				}
			}
		}
	}
finished:
	if cancelAfterDelta {
		if canceledAt.IsZero() || time.Since(canceledAt) > 3*time.Second {
			t.Fatal("cancellation did not occur after first delta")
		}
	} else if output.String() != "synthetic-ok" {
		t.Fatal("actual adapter did not parse expected synthetic output")
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != 1 || !candidateRequestID(serverID) {
		t.Fatal("adapter retried or omitted server request identity")
	}
	return serverID
}

func candidatePause(ctx context.Context) bool {
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func candidateAwaitStats(t *testing.T, parent context.Context, client *http.Client, token, id string, canceled bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	for attempt := 0; attempt < 30; attempt++ {
		stats, err := candidateReadStats(ctx, client, token, id)
		if err != nil {
			t.Fatal("fixture metadata counters unavailable")
		}
		if stats.IngressAttempts != 1 || stats.UpstreamCalls != 1 || stats.TotalIngressAttempts > 4 || stats.TotalUpstreamCalls > 4 {
			t.Fatal("fixture observed replay or excess admissions")
		}
		if canceled && stats.TerminalUsageSent {
			t.Fatal("cancellation fixture emitted terminal usage")
		}
		ready := stats.ResponseHeadersSent && stats.FirstDeltaSent
		if canceled {
			ready = ready && stats.IngressContextCancelled && stats.UpstreamClosed
		} else {
			ready = ready && stats.TerminalUsageSent
		}
		if ready {
			return
		}
		if !candidatePause(ctx) {
			break
		}
	}
	t.Fatal("fixture cancellation/stream completion evidence timed out")
}

func candidateAwaitReceipt(t *testing.T, parent context.Context, client *orcaaccount.Client, id string, canceled bool, expected int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	for attempt := 0; attempt < 30; attempt++ {
		receipt, err := client.Request(ctx, id)
		if err == nil {
			if receipt.Currency != "CNY" || receipt.RequestID != id {
				t.Fatal("receipt identity/currency mismatch")
			}
			if canceled {
				if receipt.SettledMicros != nil || receipt.Status == orcaaccount.SettlementSettled {
					t.Fatal("missing terminal usage was falsely settled")
				}
				if receipt.Status == orcaaccount.SettlementPending || receipt.Status == orcaaccount.SettlementUnknown {
					return
				}
			} else if receipt.Status == orcaaccount.SettlementSettled {
				if receipt.SettledMicros == nil || *receipt.SettledMicros != expected {
					t.Fatal("native settlement differs from exact 18 CNY micros")
				}
				if receipt.CashCostMicros != nil && receipt.GiftCostMicros != nil && *receipt.CashCostMicros+*receipt.GiftCostMicros != expected {
					t.Fatal("cash/gift settlement breakdown mismatch")
				}
				return
			}
		}
		if !candidatePause(ctx) {
			break
		}
	}
	t.Fatal("bounded native receipt query did not reach expected state")
}
