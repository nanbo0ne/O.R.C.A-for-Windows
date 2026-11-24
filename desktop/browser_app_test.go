package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
)

// Synthetic only: this backend never starts WebView2, reads a user profile, or
// uses HTTP. Every test profile is under testing.T.TempDir.
type browserFake struct {
	mu           sync.Mutex
	guard        func(string, string) bool
	tabs         map[string]browserhost.Tab
	generation   map[string]uint64
	profiles     map[string]string
	actions      int
	observations int
	lastAction   browserhost.Action
	view         browserhost.Viewport
	closed       bool
	noSupport    bool
	closeErr     error
	shutdownErr  error
	execute      func(context.Context) error
	observe      func(*browserhost.Observation)
	command      func(string, string, string) (string, error)
}

func (b *browserFake) Supported() bool { return !b.noSupport }
func (b *browserFake) SetNavigationGuard(g func(string, string) bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.guard = g
}
func (b *browserFake) Create(ctx context.Context, p, id, u string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !b.guard(id, u) {
		return browserhost.ErrNavigationDenied
	}
	b.tabs[id] = browserhost.Tab{ID: id, URL: u, Title: "Synthetic", Zoom: 1}
	b.generation[id] = 1
	b.profiles[id] = p
	return nil
}
func (b *browserFake) Close(_ context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closeErr != nil {
		return b.closeErr
	}
	delete(b.tabs, id)
	return nil
}
func (b *browserFake) Navigate(ctx context.Context, id, u string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !b.guard(id, u) {
		return browserhost.ErrNavigationDenied
	}
	v := b.tabs[id]
	v.URL = u
	b.tabs[id] = v
	b.generation[id]++
	return nil
}
func (b *browserFake) Command(_ context.Context, id, op, value string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.command != nil {
		return b.command(id, op, value)
	}
	if op == "state" {
		v, _ := json.Marshal(b.tabs[id])
		return string(v), nil
	}
	return "", nil
}
func (b *browserFake) Present(_ context.Context, v browserhost.Viewport) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.view = v
	return nil
}
func (b *browserFake) Observe(ctx context.Context, id string) (browserhost.Observation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return browserhost.Observation{}, err
	}
	b.observations++
	b.generation[id]++
	o := browserhost.Observation{TabID: id, URL: b.tabs[id].URL, Title: "Synthetic", DocumentID: "document", Generation: b.generation[id], Text: fmt.Sprintf("Synthetic page observation %d", b.observations), Elements: []browserhost.Element{{ID: "button", Role: "button", Name: "Synthetic button", X: 5, Y: 5, W: 40, H: 20}, {ID: "input", Role: "textbox", Type: "text", Options: []browserhost.SelectOption{{Value: "synthetic", Label: "Synthetic option"}}}, {ID: "password", Type: "password", Value: "secret-fixture"}}, Screenshot: []byte("synthetic-image"), MIME: "image/png"}
	if b.observe != nil {
		b.observe(&o)
	}
	return o, nil
}
func (b *browserFake) Execute(ctx context.Context, id string, a browserhost.Action) error {
	b.mu.Lock()
	if a.Generation != b.generation[id] || a.DocumentID != "document" {
		b.mu.Unlock()
		return browserhost.ErrStaleObservation
	}
	b.generation[id]++
	b.actions++
	b.lastAction = a
	fn := b.execute
	b.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return ctx.Err()
}
func (b *browserFake) Shutdown() { b.mu.Lock(); defer b.mu.Unlock(); b.closed = true }
func (b *browserFake) ShutdownConfirmed(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.shutdownErr != nil {
		return b.shutdownErr
	}
	b.closed = true
	return nil
}
func browserFixture(t *testing.T) (*browserService, *browserFake, string) {
	t.Helper()
	b := &browserFake{tabs: map[string]browserhost.Tab{}, generation: map[string]uint64{}, profiles: map[string]string{}}
	s := newBrowserService(b, t.TempDir(), nil)
	if err := s.register("owner", "project-a"); err != nil {
		t.Fatal(err)
	}
	state := s.command(context.Background(), "owner", BrowserCommand{Op: "new", Value: "https://example.test/page"})
	if state.Error != "" {
		t.Fatal(state.Error)
	}
	t.Cleanup(s.shutdown)
	return s, b, state.ActiveTabID
}
func autoBrowserConsent(s *browserService) {
	s.emit = func(owner string, state BrowserState) {
		if state.Approval != nil {
			s.approve(owner, BrowserCommand{Op: "approve", ApprovalID: state.Approval.ID, Allow: true})
		}
	}
}

func TestBrowserURLPolicy(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "file:///C:/secret", "data:text/html,hi", "https://u:p@example.test", "about:config", "http://example.test\\@evil.test", "https://example.test\n/path", "https://", "ftp://example.test"} {
		if _, _, err := browserURL(raw); err == nil {
			t.Errorf("allowed %q", raw)
		}
	}
	for _, tt := range []struct{ raw, origin string }{{"https://EXAMPLE.test:443/a", "https://example.test"}, {"http://example.test:80/", "http://example.test"}, {"https://example.test:444/a", "https://example.test:444"}, {"https://[::1]:443/a", "https://[::1]"}, {"about:blank", ""}} {
		_, origin, err := browserURL(tt.raw)
		if err != nil || origin != tt.origin {
			t.Fatalf("%s => %s %v", tt.raw, origin, err)
		}
	}
}
func TestBrowserProfileIsolation(t *testing.T) {
	root := t.TempDir()
	a := browserProfileKey("a", "project", root)
	b := browserProfileKey("b", "project", filepath.Join(root, "."))
	if a != b || len(a) != 64 || strings.Contains(a, root) {
		t.Fatal("project identity not canonical and hashed")
	}
	if browserProfileKey("a", "global", root) == browserProfileKey("b", "global", root) {
		t.Fatal("independent conversations share profile")
	}
	if browserProfileKey("a", "project", root) == browserProfileKey("a", "project", t.TempDir()) {
		t.Fatal("projects share profile")
	}
}
func TestBrowserOwnershipAndViewport(t *testing.T) {
	s, b, id := browserFixture(t)
	_ = s.register("other", "project-a")
	for _, op := range []string{"select", "close", "navigate", "back", "forward", "reload", "stop", "zoom", "find", "clear"} {
		st := s.command(context.Background(), "other", BrowserCommand{Op: op, TabID: id, Value: "https://example.test"})
		if st.Error != errBrowserOwner.Error() {
			t.Fatalf("%s: %+v", op, st)
		}
	}
	if err := s.present(context.Background(), "other", BrowserViewport{TabID: id}); !errors.Is(err, errBrowserOwner) {
		t.Fatal(err)
	}
	if len(s.state("other").Tabs) != 0 {
		t.Fatal("other owner's tabs exposed")
	}
	view := BrowserViewport{TabID: id, Visible: true, Width: 640, Height: 480, Scale: 1}
	if err := s.present(context.Background(), "owner", view); err != nil {
		t.Fatal(err)
	}
	next := s.command(context.Background(), "owner", BrowserCommand{Op: "new"})
	if next.Error != "" {
		t.Fatal(next.Error)
	}
	b.mu.Lock()
	shown := b.view.TabID
	b.mu.Unlock()
	if shown != next.ActiveTabID {
		t.Fatal("new tab not presented")
	}
	st := s.command(context.Background(), "owner", BrowserCommand{Op: "select", TabID: id})
	if st.Error != "" {
		t.Fatal(st.Error)
	}
	b.mu.Lock()
	shown = b.view.TabID
	b.mu.Unlock()
	if shown != id {
		t.Fatal("selected tab not presented")
	}
	view.X = math.NaN()
	if s.present(context.Background(), "owner", view) == nil {
		t.Fatal("NaN accepted")
	}
	view.X = 0
	view.Scale = 0
	if s.present(context.Background(), "owner", view) == nil {
		t.Fatal("zero scale accepted")
	}
	s.closeOwner("owner")
	if err := s.register("owner", "project-a"); err == nil {
		t.Fatal("closed owner resurrected")
	}
	if b.guard(id, "https://example.test") {
		t.Fatal("closed owner navigation allowed")
	}
}
func TestBrowserCommandAllowlist(t *testing.T) {
	s, _, id := browserFixture(t)
	for _, op := range []string{"evaluate", "javascript", "screenshot", "state", "cdp"} {
		if st := s.command(context.Background(), "owner", BrowserCommand{Op: op, TabID: id}); st.Error == "" {
			t.Fatalf("unexpected exposed command %s", op)
		}
	}
	for _, v := range []string{"NaN", "Inf", "0.49", "2.01"} {
		if st := s.command(context.Background(), "owner", BrowserCommand{Op: "zoom", Value: v}); st.Error == "" {
			t.Fatalf("invalid zoom %s", v)
		}
	}
}
func TestBrowserEventsDoNotAdoptBlockedURL(t *testing.T) {
	s, _, id := browserFixture(t)
	s.onEvent(browserhost.Event{TabID: id, Kind: "blocked", URL: "https://evil.test"})
	if s.state("owner").Tabs[0].URL != "https://example.test/page" {
		t.Fatal("blocked navigation changed committed URL")
	}
	s.onEvent(browserhost.Event{TabID: "unknown", Kind: "loaded", URL: "https://evil.test"})
}

func TestBrowserNativeMetadataEventsUpdateCachedState(t *testing.T) {
	s, b, id := browserFixture(t)
	task, end, err := s.beginTask(computerParentTestContext(t), "owner", "metadata-parent", id, "fake/model")
	if err != nil {
		t.Fatal(err)
	}
	defer end(nil)
	b.command = func(_, _, _ string) (string, error) {
		t.Fatal("cached event handling queried native state")
		return "", nil
	}
	for _, event := range []browserhost.Event{
		{Kind: "loaded", URL: "https://example.test/two", Title: "Second", CanGoBack: true},
		{Kind: "history", URL: "https://example.test/page", Title: "First", CanGoForward: true},
		{Kind: "title", URL: "https://example.test/page", Title: "", CanGoForward: true},
		{Kind: "source", URL: "https://example.test/page#section", Title: "Section", CanGoBack: true, CanGoForward: true},
	} {
		s.mu.Lock()
		epoch := s.tabs[id].epoch
		s.mu.Unlock()
		event.TabID = id
		s.onEvent(event)
		state := s.state("owner")
		got := state.Tabs[0]
		if got.URL != event.URL || got.Title != event.Title || got.CanGoBack != event.CanGoBack || got.CanGoForward != event.CanGoForward || got.Loading {
			t.Fatalf("%s did not update cached metadata: %+v", event.Kind, got)
		}
		s.mu.Lock()
		invalidated := s.tabs[id].epoch > epoch
		s.mu.Unlock()
		if !invalidated || task.ctx.Err() != nil {
			t.Fatalf("%s must invalidate observations without cancelling the task", event.Kind)
		}
		previous := browserObserved{Observation: browserhost.Observation{TabID: id, DocumentID: "document", Generation: 1}, epoch: epoch}
		if err := s.current(task, previous); !errors.Is(err, browserhost.ErrStaleObservation) {
			t.Fatalf("%s left previous observation actionable: %v", event.Kind, err)
		}
	}
	for _, kind := range []string{"navigation", "user-input", "error", "blocked"} {
		s.onEvent(browserhost.Event{TabID: id, Kind: kind})
		got := s.state("owner").Tabs[0]
		if !got.CanGoBack || !got.CanGoForward {
			t.Fatalf("partial %s event reset history flags", kind)
		}
	}
}
func TestBrowserApprovalOwnershipReplayAndCancellation(t *testing.T) {
	s, _, id := browserFixture(t)
	_ = s.register("other", "project-b")
	ctx := computerParentTestContext(t)
	task, end, err := s.beginTask(ctx, "owner", "parent-1", id, "fake/model")
	if err != nil {
		t.Fatal(err)
	}
	defer end(context.Canceled)
	ready := make(chan BrowserApproval, 1)
	s.emit = func(_ string, st BrowserState) {
		if st.Approval != nil {
			ready <- *st.Approval
		}
	}
	done := make(chan error, 1)
	go func() { done <- s.consent(task, "https://example.test", "Exact consent") }()
	approval := <-ready
	if st := s.approve("other", BrowserCommand{ApprovalID: approval.ID, Allow: true}); st.Error == "" {
		t.Fatal("wrong owner consent")
	}
	if st := s.approve("owner", BrowserCommand{ApprovalID: "forged", Allow: true}); st.Error == "" {
		t.Fatal("forged consent")
	}
	s.approve("owner", BrowserCommand{ApprovalID: approval.ID, Allow: true})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if st := s.approve("owner", BrowserCommand{ApprovalID: approval.ID, Allow: true}); st.Error == "" {
		t.Fatal("replayed consent")
	}
	go func() { done <- s.consent(task, "https://example.test", "Another exact consent") }()
	<-ready
	s.command(ctx, "owner", BrowserCommand{Op: "takeover"})
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("consent cancellation blocked")
	}
}
func TestBrowserProfileLeaseAndGuard(t *testing.T) {
	s, b, id := browserFixture(t)
	_ = s.register("other", "project-a")
	second := s.command(context.Background(), "other", BrowserCommand{Op: "new"}).ActiveTabID
	ctx := computerParentTestContext(t)
	task, end, err := s.beginTask(ctx, "owner", "p", id, "fake/model")
	if err != nil {
		t.Fatal(err)
	}
	defer end(context.Canceled)
	if _, _, err := s.beginTask(computerParentTestContext(t), "other", "p2", second, "fake/model"); !errors.Is(err, errBrowserBusy) {
		t.Fatal(err)
	}
	if st := s.command(ctx, "other", BrowserCommand{Op: "new"}); st.Error != errBrowserBusy.Error() {
		t.Fatal(st)
	}
	s.command(ctx, "other", BrowserCommand{Op: "takeover"})
	if task.ctx.Err() != nil {
		t.Fatal("other owner cancelled lease")
	}
	s.mu.Lock()
	task.origins["https://example.test"] = true
	s.mu.Unlock()
	if !b.guard(id, "https://example.test/path") {
		t.Fatal("approved navigation denied")
	}
	if b.guard(second, "https://example.test") {
		t.Fatal("shared profile lets another tab navigate during lease")
	}
	if b.guard(id, "https://unapproved.test") {
		t.Fatal("unapproved origin reached network")
	}
	if task.ctx.Err() == nil {
		t.Fatal("blocked navigation did not cancel controller")
	}
}
func TestBrowserConcurrentStateCommandsEvents(t *testing.T) {
	s, _, id := browserFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				switch n % 3 {
				case 0:
					s.state("owner")
				case 1:
					s.onEvent(browserhost.Event{TabID: id, Kind: "loaded", URL: "https://example.test/page"})
				case 2:
					s.command(context.Background(), "owner", BrowserCommand{Op: "find", Value: "fixture"})
				}
			}
		}(i)
	}
	wg.Wait()
}
func TestBrowserReleaseChannelEnabled(t *testing.T) {
	old := channel
	channel = "stable"
	defer func() { channel = old }()
	a := &App{}
	a.initBrowser()
	if a.browser == nil || len(a.browserTools("owner")) != 3 {
		t.Fatalf("browser control must ship on the official channel: browser=%v tools=%d", a.browser != nil, len(a.browserTools("owner")))
	}
	if st := a.GetBrowserState("owner"); st.Supported {
		t.Fatal("an unregistered owner must still report unsupported")
	}
}
