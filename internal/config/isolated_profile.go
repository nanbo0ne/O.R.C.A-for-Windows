package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
)

var isolatedProfileHome atomic.Pointer[string]
var protectedProfileRoots atomic.Pointer[[]string]

// SetIsolatedProfileHome must run before loading config or starting workers.
// It changes only ORCA-owned discovery, never the process HOME/USERPROFILE.
// An empty path restores normal discovery (primarily useful to isolated tests).
func SetIsolatedProfileHome(home string) error {
	if home == "" {
		isolatedProfileHome.Store(nil)
		protectedProfileRoots.Store(nil)
		return nil
	}
	if !filepath.IsAbs(home) {
		return errors.New("isolated profile home must be absolute")
	}
	home = filepath.Clean(home)
	var protected []string
	if base, err := os.UserConfigDir(); err == nil {
		protected = append(protected, filepath.Join(base, "orca"), filepath.Join(base, "deepseek-orca"), filepath.Join(base, "O.R.C.A"))
	}
	if base, err := os.UserHomeDir(); err == nil {
		protected = append(protected, filepath.Join(base, ".orca"), filepath.Join(base, ".deepseek-orca"), filepath.Join(base, ".env"))
	}
	protectedProfileRoots.Store(&protected)
	isolatedProfileHome.Store(&home)
	return nil
}

func IsolatedProfileEnabled() bool { return isolatedProfileHome.Load() != nil }

// ProfileHomeDir is for app-owned configuration/credentials discovery only.
// Shells, file pickers, and explicit user paths must keep using os.UserHomeDir.
func ProfileHomeDir() (string, error) {
	if home := isolatedProfileHome.Load(); home != nil {
		return *home, nil
	}
	return os.UserHomeDir()
}

func ProfileConfigDir() (string, error) {
	if home := isolatedProfileHome.Load(); home != nil {
		return *home, nil
	}
	return os.UserConfigDir()
}

// ProjectStatePath keeps beta-owned state out of an opened project's .orca.
func ProjectStatePath(root string, parts ...string) string {
	base := filepath.Join(root, ".orca")
	if home := isolatedProfileHome.Load(); home != nil {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return ""
		}
		if runtime.GOOS == "windows" {
			absolute = strings.ToLower(absolute)
		}
		base = filepath.Join(*home, "orca", "projects", fmt.Sprintf("%x", sha256.Sum256([]byte(filepath.Clean(absolute)))), "state")
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

func ProjectConfigPath(root string) string {
	if IsolatedProfileEnabled() {
		return ProjectStatePath(root, "config.toml")
	}
	return filepath.Join(root, "orca.toml")
}

func projectConfigSources(root string) []string {
	if IsolatedProfileEnabled() {
		return []string{ProjectConfigPath(root)}
	}
	return []string{filepath.Join(root, "deepseek-orca.toml"), ProjectConfigPath(root)}
}

// ValidateProfileDataPath is a fail-closed boundary for config, credentials,
// transcripts, and memory. Resolve existing prefixes to reject junction escapes
// even when a destination file has not been created yet.
func ValidateProfileDataPath(path string) error {
	home := isolatedProfileHome.Load()
	if home == nil {
		return nil
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("empty isolated profile data path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !withinProfile(*home, abs) {
		return errors.New("data path is outside the isolated profile")
	}
	for prefix := abs; ; prefix = filepath.Dir(prefix) {
		if _, err := os.Lstat(prefix); err == nil {
			resolved, err := filepath.EvalSymlinks(prefix)
			if err != nil {
				return fmt.Errorf("resolve isolated profile path: %w", err)
			}
			if withinProfile(*home, prefix) && !withinProfile(*home, resolved) {
				return errors.New("isolated profile path resolves outside its root")
			}
			if !withinProfile(*home, prefix) {
				equal := filepath.Clean(prefix) == filepath.Clean(resolved)
				if runtime.GOOS == "windows" {
					equal = strings.EqualFold(filepath.Clean(prefix), filepath.Clean(resolved))
				}
				if !equal {
					return errors.New("isolated profile ancestor resolves outside its lexical path")
				}
			}
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		if filepath.Dir(prefix) == prefix {
			return errors.New("cannot resolve isolated profile path")
		}
	}
}

func withinProfile(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ProfileEnvironmentKey permits platform/tool locations, not inherited model
// credentials. Explicit beta credentials are supplied by the scoped store.
func ProfileEnvironmentKey(key string) bool {
	if !IsolatedProfileEnabled() {
		return true
	}
	switch strings.ToUpper(key) {
	case "PATH", "PATHEXT", "COMSPEC", "SYSTEMROOT", "WINDIR", "SYSTEMDRIVE", "OS",
		"HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "USERNAME", "USER", "LOGNAME",
		"APPDATA", "LOCALAPPDATA", "PROGRAMDATA", "PROGRAMFILES", "PROGRAMFILES(X86)", "PROGRAMW6432",
		"TEMP", "TMP", "TMPDIR", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR",
		"LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "TERM", "COLORTERM", "NUMBER_OF_PROCESSORS",
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR",
		"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "KUBECONFIG", "AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE",
		"SSH_AUTH_SOCK", "SSH_AGENT_PID", "GOPATH", "GOROOT", "GOCACHE", "GOTMPDIR", "GOMODCACHE":
		return true
	}
	return false
}

func ProfileLookupEnv(key string) (string, bool) {
	if !ProfileEnvironmentKey(key) {
		return "", false
	}
	return os.LookupEnv(key)
}

// ProjectConventionPaths preserves tooling code conventions but replaces the
// official application's project state roots in isolated builds.
func ProjectConventionPaths(root, subdir string) []string {
	if !IsolatedProfileEnabled() {
		return conventionSubdirsAsc(root, subdir)
	}
	return []string{filepath.Join(root, ".claude", subdir), filepath.Join(root, ".agent", subdir), filepath.Join(root, ".agents", subdir), ProjectStatePath(root, subdir)}
}

// ProfileDiscoveryAllowed permits project tooling code and explicit external
// tool roots, but never official ORCA state or dotenv secrets, even via links.
func ProfileDiscoveryAllowed(path string) bool {
	if !IsolatedProfileEnabled() {
		return true
	}
	if ValidateProfileDataPath(path) == nil {
		return true
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	} else if !os.IsNotExist(err) {
		return false
	}
	if roots := protectedProfileRoots.Load(); roots != nil {
		for _, root := range *roots {
			if withinProfile(root, abs) {
				return false
			}
		}
	}
	parts := strings.FieldsFunc(strings.ToLower(filepath.ToSlash(abs)), func(r rune) bool { return r == '/' })
	for _, part := range parts {
		if part == ".orca" || part == ".deepseek-orca" {
			return false
		}
	}
	name := strings.ToLower(filepath.Base(abs))
	return name != "orca.toml" && name != "deepseek-orca.toml" && name != "orca.md" && name != "deepseek_orca.md" && name != "orca.local.md" && name != "deepseek_orca.local.md" && name != ".env" && !strings.HasPrefix(name, ".env.")
}
