package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

const (
	betaChannel     = "beta"
	betaName        = "ORCA beta"
	betaVersion     = "3.0.14-beta.2"
	betaWindowClass = "OrcaBetaWindow"
	betaExecutable  = "OrcaBeta.exe"
)

var initializedBetaRoot string

func betaEnabled() bool { return channel == betaChannel }

// betaDataRoot is valid before initialization too. No override can accidentally
// point beta at the official profile. initializeBeta freezes the original root.
func betaDataRoot() (string, error) {
	if !betaEnabled() {
		return "", errors.New("beta profile is unavailable outside the beta channel")
	}
	if initializedBetaRoot != "" {
		return initializedBetaRoot, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(base) {
		return "", errors.New("beta requires an absolute user config directory")
	}
	return filepath.Join(base, betaName), nil
}

// initializeBeta must precede NewApp, config reads, and window preferences.
// The profile variables are process-local; HOME/USERPROFILE stay untouched so
// tools and user-selected paths still resolve the user's real home directory.
func initializeBeta() error {
	if !betaEnabled() || initializedBetaRoot != "" {
		return nil
	}
	if runtime.GOOS == "darwin" {
		return errors.New("ORCA beta profile isolation is not available on macOS")
	}
	root, err := betaDataRoot()
	if err != nil {
		return fmt.Errorf("resolve beta data root: %w", err)
	}
	for _, dir := range []string{root, filepath.Join(root, "orca"), filepath.Join(root, "Local"), filepath.Join(root, "Cache"), filepath.Join(root, "Data"), filepath.Join(root, "workspace")} {
		if err := validateBetaDirectory(dir); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create beta directory %q: %w", dir, err)
		}
		if err := validateBetaDirectory(dir); err != nil {
			return err
		}
	}
	if err := config.SetIsolatedProfileHome(root); err != nil {
		return fmt.Errorf("set isolated beta profile: %w", err)
	}
	if err := isolateBetaEnvironment(); err != nil {
		return fmt.Errorf("isolate beta environment: %w", err)
	}
	for key, value := range map[string]string{
		"APPDATA":         root,
		"LOCALAPPDATA":    filepath.Join(root, "Local"),
		"XDG_CONFIG_HOME": root,
		"XDG_CACHE_HOME":  filepath.Join(root, "Cache"),
		"XDG_DATA_HOME":   filepath.Join(root, "Data"),
	} {
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set beta environment %s: %w", key, err)
		}
	}
	if err := os.Chdir(filepath.Join(root, "workspace")); err != nil {
		return fmt.Errorf("enter beta workspace %q: %w", filepath.Join(root, "workspace"), err)
	}
	initializedBetaRoot = root
	return nil
}

// Check an existing ancestor before MkdirAll, so a redirected parent cannot
// cause even the initial beta directory creation to write into another profile.
func validateBetaDirectory(dir string) error {
	for prefix := dir; ; prefix = filepath.Dir(prefix) {
		if _, err := os.Lstat(prefix); err == nil {
			resolved, err := filepath.EvalSymlinks(prefix)
			if err != nil {
				return fmt.Errorf("resolve beta directory %q (existing prefix %q): %w", dir, prefix, err)
			}
			equal := filepath.Clean(resolved) == filepath.Clean(prefix)
			if runtime.GOOS == "windows" {
				equal = strings.EqualFold(filepath.Clean(resolved), filepath.Clean(prefix))
			}
			if !equal {
				return fmt.Errorf("beta directory %q has redirected prefix %q: %q", dir, prefix, resolved)
			}
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect beta directory %q (existing prefix %q): %w", dir, prefix, err)
		}
		if filepath.Dir(prefix) == prefix {
			return fmt.Errorf("resolve beta directory %q: no existing ancestor", dir)
		}
	}
}

func disabledDesktopUpdateReason() string {
	if betaEnabled() {
		return "updates are disabled in ORCA beta until a beta-only signed feed is configured"
	}
	if channel == browserPreviewChannel {
		return "updates are disabled in browser preview"
	}
	return ""
}
