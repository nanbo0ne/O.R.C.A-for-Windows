package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

var desktopModelStateMu sync.Mutex

type desktopModelSnapshot struct {
	raw          []byte // nil means the file did not exist
	restoredTabs map[string]json.RawMessage
}

func (entry *desktopTabEntry) UnmarshalJSON(raw []byte) error {
	type tabEntry desktopTabEntry
	var next tabEntry
	if err := json.Unmarshal(raw, &next); err != nil {
		return err
	}
	next.modelMigrationRecord = bytes.Clone(raw)
	*entry = desktopTabEntry(next)
	return nil
}

// Bind the original record while restoring, before normalization can change
// its ID or workspace. This also handles empty and duplicate legacy IDs.
func (a *App) trackDesktopModelTabRestore(entry desktopTabEntry, id string) {
	snapshot := a.modelMigrationSnapshot
	if snapshot == nil || entry.modelMigrationRecord == nil {
		return
	}
	if snapshot.restoredTabs == nil {
		snapshot.restoredTabs = make(map[string]json.RawMessage)
	}
	snapshot.restoredTabs[id] = entry.modelMigrationRecord
}

func migrateDeepSeekV41Tabs(f *desktopTabsFile, startup *config.Config) error {
	return migrateDeepSeekV41TabsWithLoader(f, startup, config.LoadForRoot)
}

func migrateDeepSeekV41TabsWithLoader(f *desktopTabsFile, startup *config.Config, load func(string) (*config.Config, error)) error {
	desktopModelStateMu.Lock()
	defer desktopModelStateMu.Unlock()
	path := filepath.Join(desktopConfigDir(), tabsFileName)
	raw, err := readDesktopModelState(path)
	if err != nil {
		return err
	}
	doc, err := desktopStateObject(raw)
	if err != nil {
		return err
	}
	var next desktopTabsFile
	if raw != nil {
		if err := json.Unmarshal(raw, &next); err != nil {
			return err
		}
	}
	// Both the typed state and editable records come from these same bytes.
	// The caller's earlier load is never a source for model values or tab order.
	if next.DeepSeekModelVersion >= 41 && len(next.DeepSeekPendingModelRoots) == 0 {
		next.modelMigrationSnapshot = &desktopModelSnapshot{raw: raw}
		*f = next
		return nil
	}
	pending := make(map[string]bool)
	for _, root := range next.DeepSeekPendingModelRoots {
		pending[normalizeProjectRoot(root)] = true
	}
	var tabs []json.RawMessage
	if value, ok := doc["tabs"]; ok {
		if err := json.Unmarshal(value, &tabs); err != nil {
			return err
		}
	}
	projectErrors := make(map[string]string)
	configs := make(map[string]*config.Config)
	for i, rawTab := range tabs {
		fields, err := desktopStateObject(rawTab)
		if err != nil {
			return err
		}
		var entry desktopTabEntry
		if err := json.Unmarshal(rawTab, &entry); err != nil {
			return err
		}
		root := normalizeProjectRoot(entry.WorkspaceRoot)
		if next.DeepSeekModelVersion >= 41 && !pending[root] {
			continue
		}
		cfg := startup
		if root != "" {
			if _, failed := projectErrors[root]; failed {
				continue
			}
			var ok bool
			if cfg, ok = configs[root]; !ok {
				cfg, err = load(root)
				if err != nil {
					projectErrors[root] = fmt.Sprintf("Project model upgrade failed; check configuration in %q and restart: %v", root, err)
					continue
				}
				configs[root] = cfg
			}
		}
		if model := cfg.UpgradeDeepSeekV41Ref(entry.Model); model != entry.Model {
			fields["model"], _ = json.Marshal(model)
			tabs[i], _ = json.Marshal(fields)
		}
	}
	if next.DeepSeekModelVersion < 41 {
		prefs, err := desktopStateObject(doc["recentConversationPrefs"])
		if err != nil {
			return err
		}
		if model := startup.UpgradeDeepSeekV41Ref(next.RecentConversationPrefs.Model); model != next.RecentConversationPrefs.Model {
			prefs["model"], _ = json.Marshal(model)
			doc["recentConversationPrefs"], _ = json.Marshal(prefs)
		}
	}
	doc["tabs"], _ = json.Marshal(tabs)
	doc["deepseekModelVersion"] = json.RawMessage("41")
	roots := pendingDesktopModelRoots(projectErrors)
	delete(doc, "deepseekPendingModelRoots")
	if len(roots) > 0 {
		doc["deepseekPendingModelRoots"], _ = json.Marshal(roots)
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	next = desktopTabsFile{}
	if err := json.Unmarshal(body, &next); err != nil {
		return err
	}
	// A blocked backup or concurrent edit is fatal to this file, not a partial
	// in-memory success. Project failures are persisted separately for retry.
	if raw != nil {
		backup := path + ".pre-v41.bak"
		if info, err := os.Stat(backup); os.IsNotExist(err) {
			if err := atomicDesktopModelState(backup, raw); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("conversation backup is not a regular file")
		}
	}
	if err := replaceDesktopModelSnapshot(path, raw, body); err != nil {
		return err
	}
	next.modelMigrationSnapshot = &desktopModelSnapshot{raw: body}
	next.modelMigrationProjects = projectErrors
	*f = next
	return nil
}

func (a *App) stopDesktopModelStartup(err error) {
	a.mu.Lock()
	a.modelMigrationErr = "Global configuration could not be loaded. Check configuration and restart: " + err.Error()
	a.mu.Unlock()
	a.emitReady(a.ctx)
}

func (a *App) migrateDesktopModels(f *desktopTabsFile, cfg *config.Config) {
	if err := migrateDeepSeekV41Tabs(f, cfg); err != nil {
		a.modelMigrationErr = desktopModelStateError(err)
		return
	}
	a.modelMigrationErr = ""
	a.modelMigrationProjects = f.modelMigrationProjects
	a.modelMigrationSnapshot = f.modelMigrationSnapshot
}

func (a *App) desktopModelError(tab *WorkspaceTab) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.modelMigrationErr != "" || tab == nil {
		return a.modelMigrationErr
	}
	return a.modelMigrationProjects[normalizeProjectRoot(tab.WorkspaceRoot)]
}

func desktopModelStateError(err error) string {
	return "Conversation settings could not be upgraded or saved. Check file access, then restart: " + err.Error()
}

func pendingDesktopModelRoots(projects map[string]string) []string {
	var roots []string
	for root := range projects {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots
}

// Saves after migration retain unknown fields by stable tab ID. Tabs whose
// project could not be loaded keep their full original record for retry.
func (a *App) saveMigratedDesktopTabs(path string, next desktopTabsFile) error {
	raw := a.modelMigrationSnapshot.raw
	doc, err := mergeDesktopStateObject(raw, next)
	if err != nil {
		return err
	}
	original, err := desktopStateObject(raw)
	if err != nil {
		return err
	}
	var oldTabs []json.RawMessage
	if value, ok := original["tabs"]; ok {
		if err := json.Unmarshal(value, &oldTabs); err != nil {
			return err
		}
	}
	byID := make(map[string]json.RawMessage, len(oldTabs))
	for _, rawTab := range oldTabs {
		var tab desktopTabEntry
		if err := json.Unmarshal(rawTab, &tab); err != nil {
			return err
		}
		byID[tab.ID] = rawTab
	}
	for id, record := range a.modelMigrationSnapshot.restoredTabs {
		byID[id] = record
	}
	var tabs []json.RawMessage
	for _, tab := range next.Tabs {
		previous := byID[tab.ID]
		if previous != nil && a.modelMigrationProjects[normalizeProjectRoot(tab.WorkspaceRoot)] != "" {
			fields, err := desktopStateObject(previous)
			if err != nil {
				return err
			}
			// Keep a repaired ID so the next restart finds the same record.
			fields["id"], _ = json.Marshal(tab.ID)
			encoded, _ := json.Marshal(fields)
			tabs = append(tabs, encoded)
			continue
		}
		fields, err := mergeDesktopStateObject(previous, tab)
		if err != nil {
			return err
		}
		encoded, _ := json.Marshal(fields)
		tabs = append(tabs, encoded)
	}
	doc["tabs"], _ = json.Marshal(tabs)
	prefs, err := mergeDesktopStateObject(original["recentConversationPrefs"], next.RecentConversationPrefs)
	if err != nil {
		return err
	}
	doc["recentConversationPrefs"], _ = json.Marshal(prefs)
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := replaceDesktopModelSnapshot(path, raw, body); err != nil {
		return err
	}
	a.modelMigrationSnapshot = &desktopModelSnapshot{raw: body}
	return nil
}

func desktopStateObject(raw []byte) (map[string]json.RawMessage, error) {
	doc := make(map[string]json.RawMessage)
	if raw != nil {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
		if doc == nil {
			return nil, fmt.Errorf("conversation settings must contain an object")
		}
	}
	return doc, nil
}

func mergeDesktopStateObject(raw []byte, next any) (map[string]json.RawMessage, error) {
	doc, err := desktopStateObject(raw)
	if err != nil {
		return nil, err
	}
	// Clear omitted known fields too, so clearing an effort or pending list
	// does not resurrect the old value. Unknown fields remain untouched.
	typ := reflect.TypeOf(next)
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			delete(doc, name)
		}
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(encoded, &doc)
	return doc, err
}

func readDesktopModelState(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return raw, err
}

func replaceDesktopModelSnapshot(path string, expected, body []byte) error {
	return writeDesktopModelState(path, body, func() error {
		current, err := readDesktopModelState(path)
		if err != nil {
			return err
		}
		if (current == nil) != (expected == nil) || !bytes.Equal(current, expected) {
			return fmt.Errorf("conversation settings changed during model upgrade or save")
		}
		return nil
	})
}

func atomicDesktopModelState(path string, body []byte) error {
	return writeDesktopModelState(path, body, nil)
}

func writeDesktopModelState(path string, body []byte, validate func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".model-upgrade-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if validate != nil {
		if err := validate(); err != nil {
			return err
		}
	}
	// A failed rename leaves the old file intact; never truncate it as fallback.
	return os.Rename(f.Name(), path)
}
