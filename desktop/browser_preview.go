package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	browserPreviewChannel     = "browser-preview"
	browserPreviewHomeEnv     = "ORCA_BROWSER_PREVIEW_HOME"
	browserPreviewWindowClass = "OrcaBrowserPreviewWindow"
	browserPreviewTitle       = "O.R.C.A. Browser Preview"

	// stableWindowClass is the class Wails registers its main window under when
	// Windows.WindowClassName is empty. The browser host and the web-beta host
	// locate the app window by class, so the official channel must report the
	// real class instead of an empty string; an empty lookup never finds the
	// window and every tab fails with "browser host window not ready".
	stableWindowClass = "wailsWindow"
)

// browserPreviewEnabled gates the isolated preview identity: its own window
// class, single-instance lock, title and profile root. It must stay false on
// the official channel, which keeps the public window class and update path.
func browserPreviewEnabled() bool { return channel == browserPreviewChannel || betaEnabled() }

// browserControlEnabled gates the browser feature itself. Browser control has
// shipped to the official channel, so the capability is no longer tied to the
// preview or beta identity. Every feature gate must use this predicate, never
// the identity predicate above, or a stable build loses the panel and tools.
func browserControlEnabled() bool { return channel != "" }

// isolateBrowserPreview must run before constructing App or consulting config.
func isolateBrowserPreview() error {
	if channel != browserPreviewChannel {
		return nil
	}
	root := strings.TrimSpace(os.Getenv(browserPreviewHomeEnv))
	if root != "" {
		if !filepath.IsAbs(root) {
			return errors.New("ORCA_BROWSER_PREVIEW_HOME must be an absolute path")
		}
	} else {
		base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
		if base == "" {
			base = strings.TrimSpace(os.Getenv("LocalAppData"))
		}
		if base == "" {
			base = strings.TrimSpace(os.Getenv("HOME"))
			if base == "" {
				base = strings.TrimSpace(os.Getenv("USERPROFILE"))
			}
			if base == "" {
				return errors.New("cannot determine original LocalAppData for browser preview")
			}
			base = filepath.Join(base, "AppData", "Local")
		}
		root = filepath.Join(base, "O.R.C.A Browser Preview")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	workspace := filepath.Join(root, "workspace")
	for _, dir := range []string{root, workspace, filepath.Join(root, "AppData", "Roaming"), filepath.Join(root, ".config")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	for key, value := range map[string]string{
		"APPDATA":         filepath.Join(root, "AppData", "Roaming"),
		"LOCALAPPDATA":    filepath.Join(root, "AppData", "Local"),
		"HOME":            root,
		"USERPROFILE":     root,
		"XDG_CONFIG_HOME": filepath.Join(root, ".config"),
	} {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return os.Chdir(workspace)
}

func desktopTitle(goos string) string {
	if betaEnabled() {
		return betaName
	}
	if browserPreviewEnabled() {
		return browserPreviewTitle
	}
	return desktopWindowTitle(goos)
}

func browserPreviewWebviewDataPath() string {
	if betaEnabled() {
		root, err := betaDataRoot()
		if err != nil {
			return ""
		}
		return filepath.Join(root, "Local", "WebView2")
	}
	if !browserPreviewEnabled() {
		return ""
	}
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "WebView2")
}

func browserWindowClass() string {
	if betaEnabled() {
		return betaWindowClass
	}
	if browserPreviewEnabled() {
		return browserPreviewWindowClass
	}
	return stableWindowClass
}
