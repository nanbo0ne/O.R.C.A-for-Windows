package orcaaccount

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Diagnose response-buffer/store erasure independently of the native server.
// Every session read must use the newest complete opaque access token, and each
// refresh must consume the previous complete refresh token exactly once.
func TestRefreshOpaqueTokensSurviveBufferErasureAndRestore(t *testing.T) {
	f := newIssuer(t)
	var newestAccess, newestRefresh string
	sessionReads, rejectedReads := 0, 0
	f.mutateToken = func(out *tokenResponse) {
		access := sha256.Sum256([]byte(out.AccessToken))
		refresh := sha256.Sum256([]byte(out.RefreshToken))
		out.AccessToken = "oda_" + strings.Repeat(hex.EncodeToString(access[:]), 32)
		out.RefreshToken = "odr_" + strings.Repeat(hex.EncodeToString(refresh[:]), 32)
		newestAccess, newestRefresh = out.AccessToken, out.RefreshToken
	}
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != APIPrefix+"/session" {
			return false
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		sessionReads++
		if r.Header.Get("Authorization") != "Bearer "+newestAccess {
			rejectedReads++
			w.WriteHeader(http.StatusUnauthorized)
			return true
		}
		return false
	}
	store := &memoryStore{}
	client := f.client(t, store)
	profile := login(t, client)
	ctx := context.Background()
	for cycle := 0; cycle < 3; cycle++ {
		if err := client.Refresh(ctx); err != nil {
			t.Fatal("explicit opaque-token rotation failed")
		}
		got, err := client.Session(ctx)
		if err != nil || got.Subject != profile.Subject || got.SessionID != profile.SessionID {
			t.Fatal("session did not accept the just-rotated opaque token")
		}
		raw, err := store.Load(ctx)
		if err != nil {
			t.Fatal("rotated session was not persisted")
		}
		var persisted storedSession
		if json.Unmarshal(raw, &persisted) != nil {
			t.Fatal("rotated session bytes were erased")
		}
		clear(raw)
		f.mu.Lock()
		matches := persisted.AccessToken == newestAccess && persisted.RefreshToken == newestRefresh
		f.mu.Unlock()
		if !matches {
			t.Fatal("response or storage erasure changed an opaque token")
		}
		if err := client.Close(); err != nil {
			t.Fatal("close failed")
		}
		client = f.client(t, store)
		restored, err := client.Restore(ctx)
		if err != nil || restored.Subject != profile.Subject || restored.SessionID != profile.SessionID {
			t.Fatal("close/restore lost the rotated session binding")
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refreshCount != 3 || rejectedReads != 0 || sessionReads != 7 {
		t.Fatal("rotation produced a stale bearer, an extra refresh, or missing session read")
	}
}
