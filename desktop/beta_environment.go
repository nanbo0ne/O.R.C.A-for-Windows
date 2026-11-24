package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

func inheritedBetaCredentialKey(key string) bool {
	key = strings.ToUpper(key)
	// Keep explicit external tool config locations; do not read or rewrite them.
	if config.ProfileEnvironmentKey(key) || strings.HasSuffix(key, "_FILE") || strings.HasSuffix(key, "_PATH") {
		return false
	}
	return strings.Contains(key, "API_KEY") || strings.Contains(key, "APIKEY") || strings.Contains(key, "SECRET") || strings.Contains(key, "TOKEN") || strings.Contains(key, "PASSWORD") || strings.Contains(key, "CREDENTIAL") || strings.HasSuffix(key, "_AUTH")
}

func isolateBetaEnvironment() error {
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && inheritedBetaCredentialKey(key) {
			if err := os.Unsetenv(key); err != nil {
				return fmt.Errorf("remove inherited beta credential variable %s: %w", key, err)
			}
		}
	}
	var paths []string
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		entry = strings.TrimSpace(entry)
		if !betaRuntimePathAllowed(entry) {
			continue
		}
		paths = append(paths, entry)
	}
	if err := os.Setenv("PATH", strings.Join(paths, string(os.PathListSeparator))); err != nil {
		return fmt.Errorf("set beta environment PATH: %w", err)
	}
	return nil
}

func betaRuntimePathAllowed(entry string) bool {
	if entry == "" || !filepath.IsAbs(entry) || !config.ProfileDiscoveryAllowed(entry) {
		return false
	}
	// Do not borrow runtime sidecars from the official installed application.
	for _, name := range []string{"Orca.exe", "deepseek-orca-desktop.exe"} {
		if _, err := os.Stat(filepath.Join(entry, name)); err == nil || !os.IsNotExist(err) {
			return false
		}
	}
	return true
}
