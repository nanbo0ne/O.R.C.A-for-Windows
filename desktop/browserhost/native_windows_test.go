//go:build windows

package browserhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

func TestPartialBrowserInputNeverReportsRetryableStale(t *testing.T) {
	if errors.Is(uncertainBrowserInput(ErrStaleObservation), ErrStaleObservation) {
		t.Fatal("partial input may not be replayed as a rejected stale observation")
	}
}

// This fixture owns its own hidden native window and temporary browser profile.
// It cannot capture or inject input into the user's desktop.
func TestNativeIsolatedBrowser(t *testing.T) {
	if os.Getenv("ORCA_BROWSER_NATIVE_TEST") != "1" {
		t.Skip("isolated WebView2 test is opt-in")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><title>ORCA isolated fixture</title><label>姓名<input id="name"></label><button id="submit" onclick="document.querySelector('#result').textContent=document.querySelector('#name').value">提交</button><p id="result"></p><a href="http://127.0.0.1:1/forbidden">blocked navigation</a>`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	failures := make(chan Event, 8)
	h := New(func(e Event) {
		if e.Kind == "error" {
			select {
			case failures <- e:
			default:
			}
		}
	})
	h.SetNavigationGuard(func(id, url string) bool { return strings.HasPrefix(url, server.URL) })
	if err := h.call(ctx, func() error {
		hwnd := win.CreateWindowEx(0, windows.StringToUTF16Ptr("STATIC"), windows.StringToUTF16Ptr("ORCA synthetic browser test"), win.WS_OVERLAPPEDWINDOW, 0, 0, 800, 600, 0, 0, 0, nil)
		h.parent = uintptr(hwnd)
		if hwnd == 0 {
			return fmt.Errorf("create test window")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = h.call(context.Background(), func() error { win.DestroyWindow(win.HWND(h.parent)); return nil })
		h.Shutdown()
	}()
	profile := t.TempDir()
	if err := h.Create(ctx, profile, "test", server.URL); err != nil {
		t.Fatal(err)
	}
	if err := h.Present(ctx, Viewport{TabID: "test", Width: 640, Height: 480, Scale: 1, Visible: true}); err != nil {
		t.Fatal(err)
	}
	var o Observation
	for {
		var err error
		o, err = h.Observe(ctx, "test")
		if err == nil && strings.Contains(o.Text, "提交") {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("page did not load: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(o.Screenshot) < 100 {
		t.Fatal("blank capture")
	}
	var input string
	for _, e := range o.Elements {
		if e.Role == "input" {
			input = e.ID
		}
	}
	if input == "" {
		t.Fatal("no input")
	}
	if err := h.Execute(ctx, "test", Action{Kind: "type", ElementID: input, Text: "测试访客", Generation: o.Generation, DocumentID: o.DocumentID}); err != nil {
		t.Fatal(err)
	}
	if err := h.Execute(ctx, "test", Action{Kind: "click", ElementID: input, Generation: o.Generation, DocumentID: o.DocumentID}); err == nil {
		t.Fatal("accepted stale observation")
	}
	o, _ = h.Observe(ctx, "test")
	var submit string
	for _, e := range o.Elements {
		if e.Name == "提交" {
			submit = e.ID
		}
	}
	if err := h.Execute(ctx, "test", Action{Kind: "click", ElementID: submit, Generation: o.Generation, DocumentID: o.DocumentID}); err != nil {
		t.Fatal(err)
	}
	value, err := h.evaluate(ctx, "test", "document.querySelector('#result').textContent")
	if err != nil || string(value) != `"测试访客"` {
		t.Fatalf("native click result = %s, %v", value, err)
	}
	if err := h.Navigate(ctx, "test", "http://127.0.0.1:1/forbidden"); err == nil {
		t.Fatal("origin bypass")
	}
	err = h.call(ctx, func() error {
		settings, e := h.tabs["test"].view.GetSettings()
		if e != nil {
			return e
		}
		defer raw(unsafe.Pointer(settings), settings.Vtbl.Release)
		if readBool(unsafe.Pointer(settings), settings.Vtbl.GetAreHostObjectsAllowed) || readBool(unsafe.Pointer(settings), settings.Vtbl.GetIsWebMessageEnabled) {
			return fmt.Errorf("host bridge exposed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Present(ctx, Viewport{Scale: 1, Visible: false}); err != nil {
		t.Fatal(err)
	}
	err = h.call(ctx, func() error {
		c := h.tabs["test"].controller
		if readBool(unsafe.Pointer(c), c.Vtbl.GetIsVisible) {
			return fmt.Errorf("hidden surface still visible")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := h.Command(ctx, "test", "state", "")
	var info Tab
	if err != nil || json.Unmarshal([]byte(state), &info) != nil || info.Title != "ORCA isolated fixture" {
		t.Fatalf("state: %s %v", state, err)
	}
	t.Run("mutation-invalidates-input", func(t *testing.T) {
		o, err := h.Observe(ctx, "test")
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.evaluate(ctx, "test", "document.querySelector('#submit').textContent='changed'")
		if err != nil {
			t.Fatal(err)
		}
		if err = h.Execute(ctx, "test", Action{Kind: "click", ElementID: submit, Generation: o.Generation, DocumentID: o.DocumentID}); err != ErrStaleObservation {
			t.Fatalf("mutation accepted: %v", err)
		}
	})
	t.Run("page-cannot-replace-references", func(t *testing.T) {
		if _, err := h.cdp(ctx, "test", "Runtime.evaluate", map[string]any{"expression": "window.__orcaObserved = new Map(); window.__orcaObservationRevision = 900"}); err != nil {
			t.Fatal(err)
		}
		o, err := h.Observe(ctx, "test")
		if err != nil {
			t.Fatal(err)
		}
		if err = h.Execute(ctx, "test", Action{Kind: "type", ElementID: input, Text: "安全", Generation: o.Generation, DocumentID: o.DocumentID}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("covered-reference-never-clicks-overlay", func(t *testing.T) {
		_, err := h.evaluate(ctx, "test", `(()=>{const r=document.querySelector('#submit').getBoundingClientRect();const b=document.createElement('button');b.id='overlay';b.style.cssText='position:fixed;z-index:99999;left:'+r.x+'px;top:'+r.y+'px;width:'+r.width+'px;height:'+r.height+'px';b.textContent='unapproved';b.onclick=()=>document.body.dataset.badClick='1';document.body.append(b)})()`)
		if err != nil {
			t.Fatal(err)
		}
		o, err := h.Observe(ctx, "test")
		if err != nil {
			t.Fatal(err)
		}
		if err = h.Execute(ctx, "test", Action{Kind: "click", ElementID: submit, Generation: o.Generation, DocumentID: o.DocumentID}); err == nil {
			t.Fatal("covered approved reference was accepted")
		}
		result, err := h.evaluate(ctx, "test", `(()=>{const v=document.body.dataset.badClick||'';document.querySelector('#overlay').remove();return v})()`)
		if err != nil || string(result) != `""` {
			t.Fatalf("overlay received input: %s %v", result, err)
		}
	})
	t.Run("fill-cannot-be-redirected-by-focus-handler", func(t *testing.T) {
		_, err := h.evaluate(ctx, "test", `(()=>{const p=document.createElement('input');p.id='protected';p.type='password';document.body.append(p);document.querySelector('#name').onfocus=()=>p.focus()})()`)
		if err != nil {
			t.Fatal(err)
		}
		o, err := h.Observe(ctx, "test")
		if err != nil {
			t.Fatal(err)
		}
		if err = h.Execute(ctx, "test", Action{Kind: "type", ElementID: input, Text: "approved text", Generation: o.Generation, DocumentID: o.DocumentID}); err != nil {
			t.Fatal(err)
		}
		result, err := h.evaluate(ctx, "test", `(()=>{const v={target:document.querySelector('#name').value,protected:document.querySelector('#protected').value};document.querySelector('#name').onfocus=null;document.querySelector('#protected').remove();return v})()`)
		if err != nil || !strings.Contains(string(result), `"protected":""`) || !strings.Contains(string(result), `"target":"approved text"`) {
			t.Fatalf("focus changed fill target: %s %v", result, err)
		}
	})
	t.Run("closed-shadow-screenshot-denied", func(t *testing.T) {
		_, err := h.cdp(ctx, "test", "Runtime.evaluate", map[string]any{"expression": `const host=document.createElement('div');host.id='closed-shadow';host.attachShadow({mode:'closed'}).innerHTML='<input autocomplete="one-time-code" value="synthetic-secret">';document.body.append(host)`})
		if err != nil {
			t.Fatal(err)
		}
		if err = h.Present(ctx, Viewport{TabID: "test", Width: 640, Height: 480, Scale: 1, Visible: true}); err != nil {
			t.Fatal(err)
		}
		o, err := h.Observe(ctx, "test")
		if err != nil || len(o.Screenshot) != 0 || o.ScreenshotBlocked == "" {
			t.Fatalf("uninspected shadow pixels exposed: %d %q %v", len(o.Screenshot), o.ScreenshotBlocked, err)
		}
		_, _ = h.evaluate(ctx, "test", `document.querySelector('#closed-shadow').remove()`)
		_ = h.Present(ctx, Viewport{Visible: false, Scale: 1})
	})
	t.Run("profile-clear", func(t *testing.T) {
		if _, err := h.evaluate(ctx, "test", "document.cookie='orca_test=synthetic; path=/';localStorage.setItem('orca_test','synthetic')"); err != nil {
			t.Fatal(err)
		}
		for _, item := range []struct {
			id, profile string
			shared      bool
		}{{"same-project", profile, true}, {"different-project", t.TempDir(), false}} {
			if err := h.Create(ctx, item.profile, item.id, server.URL); err != nil {
				t.Fatal(err)
			}
			for {
				data, e := h.evaluate(ctx, item.id, "({ready:document.title,cookie:document.cookie})")
				if e == nil && strings.Contains(string(data), "ORCA isolated fixture") {
					if strings.Contains(string(data), "orca_test=synthetic") != item.shared {
						t.Fatalf("profile cookie isolation: %s %s", item.id, data)
					}
					break
				}
				if ctx.Err() != nil {
					t.Fatal("profile tab did not load")
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err := h.Close(ctx, item.id); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := h.Command(ctx, "test", "clear", ""); err != nil {
			t.Fatal(err)
		}
		value, err := h.evaluate(ctx, "test", "({cookie:document.cookie,storage:localStorage.getItem('orca_test')})")
		if err != nil || strings.Contains(string(value), "synthetic") {
			t.Fatalf("clear did not remove synthetic state: %s %v", value, err)
		}
	})
	t.Run("stale-navigation-fence", func(t *testing.T) {
		o, err := h.Observe(ctx, "test")
		if err != nil {
			t.Fatal(err)
		}
		stale := context.WithValue(ctx, actionFenceKey{}, actionFence{document: o.DocumentID, generation: o.Generation - 1})
		if err := h.Navigate(stale, "test", server.URL); err != ErrStaleObservation {
			t.Fatalf("stale navigation accepted: %v", err)
		}
	})
	t.Run("native-zoom-and-script-dialog-suppression", func(t *testing.T) {
		if _, err := h.Command(ctx, "test", "zoom", "1.25"); err != nil {
			t.Fatal(err)
		}
		if err := h.call(ctx, func() error {
			var zoom float64
			c := h.tabs["test"].controller
			if err := raw(unsafe.Pointer(c), c.Vtbl.GetZoomFactor, uintptr(unsafe.Pointer(&zoom))); err != nil {
				return err
			}
			if zoom != 1.25 {
				return fmt.Errorf("native zoom=%f", zoom)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		result, err := h.evaluate(ctx, "test", `confirm('synthetic confirmation must not open a system dialog')`)
		if err != nil || string(result) != "false" {
			t.Fatalf("script dialog not suppressed: %s %v", result, err)
		}
	})
	t.Run("network-failure-is-not-loaded-success", func(t *testing.T) {
		closed := httptest.NewServer(http.NotFoundHandler())
		closedURL := closed.URL
		closed.Close()
		h.SetNavigationGuard(func(id, address string) bool { return address == closedURL || address == closedURL+"/" })
		if err := h.Navigate(ctx, "test", closedURL); err != nil {
			t.Fatal(err)
		}
		select {
		case e := <-failures:
			if !strings.Contains(e.Error, "Page did not load") {
				t.Fatalf("missing actionable page error: %s", e.Error)
			}
		case <-time.After(8 * time.Second):
			t.Fatal("failed navigation did not report an error")
		}
	})
}
