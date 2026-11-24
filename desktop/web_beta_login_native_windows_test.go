//go:build windows && production && webbeta_acceptance

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/webbetahost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	winopt "github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Run ONLY after the native-window owner releases the queue and the parent
// explicitly authorizes real-site login-page navigation. This is not full App,
// authenticated readiness, model/vision/tool acceptance, or a Wails cold-start
// benchmark. No production App bindings, engines, account clients or agents run.
func TestWebBetaNativeFreshLoginOnly(t *testing.T) {
	if os.Getenv("ORCA_WEB_BETA_LOGIN_ONLY_TEST") != "1" {
		t.Skip("requires explicit native queue approval and ORCA_WEB_BETA_LOGIN_ONLY_TEST=1")
	}
	root := os.Getenv("ORCA_WEB_BETA_LOGIN_ROOT")
	_, source, _, _ := runtime.Caller(0)
	allowed := filepath.Join(filepath.Dir(source), "..", ".tmp")
	if err := createWebBetaLoginRoot(allowed, root); err != nil {
		t.Fatal(err)
	}
	withBetaProfile(t)
	t.Setenv("APPDATA", root)
	// WebView2's process environment must not override our fresh profile or
	// attach remote debugging/proxy arguments inherited from another experiment.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "WEBVIEW2_") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("ORCA_WEB_BETA_ACCEPTANCE", "")
	t.Setenv("ORCA_WEB_BETA_PROBE", "")
	if err := initializeBeta(); err != nil {
		t.Fatal(err)
	}
	betaRoot, err := betaDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	profile, err := webbetahost.OpenProfile(betaRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(profile.BrowserPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refusing existing or inaccessible cookie profile")
	}
	if err := profile.Ensure(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	done := make(chan error, 1)
	var once sync.Once
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Deliberately blank shell: no production frontend, bindings, input injection,
	// screenshots, login entry, credentials, DOM values, or conversation reads.
	assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><html><body></body></html>")}}
	err = wails.Run(&options.App{
		Title: "ORCA beta login-only acceptance", Width: 1000, Height: 700, StartHidden: true,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup: func(wctx context.Context) {
			go func() { <-ctx.Done(); wruntime.Quit(wctx) }()
		},
		OnDomReady: func(wctx context.Context) {
			once.Do(func() {
				go func() {
					done <- exerciseWebBetaFreshLogin(ctx, root, profile)
					wruntime.Quit(wctx)
				}()
			})
		},
		OnShutdown: func(context.Context) { cancel() },
		Windows:    &winopt.Options{WindowClassName: betaWindowClass, WebviewUserDataPath: filepath.Join(root, "frontend-webview")},
	})
	if err != nil {
		t.Error(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(40 * time.Second):
		t.Fatal("native login worker did not complete/release; profile retained, do not reuse")
	}
	t.Logf("login-only structural evidence: %s; no authenticated readiness claim", filepath.Join(root, "login-only-result.json"))
}

func createWebBetaLoginRoot(allowed, root string) error {
	rel, err := filepath.Rel(allowed, root)
	if !filepath.IsAbs(root) || err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return errors.New("ORCA_WEB_BETA_LOGIN_ROOT must be a fresh absolute directory below this worktree's .tmp")
	}
	if err := validateBetaDirectory(root); err != nil {
		return err
	}
	// No adoption, copying, or cleanup of an existing root, even if it is empty.
	if err := os.Mkdir(root, 0700); err != nil {
		return fmt.Errorf("refusing existing/inaccessible login evidence root: %w", err)
	}
	return validateBetaDirectory(root)
}

func exerciseWebBetaFreshLogin(ctx context.Context, root string, profile *webbetahost.Profile) (result error) {
	b, err := webbetahost.NewLoginOnlyWithWindowClass(profile.BrowserPath(), betaWindowClass)
	if b != nil {
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			releaseErr := b.CloseAndWait(releaseCtx)
			result = errors.Join(result, releaseErr)
			if releaseErr == nil {
				// Clear only the newly created owned DeepSeek profile after release.
				result = errors.Join(result, profile.Clear())
			}
			if result == nil {
				raw, _ := json.MarshalIndent(map[string]any{
					"scope":       "fresh-cookie login-only; not authenticated readiness",
					"windowClass": betaWindowClass, "origin": webbeta.Origin,
					"loginControlsPresent": true, "presented": false,
					"runRefused": true, "adapterInstalled": false, "probesOrUploads": false,
					"profileReleasedAndCleared": true,
				}, "", "  ")
				result = os.WriteFile(filepath.Join(root, "login-only-result.json"), raw, 0600)
			}
		}()
	}
	if err != nil {
		return err
	}
	if err := b.Run(ctx, webbeta.Command{}, func(webbeta.Event) error { return errors.New("unexpected login automation event") }); !errors.Is(err, webbetahost.ErrLoginOnly) {
		return fmt.Errorf("login-only Run did not refuse: %v", err)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		state := b.State()
		if b.Presented() || state.Ready || state.Busy || state.Blocked != "" {
			return errors.New("login-only hidden-state invariant failed")
		}
		if state.URL != "" && !webbeta.AllowedSource(state.URL) {
			return errors.New("login-only escaped authorized origin")
		}
		raw, err := os.ReadFile(filepath.Join(filepath.Dir(profile.BrowserPath()), "login-health.json"))
		if err == nil {
			var health struct {
				Stage            string `json:"stage"`
				OfficialOrigin   bool   `json:"officialOrigin"`
				AutomaticSending bool   `json:"automaticSending"`
			}
			if json.Unmarshal(raw, &health) == nil && health.Stage == "login_controls_present" && health.OfficialOrigin && !health.AutomaticSending && webbeta.AllowedSource(state.URL) {
				return nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("login controls not observed (no authenticated readiness assertion): %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestWebBetaLoginRootRejectsReuseAndEscape(t *testing.T) {
	allowed := t.TempDir()
	root := filepath.Join(allowed, "new-login")
	if err := createWebBetaLoginRoot(allowed, root); err != nil {
		t.Fatal(err)
	}
	if err := createWebBetaLoginRoot(allowed, root); err == nil {
		t.Fatal("existing root accepted")
	}
	for _, path := range []string{allowed, filepath.Join(allowed, "..", "outside"), "relative"} {
		if err := createWebBetaLoginRoot(allowed, path); err == nil {
			t.Fatalf("unsafe root accepted: %s", path)
		}
	}
}
