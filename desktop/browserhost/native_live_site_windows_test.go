//go:build windows

package browserhost

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// This fixture drives the real WebView2 host against the public practice site
// the product is measured by (https://the-internet.herokuapp.com). It owns its
// own hidden window and temporary profile, so it cannot see or drive the user's
// desktop. Opt in with ORCA_BROWSER_LIVE_TEST=1 because it needs the network.
const liveSiteRoot = "https://the-internet.herokuapp.com"

func TestNativeLiveSite(t *testing.T) {
	if os.Getenv("ORCA_BROWSER_LIVE_TEST") != "1" {
		t.Skip("live-site browser test is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	h := New(func(e Event) {
		if e.Kind == "error" {
			t.Logf("browser event error: %s", e.Error)
		}
	})
	h.SetNavigationGuard(func(id, url string) bool { return strings.HasPrefix(url, liveSiteRoot) })
	if err := h.call(ctx, func() error {
		hwnd := win.CreateWindowEx(0, windows.StringToUTF16Ptr("STATIC"), windows.StringToUTF16Ptr("ORCA live site test"), win.WS_OVERLAPPEDWINDOW, 0, 0, 1000, 760, 0, 0, 0, nil)
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
		// WebView2 flushes profile metrics just after shutdown; give it a moment
		// so the temporary profile can be removed.
		time.Sleep(500 * time.Millisecond)
	}()
	// A plain directory rather than t.TempDir(): WebView2 can still be flushing
	// profile metrics when the test returns, and a failed forced cleanup would
	// report a product failure that is not one.
	profile, err := os.MkdirTemp("", "orca-live-profile-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(profile)
	if err := h.Create(ctx, profile, "live", liveSiteRoot+"/"); err != nil {
		t.Fatal(err)
	}
	if err := h.Present(ctx, Viewport{TabID: "live", Width: 1000, Height: 700, Scale: 1, Visible: true}); err != nil {
		t.Fatal(err)
	}

	// waitText reloads if needed and waits for visible text to appear.
	waitText := func(t *testing.T, want string) Observation {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		var o Observation
		var err error
		for time.Now().Before(deadline) {
			if o, err = h.Observe(ctx, "live"); err == nil && strings.Contains(o.Text, want) {
				return o
			}
			time.Sleep(150 * time.Millisecond)
		}
		t.Fatalf("page never showed %q (last error %v, text %q)", want, err, lastText(o))
		return o
	}
	// waitUntil polls a page predicate because frames, images and editors mount
	// after the text that announces them.
	waitUntil := func(t *testing.T, label, expr string) Observation {
		t.Helper()
		deadline := time.Now().Add(25 * time.Second)
		var o Observation
		for time.Now().Before(deadline) {
			var err error
			if o, err = h.Observe(ctx, "live"); err == nil {
				if data, e := h.evaluate(ctx, "live", expr); e == nil && string(data) == "true" {
					return o
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("%s never became true", label)
		return o
	}
	go_ := func(t *testing.T, path, want string) Observation {
		t.Helper()
		if err := h.Navigate(ctx, "live", liveSiteRoot+path); err != nil {
			t.Fatalf("%s: navigate: %v", path, err)
		}
		o := waitText(t, want)
		// Text is painted before images and iframes finish, so let the layout
		// settle before the caller reads element geometry.
		time.Sleep(400 * time.Millisecond)
		if settled, err := h.Observe(ctx, "live"); err == nil {
			o = settled
		}
		t.Logf("%s: %d elements, %d frames, %d shadow roots", path, len(o.Elements), o.Frames, o.ShadowRoots)
		return o
	}
	act := func(t *testing.T, kind, elementID, text string) Observation {
		t.Helper()
		o, err := h.Observe(ctx, "live")
		if err != nil {
			t.Fatalf("observe before %s: %v", kind, err)
		}
		if elementID != "" {
			if _, ok := elementByID(o, elementID); !ok {
				t.Fatalf("element %s missing from observation", elementID)
			}
		}
		if err := h.Execute(ctx, "live", Action{Kind: kind, ElementID: elementID, Text: text, Generation: o.Generation, DocumentID: o.DocumentID}); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		return o
	}
	value := func(t *testing.T, expr string) string {
		t.Helper()
		data, err := h.evaluate(ctx, "live", expr)
		if err != nil {
			t.Fatalf("evaluate %s: %v", expr, err)
		}
		return string(data)
	}

	t.Run("homepage-lists-examples", func(t *testing.T) {
		o := go_(t, "/", "Available Examples")
		if len(o.Elements) < 20 {
			t.Fatalf("homepage observation only had %d elements", len(o.Elements))
		}
	})

	t.Run("form-inputs", func(t *testing.T) {
		o := go_(t, "/login", "Login Page")
		user := findElement(o, func(e Element) bool { return e.Role == "input" && e.Type == "text" })
		pass := findElement(o, func(e Element) bool { return e.Type == "password" })
		if user == nil || pass == nil {
			t.Fatalf("login form inputs missing (user %v pass %v)", user, pass)
		}
		act(t, "type", user.ID, "tomsmith")
		if v := value(t, "document.querySelector('#username').value"); v != `"tomsmith"` {
			t.Fatalf("username value = %s", v)
		}
		// Password fields stay out of the agent's reach; that refusal is the
		// contract, not a failure of the page.
		o, _ = h.Observe(ctx, "live")
		if p := findElement(o, func(e Element) bool { return e.Type == "password" }); p != nil {
			if err := h.Execute(ctx, "live", Action{Kind: "type", ElementID: p.ID, Text: "x", Generation: o.Generation, DocumentID: o.DocumentID}); err == nil {
				t.Fatal("typed into a password field")
			}
		}
		o, _ = h.Observe(ctx, "live")
		btn := findElement(o, func(e Element) bool { return e.Role == "button" && strings.Contains(strings.ToLower(e.Name), "login") })
		if btn == nil {
			t.Fatalf("login button missing: %+v", names(o))
		}
		act(t, "click", btn.ID, "")
		waitText(t, "Your password is invalid!")
	})

	t.Run("forgot-password-round-trip", func(t *testing.T) {
		o := go_(t, "/forgot_password", "Forgot Password")
		email := findElement(o, func(e Element) bool { return e.Role == "input" })
		if email == nil {
			t.Fatalf("email input missing: %+v", names(o))
		}
		act(t, "type", email.ID, "orca@example.com")
		o, _ = h.Observe(ctx, "live")
		btn := findElement(o, func(e Element) bool { return e.Role == "button" })
		if btn == nil {
			t.Fatalf("submit button missing: %+v", names(o))
		}
		act(t, "click", btn.ID, "")
		waitText(t, "Your e-mail's been sent!")
	})

	t.Run("dropdown-select", func(t *testing.T) {
		o := go_(t, "/dropdown", "Dropdown List")
		sel := findElement(o, func(e Element) bool { return e.Role == "select" })
		if sel == nil {
			t.Fatalf("dropdown missing: %+v", names(o))
		}
		act(t, "select", sel.ID, "1")
		if v := value(t, "document.querySelector('#dropdown').value"); v != `"1"` {
			t.Fatalf("dropdown value = %s", v)
		}
	})

	t.Run("checkboxes-toggle", func(t *testing.T) {
		o := go_(t, "/checkboxes", "Checkboxes")
		box := findElement(o, func(e Element) bool { return e.Type == "checkbox" })
		if box == nil {
			t.Fatalf("checkbox missing: %+v", names(o))
		}
		act(t, "click", box.ID, "")
		state := value(t, "document.querySelectorAll('input[type=checkbox]')[0].checked")
		if state != "true" && state != "false" {
			t.Fatalf("checkbox state = %s", state)
		}
		if (state == "true") == box.Checked {
			t.Fatalf("checkbox did not toggle (was %v, now %s)", box.Checked, state)
		}
	})

	t.Run("add-remove-elements", func(t *testing.T) {
		o := go_(t, "/add_remove_elements/", "Add Element")
		add := findElement(o, func(e Element) bool { return e.Role == "button" && strings.Contains(e.Name, "Add Element") })
		if add == nil {
			t.Fatalf("add button missing: %+v", names(o))
		}
		act(t, "click", add.ID, "")
		o, err := h.Observe(ctx, "live")
		if err != nil {
			t.Fatal(err)
		}
		del := findElement(o, func(e Element) bool { return e.Role == "button" && strings.Contains(e.Name, "Delete") })
		if del == nil {
			t.Fatalf("delete button never appeared: %+v", names(o))
		}
		act(t, "click", del.ID, "")
		o, err = h.Observe(ctx, "live")
		if err != nil {
			t.Fatal(err)
		}
		if findElement(o, func(e Element) bool { return e.Role == "button" && strings.Contains(e.Name, "Delete") }) != nil {
			t.Fatal("delete button survived its click")
		}
	})

	t.Run("key-presses", func(t *testing.T) {
		o := go_(t, "/key_presses", "Key Presses")
		input := findElement(o, func(e Element) bool { return e.Role == "input" })
		if input == nil {
			t.Fatalf("key input missing: %+v", names(o))
		}
		act(t, "click", input.ID, "")
		o, _ = h.Observe(ctx, "live")
		if err := h.Execute(ctx, "live", Action{Kind: "key", Key: "Escape", Generation: o.Generation, DocumentID: o.DocumentID}); err != nil {
			t.Fatalf("key: %v", err)
		}
		waitText(t, "You entered: ESCAPE")
	})

	t.Run("javascript-alerts", func(t *testing.T) {
		go_(t, "/javascript_alerts", "JavaScript Alerts")
		o, err := h.Observe(ctx, "live")
		if err != nil {
			t.Fatal(err)
		}
		btn := findElement(o, func(e Element) bool { return e.Role == "button" && strings.Contains(e.Name, "Click for JS Alert") })
		if btn == nil {
			t.Fatalf("alert button missing: %+v", names(o))
		}
		act(t, "click", btn.ID, "")
		// The dialog must be accepted so the page keeps running and reports it.
		waitText(t, "You successfully clicked an alert")
	})

	t.Run("frames-are-observed", func(t *testing.T) {
		if err := h.Navigate(ctx, "live", liveSiteRoot+"/nested_frames"); err != nil {
			t.Fatal(err)
		}
		o := waitUntil(t, "nested frames", "document.querySelectorAll('frame,iframe').length>=2")
		deadline := time.Now().Add(20 * time.Second)
		for o.Frames < 3 && time.Now().Before(deadline) {
			time.Sleep(200 * time.Millisecond)
			if next, err := h.Observe(ctx, "live"); err == nil {
				o = next
			}
		}
		if o.Frames < 3 {
			t.Fatalf("only %d frames counted", o.Frames)
		}
		if findElement(o, func(e Element) bool { return e.Role == "frame" }) == nil {
			t.Fatalf("frame elements not observed: %+v", names(o))
		}
		// Frame documents are same-origin here, so their text is readable too.
		if v := value(t, "(document.querySelector('frame').contentDocument.body.innerText||document.querySelector('frame').contentDocument.documentElement.textContent||'').length>0"); v != "true" {
			t.Fatalf("frame document unreadable: %s", v)
		}
	})

	t.Run("iframe-embedded", func(t *testing.T) {
		if err := h.Navigate(ctx, "live", liveSiteRoot+"/iframe"); err != nil {
			t.Fatal(err)
		}
		o := waitUntil(t, "tinymce iframe", "document.querySelectorAll('iframe').length>=1")
		if o.Frames < 1 {
			t.Fatalf("no iframe counted: %+v", names(o))
		}
	})

	t.Run("shadow-dom-is-readable", func(t *testing.T) {
		o := go_(t, "/shadowdom", "Simple template")
		if o.ShadowRoots < 2 {
			t.Fatalf("only %d shadow roots counted", o.ShadowRoots)
		}
		if v := value(t, "document.querySelector('my-paragraph').shadowRoot.textContent.includes('My default text')"); v != "true" {
			t.Fatalf("shadow content unreadable: %s", v)
		}
	})

	t.Run("shadow-dom-controls", func(t *testing.T) {
		// The site has no control inside a shadow root, so build the smallest
		// one a real app would ship and drive it through the same observation.
		go_(t, "/shadowdom", "Simple template")
		if _, err := h.evaluate(ctx, "live", `(()=>{if(document.querySelector('#orca-shadow-host'))return;const h=document.createElement('div');h.id='orca-shadow-host';h.style.cssText='margin:16px';const r=h.attachShadow({mode:'open'});r.innerHTML='<label>Orca shadow field <input id="inner" type="text"></label><button id="go">Go</button>';document.querySelector('#content').append(h)})()`); err != nil {
			t.Fatal(err)
		}
		o, err := h.Observe(ctx, "live")
		if err != nil {
			t.Fatal(err)
		}
		field := findElement(o, func(e Element) bool { return e.Role == "input" && e.Type == "text" })
		if field == nil {
			t.Fatalf("shadow input not observed: %+v", names(o))
		}
		act(t, "type", field.ID, "orca")
		if v := value(t, "document.querySelector('#orca-shadow-host').shadowRoot.querySelector('#inner').value"); v != `"orca"` {
			t.Fatalf("shadow input value = %s", v)
		}
	})

	t.Run("dynamic-loading-waitfor", func(t *testing.T) {
		o := go_(t, "/dynamic_loading/1", "Dynamically Loaded Page Elements")
		start := findElement(o, func(e Element) bool { return e.Role == "button" && strings.Contains(e.Name, "Start") })
		if start == nil {
			t.Fatalf("start button missing: %+v", names(o))
		}
		o, err := h.Observe(ctx, "live")
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Execute(ctx, "live", Action{Kind: "click", ElementID: start.ID, Generation: o.Generation, DocumentID: o.DocumentID}); err != nil {
			t.Fatal(err)
		}
		o, err = h.Observe(ctx, "live")
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Execute(ctx, "live", Action{Kind: "waitfor", Text: "Hello World!", Generation: o.Generation, DocumentID: o.DocumentID}); err != nil {
			t.Fatalf("waitfor: %v", err)
		}
	})

	t.Run("hover-reveals-caption", func(t *testing.T) {
		if err := h.Navigate(ctx, "live", liveSiteRoot+"/hovers"); err != nil {
			t.Fatal(err)
		}
		// The figure images have no intrinsic size; they are only clickable once
		// the stylesheet has laid them out.
		o := waitUntil(t, "hover figures laid out", "(()=>{const i=[...document.querySelectorAll('img')].find(i=>i.src.includes('avatar'));return !!i&&i.getBoundingClientRect().width>0})()")
		fig := findElement(o, func(e Element) bool { return e.Role == "img" })
		if fig == nil {
			t.Fatalf("hover figure missing: %+v", names(o))
		}
		act(t, "hover", fig.ID, "")
		time.Sleep(400 * time.Millisecond)
		visible := value(t, "(()=>{const c=document.querySelector('.figcaption');return !!c && getComputedStyle(c).opacity!=='0'})()")
		if visible != "true" {
			t.Fatalf("hover caption did not appear: %s", visible)
		}
	})

	t.Run("tables-read-cell", func(t *testing.T) {
		o := go_(t, "/tables", "Example 1")
		if !strings.Contains(o.Text, "Smith") || !strings.Contains(o.Text, "jsmith@gmail.com") {
			t.Fatalf("table content missing from observation text")
		}
	})
}

func elementByID(o Observation, id string) (Element, bool) {
	for _, e := range o.Elements {
		if e.ID == id {
			return e, true
		}
	}
	return Element{}, false
}

func findElement(o Observation, match func(Element) bool) *Element {
	for i := range o.Elements {
		if match(o.Elements[i]) {
			return &o.Elements[i]
		}
	}
	return nil
}

func names(o Observation) []string {
	out := make([]string, 0, len(o.Elements))
	for _, e := range o.Elements {
		out = append(out, e.Role+":"+e.Name)
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

func lastText(o Observation) string {
	if len(o.Text) > 400 {
		return o.Text[:400]
	}
	return o.Text
}
