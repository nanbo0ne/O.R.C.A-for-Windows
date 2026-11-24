package memory

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

const assistantProfileMigrationMarker = ".legacy-assistant-memory-imported-v1"

// CanonicalAssistantStore is the shared assistant identity used by automation
// conversations and remote channels.
func CanonicalAssistantStore(userDir string) Store {
	if strings.TrimSpace(userDir) == "" {
		return Store{}
	}
	return Store{Dir: filepath.Join(userDir, "assistant-profile", "memory")}
}

// EnsureCanonicalAssistantStore imports legacy per-workspace assistant stores
// once. Legacy files remain untouched so older product shells stay compatible.
func EnsureCanonicalAssistantStore(userDir string) (Store, error) {
	dest := CanonicalAssistantStore(userDir)
	if err := config.ValidateProfileDataPath(dest.Dir); err != nil {
		return Store{}, err
	}
	if config.IsolatedProfileEnabled() {
		return dest, nil
	}
	if dest.Dir == "" {
		return dest, nil
	}
	marker := filepath.Join(filepath.Dir(dest.Dir), assistantProfileMigrationMarker)
	if _, err := os.Stat(marker); err == nil {
		return dest, nil
	}

	legacyRoot := filepath.Join(userDir, "projects")
	projects, err := os.ReadDir(legacyRoot)
	if err != nil && !os.IsNotExist(err) {
		return dest, err
	}
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		legacy := Store{Dir: filepath.Join(legacyRoot, project.Name(), "assistant-memory")}
		for _, candidate := range legacy.List() {
			existing := findEquivalentMemory(dest, candidate)
			if existing.Name != "" && !preferMemory(candidate, existing) {
				continue
			}
			if existing.Name != "" {
				candidate.Name = existing.Name
			}
			if _, err := dest.Save(candidate); err != nil {
				return dest, err
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return dest, err
	}
	if err := os.WriteFile(marker, []byte("imported\n"), 0o644); err != nil {
		return dest, err
	}
	return dest, nil
}

func findEquivalentMemory(store Store, candidate Memory) Memory {
	name := strings.TrimSpace(candidate.Name)
	title := strings.TrimSpace(candidate.Title)
	description := strings.TrimSpace(candidate.Description)
	for _, existing := range store.List() {
		if name != "" && strings.EqualFold(existing.Name, name) {
			return existing
		}
		if title != "" && strings.EqualFold(strings.TrimSpace(existing.Title), title) {
			return existing
		}
		if description != "" && strings.EqualFold(strings.TrimSpace(existing.Description), description) {
			return existing
		}
	}
	return Memory{}
}

func preferMemory(candidate, existing Memory) bool {
	candidateTime := firstMemoryTime(candidate.UpdatedAt, candidate.LastEvidenceAt, candidate.CreatedAt)
	existingTime := firstMemoryTime(existing.UpdatedAt, existing.LastEvidenceAt, existing.CreatedAt)
	if candidateTime != existingTime {
		return candidateTime > existingTime
	}
	return candidate.Confidence > existing.Confidence
}

func firstMemoryTime(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
