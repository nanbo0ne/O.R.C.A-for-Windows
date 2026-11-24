package webbeta

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestAcknowledgedUncertainDoesNotRelockAfterSuccessfulRestart(t *testing.T) {
	b := &fakeBrowser{fail: &Failure{Code: "connection_lost", Sent: true}}
	e := testEngine(t, b)
	e.state.Conversations = append(e.state.Conversations, Conversation{ID: "prepared", URL: Origin + "/a/chat/s/prepared", Phase: "ready"})
	emit := func(Event) error { return nil }
	oldRequest := Request{Owner: "old-owner", ID: "unknown-send", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic failed task"}}}
	if err := e.Send(context.Background(), oldRequest, emit); err == nil {
		t.Fatal("expected unknown send")
	}
	b.fail = nil
	if err := e.ConfirmRecovery(); err != nil {
		t.Fatal(err)
	}
	if !e.Status().Settings.Enabled || !e.Status().SendAuthorized {
		t.Fatal("recovery discarded prior consent")
	}
	s := e.Status().Settings
	s.Enabled, s.ReservePaused = true, true
	if err := e.Configure(s); err != nil {
		t.Fatal(err)
	}
	newRequest := Request{Owner: "new-owner", ID: "successful-send", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic successful task"}}}
	if err := e.Send(context.Background(), newRequest, emit); err != nil {
		t.Fatal(err)
	}
	if e.Status().Blocked != "" {
		t.Fatal("new request failed before restart")
	}
	e.Close()
	restarted, err := NewEngine(e.path, b)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { restarted.Close() }()
	status := restarted.Status()
	if status.Blocked != "" || !status.Settings.Enabled || !status.Settings.ReservePaused {
		t.Fatalf("acknowledged old record relocked successful restart: blocked=%q enabled=%t paused=%t", status.Blocked, status.Settings.Enabled, status.Settings.ReservePaused)
	}
	before := len(b.commands)
	oldRequest.ID = "new-attempt-on-unknown-context"
	if err := restarted.Send(context.Background(), oldRequest, emit); err == nil {
		t.Fatal("old uncertain context revived")
	}
	if len(b.commands) != before {
		t.Fatal("old unknown send replayed")
	}
	for i := 0; i < 3; i++ {
		restarted.Close()
		restarted, err = NewEngine(e.path, b)
		if err != nil {
			t.Fatal(err)
		}
		if status := restarted.Status(); status.Blocked != "" || !status.Settings.Enabled {
			t.Fatalf("restart %d relocked", i)
		}
		if len(b.commands) != before {
			t.Fatal("restart sent a request")
		}
	}
	restarted.Close()
}

func TestNormalSuccessfulClosePreservesConsentOnRestart(t *testing.T) {
	b := &fakeBrowser{}
	e := testEngine(t, b)
	request := Request{Owner: "success-owner", ID: "success", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic success"}}}
	if err := e.Send(context.Background(), request, func(Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	before := len(b.commands)
	e.Close()
	restarted, err := NewEngine(e.path, b)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	status := restarted.Status()
	if status.Blocked != "" || !status.Settings.Enabled || !status.Settings.ReservePaused {
		t.Fatalf("normal close changed consent: blocked=%q enabled=%t paused=%t", status.Blocked, status.Settings.Enabled, status.Settings.ReservePaused)
	}
	if len(b.commands) != before {
		t.Fatal("normal close or restart sent a request")
	}
}

func TestRecoveryAcknowledgementIsExactAndPhaseScoped(t *testing.T) {
	for _, tc := range []struct {
		name, phase, inflight, requestID string
		ack                              *UncertaintyAcknowledgement
		blocked                          bool
	}{
		{"matched", "uncertain", "pending", "previous", &UncertaintyAcknowledgement{"pending", "previous"}, false},
		{"empty-inflight", "uncertain", "", "previous", &UncertaintyAcknowledgement{"", "previous"}, false},
		{"changed-inflight", "uncertain", "new", "previous", &UncertaintyAcknowledgement{"pending", "previous"}, true},
		{"changed-request", "uncertain", "pending", "new", &UncertaintyAcknowledgement{"pending", "previous"}, true},
		{"bound", "bound", "pending", "previous", &UncertaintyAcknowledgement{"pending", "previous"}, true},
		{"preparing", "preparing", "", "", &UncertaintyAcknowledgement{"", ""}, true},
		{"legacy-uncertain-empty", "uncertain", "", "", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "state.json")
			s := State{Version: stateVersion, Settings: DefaultSettings(), Conversations: []Conversation{{ID: "synthetic", Phase: tc.phase, Inflight: tc.inflight, RequestID: tc.requestID, Recovery: tc.ack}}}
			s.Settings.Enabled = true
			s.SendAuthorized = true
			if err := saveState(p, s); err != nil {
				t.Fatal(err)
			}
			got, err := loadState(p)
			if err != nil {
				t.Fatal(err)
			}
			if (got.Blocked != "") != tc.blocked || got.Settings.Enabled == tc.blocked {
				t.Fatalf("unexpected recovery: %+v", got)
			}
			if tc.blocked && got.Conversations[0].Recovery != nil {
				t.Fatal("stale acknowledgement retained")
			}
		})
	}
}

func TestRecoveryLegacyStateRequiresExplicitAcknowledgement(t *testing.T) {
	for _, version := range []int{1, 2} {
		p := filepath.Join(t.TempDir(), "state.json")
		s := State{Version: version, Settings: DefaultSettings(), Conversations: []Conversation{{ID: "legacy", Phase: "bound", Inflight: "pending"}}}
		s.Settings.Enabled = true
		if err := saveState(p, s); err != nil {
			t.Fatal(err)
		}
		got, err := loadState(p)
		if err != nil {
			t.Fatal(err)
		}
		if got.Blocked != "send_unconfirmed" || got.Settings.Enabled || got.Conversations[0].Recovery != nil {
			t.Fatalf("legacy v%d silently recovered", version)
		}
	}
}

func TestRecoverySaveFailureDoesNotMutateState(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	e.state.Blocked = "send_unconfirmed"
	e.state.Conversations = []Conversation{{ID: "old", Owner: "owner", Phase: "uncertain", Inflight: "pending", RequestID: "previous"}}
	if err := saveState(e.path, e.state); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(e.state)
	diskBefore, err := os.ReadFile(e.path)
	if err != nil {
		t.Fatal(err)
	}
	actualPath := e.path
	blocker := filepath.Join(t.TempDir(), "file-not-directory")
	if err := os.WriteFile(blocker, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	e.path = filepath.Join(blocker, "state.json")
	if err := e.ConfirmRecovery(); err == nil {
		t.Fatal("expected save failure")
	}
	e.path = actualPath
	after, _ := json.Marshal(e.state)
	diskAfter, err := os.ReadFile(actualPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || string(diskBefore) != string(diskAfter) {
		t.Fatal("failed save changed memory or disk")
	}
}

func TestRecoveryRejectsActiveWorkButNotHistoricalOwners(t *testing.T) {
	for _, kind := range []string{"operation", "request", "stream", "preparing"} {
		t.Run(kind, func(t *testing.T) {
			e := testEngine(t, &fakeBrowser{})
			e.state.Blocked = "send_unconfirmed"
			var finish func()
			switch kind {
			case "operation":
				var err error
				finish, err = e.beginOperation("historical-owner")
				if err != nil {
					t.Fatal(err)
				}
			case "request":
				_, finish = e.requestContext(context.Background())
			case "stream":
				e.streams = map[string]bool{"active": true}
				finish = func() { clear(e.streams) }
			case "preparing":
				e.filling = true
				finish = func() { e.filling = false }
			}
			err := e.ConfirmRecovery()
			finish()
			if err == nil || e.state.Blocked != "send_unconfirmed" {
				t.Fatal("active work acknowledged")
			}
			if err := e.ConfirmRecovery(); err != nil {
				t.Fatalf("finished work prevents recovery: %v", err)
			}
		})
	}
}

func TestNewUncertaintyClearsAcknowledgementAndRelocks(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	e.state.Conversations = []Conversation{{ID: "old", Phase: "uncertain", Inflight: "pending", RequestID: "previous"}}
	if err := e.ConfirmRecovery(); err != nil {
		t.Fatal(err)
	}
	confirmed := e.state.Conversations[0]
	if !confirmed.uncertaintyAcknowledged() {
		t.Fatal("missing acknowledgement")
	}
	e.mu.Lock()
	e.uncertainLocked(&e.state.Conversations[0])
	e.mu.Unlock()
	got, err := loadState(e.path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocked != "send_unconfirmed" || got.Settings.Enabled || got.Conversations[0].Recovery != nil {
		t.Fatal("new uncertainty did not relock")
	}
	confirmed.Recovery = nil
	if !reflect.DeepEqual(confirmed, got.Conversations[0]) {
		t.Fatal("uncertainty destroyed evidence")
	}
}

func TestRecoveryPreservesUncertainEvidenceAndCalls(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	e.state.Blocked = "send_unconfirmed"
	old := Conversation{ID: "old", Owner: "owner", Phase: "uncertain", Inflight: "pending", RequestID: "previous", Prefix: []string{"hash"}, Reply: "reply-hash", Epoch: 3, Grammar: 2}
	e.state.Conversations = []Conversation{old}
	calls := []CallRecord{{Owner: "owner", ID: "tool", Name: "read_file", ArgumentsHash: "args-hash", RequestID: "previous", Status: "issued"}}
	e.state.Calls = append([]CallRecord(nil), calls...)
	if err := e.ConfirmRecovery(); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadState(e.path)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Conversations[0]
	if !got.uncertaintyAcknowledged() {
		t.Fatal("confirmation was not persisted")
	}
	got.Recovery = nil
	if !reflect.DeepEqual(got, old) || !reflect.DeepEqual(loaded.Calls, calls) {
		t.Fatal("confirmation changed conversation or call evidence")
	}
	if !loaded.Settings.Enabled || !loaded.SendAuthorized || !loaded.Settings.ReservePaused {
		t.Fatal("confirmation discarded prior consent or resumed reserves")
	}
}

func TestRecoveryStatusIsAnIndependentSnapshot(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	e.state.Conversations = []Conversation{{ID: "old", Phase: "uncertain", Inflight: "pending", RequestID: "previous"}}
	if err := e.ConfirmRecovery(); err != nil {
		t.Fatal(err)
	}
	status := e.Status()
	status.Conversations[0].Recovery.Inflight = "changed"
	status.Conversations[0].Recovery.RequestID = "changed"
	if !e.state.Conversations[0].uncertaintyAcknowledged() || !e.Status().Conversations[0].uncertaintyAcknowledged() {
		t.Fatal("status mutation changed the engine acknowledgement")
	}
	if err := saveState(e.path, e.state); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadState(e.path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Conversations[0].uncertaintyAcknowledged() || loaded.Blocked != "" {
		t.Fatal("status mutation changed the persisted acknowledgement")
	}
}
