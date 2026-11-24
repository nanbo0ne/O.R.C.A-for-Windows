package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestBrowserTopicStorageSurvivesNewRuntimeOwnerAndRestart(t *testing.T) {
	for _, scope := range []string{"global", "project"} {
		t.Run(scope, func(t *testing.T) {
			isolateDesktopUserDirs(t)
			oldChannel := channel
			channel = browserPreviewChannel
			defer func() { channel = oldChannel }()
			workspace, storage := t.TempDir(), t.TempDir()
			fake := func() *browserFake {
				return &browserFake{tabs: map[string]browserhost.Tab{}, profiles: map[string]string{}, generation: map[string]uint64{}}
			}
			backend := fake()
			s := newBrowserService(backend, storage, nil)
			defer s.shutdown()
			app := &App{browser: s, tabs: map[string]*WorkspaceTab{}, tabOrder: []string{"runtime-old", "sentinel"}, activeTabID: "runtime-old"}
			app.tabs["runtime-old"] = &WorkspaceTab{ID: "runtime-old", TopicID: "persistent-topic", Scope: scope, WorkspaceRoot: workspace}
			app.tabs["sentinel"] = &WorkspaceTab{ID: "sentinel", TopicID: "sentinel-topic", Scope: "global", WorkspaceRoot: workspace}
			if _, err := app.browserOwner("runtime-old"); err != nil {
				t.Fatal(err)
			}
			created := s.command(context.Background(), "runtime-old", BrowserCommand{Op: "new", Value: "https://example.test/persisted"})
			if created.Error != "" {
				t.Fatal(created.Error)
			}
			oldID := created.ActiveTabID
			s.onEvent(browserhost.Event{TabID: oldID, Kind: "loaded", URL: "https://example.test/persisted", Title: "Persisted synthetic title"})
			profilePath := backend.profiles[oldID]
			if err := os.MkdirAll(profilePath, 0700); err != nil {
				t.Fatal(err)
			}
			// This marker models cookie data owned by the backend profile. The
			// service must reuse that exact directory; no real WebView is started.
			cookiePath := filepath.Join(profilePath, "synthetic-cookie.json")
			if err := os.WriteFile(cookiePath, []byte(`{"session":"synthetic-cookie-value"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := app.CloseTab("runtime-old"); err != nil {
				t.Fatal(err)
			}
			app.mu.Lock()
			newOwner := app.newUniqueTabIDLocked()
			app.tabs[newOwner] = &WorkspaceTab{ID: newOwner, TopicID: "persistent-topic", Scope: scope, WorkspaceRoot: workspace}
			app.tabOrder = append(app.tabOrder, newOwner)
			app.activeTabID = newOwner
			app.mu.Unlock()
			if _, err := app.browserOwner(newOwner); err != nil {
				t.Fatal(err)
			}
			state := s.state(newOwner)
			if len(state.Tabs) != 1 || state.Tabs[0].URL != created.Tabs[0].URL || state.Tabs[0].Title != "Persisted synthetic title" || state.ActiveTabID == oldID || len(backend.tabs) != 0 {
				t.Fatal("topic metadata not lazily rebound to fresh runtime identities", state)
			}
			s.onEvent(browserhost.Event{TabID: oldID, Kind: "loaded", URL: "https://stale-owner.test"})
			if s.state(newOwner).Tabs[0].URL != "https://example.test/persisted" {
				t.Fatal("retired native event changed reopened topic")
			}
			if _, err := app.browserOwner("runtime-old"); !errors.Is(err, errBrowserOwner) {
				t.Fatal("closed app owner recovered authority", err)
			}
			if err := s.present(context.Background(), "runtime-old", BrowserViewport{TabID: state.ActiveTabID}); !errors.Is(err, errBrowserOwner) {
				t.Fatal("closed service owner controlled new tab", err)
			}
			selected := s.command(context.Background(), newOwner, BrowserCommand{Op: "select", TabID: state.ActiveTabID})
			if selected.Error != "" || backend.profiles[state.ActiveTabID] != profilePath {
				t.Fatal("reopen changed the cookie profile directory", selected.Error)
			}
			s.shutdown()
			restartedBackend := fake()
			restarted := newBrowserService(restartedBackend, storage, nil)
			defer restarted.shutdown()
			restartedApp := &App{browser: restarted, tabs: map[string]*WorkspaceTab{"runtime-after-restart": {ID: "runtime-after-restart", TopicID: "persistent-topic", Scope: scope, WorkspaceRoot: workspace}}}
			if _, err := restartedApp.browserOwner("runtime-after-restart"); err != nil {
				t.Fatal(err)
			}
			restored := restarted.state("runtime-after-restart")
			if len(restored.Tabs) != 1 || restored.Tabs[0].URL != "https://example.test/persisted" || len(restartedBackend.tabs) != 0 {
				t.Fatal("restart metadata restore failed or started native engine", restored)
			}
			restored = restarted.command(context.Background(), "runtime-after-restart", BrowserCommand{Op: "select", TabID: restored.ActiveTabID})
			if restored.Error != "" || restartedBackend.profiles[restored.ActiveTabID] != profilePath {
				t.Fatal("restart did not reuse cookie profile", restored.Error)
			}
			if cookie, err := os.ReadFile(cookiePath); err != nil || string(cookie) != `{"session":"synthetic-cookie-value"}` {
				t.Fatal("synthetic profile data changed", err)
			}
			restartedApp.tabs["another-topic"] = &WorkspaceTab{ID: "another-topic", TopicID: "other-topic", Scope: scope, WorkspaceRoot: workspace}
			if _, err := restartedApp.browserOwner("another-topic"); err != nil {
				t.Fatal(err)
			}
			if len(restarted.state("another-topic").Tabs) != 0 {
				t.Fatal("another topic inherited metadata")
			}
			sharedProfile := restarted.owners["another-topic"].profile == restarted.owners["runtime-after-restart"].profile
			if sharedProfile != (scope == "project") {
				t.Fatal("project sharing or independent-topic isolation changed")
			}
		})
	}
}

func TestBrowserPersistentIdentityScopeAndRebindingGuards(t *testing.T) {
	project := t.TempDir()
	key := browserStorageKey("old-runtime", "topic", "project", project)
	if key != browserStorageKey("new-runtime", "topic", "project", filepath.Join(project, ".")) {
		t.Fatal("runtime ID or equivalent root changed stable storage identity")
	}
	for _, different := range []string{
		browserStorageKey("old-runtime", "topic", "project", t.TempDir()),
		browserStorageKey("old-runtime", "other-topic", "project", project),
		browserStorageKey("old-runtime", "topic", "global", project),
		browserStorageKey("old-runtime", "topic", "automation", project),
	} {
		if different == key {
			t.Fatal("topic/scope/project storage collision")
		}
	}
	if browserStorageKey("a", "", "global", "") == browserStorageKey("b", "", "global", "") {
		t.Fatal("empty TopicID shared storage")
	}
	s, _, _ := browserFixture(t)
	if err := s.register("first", "profile", key); err != nil {
		t.Fatal(err)
	}
	if err := s.register("second", "profile", key); !errors.Is(err, errBrowserOwner) {
		t.Fatal("simultaneous owners aliased one storage identity", err)
	}
	if err := s.register("first", "profile", "another-storage-key"); !errors.Is(err, errBrowserOwner) {
		t.Fatal("existing runtime owner rebound to another topic", err)
	}
	s.closeOwner("first")
	if err := s.register("second", "profile", key); err != nil {
		t.Fatal("closed topic could not reopen", err)
	}
}

func TestBrowserMetadataRestoreIsLazyAndScoped(t *testing.T) {
	s, _, id := browserFixture(t)
	s.saveMetadata("owner")
	data, err := os.ReadFile(s.metadataPath("owner"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "screenshot") || strings.Contains(string(data), "consent") || strings.Contains(string(data), "secret") {
		t.Fatal("private state persisted")
	}
	b := &browserFake{tabs: map[string]browserhost.Tab{}, profiles: map[string]string{}, generation: map[string]uint64{}}
	restored := newBrowserService(b, s.root, nil)
	defer restored.shutdown()
	if err := restored.register("owner", "project-a"); err != nil {
		t.Fatal(err)
	}
	if st := restored.state("owner"); len(st.Tabs) != 1 || st.ActiveTabID != id {
		t.Fatal(st)
	}
	if len(b.tabs) != 0 {
		t.Fatal("metadata restore started native browser/network")
	}
	_ = restored.register("other", "project-a")
	if len(restored.state("other").Tabs) != 0 {
		t.Fatal("metadata leaked to other conversation")
	}
	if st := restored.command(context.Background(), "owner", BrowserCommand{Op: "select", TabID: id}); st.Error != "" {
		t.Fatal(st.Error)
	}
	if len(b.tabs) != 1 {
		t.Fatal("selection did not lazily create restored tab")
	}
}
func TestBrowserClearRequiresApprovalAndRealBackendSuccess(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{true: "backend_failure", false: "cleared"}[fail], func(t *testing.T) {
			s, b, id := browserFixture(t)
			_ = s.register("other", "project-a")
			other := s.command(context.Background(), "other", BrowserCommand{Op: "new", Value: "https://example.test/other"}).ActiveTabID
			clears := 0
			b.command = func(tabID, op, value string) (string, error) {
				if op == "state" {
					body, _ := json.Marshal(b.tabs[tabID])
					return string(body), nil
				}
				if op == "clear" {
					clears++
					if _, ok := b.tabs[other]; ok {
						t.Fatal("other profile tab still active during clear")
					}
					if b.tabs[id].URL != "about:blank" {
						t.Fatal("clearing while page can rewrite storage")
					}
					if fail {
						return "", errors.New("native clear failed")
					}
				}
				return "", nil
			}
			state := s.command(context.Background(), "owner", BrowserCommand{Op: "clear", TabID: id})
			if state.Approval == nil || clears != 0 {
				t.Fatal("clear did not wait for confirmation")
			}
			result := s.approve("owner", BrowserCommand{ApprovalID: state.Approval.ID, Allow: true})
			if clears != 1 {
				t.Fatal(clears)
			}
			if fail {
				if result.Error == "" || len(result.Tabs) != 1 || result.Tabs[0].URL != "https://example.test/page" {
					t.Fatal("failed clear destroyed recovery metadata")
				}
			} else {
				if result.Error != "" || len(result.Tabs) != 0 || len(s.state("other").Tabs) != 0 {
					t.Fatal("successful clear did not remove shared metadata")
				}
				data, err := os.ReadFile(s.metadataPath("other"))
				if err != nil {
					t.Fatal(err)
				}
				var saved browserMetadata
				if json.Unmarshal(data, &saved) != nil || len(saved.Tabs) != 0 {
					t.Fatal("other owner metadata still restores cleared tabs")
				}
			}
		})
	}
}
func TestBrowserDownloadApprovalOwnership(t *testing.T) {
	s, b, id := browserFixture(t)
	_ = s.register("other", "different")
	s.onEvent(browserhost.Event{TabID: id, Kind: "download", Download: &browserhost.Download{ID: "download", TabID: id, URL: "https://example.test/file", Status: "pending", RequiresApproval: true}})
	if len(s.state("other").Downloads) != 0 || len(s.state("owner").Downloads) != 1 {
		t.Fatal("download ownership")
	}
	called := false
	b.command = func(_, op, _ string) (string, error) {
		if op == "download-approve" {
			called = true
		}
		return "", nil
	}
	if err := s.downloadCommand(context.Background(), "owner", id, BrowserCommand{Op: "download-approve", Value: `{"id":"download","path":"relative.txt"}`}); err == nil {
		t.Fatal("relative destination accepted")
	}
	if called {
		t.Fatal("native reached before destination validation")
	}
	if err := s.downloadCommand(context.Background(), "other", id, BrowserCommand{Op: "download-deny", Value: "download"}); !errors.Is(err, errBrowserOwner) {
		t.Fatal(err)
	}
}
func TestBrowserCanvasDetectionAndCoordinateMapping(t *testing.T) {
	o := browserObserved{Observation: browserhost.Observation{TabID: "t", DocumentID: "d", Generation: 1, Text: "Canvas UI", Elements: []browserhost.Element{{ID: "canvas", Role: "canvas"}}, Screenshot: []byte("validated by observationForModel"), ViewportWidth: 640, ViewportHeight: 480, ScreenshotWidth: 1280, ScreenshotHeight: 960}}
	if !browserNeedsVision(o.Observation) {
		t.Fatal("canvas incorrectly treated as DOM-capable")
	}
	a, err := browserAction(`{"kind":"click","x":640,"y":480}`, o, true)
	if err != nil || a.X != 320 || a.Y != 240 {
		t.Fatal(a, err)
	}
	if _, err = browserAction(`{"kind":"click","x":1280,"y":10}`, o, true); err == nil {
		t.Fatal("out-of-bounds screenshot coordinate")
	}
	for _, input := range []string{`{"kind":"click","elementId":"canvas"}`, `{"kind":"click","elementId":"canvas","x":640,"y":480}`} {
		if _, err = browserAction(input, o, true); err == nil || !strings.Contains(err.Error(), "canvas IDs") {
			t.Fatal("canvas container accepted as clickable control", err)
		}
	}
	if !strings.Contains(browserSchemas[0].Description, "omit elementId") || !strings.Contains(browserSchemas[0].Description, "screenshot pixel") {
		t.Fatal("action schema omits canvas coordinate rules")
	}
	o.Elements = append(o.Elements, browserhost.Element{ID: "button", Role: "button"})
	if browserNeedsVision(o.Observation) {
		t.Fatal("usable DOM ignored")
	}
	a, err = browserAction(`{"kind":"click","x":640,"y":480}`, o, true)
	if err != nil || a.X != 320 || a.Y != 240 {
		t.Fatal("qualified screenshot coordinates rejected when DOM appears", a, err)
	}
	for _, raw := range []string{`{"kind":"click"}`, `{"kind":"click","x":0}`, `{"kind":"click","y":0}`, `{"kind":"click","x":null,"y":0}`, `{"kind":"click","x":0,"y":null}`} {
		if _, err := browserAction(raw, o, true); err == nil {
			t.Fatal("missing screenshot coordinate silently defaulted to zero", raw)
		}
	}
	if a, err := browserAction(`{"kind":"click","x":0,"y":0}`, o, true); err != nil || a.X != 0 || a.Y != 0 {
		t.Fatal("explicit image origin rejected", a, err)
	}
	o.ScreenshotBlocked = "protected frame"
	if _, err := browserAction(`{"kind":"click","x":0,"y":0}`, o, true); err == nil {
		t.Fatal("blocked image allowed coordinate click")
	}
}
func TestBrowserHiddenScreenshotDoesNotProbeModel(t *testing.T) {
	s, b, _ := browserFixture(t)
	autoBrowserConsent(s)
	b.observe = func(o *browserhost.Observation) { o.Elements = nil; o.Screenshot = nil }
	p := browserCompleter(func(r provider.Request) <-chan provider.Chunk { t.Fatal("hidden screenshot reached model"); return nil })
	_, err := s.run(computerParentTestContext(t), "owner", "outer", browserInput(), p, "fake/model")
	if err == nil || !strings.Contains(err.Error(), "expand the browser panel") {
		t.Fatal(err)
	}
}
