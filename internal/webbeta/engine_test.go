package webbeta

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type fakeBrowser struct {
	mu       sync.Mutex
	commands []Command
	fail     error
}

func (b *fakeBrowser) State() BrowserState          { return BrowserState{Ready: true, URL: Origin} }
func (b *fakeBrowser) Logout(context.Context) error { return nil }
func (b *fakeBrowser) Run(ctx context.Context, c Command, emit func(Event) error) error {
	b.mu.Lock()
	b.commands = append(b.commands, c)
	err := b.fail
	b.mu.Unlock()
	if err != nil {
		return err
	}
	u := c.URL
	if u == "" {
		u = Origin + "/a/chat/s/" + c.ID
	}
	if err := emit(Event{ID: c.ID, Kind: "text", Text: "synthetic"}); err != nil {
		return err
	}
	return emit(Event{ID: c.ID, Kind: "done", URL: u})
}
func testEngine(t *testing.T, b Browser) *Engine {
	t.Helper()
	e, err := NewEngine(filepath.Join(t.TempDir(), "state.json"), b)
	if err != nil {
		t.Fatal(err)
	}
	// Historical fixtures exercise the V2 wire contract explicitly. New V3
	// integration tests opt in to the production default.
	e.newGrammar = currentGrammar
	e.queue.mu.Lock()
	e.queue.spacing = time.Millisecond
	e.queue.mu.Unlock()
	s := DefaultSettings()
	s.Enabled = true
	s.ReservePaused = true
	if err = e.Configure(s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e
}
func TestStickyAndCompactionPreservesArchivedMapping(t *testing.T) {
	b := &fakeBrowser{}
	e := testEngine(t, b)
	emit := func(Event) error { return nil }
	m := []provider.Message{{Role: provider.RoleUser, Content: "synthetic one"}}
	if err := e.Send(context.Background(), Request{Owner: "a", ID: "1", Messages: m}, emit); err != nil {
		t.Fatal(err)
	}
	m = append(m, provider.Message{Role: provider.RoleAssistant, Content: "synthetic"}, provider.Message{Role: provider.RoleUser, Content: "two"})
	if err := e.Send(context.Background(), Request{Owner: "a", ID: "2", Messages: m}, emit); err != nil {
		t.Fatal(err)
	}
	if b.commands[1].URL == "" {
		t.Fatal("did not reuse upstream conversation")
	}
	if strings.Contains(b.commands[2].Text, "[assistant]") {
		t.Fatal("confirmed assistant reply resent to its own upstream chat")
	}
	if err := e.Send(context.Background(), Request{Owner: "a", ID: "3", Epoch: 1, Messages: []provider.Message{{Role: provider.RoleUser, Content: "summary"}}}, emit); err != nil {
		t.Fatal(err)
	}
	s := e.Status()
	if len(s.Conversations) != 2 || s.Conversations[0].Phase != "archived" {
		t.Fatalf("wrong mappings: %+v", s.Conversations)
	}
}
func TestUncertainDoesNotSwitchOrReplay(t *testing.T) {
	b := &fakeBrowser{fail: &Failure{Code: "connection_lost", Sent: true}}
	e := testEngine(t, b)
	e.state.Conversations = append(e.state.Conversations, Conversation{ID: "prepared", URL: Origin + "/a/chat/s/prepared", Phase: "ready"})
	r := Request{Owner: "a", ID: "1", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}}
	if e.Send(context.Background(), r, func(Event) error { return nil }) == nil {
		t.Fatal("expected failure")
	}
	b.fail = nil
	r.ID = "2"
	if e.Send(context.Background(), r, func(Event) error { return nil }) == nil {
		t.Fatal("uncertain replay accepted")
	}
	if len(b.commands) != 1 {
		t.Fatal("uncertain send replayed")
	}
}

type rejectBeforeDispatchBrowser struct {
	fakeBrowser
	rejected bool
}

func (b *rejectBeforeDispatchBrowser) Run(ctx context.Context, c Command, emit func(Event) error) error {
	if c.Operation == "task" && !b.rejected {
		b.rejected = true
		return KnownUnsent(errors.New("synthetic pre-dispatch rejection"))
	}
	return b.fakeBrowser.Run(ctx, c, emit)
}

func TestKnownUnsentHostRejectionDoesNotLockOtherTasks(t *testing.T) {
	b := &rejectBeforeDispatchBrowser{}
	e := testEngine(t, b)
	e.state.Conversations = append(e.state.Conversations, Conversation{ID: "prepared", URL: Origin + "/a/chat/s/prepared", Phase: "ready"})
	first := Request{Owner: "first-owner", ID: "rejected-before-dispatch", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic first"}}}
	if err := e.Send(context.Background(), first, func(Event) error { return nil }); err == nil {
		t.Fatal("missing pre-dispatch error")
	}
	if status := e.Status(); status.Blocked != "" || !status.Settings.Enabled || len(status.Conversations) != 1 || status.Conversations[0].Inflight != "" {
		t.Fatalf("known unsent request locked the account or left an inflight send: %+v", status)
	}
	second := Request{Owner: "second-owner", ID: "new-task", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic second"}}}
	if err := e.Send(context.Background(), second, func(Event) error { return nil }); err != nil {
		t.Fatal("new task blocked by known unsent request:", err)
	}
	if status := e.Status(); status.Blocked != "" || !status.Settings.Enabled {
		t.Fatal("known unsent rejection poisoned later work")
	}
}

func TestRestrictionSurvivesSettingsAndRestart(t *testing.T) {
	b := &fakeBrowser{}
	e := testEngine(t, b)
	e.fail(&Failure{Code: "restricted", Sent: true})
	s := e.Status().Settings
	s.Enabled = true
	s.RPM = 1
	if err := e.Configure(s); err == nil {
		t.Fatal("settings resumed a restricted account")
	}
	e.Close()
	restored, err := NewEngine(e.path, b)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.Configure(s); err == nil {
		t.Fatal("restart cleared restriction")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = restored.Send(ctx, Request{Owner: "other", ID: "r", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}}, func(Event) error { return nil })
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("restriction must fail immediately: %v", err)
	}
	if len(b.commands) != 0 {
		t.Fatal("restricted account sent another request")
	}
}

func TestManualRecoveryRetainsConsentWithoutReplay(t *testing.T) {
	b := &fakeBrowser{}
	e := testEngine(t, b)
	e.state.Conversations = append(e.state.Conversations, Conversation{ID: "old", Owner: "task", Phase: "uncertain", Inflight: "unknown-send"})
	e.fail(&Failure{Code: "restricted", Sent: true})
	if err := e.ConfirmRecovery(); err != nil {
		t.Fatal(err)
	}
	s := e.Status()
	if s.Blocked != "" || !s.Settings.Enabled || !s.SendAuthorized || !s.Settings.ReservePaused {
		t.Fatalf("unsafe recovery: %+v", s.Settings)
	}
	if len(b.commands) != 0 || s.Conversations[0].Phase != "uncertain" || s.Conversations[0].Inflight != "unknown-send" {
		t.Fatal("recovery replayed or erased uncertain request")
	}
}
func TestStateRecoveryDoesNotRepeatOpening(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	s := State{Version: 1, Settings: DefaultSettings(), Conversations: []Conversation{{ID: "a", Phase: "preparing", Inflight: "first"}}}
	if err := saveState(p, s); err != nil {
		t.Fatal(err)
	}
	got, err := loadState(p)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Settings.ReservePaused || got.Conversations[0].Phase != "uncertain" {
		t.Fatal("unsafe restart")
	}
}
func TestVaultOwnershipAndImmutableDedup(t *testing.T) {
	v := NewVault()
	raw := []byte("synthetic plain text")
	id, err := v.Grant("a", "r", "note.txt", raw)
	if err != nil {
		t.Fatal(err)
	}
	id2, _ := v.Grant("a", "r", "note.txt", raw)
	if id != id2 {
		t.Fatal("same paste duplicated")
	}
	raw[0] = 'X'
	uploads, err := v.Resolve("a", "r", []string{id, id})
	if err != nil || len(uploads) != 1 || uploads[0].Data[0] == 'X' {
		t.Fatal("snapshot was not immutable")
	}
	if _, err = v.Resolve("other", "r", []string{id}); err == nil {
		t.Fatal("cross-session upload")
	}
	if _, err = v.Resolve("a", "migration", []string{id}); err == nil {
		t.Fatal("migration reused authorization")
	}
	v.Release("a", "r")
	if _, err = v.Resolve("a", "r", []string{id}); err == nil {
		t.Fatal("released grant retained")
	}
}
func TestSourceValidation(t *testing.T) {
	for _, u := range []string{"https://chat.deepseek.com.evil/a", "http://chat.deepseek.com/", "https://chat.deepseek.com@evil/", "https://chat.deepseek.com:8443/"} {
		if AllowedSource(u) {
			t.Fatal(u)
		}
	}
}
func TestCorruptStateFailsClosed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(p, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadState(p); err == nil {
		t.Fatal("corrupt state accepted")
	}
}
func TestRestrictionClassification(t *testing.T) {
	if !IsAccountBlocked(&Failure{Code: "restricted"}) || IsAccountBlocked(errors.New("empty response")) {
		t.Fatal("wrong classification")
	}
}
