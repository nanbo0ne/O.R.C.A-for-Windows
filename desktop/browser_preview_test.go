package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBrowserPreviewIsolation(t *testing.T) {
	skipOnMacOS(t, "the browser host is Windows-only and macOS /var is a system symlink to /private/var")
	oldChannel := channel
	channel = browserPreviewChannel
	t.Cleanup(func() { channel = oldChannel })
	root := filepath.Join(t.TempDir(), "preview-home")
	t.Setenv(browserPreviewHomeEnv, root)
	t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "real-local-app-data"))
	t.Setenv("APPDATA", filepath.Join(t.TempDir(), "real-app-data"))
	t.Setenv("HOME", filepath.Join(t.TempDir(), "real-home"))
	t.Setenv("USERPROFILE", filepath.Join(t.TempDir(), "real-profile"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "real-config"))
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	if err := isolateBrowserPreview(); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	if got, _ := os.Getwd(); got != workspace {
		t.Fatalf("working directory = %q, want %q", got, workspace)
	}
	wantEnv := map[string]string{
		"APPDATA":         filepath.Join(root, "AppData", "Roaming"),
		"LOCALAPPDATA":    filepath.Join(root, "AppData", "Local"),
		"HOME":            root,
		"USERPROFILE":     root,
		"XDG_CONFIG_HOME": filepath.Join(root, ".config"),
	}
	for key, want := range wantEnv {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if got := browserPreviewWebviewDataPath(); got != filepath.Join(root, "AppData", "Local", "WebView2") {
		t.Errorf("Webview data path = %q", got)
	}
}

func TestBrowserPreviewRejectsRelativeOverride(t *testing.T) {
	oldChannel := channel
	channel = browserPreviewChannel
	t.Cleanup(func() { channel = oldChannel })
	t.Setenv(browserPreviewHomeEnv, "relative-preview")
	if err := isolateBrowserPreview(); err == nil {
		t.Fatal("expected relative preview home to be rejected")
	}
}

func TestBrowserPreviewUpdaterDisabledForManualCalls(t *testing.T) {
	oldChannel := channel
	channel = browserPreviewChannel
	t.Cleanup(func() { channel = oldChannel })
	a := &App{}

	info, err := a.CheckUpdate()
	if err != nil || info == nil || info.Available || info.Err == "" {
		t.Fatalf("CheckUpdate() = (%+v, %v), want disabled result", info, err)
	}
	for name, call := range map[string]func() error{
		"source":   func() error { return a.SetUpdateSource(updateSourceGitHub) },
		"download": a.DownloadUpdate,
		"cancel":   a.CancelUpdateDownload,
		"apply":    a.ApplyUpdate,
		"open":     a.OpenDownloadedUpdate,
	} {
		if err := call(); err == nil {
			t.Errorf("%s unexpectedly succeeded", name)
		}
	}
}

func TestBrowserPreviewIdentityOnlyOnPreviewChannel(t *testing.T) {
	oldChannel := channel
	t.Cleanup(func() { channel = oldChannel })
	channel = browserPreviewChannel
	if !browserPreviewEnabled() || desktopTitle("windows") != browserPreviewTitle || browserWindowClass() != browserPreviewWindowClass {
		t.Fatal("preview channel should enable preview identity")
	}
	previewLock := singleInstanceLock(&App{})
	if previewLock == nil || previewLock.UniqueId == singleInstanceID {
		t.Fatalf("preview lock identity = %#v, want distinct identity", previewLock)
	}
	channel = "stable"
	if browserPreviewEnabled() || desktopTitle("windows") != desktopWindowTitle("windows") || browserWindowClass() != "" {
		t.Fatal("stable channel should retain normal identity")
	}
	stableLock := singleInstanceLock(&App{})
	if stableLock == nil || stableLock.UniqueId != singleInstanceID {
		t.Fatalf("stable lock identity = %#v, want %q", stableLock, singleInstanceID)
	}
}

func TestBrowserControlShipsOnOfficialChannel(t *testing.T) {
	for _, tc := range []struct {
		channel string
		want    bool
	}{
		{"stable", true},
		{"canary", true},
		{browserPreviewChannel, true},
		{betaChannel, true},
		{"", false},
	} {
		old := channel
		channel = tc.channel
		got := browserControlEnabled()
		identity := browserPreviewEnabled()
		channel = old
		if got != tc.want {
			t.Fatalf("browserControlEnabled(%q) = %v, want %v", tc.channel, got, tc.want)
		}
		if tc.channel == "stable" && identity {
			t.Fatal("official channel must not adopt the preview identity")
		}
	}
}
