package browserhost

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

// ValidateManagedPath confines beta browser state without restricting ordinary
// user-selected files or changing the non-isolated browser host.
func ValidateManagedPath(path string) error {
	if !config.IsolatedProfileEnabled() {
		return nil
	}
	if !filepath.IsAbs(path) {
		return errors.New("browser state requires an absolute isolated path")
	}
	if err := config.ValidateProfileDataPath(path); err != nil {
		return err
	}
	return validateUnredirectedPath(path)
}

func validateUnredirectedPath(path string) error {
	for prefix := filepath.Clean(path); ; prefix = filepath.Dir(prefix) {
		if info, err := os.Lstat(prefix); err == nil {
			linked, err := browserPathLink(prefix, info)
			if err != nil {
				return err
			}
			if linked {
				return errors.New("browser path cannot traverse a symlink or reparse point")
			}
			resolved, err := filepath.EvalSymlinks(prefix)
			if err != nil {
				return err
			}
			equal := filepath.Clean(resolved) == prefix
			if runtime.GOOS == "windows" {
				equal = strings.EqualFold(filepath.Clean(resolved), prefix)
			}
			if !equal {
				return errors.New("browser state cannot traverse a symlink or junction")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if filepath.Dir(prefix) == prefix {
			return nil
		}
	}
}

// Reuse the isolated profile's official-state exclusions, not a beta-only
// workspace allowlist: explicitly selected ordinary external files remain valid.
func ValidateTransferPath(path string) error {
	if !config.IsolatedProfileEnabled() {
		return nil
	}
	if !config.ProfileDiscoveryAllowed(path) {
		return errors.New("browser transfer cannot access protected ORCA data or credentials")
	}
	if !filepath.IsAbs(path) {
		return errors.New("browser transfer requires an absolute path")
	}
	return validateUnredirectedPath(path)
}
