package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

func effortPersistenceV310Fixture(t *testing.T) (*App, *WorkspaceTab, *WorkspaceTab, <-chan map[string]any) {
	t.Helper()
	isolateDesktopUserDirs(t)
	t.Chdir(t.TempDir())
	requests := make(chan map[string]any, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case requests <- body:
		default:
			t.Error("unexpected excess model requests")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"synthetic reply\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Codegraph.Enabled = false
	cfg.DefaultModel = "effort-v310/synthetic-effort-v310"
	cfg.Providers = []config.ProviderEntry{{
		Name: "effort-v310", Kind: "openai", BaseURL: srv.URL + "/v1", Model: "synthetic-effort-v310",
		ReasoningProtocol: "openai", SupportedEfforts: []string{"high", "xhigh"},
		APIKeyEnv: "ORCA_EFFORT_V310_TEST_KEY", NoProxy: true,
	}}
	t.Setenv("ORCA_EFFORT_V310_TEST_KEY", "synthetic-local-test-key")
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.readyHook = func() {}
	a, b := testTab("effort-a", t.TempDir()), testTab("effort-b", t.TempDir())
	for _, tab := range []*WorkspaceTab{a, b} {
		tab.model = cfg.DefaultModel
		tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	}
	app.tabs = map[string]*WorkspaceTab{a.ID: a, b.ID: b}
	app.tabOrder = []string{a.ID, b.ID}
	app.activeTabID = a.ID
	t.Cleanup(func() {
		for _, tab := range []*WorkspaceTab{a, b} {
			if tab.Ctrl != nil {
				tab.Ctrl.Close()
			}
		}
	})
	return app, a, b, requests
}

func effortPersistenceV310Turn(t *testing.T, app *App, tab *WorkspaceTab, requests <-chan map[string]any, input, want string) map[string]any {
	t.Helper()
	if err := app.SubmitToTab(tab.ID, input); err != nil {
		t.Fatalf("SubmitToTab: %v", err)
	}
	var body map[string]any
	select {
	case body = <-requests:
	case <-time.After(10 * time.Second):
		t.Fatal("no synthetic model request")
	}
	deadline := time.Now().Add(10 * time.Second)
	for tab.Ctrl.Running() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if tab.Ctrl.Running() {
		t.Fatal("synthetic turn did not settle")
	}
	if got, present := body["reasoning_effort"]; (want == "" && present) || (want != "" && got != want) {
		t.Fatalf("reasoning_effort=%#v present=%v, want %q", got, present, want)
	}
	if body["model"] != "synthetic-effort-v310" {
		t.Fatalf("unexpected model: %v", body["model"])
	}
	if _, present := body["thinking"]; present {
		t.Fatal("generic OpenAI request included thinking")
	}
	history := tab.Ctrl.History()
	if len(history) == 0 || history[len(history)-1].Content != "synthetic reply" {
		t.Fatal("turn did not retain the synthetic assistant reply")
	}
	return body
}

func TestEffortPersistenceV310TurnsAutoAndReload(t *testing.T) {
	app, a, b, requests := effortPersistenceV310Fixture(t)
	configBefore, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	// Build the second tab through the same public workflow, with its own effort.
	if err := app.SetEffortForTab(b.ID, "high"); err != nil {
		t.Fatal(err)
	}
	otherCtrl := b.Ctrl
	if err := app.SetEffortForTab(a.ID, "xhigh"); err != nil {
		t.Fatal(err)
	}
	for turn := 1; turn <= 3; turn++ {
		body := effortPersistenceV310Turn(t, app, a, requests, fmt.Sprintf("turn %d", turn), "xhigh")
		messages, ok := body["messages"].([]any)
		if !ok {
			t.Fatal("missing request messages")
		}
		users := 0
		for _, raw := range messages {
			if message, ok := raw.(map[string]any); ok && message["role"] == "user" {
				users++
			}
		}
		if users != turn {
			t.Fatalf("turn %d carried %d user messages", turn, users)
		}
	}
	if err := app.SetEffortForTab(a.ID, "auto"); err != nil {
		t.Fatal(err)
	}
	effortPersistenceV310Turn(t, app, a, requests, "auto turn", "")
	if got := app.EffortForTab(a.ID).Current; got != "auto" {
		t.Fatalf("auto selection = %q", got)
	}
	if saved := loadTabsFile(); len(saved.Tabs) != 2 || saved.Tabs[0].ID != a.ID || saved.Tabs[0].Effort == nil || *saved.Tabs[0].Effort != "" {
		t.Fatal("auto did not persist an explicit empty tab override")
	}
	if err := app.SetEffortForTab(a.ID, "xhigh"); err != nil {
		t.Fatal(err)
	}
	effortPersistenceV310Turn(t, app, a, requests, "xhigh again", "xhigh")
	if b.Ctrl != otherCtrl || app.EffortForTab(b.ID).Current != "high" {
		t.Fatal("changing tab A changed tab B")
	}
	effortPersistenceV310Turn(t, app, b, requests, "independent tab", "high")
	app.snapshotAllTabs()
	app.mu.Lock()
	app.saveTabsLocked()
	app.mu.Unlock()
	saved := loadTabsFile()
	if len(saved.Tabs) != 2 || saved.ActiveTab != a.ID {
		t.Fatalf("saved tab count/active = %d/%q", len(saved.Tabs), saved.ActiveTab)
	}
	for _, tab := range saved.Tabs {
		want := map[string]string{a.ID: "xhigh", b.ID: "high"}[tab.ID]
		if want == "" || tab.Effort == nil || *tab.Effort != want || tab.Model != a.model {
			t.Fatalf("reloaded tab profile: %+v", tab)
		}
		if tab.SessionPath == "" {
			t.Fatal("snapshot did not persist the session path")
		}
		if _, err := os.Stat(tab.SessionPath); err != nil {
			t.Fatalf("snapshot file: %v", err)
		}
	}
	configAfter, err := os.ReadFile(config.UserConfigPath())
	if err != nil || !bytes.Equal(configBefore, configAfter) {
		t.Fatalf("tab-local effort modified provider configuration: %v", err)
	}
}

func TestEffortPersistenceV310FailedRebuildRetainsValue(t *testing.T) {
	app, a, _, requests := effortPersistenceV310Fixture(t)
	if err := app.SetEffortForTab(a.ID, "xhigh"); err != nil {
		t.Fatal(err)
	}
	effortPersistenceV310Turn(t, app, a, requests, "before failed rebuild", "xhigh")
	oldCtrl := a.Ctrl
	// The requested effort remains valid; an invalid endpoint fails the real
	// provider construction after validation, without contacting another server.
	cfg := config.LoadForEdit(config.UserConfigPath())
	entry, ok := cfg.Provider("effort-v310")
	if !ok {
		t.Fatal("missing synthetic provider")
	}
	entry.BaseURL = "://invalid-endpoint"
	if got, err := config.NormalizeEffort(entry, "high"); err != nil || got != "high" {
		t.Fatalf("failure fixture must pass effort validation: %q/%v", got, err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	if err := app.SetEffortForTab(a.ID, "high"); err == nil {
		t.Fatal("invalid endpoint unexpectedly rebuilt")
	}
	if a.Ctrl != oldCtrl || a.effort == nil || *a.effort != "xhigh" || app.EffortForTab(a.ID).Current != "xhigh" {
		t.Fatal("failed rebuild replaced the controller or old effort")
	}
	found := false
	for _, tab := range loadTabsFile().Tabs {
		if tab.ID == a.ID {
			found = true
			if tab.Effort == nil || *tab.Effort != "xhigh" {
				t.Fatal("failed rebuild persisted the rejected effort")
			}
		}
	}
	if !found {
		t.Fatal("failed rebuild lost the persisted tab")
	}
	effortPersistenceV310Turn(t, app, a, requests, "old controller still works", "xhigh")
}

func TestEffortPersistenceV310DiskFailureRollsBackAndRetries(t *testing.T) {
	app, a, _, requests := effortPersistenceV310Fixture(t)
	if err := app.SetEffortForTab(a.ID, "xhigh"); err != nil {
		t.Fatal(err)
	}
	effortPersistenceV310Turn(t, app, a, requests, "before disk failure", "xhigh")
	path := filepath.Join(desktopConfigDir(), tabsFileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	oldCtrl, oldEffort := a.Ctrl, a.effort
	oldLabel, oldStartupErr, oldReady := a.Label, a.StartupErr, a.Ready
	oldPrefs := app.recentPrefs

	// Keep the committed file intact while making the temporary write fail on
	// every platform, without relying on permissions or touching user data.
	tmp := path + ".tmp"
	if err := os.Mkdir(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove disk failure fixture: %v", err)
		}
	})
	err = app.SetEffortForTab(a.ID, "high")
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || pathErr.Path != tmp {
		t.Fatalf("expected temporary-file write failure, got %v", err)
	}
	if a.Ctrl != oldCtrl || a.effort != oldEffort || a.effort == nil || *a.effort != "xhigh" || app.EffortForTab(a.ID).Current != "xhigh" {
		t.Fatal("disk failure changed the working controller or xhigh override")
	}
	if a.Label != oldLabel || a.StartupErr != oldStartupErr || a.Ready != oldReady || !reflect.DeepEqual(app.recentPrefs, oldPrefs) {
		t.Fatal("disk failure did not restore tab metadata and recent preferences")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("disk failure changed committed tabs bytes: %v", err)
	}
	effortPersistenceV310Turn(t, app, a, requests, "after disk failure", "xhigh")

	if err := os.Remove(tmp); err != nil {
		t.Fatal(err)
	}
	if err := app.SetEffortForTab(a.ID, "high"); err != nil {
		t.Fatalf("retry after removing disk failure fixture: %v", err)
	}
	if a.Ctrl == oldCtrl || a.effort == nil || *a.effort != "high" || app.EffortForTab(a.ID).Current != "high" {
		t.Fatal("successful retry did not install the high controller and override")
	}
	saved := loadTabsFile()
	if len(saved.Tabs) != 2 || saved.Tabs[0].ID != a.ID || saved.Tabs[0].Effort == nil || *saved.Tabs[0].Effort != "high" {
		t.Fatal("successful retry did not persist high")
	}
	effortPersistenceV310Turn(t, app, a, requests, "after successful retry", "high")
}
