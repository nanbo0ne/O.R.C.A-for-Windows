//go:build windows

package browserhost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// Opt-in adversarial native tests. All input stays in this test's hidden child
// WebView; there is no provider, user profile or system input backend.
func TestNativeAdversarialActionBoundaries(t *testing.T) {
	if os.Getenv("ORCA_BROWSER_NATIVE_TEST") != "1" {
		t.Skip("isolated native safety test is opt-in")
	}
	var trustedClicks atomic.Int64
	var frameLoads atomic.Int64
	hit := func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Trusted bool `json:"trusted"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(http.MaxBytesReader(w, r.Body, 128)).Decode(&v) != nil || !v.Trusted {
			http.Error(w, "invalid synthetic event", http.StatusBadRequest)
			return
		}
		trustedClicks.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}
	frame := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hit" {
			hit(w, r)
			return
		}
		frameLoads.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><style>body{margin:0}button{width:100vw;height:100vh}</style><button onclick="fetch('/hit',{method:'POST',body:JSON.stringify({trusted:event.isTrusted})})">unapproved frame</button>`)
	}))
	defer frame.Close()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hit" {
			hit(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><style>body{margin:0}.target{position:fixed;left:40px;top:40px;width:240px;height:100px;box-sizing:border-box}input{width:100%;height:100%;box-sizing:border-box}</style><body>`)
		switch r.URL.Path {
		case "/iframe":
			fmt.Fprintf(w, `<button class="target" aria-label="approved outer">approved outer</button><iframe class="target" style="border:0;z-index:2" src="%s" onload="document.body.dataset.ready='yes'"></iframe>`, frame.URL)
		case "/closed-shadow":
			fmt.Fprint(w, `<div class="target" id="host" role="button" aria-label="approved outer"></div><script>const shadow=document.querySelector('#host').attachShadow({mode:'closed'});shadow.innerHTML='<button style="width:100%;height:100%">unapproved shadow</button>';shadow.querySelector('button').onclick=e=>fetch('/hit',{method:'POST',body:JSON.stringify({trusted:e.isTrusted})});document.body.dataset.ready='yes';</script>`)
		case "/protected-descendant":
			fmt.Fprint(w, `<div class="target" role="button" aria-label="approved outer"><input autocomplete="one-time-code" aria-label="protected synthetic input" onclick="fetch('/hit',{method:'POST',body:JSON.stringify({trusted:event.isTrusted})})"></div><script>document.body.dataset.ready='yes';</script>`)
		case "/plain-descendant":
			fmt.Fprint(w, `<button class="target" aria-label="approved outer" onclick="fetch('/hit',{method:'POST',body:JSON.stringify({trusted:event.isTrusted})})"><span>approved child</span></button><script>document.body.dataset.ready='yes';</script>`)
		default:
			fmt.Fprint(w, `<script>document.body.dataset.ready='yes';</script>`)
		}
	}))
	defer page.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	h := New(nil)
	h.SetNavigationGuard(func(_ string, address string) bool {
		u, err := url.Parse(address)
		return err == nil && u.Scheme+"://"+u.Host == page.URL
	})
	root, err := os.MkdirTemp("", "orca-native-boundary-")
	if err != nil {
		t.Fatal("fixture profile creation failed")
	}
	t.Cleanup(func() {
		root, err := filepath.Abs(root)
		temp, tempErr := filepath.Abs(os.TempDir())
		if err != nil || tempErr != nil || !strings.EqualFold(filepath.Dir(root), temp) || !strings.HasPrefix(filepath.Base(root), "orca-native-boundary-") {
			t.Error("fixture cleanup scope invalid")
			return
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if err := os.RemoveAll(root); err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Error("fixture profile cleanup timeout")
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	defer h.Shutdown()
	if err := h.call(ctx, func() error {
		hwnd := win.CreateWindowEx(0, windows.StringToUTF16Ptr("STATIC"), windows.StringToUTF16Ptr("ORCA isolated boundary fixture"), win.WS_OVERLAPPEDWINDOW, 0, 0, 800, 600, 0, 0, 0, nil)
		if hwnd == 0 {
			return fmt.Errorf("cannot create hidden fixture window")
		}
		h.parent = uintptr(hwnd)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = h.call(cleanup, func() error { win.DestroyWindow(win.HWND(h.parent)); return nil })
	}()
	for _, name := range []string{"iframe", "closed-shadow", "protected-descendant", "plain-descendant"} {
		t.Run(name, func(t *testing.T) {
			if err := h.Create(ctx, root, name, page.URL+"/"+name); err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
				defer stop()
				if err := h.Close(cleanup, name); err != nil {
					t.Error("fixture tab close failed")
				}
			}()
			if err := h.Present(ctx, Viewport{TabID: name, Width: 640, Height: 480, Scale: 1, Visible: true}); err != nil {
				t.Fatal(err)
			}
			var obs Observation
			var target string
			for {
				ready, err := h.evaluate(ctx, name, "document.body?.dataset.ready==='yes'")
				if err == nil && string(ready) == "true" {
					obs, err = h.Observe(ctx, name)
					if err == nil {
						for _, e := range obs.Elements {
							if e.Name == "approved outer" {
								target = e.ID
							}
						}
						if target != "" {
							break
						}
					}
				}
				if ctx.Err() != nil {
					t.Fatal("fixture observation timeout")
				}
				time.Sleep(20 * time.Millisecond)
			}
			if name != "plain-descendant" && (len(obs.Screenshot) != 0 || obs.ScreenshotBlocked == "") {
				t.Error("protected screenshot boundary was not enforced")
			}
			before := trustedClicks.Load()
			err := h.Execute(ctx, name, Action{Kind: "click", ElementID: target, Generation: obs.Generation, DocumentID: obs.DocumentID})
			deadline := time.Now().Add(300 * time.Millisecond)
			for trustedClicks.Load() == before && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			clicked := trustedClicks.Load() - before
			if name == "plain-descendant" {
				if err != nil || clicked != 1 {
					t.Errorf("approved ordinary descendant failed: error=%v trusted_events=%d", err, clicked)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "covered, protected or outside") || clicked != 0 {
				t.Errorf("action target boundary bypass: rejected=%t trusted_events=%d", err != nil, clicked)
			}
			if name == "iframe" && frameLoads.Load() == 0 {
				t.Error("cross-origin iframe was never loaded")
			}
		})
	}
}
