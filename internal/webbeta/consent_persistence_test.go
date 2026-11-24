package webbeta

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestRecoveryRetainsPriorConsentWithoutSending(t *testing.T) {
	b := &fakeBrowser{}
	e := testEngine(t, b)
	for round := 0; round < 3; round++ {
		e.fail(&Failure{Code: "restricted", Sent: true})
		if e.Status().Settings.Enabled {
			t.Fatal("temporary block must disable actual sending")
		}
		if err := e.ConfirmRecovery(); err != nil {
			t.Fatal(err)
		}
		if s := e.Status(); !s.Settings.Enabled || !s.Settings.ReservePaused || s.Blocked != "" {
			t.Fatal("manual recovery lost the existing sending grant")
		}
		if len(b.commands) != 0 {
			t.Fatal("recovery sent a message or replenished reservations")
		}
	}
	e.Close()
	restored, err := NewEngine(e.path, b)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if !restored.Status().Settings.Enabled || len(b.commands) != 0 {
		t.Fatal("restart lost consent or sent a message")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := restored.Send(ctx, Request{Owner: "new-owner", ID: "new-request", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic new task"}}}, func(Event) error { return nil }); err != nil {
		t.Fatalf("a newly submitted task must work after recovery: %v", err)
	}
}

func TestRecoveryNeverCreatesConsentOrRestoresRevokedConsent(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		b := &fakeBrowser{}
		e := testEngine(t, b)
		s := e.Status().Settings
		s.Enabled = false
		if err := e.Configure(s); err != nil {
			t.Fatal(err)
		}
		if revoke {
			e.fail(&Failure{Code: "restricted"})
		}
		if err := e.ConfirmRecovery(); err != nil {
			t.Fatal(err)
		}
		if e.Status().Settings.Enabled || len(b.commands) != 0 {
			t.Fatal("recovery created or restored a revoked grant")
		}
	}
}

func TestIdentityChangeRevokesPersistentConsent(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	if err := e.StopForIdentityChange(); err != nil {
		t.Fatal(err)
	}
	e.Close()
	if err := e.BeginIdentityReset(); err != nil {
		t.Fatal(err)
	}
	if err := CompleteIdentityReset(e.path); err != nil {
		t.Fatal(err)
	}
	restored, err := NewEngine(e.path, &fakeBrowser{})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.ConfirmRecovery(); err != nil {
		t.Fatal(err)
	}
	if restored.Status().Settings.Enabled {
		t.Fatal("old account consent crossed the identity reset")
	}
}

func TestConsentMigrationIsConservative(t *testing.T) {
	for _, version := range []int{1, 2, 3, stateVersion} {
		for _, enabled := range []bool{false, true} {
			path := filepath.Join(t.TempDir(), "synthetic-state.json")
			s := State{Version: version, Settings: DefaultSettings()}
			s.Settings.Enabled = enabled
			if err := saveState(path, s); err != nil {
				t.Fatal(err)
			}
			loaded, err := loadState(path)
			if err != nil {
				t.Fatal(err)
			}
			want := version < 4 && enabled
			if loaded.SendAuthorized != want || loaded.Settings.Enabled != want {
				t.Fatalf("version %d enabled %t migrated without proven grant", version, enabled)
			}
		}
	}
}

func TestUncertainRestartRetainsGrantButBlocksSending(t *testing.T) {
	b := &fakeBrowser{}
	e := testEngine(t, b)
	e.mu.Lock()
	e.uncertainLocked(&Conversation{Phase: "uncertain"})
	e.mu.Unlock()
	e.Close()
	restored, err := NewEngine(e.path, b)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	s := restored.Status()
	if !s.SendAuthorized || s.Settings.Enabled || s.Blocked == "" {
		t.Fatal("restart must retain consent without clearing uncertainty")
	}
	if err := restored.Send(context.Background(), Request{Owner: "new", ID: "new"}, func(Event) error { return nil }); err == nil || len(b.commands) != 0 {
		t.Fatal("restart bypassed uncertainty")
	}
}

func TestFreshRecoveryDoesNotAuthorizeSending(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "state.json"), &fakeBrowser{})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.ConfirmRecovery(); err != nil {
		t.Fatal(err)
	}
	if s := e.Status(); s.SendAuthorized || s.Settings.Enabled {
		t.Fatal("fresh recovery granted consent")
	}
}

func TestLostIdentityRetainsGrantButRequiresAccountAdoption(t *testing.T) {
	for _, code := range []string{"signed_out", "identity_changed"} {
		t.Run(code, func(t *testing.T) {
			e := testEngine(t, &fakeBrowser{})
			e.fail(&Failure{Code: code})
			if s := e.Status(); !s.SendAuthorized || s.Settings.Enabled || s.Blocked != IdentitySwitchPending {
				t.Fatal("identity loss must retain the grant without permitting dispatch")
			}
			if err := e.ConfirmRecovery(); !errors.Is(err, ErrIdentityClosed) {
				t.Fatal("generic recovery bypassed account adoption", err)
			}
			e.Close()
			b := &fakeBrowser{}
			restored, err := NewEngine(e.path, b)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			if s := restored.Status(); !s.SendAuthorized || s.Settings.Enabled || s.Blocked != IdentitySwitchPending {
				t.Fatal("restart cleared pending adoption or lost the source grant")
			}
			if err := restored.Send(context.Background(), Request{Owner: "new", ID: "new"}, func(Event) error { return nil }); err == nil || len(b.commands) != 0 {
				t.Fatal("restart sent without explicit account adoption")
			}
		})
	}
}
