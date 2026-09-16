package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

func TestDeepSeekV41TabsMigration(t *testing.T) {
	isolateDesktopUserDirs(t)
	cfg := config.Default()
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{Name: "relay", Kind: "openai", BaseURL: "https://relay.example", Model: "deepseek-v4-pro"})
	f := desktopTabsFile{Tabs: []desktopTabEntry{{ID: "a", Model: "deepseek/deepseek-v4-pro", SessionPath: "keep.jsonl"}, {ID: "b", Model: "relay/deepseek-v4-pro"}}, ActiveTab: "a", RecentConversationPrefs: recentConversationPrefs{Model: "deepseek/deepseek-v4-flash-vision-exp"}}
	body, _ := json.Marshal(f)
	path := filepath.Join(desktopConfigDir(), tabsFileName)
	if err := atomicDesktopModelState(path, body); err != nil {
		t.Fatal(err)
	}
	if err := migrateDeepSeekV41Tabs(&f, cfg); err != nil {
		t.Fatal(err)
	}
	if f.DeepSeekModelVersion != 41 || f.Tabs[0].Model != "deepseek/deepseek-flash" || f.Tabs[1].Model != "relay/deepseek-v4-pro" || f.RecentConversationPrefs.Model != f.Tabs[0].Model || f.Tabs[0].SessionPath != "keep.jsonl" {
		t.Fatalf("migration: %+v", f)
	}
	backup, err := os.ReadFile(path + ".pre-v41.bak")
	if err != nil || string(backup) != string(body) {
		t.Fatalf("backup: %v", err)
	}
	f.Tabs[0].Model = "deepseek/deepseek-v4-pro"
	body, _ = json.Marshal(f)
	if err := atomicDesktopModelState(path, body); err != nil {
		t.Fatal(err)
	}
	if err := migrateDeepSeekV41Tabs(&f, cfg); err != nil {
		t.Fatal(err)
	}
	if f.Tabs[0].Model != "deepseek/deepseek-v4-pro" {
		t.Fatal("manual Pro overwritten")
	}
}

func TestDeepSeekV41TabsUsesCurrentDiskSnapshot(t *testing.T) {
	for name, initial := range map[string]desktopTabsFile{
		"stale_order":     {Tabs: []desktopTabEntry{{ID: "a", Model: "deepseek/deepseek-v4-pro"}, {ID: "b", Model: "relay/deepseek-v4-pro"}}},
		"empty":           {},
		"stale_completed": {DeepSeekModelVersion: 41, Tabs: []desktopTabEntry{{ID: "stale", Model: "relay/stale"}}},
	} {
		for _, addTab := range []bool{false, true} {
			t.Run(name+"/"+map[bool]string{false: "reordered", true: "new_tab"}[addTab], func(t *testing.T) {
				stale := initial
				isolateDesktopUserDirs(t)
				cfg := config.Default()
				cfg.Providers = append(cfg.Providers, config.ProviderEntry{Name: "relay", Kind: "openai", BaseURL: "https://relay.example", Model: "deepseek-v4-pro"})
				disk := `{"tabs":[{"id":"b","model":"relay/deepseek-v4-pro","sessionPath":"b.jsonl","extra":{"keep":true}},{"id":"a","model":"deepseek/deepseek-v4-pro","sessionPath":"a.jsonl","extra":{"keep":9007199254740993}}],"activeTab":"b","recentConversationPrefs":{"model":"deepseek/deepseek-v4-pro","future":"keep"},"future":{"nested":[1,2,3]}}`
				if addTab {
					disk = strings.Replace(disk, `"tabs":[`, `"tabs":[{"id":"new","model":"relay/deepseek-v4-pro","sessionPath":"new.jsonl","newField":"preserve"},`, 1)
				}
				path := filepath.Join(desktopConfigDir(), tabsFileName)
				if err := atomicDesktopModelState(path, []byte(disk)); err != nil {
					t.Fatal(err)
				}
				if err := migrateDeepSeekV41Tabs(&stale, cfg); err != nil {
					t.Fatal(err)
				}
				if stale.ActiveTab != "b" || stale.DeepSeekModelVersion != 41 {
					t.Fatalf("used stale state: %+v", stale)
				}
				wantIDs := []string{"b", "a"}
				if addTab {
					wantIDs = append([]string{"new"}, wantIDs...)
				}
				if len(stale.Tabs) != len(wantIDs) {
					t.Fatalf("tabs=%v", stale.Tabs)
				}
				for i, id := range wantIDs {
					wantModel := "relay/deepseek-v4-pro"
					if id == "a" {
						wantModel = "deepseek/deepseek-flash"
					}
					if stale.Tabs[i].ID != id || stale.Tabs[i].Model != wantModel || stale.Tabs[i].SessionPath != id+".jsonl" {
						t.Fatalf("model/identity mismatch: %+v", stale.Tabs[i])
					}
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var originalDoc, migratedDoc map[string]json.RawMessage
				json.Unmarshal([]byte(disk), &originalDoc)
				json.Unmarshal(got, &migratedDoc)
				if !equalDesktopJSON(originalDoc["future"], migratedDoc["future"]) {
					t.Fatal("unknown top-level field changed")
				}
				var oldTabs, newTabs []map[string]json.RawMessage
				json.Unmarshal(originalDoc["tabs"], &oldTabs)
				json.Unmarshal(migratedDoc["tabs"], &newTabs)
				for i := range oldTabs {
					delete(oldTabs[i], "model")
					delete(newTabs[i], "model")
					x, _ := json.Marshal(oldTabs[i])
					y, _ := json.Marshal(newTabs[i])
					if !equalDesktopJSON(x, y) {
						t.Fatal("migration changed non-model tab fields")
					}
				}
				backup, _ := os.ReadFile(path + ".pre-v41.bak")
				if string(backup) != disk {
					t.Fatal("backup is not the authoritative disk snapshot")
				}
			})
		}
	}
}

func equalDesktopJSON(a, b []byte) bool {
	var x, y bytes.Buffer
	return json.Compact(&x, a) == nil && json.Compact(&y, b) == nil && bytes.Equal(x.Bytes(), y.Bytes())
}

func TestDeepSeekV41TabsRejectsConcurrentEdits(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed", true: "deleted"}[remove], func(t *testing.T) {
			isolateDesktopUserDirs(t)
			root := t.TempDir()
			f := desktopTabsFile{Tabs: []desktopTabEntry{{ID: "keep", WorkspaceRoot: root, Model: "deepseek/deepseek-v4-pro"}}, ActiveTab: "keep"}
			before := f
			raw, _ := json.Marshal(f)
			path := filepath.Join(desktopConfigDir(), tabsFileName)
			if err := atomicDesktopModelState(path, raw); err != nil {
				t.Fatal(err)
			}
			concurrent := []byte(`{"tabs":[{"id":"new","model":"custom"}],"activeTab":"new","concurrent":true}`)
			err := migrateDeepSeekV41TabsWithLoader(&f, config.Default(), func(string) (*config.Config, error) {
				if remove {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				} else if err := atomicDesktopModelState(path, concurrent); err != nil {
					t.Fatal(err)
				}
				return config.Default(), nil
			})
			if err == nil || !strings.Contains(err.Error(), "changed during") {
				t.Fatalf("concurrent edit not rejected: %v", err)
			}
			if !reflect.DeepEqual(f, before) {
				t.Fatal("failure changed caller state")
			}
			got, readErr := os.ReadFile(path)
			if remove {
				if !os.IsNotExist(readErr) {
					t.Fatal("deleted disk file was resurrected")
				}
			} else if !bytes.Equal(got, concurrent) {
				t.Fatal("concurrent state overwritten")
			}
		})
	}
}

func TestDeepSeekV41TabsMissingSnapshotRejectsNewDiskFile(t *testing.T) {
	isolateDesktopUserDirs(t)
	path := filepath.Join(desktopConfigDir(), tabsFileName)
	concurrent := []byte(`{"tabs":[{"id":"new"}]}`)
	if err := atomicDesktopModelState(path, concurrent); err != nil {
		t.Fatal(err)
	}
	if err := replaceDesktopModelSnapshot(path, nil, []byte(`{"tabs":[]}`)); err == nil {
		t.Fatal("new disk file overwritten by missing snapshot")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, concurrent) {
		t.Fatal("new file lost")
	}
}

func TestDeepSeekV41ProjectFailureDoesNotBlockOtherTabSaves(t *testing.T) {
	isolateDesktopUserDirs(t)
	badRoot := t.TempDir()
	badConfig := filepath.Join(badRoot, "orca.toml")
	if err := os.WriteFile(badConfig, []byte("[broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := map[string]any{"id": "bad", "scope": "project", "workspaceRoot": badRoot, "model": "deepseek/deepseek-v4-pro", "sessionPath": "bad.jsonl", "goal": "keep goal", "future": map[string]any{"keep": true}}
	badRaw, _ := json.Marshal(bad)
	doc := map[string]any{"tabs": []any{bad, map[string]any{"id": "good", "model": "deepseek/deepseek-v4-pro", "sessionPath": "good.jsonl", "extra": "keep"}}, "activeTab": "good", "future": "keep", "recentConversationPrefs": map[string]any{"model": "deepseek/deepseek-v4-pro", "extra": "keep"}}
	raw, _ := json.Marshal(doc)
	path := filepath.Join(desktopConfigDir(), tabsFileName)
	if err := atomicDesktopModelState(path, raw); err != nil {
		t.Fatal(err)
	}
	f := loadTabsFile()
	a := NewApp()
	a.migrateDesktopModels(&f, config.Default())
	if a.modelMigrationErr != "" || len(a.modelMigrationProjects) != 1 || len(f.DeepSeekPendingModelRoots) != 1 {
		t.Fatalf("project failure became global: %q / %v", a.modelMigrationErr, a.modelMigrationProjects)
	}
	if f.Tabs[0].Model != "deepseek/deepseek-v4-pro" || f.Tabs[1].Model != "deepseek/deepseek-flash" {
		t.Fatal("partial migration altered the wrong project")
	}
	blocked := &WorkspaceTab{ID: "bad", Scope: "project", WorkspaceRoot: badRoot, SessionPath: "bad.jsonl", model: f.Tabs[0].Model}
	good := &WorkspaceTab{ID: "good", Scope: "global", SessionPath: "good.jsonl", model: f.Tabs[1].Model, Ready: true}
	a.tabs = map[string]*WorkspaceTab{"bad": blocked, "good": good}
	a.tabOrder = []string{"bad", "good"}
	a.activeTabID = "good"
	a.deepSeekModelVersion = f.DeepSeekModelVersion
	a.recentPrefs = f.RecentConversationPrefs
	a.buildTabController(blocked)
	if blocked.StartupErr == "" || blocked.Ready || a.MetaForTab("bad").StartupErr == "" || a.MetaForTab("good").StartupErr != "" {
		t.Fatal("project error was not scoped to the failed tab")
	}
	// A usable tab can change its model, close/open tabs, and update preferences.
	good.model = "deepseek/deepseek-v4-pro"
	good.SessionPath = "new-good.jsonl"
	a.recentPrefs.Model = "deepseek/deepseek-v4-pro"
	added := &WorkspaceTab{ID: "added", Scope: "global", model: "custom/new", SessionPath: "added.jsonl"}
	a.tabs[added.ID] = added
	a.tabOrder = []string{"good", "added", "bad"}
	a.activeTabID = "added"
	for i := 0; i < 2; i++ {
		a.mu.Lock()
		a.saveTabsLocked()
		a.mu.Unlock()
	}
	if a.modelMigrationErr != "" {
		t.Fatal(a.modelMigrationErr)
	}
	saved := loadTabsFile()
	if len(saved.Tabs) != 3 || saved.ActiveTab != "added" || saved.Tabs[0].ID != "good" || saved.Tabs[0].Model != good.model || saved.Tabs[0].SessionPath != good.SessionPath || saved.RecentConversationPrefs.Model != good.model {
		t.Fatalf("usable-tab edits lost: %+v", saved)
	}
	savedRaw, _ := os.ReadFile(path)
	var savedDoc map[string]json.RawMessage
	json.Unmarshal(savedRaw, &savedDoc)
	var savedTabs []map[string]json.RawMessage
	json.Unmarshal(savedDoc["tabs"], &savedTabs)
	encodedBad, _ := json.Marshal(savedTabs[2])
	if !equalDesktopJSON(encodedBad, badRaw) || string(savedDoc["future"]) != `"keep"` || string(savedTabs[0]["extra"]) != `"keep"` {
		t.Fatal("failed record or unknown metadata changed on unrelated save")
	}
	// Repairing the project retries only its pending records, preserving manual Pro.
	if err := os.Remove(badConfig); err != nil {
		t.Fatal(err)
	}
	restarted := loadTabsFile()
	if err := migrateDeepSeekV41Tabs(&restarted, config.Default()); err != nil {
		t.Fatal(err)
	}
	if len(restarted.DeepSeekPendingModelRoots) != 0 || len(restarted.modelMigrationProjects) != 0 || restarted.Tabs[0].Model != good.model || restarted.Tabs[2].Model != "deepseek/deepseek-flash" || restarted.RecentConversationPrefs.Model != good.model {
		t.Fatalf("retry changed completed selections: %+v", restarted)
	}
}

func TestDeepSeekV41BlockedBackupLeavesCallerAndDiskIntact(t *testing.T) {
	isolateDesktopUserDirs(t)
	f := desktopTabsFile{Tabs: []desktopTabEntry{{ID: "a", Model: "deepseek/deepseek-v4-pro"}}}
	before := f
	raw, _ := json.Marshal(f)
	path := filepath.Join(desktopConfigDir(), tabsFileName)
	if err := atomicDesktopModelState(path, raw); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".pre-v41.bak", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := migrateDeepSeekV41Tabs(&f, config.Default()); err == nil {
		t.Fatal("blocked backup accepted")
	}
	got, _ := os.ReadFile(path)
	if !reflect.DeepEqual(f, before) || !bytes.Equal(raw, got) {
		t.Fatal("failed migration changed state")
	}
}

func TestDeepSeekV41ProjectFailureCanBeRetriedWithoutOtherProjects(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	f := desktopTabsFile{DeepSeekModelVersion: 41, DeepSeekPendingModelRoots: []string{root}, Tabs: []desktopTabEntry{{ID: "pending", WorkspaceRoot: root, Model: "deepseek/deepseek-v4-pro"}, {ID: "done", WorkspaceRoot: t.TempDir(), Model: "deepseek/deepseek-v4-pro"}}}
	raw, _ := json.Marshal(f)
	path := filepath.Join(desktopConfigDir(), tabsFileName)
	if err := atomicDesktopModelState(path, raw); err != nil {
		t.Fatal(err)
	}
	var calls []string
	if err := migrateDeepSeekV41TabsWithLoader(&f, config.Default(), func(got string) (*config.Config, error) {
		calls = append(calls, got)
		return nil, errors.New("blocked project config")
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{root}) || f.Tabs[1].Model != "deepseek/deepseek-v4-pro" || len(f.modelMigrationProjects) != 1 {
		t.Fatal("retry included unrelated project")
	}
}

func TestDeepSeekV41TabsCorruptStateIsNotOverwritten(t *testing.T) {
	isolateDesktopUserDirs(t)
	path := filepath.Join(desktopConfigDir(), tabsFileName)
	if err := atomicDesktopModelState(path, []byte("{broken")); err != nil {
		t.Fatal(err)
	}
	f := desktopTabsFile{}
	if err := migrateDeepSeekV41Tabs(&f, config.Default()); err == nil {
		t.Fatal("accepted corrupt state")
	}
	if f.DeepSeekModelVersion != 0 {
		t.Fatal("marked incomplete migration")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "{broken" {
		t.Fatal("state lost")
	}
}

func TestDeepSeekV41SavesRestoredTabIDsWithoutLosingMetadata(t *testing.T) {
	isolateDesktopUserDirs(t)
	badRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(badRoot, "orca.toml"), []byte("[broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	for i, id := range []string{"", "", " dup ", "dup"} {
		record := map[string]any{"id": id, "model": "deepseek/deepseek-v4-pro", "future": i, "sessionPath": []string{"first.jsonl", "second.jsonl", "third.jsonl", "fourth.jsonl"}[i]}
		if i == 3 {
			record["scope"] = "project"
			record["workspaceRoot"] = badRoot
			record["goal"] = "preserve pending project"
		}
		records = append(records, record)
	}
	raw, _ := json.Marshal(map[string]any{"tabs": records})
	path := filepath.Join(desktopConfigDir(), tabsFileName)
	if err := atomicDesktopModelState(path, raw); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	f := loadTabsFile()
	a.migrateDesktopModels(&f, config.Default())
	if a.modelMigrationErr != "" {
		t.Fatal(a.modelMigrationErr)
	}
	a.deepSeekModelVersion = f.DeepSeekModelVersion
	wantRecords := make(map[string]map[string]any)
	for i, entry := range f.Tabs {
		id := a.restoredTabIDLocked(entry.ID)
		a.trackDesktopModelTabRestore(entry, id)
		a.tabs[id] = &WorkspaceTab{ID: id, Scope: entry.Scope, WorkspaceRoot: entry.WorkspaceRoot, SessionPath: entry.SessionPath, model: entry.Model}
		// Save in a different order from both the disk snapshot and restoration.
		a.tabOrder = append([]string{id}, a.tabOrder...)
		wantRecords[id] = records[i]
	}
	for i := 0; i < 2; i++ {
		a.mu.Lock()
		a.saveTabsLocked()
		a.mu.Unlock()
		if a.modelMigrationErr != "" {
			t.Fatal(a.modelMigrationErr)
		}
		saved := loadTabsFile()
		if len(saved.Tabs) != len(records) {
			t.Fatal("restored tabs lost")
		}
		seen := make(map[string]bool)
		for _, entry := range saved.Tabs {
			if entry.ID == "" || seen[entry.ID] {
				t.Fatal("restored IDs were not persisted")
			}
			seen[entry.ID] = true
			var fields map[string]any
			if err := json.Unmarshal(entry.modelMigrationRecord, &fields); err != nil {
				t.Fatal(err)
			}
			want := wantRecords[entry.ID]
			if want == nil || fields["future"] != float64(want["future"].(int)) || fields["sessionPath"] != want["sessionPath"] {
				t.Fatalf("metadata followed the old ID or array position: saved=%v want=%v", fields, want)
			}
			if entry.WorkspaceRoot == badRoot && (entry.Model != "deepseek/deepseek-v4-pro" || fields["goal"] != want["goal"]) {
				t.Fatal("pending project data changed during ID repair")
			}
		}
	}
}

func TestDeepSeekV41SaveClearsKnownFieldsAndPreservesUnknownPrefs(t *testing.T) {
	isolateDesktopUserDirs(t)
	path := filepath.Join(desktopConfigDir(), tabsFileName)
	raw := []byte(`{"deepseekModelVersion":41,"tabs":[{"id":"a","model":"custom/model","effort":"high","sessionPath":"old.jsonl","goal":"old goal","askWorkflowEnabled":true,"stepThinkingEnabled":true,"future":9007199254740993}],"recentConversationPrefs":{"model":"custom/model","effort":"high","future":{"keep":true}},"future":"keep"}`)
	if err := atomicDesktopModelState(path, raw); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	f := loadTabsFile()
	a.migrateDesktopModels(&f, config.Default())
	a.deepSeekModelVersion = f.DeepSeekModelVersion
	a.trackDesktopModelTabRestore(f.Tabs[0], "a")
	a.tabs["a"] = &WorkspaceTab{ID: "a", model: "custom/model"}
	a.tabOrder = []string{"a"}
	for i := 0; i < 2; i++ {
		a.mu.Lock()
		a.saveTabsLocked()
		a.mu.Unlock()
		if a.modelMigrationErr != "" {
			t.Fatal(a.modelMigrationErr)
		}
		savedRaw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]json.RawMessage
		json.Unmarshal(savedRaw, &doc)
		var tabs []map[string]json.RawMessage
		json.Unmarshal(doc["tabs"], &tabs)
		for _, key := range []string{"effort", "sessionPath", "goal", "askWorkflowEnabled", "stepThinkingEnabled"} {
			if _, exists := tabs[0][key]; exists {
				t.Fatalf("cleared setting %q was resurrected", key)
			}
		}
		var prefs map[string]json.RawMessage
		json.Unmarshal(doc["recentConversationPrefs"], &prefs)
		if prefs["model"] != nil || prefs["effort"] != nil || !equalDesktopJSON(prefs["future"], []byte(`{"keep":true}`)) || string(doc["future"]) != `"keep"` || string(tabs[0]["future"]) != "9007199254740993" {
			t.Fatal("cleared preferences or unknown fields changed")
		}
	}
}

func TestDeepSeekV41ConcurrentSaveSurfacesErrorAndPreservesDisk(t *testing.T) {
	isolateDesktopUserDirs(t)
	path := filepath.Join(desktopConfigDir(), tabsFileName)
	if err := atomicDesktopModelState(path, []byte(`{"tabs":[{"id":"a","model":"deepseek/deepseek-v4-pro"}]}`)); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	f := loadTabsFile()
	a.migrateDesktopModels(&f, config.Default())
	a.deepSeekModelVersion = f.DeepSeekModelVersion
	a.tabs["a"] = &WorkspaceTab{ID: "a", model: "custom/new", Ready: true}
	a.activeTabID = "a"
	concurrent := []byte(`{"tabs":[{"id":"external","model":"custom/external"}]}`)
	if err := atomicDesktopModelState(path, concurrent); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		a.mu.Lock()
		a.saveTabsLocked()
		a.mu.Unlock()
		if !strings.Contains(a.MetaForTab("a").StartupErr, "changed during") {
			t.Fatal("save conflict was not surfaced")
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, concurrent) {
			t.Fatal("concurrent save overwritten")
		}
		if a.tabs["a"].model != "custom/new" {
			t.Fatal("unsaved in-memory selection discarded")
		}
	}
}

func TestDeepSeekV41GlobalConfigFailureStopsStartupWithoutMigratingTabs(t *testing.T) {
	for _, failure := range []string{"parse", "migration_backup"} {
		t.Run(failure, func(t *testing.T) {
			isolateDesktopUserDirs(t)
			t.Chdir(t.TempDir())
			configPath := config.UserConfigPath()
			configRaw := []byte("config_version = 11\n[[providers]]\nname = \"deepseek\"\nkind = \"openai\"\nbase_url = \"https://relay.example/v1\"\nmodel = \"deepseek-v4-pro\"\n")
			if failure == "parse" {
				configRaw = append(configRaw, []byte("[broken")...)
			}
			if err := atomicDesktopModelState(configPath, configRaw); err != nil {
				t.Fatal(err)
			}
			if failure == "migration_backup" {
				if err := os.Mkdir(configPath+".pre-v41.bak", 0o700); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(desktopConfigDir(), tabsFileName)
			raw := []byte(`{"tabs":[{"id":"custom","model":"deepseek/deepseek-v4-pro","sessionPath":"keep.jsonl","future":{"keep":true}}],"activeTab":"custom","recentConversationPrefs":{"model":"deepseek/deepseek-v4-pro"}}`)
			if err := atomicDesktopModelState(path, raw); err != nil {
				t.Fatal(err)
			}
			sessionRaw := []byte("retained session fixture\n")
			if err := os.WriteFile("keep.jsonl", sessionRaw, 0o600); err != nil {
				t.Fatal(err)
			}
			a := NewApp()
			notified := false
			a.readyHook = func() { notified = true }
			a.restoreOrBuildTabs()
			if !notified || !strings.Contains(a.Meta().StartupErr, "Global configuration could not be loaded") {
				t.Fatal("global configuration error was not surfaced")
			}
			if len(a.tabs) != 0 || a.modelMigrationSnapshot != nil || a.deepSeekModelVersion != 0 {
				t.Fatal("startup continued with an untrusted configuration")
			}
			// Later controller/save attempts must remain blocked too.
			tab := &WorkspaceTab{ID: "custom", model: "deepseek/deepseek-v4-pro"}
			a.tabs[tab.ID] = tab
			a.buildTabController(tab)
			if tab.Ctrl != nil || tab.Ready || tab.StartupErr == "" {
				t.Fatal("controller started after global configuration failure")
			}
			a.mu.Lock()
			a.saveTabsLocked()
			a.mu.Unlock()
			for file, want := range map[string][]byte{path: raw, configPath: configRaw, "keep.jsonl": sessionRaw} {
				got, err := os.ReadFile(file)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("startup failure changed original state in %s", filepath.Base(file))
				}
			}
			if _, err := os.Stat(path + ".pre-v41.bak"); !os.IsNotExist(err) {
				t.Fatal("tab migration was attempted after configuration load failed")
			}
		})
	}
}
