package config

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/billing"
)

const OfficialDeepSeekFlashModel = "deepseek-flash"

var deepSeekMigrationMu sync.Mutex

// IsDeepSeekV41OfficialProvider deliberately excludes custom relay identities.
func IsDeepSeekV41OfficialProvider(p *ProviderEntry) bool {
	if p == nil || (p.Kind != "" && p.Kind != "openai") {
		return false
	}
	return billing.OfficialDeepSeekModel(p.Name, p.BaseURL, OfficialDeepSeekFlashModel) != ""
}

func canonicalDeepSeekFlash(model string) string {
	switch model {
	case "deepseek-v4-flash", "deepseek-v4-flash-vision-exp":
		return OfficialDeepSeekFlashModel
	default:
		return model
	}
}

func isMigratableDeepSeekModel(model string) bool {
	switch model {
	case "deepseek-flash", "deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp", "deepseek-chat", "deepseek-reasoner":
		return true
	default:
		return false
	}
}

// UpgradeDeepSeekV41Ref is used only by one-time configuration/tab migrations.
func (c *Config) UpgradeDeepSeekV41Ref(ref string) string {
	if c == nil || strings.TrimSpace(ref) == "" {
		return ref
	}
	name, model, qualified := strings.Cut(strings.TrimSpace(ref), "/")
	var entry *ProviderEntry
	if p, ok := c.Provider(name); ok {
		entry = p
		if !qualified {
			model = p.DefaultModel()
			if model == "" {
				model = legacyOfficialProviderModel(p.Name)
			}
		}
	} else if !qualified {
		for i := range c.Providers {
			if c.Providers[i].HasModel(ref) {
				entry = &c.Providers[i]
				model = ref
				break
			}
		}
	}
	if !IsDeepSeekV41OfficialProvider(entry) || !isMigratableDeepSeekModel(model) {
		return ref
	}
	// Keep the provider identity: a project may override this name with its
	// own endpoint or credentials even when today's global entries match.
	return entry.Name + "/" + OfficialDeepSeekFlashModel
}

func normalizeDeepSeekV41Catalog(c *Config) {
	if _, ok := c.Provider("deepseek"); !ok {
		for _, name := range []string{"deepseek-flash", "deepseek-pro"} {
			if old, ok := c.Provider(name); ok && IsDeepSeekV41OfficialProvider(old) {
				entry := *old
				entry.Name = "deepseek"
				entry.Models = mergeModelLists([]string{OfficialDeepSeekFlashModel, "deepseek-v4-pro"}, old.ModelList())
				entry.Model = canonicalDeepSeekFlash(old.DefaultModel())
				entry.Default = entry.Model
				c.Providers = append(c.Providers, entry)
				break
			}
		}
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if !IsDeepSeekV41OfficialProvider(p) {
			continue
		}
		p.Model = canonicalDeepSeekFlash(p.Model)
		p.Default = canonicalDeepSeekFlash(p.Default)
		var models []string
		for _, model := range p.Models {
			models = mergeModelLists(models, []string{canonicalDeepSeekFlash(model)})
		}
		if len(p.Models) > 0 {
			p.Models = models
		}
		if p.Name == "deepseek" {
			p.Models = mergeModelLists(p.ModelList(), []string{OfficialDeepSeekFlashModel, "deepseek-v4-pro"})
		}
	}
}

// HiddenDeepSeekCompatibilityEntry hides only duplicates sharing credentials.
func (c *Config) HiddenDeepSeekCompatibilityEntry(p *ProviderEntry) bool {
	if !IsDeepSeekV41OfficialProvider(p) || p.Name == "deepseek" {
		return false
	}
	canonical, ok := c.Provider("deepseek")
	return ok && IsDeepSeekV41OfficialProvider(canonical) && canonical.APIKeyEnv == p.APIKeyEnv && canonical.BaseURL == p.BaseURL
}

func (c *Config) retargetOfficialRef(ref string, access map[string]bool) string {
	name, _, _ := strings.Cut(ref, "/")
	if name == "deepseek-flash" || name == "deepseek-pro" {
		if entry, ok := c.Provider(name); ok && !c.HiddenDeepSeekCompatibilityEntry(entry) {
			return ref
		}
	}
	return retargetDesktopOfficialRef(ref, access)
}

// Each source is decoded independently. Unknown settings survive, inherited
// settings are never copied into projects, and the original is retained.
func migrateDeepSeekV41File(path string, inherited *Config) error {
	deepSeekMigrationMu.Lock()
	defer deepSeekMigrationMu.Unlock()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var doc map[string]any
	if _, err = toml.Decode(string(raw), &doc); err != nil {
		return err
	}
	version, _ := doc["config_version"].(int64)
	if version >= 12 || doc["deepseek_v41_migrated"] == true {
		return nil
	}
	// Completed sources are read-only. Re-read after taking the write lock so
	// a concurrent upgrade or settings save is never replaced by stale bytes.
	lock, err := openMigrationLock(path + ".v41.lock")
	if err != nil {
		return err
	}
	defer releaseMigrationLock(lock)
	raw, err = os.ReadFile(path)
	if err != nil {
		return err
	}
	doc = nil
	if _, err = toml.Decode(string(raw), &doc); err != nil {
		return err
	}
	version, _ = doc["config_version"].(int64)
	if version >= 12 || doc["deepseek_v41_migrated"] == true {
		return nil
	}
	identity := *inherited
	var source Config
	if _, err = toml.Decode(string(raw), &source); err != nil {
		return err
	}
	identity.Providers = providerCatalogForSource(inherited, source.Providers)
	update := func(table map[string]any, key string) {
		if ref, ok := table[key].(string); ok {
			if next := identity.UpgradeDeepSeekV41Ref(ref); next != ref {
				table[key] = next
			}
		}
	}
	update(doc, "default_model")
	for section, keys := range map[string][]string{
		"agent": {"planner_model", "subagent_model", "auto_plan_classifier"},
		"bot":   {"model"}, "permissions": {"auto_review_model"}, "desktop": {"computer_control_model"},
	} {
		if table, ok := doc[section].(map[string]any); ok {
			for _, key := range keys {
				update(table, key)
			}
		}
	}
	if agent, ok := doc["agent"].(map[string]any); ok {
		if roles, ok := agent["subagent_models"].(map[string]any); ok {
			for role := range roles {
				update(roles, role)
			}
		}
	}
	if providers, ok := doc["providers"].([]map[string]any); ok {
		for i, p := range providers {
			if i >= len(source.Providers) || !IsDeepSeekV41OfficialProvider(&source.Providers[i]) {
				continue
			}
			for _, key := range []string{"model", "default"} {
				if model, ok := p[key].(string); ok && isMigratableDeepSeekModel(model) && model != OfficialDeepSeekFlashModel {
					p[key] = OfficialDeepSeekFlashModel
				}
			}
			if list, ok := p["models"].([]any); ok {
				var models []string
				for _, value := range list {
					if model, ok := value.(string); ok {
						models = mergeModelLists(models, []string{canonicalDeepSeekFlash(model)})
					}
				}
				models = mergeModelLists([]string{OfficialDeepSeekFlashModel, "deepseek-v4-pro"}, models)
				p["models"] = models
			}
		}
	}
	// Older schema migrations still need their original version on the next
	// read; their next ordinary save writes V12 and removes this interim marker.
	if version >= 11 {
		doc["config_version"] = int64(12)
	} else {
		doc["deepseek_v41_migrated"] = true
	}
	var out bytes.Buffer
	if err = toml.NewEncoder(&out).Encode(doc); err != nil {
		return err
	}
	var verified Config
	if _, err = toml.Decode(out.String(), &verified); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	backup := path + ".pre-v41.bak"
	if backupInfo, backupErr := os.Stat(backup); os.IsNotExist(backupErr) {
		if err = writeDeepSeekV41File(backup, raw, info.Mode().Perm()); err != nil {
			return fmt.Errorf("backup model settings: %w", err)
		}
	} else if backupErr != nil {
		return backupErr
	} else if !backupInfo.Mode().IsRegular() {
		return fmt.Errorf("model settings backup is not a regular file")
	}
	latest, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(latest, raw) {
		return fmt.Errorf("model settings changed during migration: %s", path)
	}
	return writeDeepSeekV41File(path, out.Bytes(), info.Mode().Perm())
}

// Unlike the legacy writer's copy fallback, a failed replacement here must
// leave the source intact. Startup can then report and retry the migration.
func writeDeepSeekV41File(path string, body []byte, mode fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".deepseek-v41-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err = tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
