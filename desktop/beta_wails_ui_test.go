//go:build windows && production && browserui

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/lxn/win"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/webbetahost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/orcaaccount"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	winopt "github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

// This suite exercises real production React/App bindings, a beta-class Wails
// window and two native browser surfaces. DeepSeek is replaced ONLY at the
// backend browser factory with a loopback fixture; no site/model/auth request
// or system-browser launch is permitted. It does not certify the site adapter.
func TestBetaConnectionsInProductionUI(t *testing.T) {
	if os.Getenv("ORCA_BETA_NATIVE_UI_TEST") != "1" {
		t.Skip("explicit ORCA_BETA_NATIVE_UI_TEST=1 and fresh ORCA_BETA_NATIVE_ROOT required")
	}
	if os.Getenv("ORCA_BETA_NATIVE_CHILD") == "1" {
		runBetaNativeChild(t)
		return
	}
	root := os.Getenv("ORCA_BETA_NATIVE_ROOT")
	_, source, _, _ := runtime.Caller(0)
	allowed, err := filepath.Abs(filepath.Join(filepath.Dir(source), "..", ".tmp"))
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(allowed, root)
	if !filepath.IsAbs(root) || err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		t.Fatal("ORCA_BETA_NATIVE_ROOT must be a fresh absolute directory below this worktree's .tmp")
	}
	if err := validateBetaDirectory(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatalf("refusing to reuse or replace native evidence/profile root: %v", err)
	}
	selected := os.Getenv("ORCA_BETA_NATIVE_CASE")
	matched := false
	for _, style := range []string{"modern", "classic"} {
		for _, lang := range []string{"en", "zh"} {
			for _, width := range []int{760, 1024, 1366, 1920} {
				name := fmt.Sprintf("%s-%s-%d", style, lang, width)
				if selected != "" && selected != name {
					continue
				}
				matched = true
				t.Run(name, func(t *testing.T) {
					child := filepath.Join(root, name)
					if err := os.Mkdir(child, 0700); err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBetaConnectionsInProductionUI$", "-test.timeout=105s", "-test.v")
					cmd.Env = append(os.Environ(), "ORCA_BETA_NATIVE_CHILD=1", "ORCA_BETA_NATIVE_ROOT="+child, "ORCA_BETA_NATIVE_CASE="+name, "APPDATA="+child)
					output, err := cmd.CombinedOutput()
					if writeErr := os.WriteFile(filepath.Join(child, "test.log"), output, 0600); writeErr != nil {
						t.Error(writeErr)
					}
					if err != nil {
						t.Fatalf("isolated beta native UI: %v\n%s", err, output)
					}
					t.Logf("%s; retained isolated evidence: %s", output, child)
				})
				if t.Failed() {
					return
				}
			}
		}
	}
	if !matched {
		t.Fatal("ORCA_BETA_NATIVE_CASE did not match a style-language-width case")
	}
}

type betaNativeDenyHTTP struct{ calls, probes atomic.Int32 }

func (d *betaNativeDenyHTTP) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodGet && r.URL.String() == orcaaccount.Issuer+orcaaccount.APIPrefix+"/session" && r.Header.Get("Authorization") == "" && r.Header.Get("Cookie") == "" {
		d.probes.Add(1)
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	d.calls.Add(1)
	return nil, errors.New("synthetic native QA forbids external HTTP")
}

type betaLocalWeb struct {
	host  *browserhost.Host
	calls atomic.Int32
}

func (b *betaLocalWeb) State() webbeta.BrowserState { return webbeta.BrowserState{Ready: true} }
func (b *betaLocalWeb) Run(context.Context, webbeta.Command, func(webbeta.Event) error) error {
	b.calls.Add(1)
	return errors.New("synthetic native QA forbids model sends")
}
func (b *betaLocalWeb) Logout(context.Context) error {
	return errors.New("synthetic native QA does not log out a real website")
}
func (b *betaLocalWeb) Recheck(context.Context) error          { return nil }
func (b *betaLocalWeb) GuardIdentity(bool)                     {}
func (b *betaLocalWeb) CloseAndWait(ctx context.Context) error { return b.host.ShutdownConfirmed(ctx) }
func (b *betaLocalWeb) SetBounds(r webbetahost.Bounds) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return b.host.Present(ctx, browserhost.Viewport{TabID: "synthetic-web-login", X: float64(r.X), Y: float64(r.Y), Width: float64(r.Width), Height: float64(r.Height), Scale: 1, Visible: r.Visible})
}

type betaNativeRect struct{ X, Y, Width, Height int32 }
type BetaNativeUIFixture struct {
	app          *App
	root         string
	done         chan error
	checkpoints  []string
	clicks       atomic.Int32
	nativeWaitMS atomic.Int64
	initial      *webbeta.Status
}

func betaVisibleChildren(parent uintptr) []betaNativeRect {
	var result []betaNativeRect
	win.EnumChildWindows(win.HWND(parent), syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var class [128]uint16
		win.GetClassName(win.HWND(hwnd), &class[0], len(class))
		if windows.UTF16ToString(class[:]) != "Static" || !win.IsWindowVisible(win.HWND(hwnd)) {
			return 1
		}
		var r win.RECT
		win.GetWindowRect(win.HWND(hwnd), &r)
		p := win.POINT{X: r.Left, Y: r.Top}
		win.ScreenToClient(win.HWND(parent), &p)
		result = append(result, betaNativeRect{p.X, p.Y, r.Right - r.Left, r.Bottom - r.Top})
		return 1
	}), 0)
	return result
}

// Route mouse messages only through the descendant HWND hierarchy belonging to
// this test's PID-checked beta parent. Never use SendInput/global cursor/focus.
func (f *BetaNativeUIFixture) NativeClick(x, y int32) error {
	parent := findWailsWindowClass(betaWindowClass)
	if parent == 0 {
		return errors.New("owned beta HWND missing")
	}
	var client win.RECT
	win.GetClientRect(win.HWND(parent), &client)
	if x < 0 || y < 0 || x >= client.Right || y >= client.Bottom {
		return errors.New("click outside owned beta client")
	}
	// Like DOM actionability, wait for the actual native cover to leave before
	// sending input. Bound and record this transition; never replay a click.
	started := time.Now()
	waited := false
	for {
		covered := false
		for _, r := range betaVisibleChildren(parent) {
			if x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height {
				covered = true
				break
			}
		}
		if !covered {
			break
		}
		waited = true
		if time.Since(started) > 2*time.Second {
			return errors.New("native child still occludes frontend click target after 2 seconds")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if waited {
		elapsed := time.Since(started).Milliseconds()
		for previous := f.nativeWaitMS.Load(); elapsed > previous; previous = f.nativeWaitMS.Load() {
			if f.nativeWaitMS.CompareAndSwap(previous, elapsed) {
				break
			}
		}
	}
	screen := win.POINT{X: x, Y: y}
	win.ClientToScreen(win.HWND(parent), &screen)
	target := parent
	user32 := windows.NewLazySystemDLL("user32.dll")
	for depth := 0; depth < 16; depth++ {
		p := screen
		win.ScreenToClient(win.HWND(target), &p)
		packed := uintptr(uint64(uint32(p.X)) | uint64(uint32(p.Y))<<32)
		next, _, _ := user32.NewProc("ChildWindowFromPointEx").Call(target, packed, 3)
		if next == 0 || next == target {
			break
		}
		target = next
	}
	if target == parent {
		return errors.New("frontend native input child not found")
	}
	p := screen
	win.ScreenToClient(win.HWND(target), &p)
	packed := uintptr(uint32(uint16(p.X)) | uint32(uint16(p.Y))<<16)
	for _, event := range []struct{ message, buttons uintptr }{{0x200, 0}, {0x201, 1}, {0x202, 0}} {
		if ok, _, err := user32.NewProc("PostMessageW").Call(target, event.message, event.buttons, packed); ok == 0 {
			return fmt.Errorf("targeted native click: %v", err)
		}
	}
	f.clicks.Add(1)
	return nil
}

func (f *BetaNativeUIFixture) Checkpoint(stage string, count int, bounds betaNativeRect) error {
	allowed := map[string]bool{"browser": true, "account": true, "signing-in": true, "web-login": true, "web-closed": true, "web-settings": true, "account-adopted": true, "web-reopened": true, "primary-restored": true}
	if !allowed[stage] {
		return errors.New("unknown native checkpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var actual []betaNativeRect
	for {
		parent := findWailsWindowClass(betaWindowClass)
		if parent == 0 {
			return errors.New("owned beta parent missing")
		}
		actual = betaVisibleChildren(parent)
		if len(actual) == count && (count == 0 || actual[0] == bounds) {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: native children=%+v; expected count=%d bounds=%+v", stage, actual, count, bounds)
		case <-time.After(60 * time.Millisecond):
		}
	}
	s := f.app.WebBetaStatus()
	if stage == "web-login" {
		f.initial = &s
	}
	if stage == "account-adopted" {
		if f.initial == nil || s.Generation == f.initial.Generation || s.Blocked != "" || s.Settings.Enabled || s.SendAuthorized || s.Sent != f.initial.Sent {
			return errors.New("synthetic account adoption did not rotate context or invented authorization")
		}
	}
	if stage == "web-closed" || stage == "primary-restored" {
		if f.initial == nil || s.Pending != f.initial.Pending || s.Sent != f.initial.Sent || !reflect.DeepEqual(s.Settings, f.initial.Settings) {
			return errors.New("closing presentation changed queue/sends/settings")
		}
	}
	var capture string
	// Native bounds can precede the compositor's first painted frame. Require
	// the actual fixture pixels, not an arbitrary sleep or a blank HWND capture.
	for attempt := 0; ; attempt++ {
		var err error
		capture, err = captureWailsParentWindowClass(ctx, f.root, betaWindowClass)
		if err != nil {
			return err
		}
		file, err := os.Open(capture)
		if err != nil {
			return err
		}
		pixels, err := png.Decode(file)
		file.Close()
		if err != nil {
			return err
		}
		samples := betaFixturePixels(pixels)
		if count == 1 && samples >= 50 || count == 0 && samples == 0 {
			break
		}
		_ = os.Rename(capture, filepath.Join(f.root, fmt.Sprintf("%s-paint-wait-%d.png", stage, attempt)))
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: native paint did not settle; fixture samples=%d expected native count=%d", stage, samples, count)
		case <-time.After(60 * time.Millisecond):
		}
	}
	if err := os.Rename(capture, filepath.Join(f.root, stage+".png")); err != nil {
		return err
	}
	f.checkpoints = append(f.checkpoints, stage)
	evidence, _ := json.MarshalIndent(map[string]any{"checkpoints": f.checkpoints, "nativeClicks": f.clicks.Load(), "nativeActionabilityWaitMS": f.nativeWaitMS.Load(), "lastBounds": actual, "windowClass": betaWindowClass, "pid": os.Getpid()}, "", "  ")
	return os.WriteFile(filepath.Join(f.root, "report.json"), evidence, 0600)
}
func (f *BetaNativeUIFixture) Finish(message string) {
	var err error
	if message != "" {
		err = errors.New(message)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		capture, captureErr := captureWailsParentWindowClass(ctx, f.root, betaWindowClass)
		if captureErr == nil {
			captureErr = os.Rename(capture, filepath.Join(f.root, "failure.png"))
		}
		var viewport BrowserViewport
		var shownOwner string
		f.app.browser.mu.Lock()
		if owner := f.app.browser.owners["synthetic-owner"]; owner != nil {
			viewport = owner.viewport
		}
		shownOwner = f.app.browser.shownOwner
		f.app.browser.mu.Unlock()
		diagnostics, _ := json.MarshalIndent(map[string]any{"error": message, "captureError": fmt.Sprint(captureErr), "browser": f.app.GetBrowserState("synthetic-owner"), "viewport": viewport, "shownOwner": shownOwner, "nativeClicks": f.clicks.Load(), "checkpoints": f.checkpoints}, "", "  ")
		_ = os.WriteFile(filepath.Join(f.root, "failure.json"), diagnostics, 0600)
	}
	select {
	case f.done <- err:
	default:
	}
}

func runBetaNativeChild(t *testing.T) {
	root := os.Getenv("ORCA_BETA_NATIVE_ROOT")
	_, source, _, _ := runtime.Caller(0)
	allowed := filepath.Join(filepath.Dir(source), "..", ".tmp")
	rel, relErr := filepath.Rel(allowed, root)
	if !filepath.IsAbs(root) || relErr != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		t.Fatal("child profile must remain below worktree .tmp")
	}
	if err := validateBetaDirectory(root); err != nil {
		t.Fatal(err)
	}
	// Keep native profiles out of nested build/evidence paths: Chromium adds
	// substantial subpaths of its own. Evidence stays in the validated root.
	profileRoot := t.TempDir()
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// NewApp may enter its workspace. Leave it before TempDir cleanup.
		if err := os.Chdir(previousDir); err != nil {
			t.Error(err)
		}
	})
	if err := os.Setenv("APPDATA", profileRoot); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(os.Getenv("ORCA_BETA_NATIVE_CASE"), "-")
	if len(parts) != 3 {
		t.Fatal("invalid case")
	}
	width, err := strconv.Atoi(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	channel = betaChannel
	if err := initializeBeta(); err != nil {
		t.Fatal(err)
	}
	var localRequests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		localRequests.Add(1)
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		fmt.Fprint(w, `<!doctype html><title>Synthetic native fixture</title><main style="background:#e6f4ee;color:#162c25;font:24px sans-serif;padding:30px">SyntheticProject / TestConversation<br>Local native surface</main>`)
	}))
	// Windows may allocate Chromium-blocked ports such as SIP 5061 for :0.
	// Reserve an available high loopback port; never reuse another service.
	server.Listener.Close()
	var listener net.Listener
	for port := 18080; port < 18180; port++ {
		listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("reserve safe loopback fixture port: %v", err)
	}
	server.Listener = listener
	server.Start()
	defer server.Close()
	app := NewApp()
	deny := &betaNativeDenyHTTP{}
	client, err := orcaaccount.New(orcaaccount.Options{Store: accountTestStore{}, HTTPClient: &http.Client{Transport: deny}, OpenBrowser: func(context.Context, string) error { return nil }, DeviceName: "Synthetic native fixture"})
	if err != nil {
		t.Fatal(err)
	}
	app.account = newAccountDesktop(context.Background(), client)
	app.mu.Lock()
	app.tabs["synthetic-owner"] = &WorkspaceTab{ID: "synthetic-owner", Scope: "global", TopicTitle: "TestConversation", Label: "SyntheticProject", Ready: true, promptMode: promptModeCoding}
	app.tabOrder = []string{"synthetic-owner"}
	app.activeTabID = "synthetic-owner"
	app.mu.Unlock()
	if app.browser == nil {
		t.Fatal("beta ordinary native browser unavailable")
	}
	app.browser.host.SetNavigationGuard(func(_ string, address string) bool {
		return address == "about:blank" || strings.HasPrefix(address, server.URL+"/")
	})
	for _, err := range []error{app.SetDesktopCheckUpdates(false), app.SetDesktopLanguage(parts[1]), app.SetDesktopUIStyle(parts[0])} {
		if err != nil {
			t.Fatal(err)
		}
	}
	var web *betaLocalWeb
	newWebBetaBrowser = func(profile, windowClass string) (webBetaBrowser, error) {
		if windowClass != betaWindowClass {
			return nil, errors.New("not production beta class")
		}
		loaded := make(chan error, 1)
		web = &betaLocalWeb{host: browserhost.NewWithWindowClass(windowClass, func(event browserhost.Event) {
			if event.URL != server.URL+"/web-login" {
				return
			}
			if event.Kind == "loaded" {
				select {
				case loaded <- nil:
				default:
				}
			}
			if event.Kind == "error" {
				select {
				case loaded <- errors.New(event.Error):
				default:
				}
			}
		})}
		web.host.SetNavigationGuard(func(_ string, address string) bool { return strings.HasPrefix(address, server.URL+"/") })
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := web.host.Create(ctx, profile, "synthetic-web-login", server.URL+"/web-login"); err != nil {
			return web, err
		}
		select {
		case err := <-loaded:
			return web, err
		case <-ctx.Done():
			return web, ctx.Err()
		}
	}
	fixture := &BetaNativeUIFixture{app: app, root: root, done: make(chan error, 1)}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err = wails.Run(&options.App{Title: "ORCA beta synthetic QA", Width: width, Height: 900, MinWidth: 760, MinHeight: 480, Frameless: desktopFrameless(runtime.GOOS, parts[0]), BackgroundColour: desktopBackground(parts[0]), StartHidden: true, AssetServer: &assetserver.Options{Assets: assets, Middleware: app.workspaceMediaMiddleware()}, Bind: []any{app, fixture}, OnStartup: func(wctx context.Context) { app.ctx = wctx }, OnDomReady: func(wctx context.Context) {
		wruntime.WindowShow(wctx)
		go func() {
			wruntime.WindowExecJS(wctx, betaNativeFlow(parts[0], parts[1], server.URL+"/ordinary"))
			select {
			case err := <-fixture.done:
				if err != nil {
					t.Error(err)
				}
			case <-ctx.Done():
				t.Error(ctx.Err())
			}
			wruntime.Quit(wctx)
		}()
	}, OnShutdown: func(wctx context.Context) { app.shutdown(wctx); closeWebBeta() }, Windows: &winopt.Options{WindowClassName: betaWindowClass, WebviewUserDataPath: filepath.Join(profileRoot, "frontend-webview")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.checkpoints) != 9 || fixture.clicks.Load() < 13 {
		t.Errorf("incomplete native UI flow: %+v clicks=%d", fixture.checkpoints, fixture.clicks.Load())
	}
	if deny.calls.Load() != 0 || deny.probes.Load() != 1 || web == nil || web.calls.Load() != 0 || localRequests.Load() < 2 {
		t.Errorf("network/fixture/send invariants incomplete: denied HTTP=%d web created=%v local requests=%d", deny.calls.Load(), web != nil, localRequests.Load())
	}
}

func betaNativeFlow(style, language, address string) string {
	data, _ := json.Marshal(map[string]string{"style": style, "language": language, "address": address})
	return strings.Replace(betaNativeScript, "__CASE__", string(data), 1)
}

const betaNativeScript = `(async()=>{
const config=__CASE__,f=window.go.main.BetaNativeUIFixture,zh=config.language==='zh';let stage='boot';
const viewportCalls=[],setViewport=window.go.main.App.SetBrowserViewport;window.go.main.App.SetBrowserViewport=(owner,viewport)=>{const record={owner,viewport,start:performance.now()};viewportCalls.push(record);return setViewport(owner,viewport).then(value=>{record.end=performance.now();return value},error=>{record.error=String(error);throw error})};
const wait=async(fn,label)=>{const end=Date.now()+16000;while(Date.now()<end){const value=fn();if(value)return value;await new Promise(r=>setTimeout(r,50));}throw Error('timeout '+label);};
const shown=node=>node&&node.getBoundingClientRect().width>0&&!node.closest('[inert]');
const button=(root,label)=>[...root.querySelectorAll('button')].find(n=>(n.getAttribute('aria-label')||n.textContent).trim()===label&&shown(n));
const rect=node=>{const r=node.getBoundingClientRect(),d=devicePixelRatio;return {X:Math.round(r.x*d),Y:Math.round(r.y*d),Width:Math.round(r.width*d),Height:Math.round(r.height*d)}};
const click=async node=>{if(!node||node.disabled)throw Error('disabled/missing click target at '+stage);node.scrollIntoView({block:'nearest'});await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));const r=node.getBoundingClientRect(),x=r.x+r.width/2,y=r.y+r.height/2,hit=document.elementFromPoint(x,y);if(!shown(node)||!(hit===node||node.contains(hit)))throw Error('DOM obstruction at '+stage);let trusted=false;const listener=e=>{if(node.contains(e.target)&&e.isTrusted)trusted=true};document.addEventListener('click',listener,true);try{await f.NativeClick(Math.round(x*devicePixelRatio),Math.round(y*devicePixelRatio));await wait(()=>trusted,'trusted native click '+stage)}finally{document.removeEventListener('click',listener,true)}};
const active=()=>[...document.querySelectorAll('.beta-connections-dialog')].findLast(shown);
const close=async()=>{await click(active().querySelector('.beta-connections-header button'))};
const checkpoint=async(name,count,node)=>{stage=name;await f.Checkpoint(name,count,node?rect(node):{X:0,Y:0,Width:0,Height:0})};
try{
 await click(await wait(()=>document.querySelector('.onboarding__skip'),'onboarding'));
 await wait(()=>!document.querySelector('.onboarding'),'onboarding dismissed');
 stage='browser-open';await click(await wait(()=>document.querySelector('.app-chrome__browser-toggle,.modern-chrome__workspace-actions button:has(svg.lucide-globe)'),'browser entry'));
 await wait(()=>document.querySelector('.browser-panel__address input'),'browser panel');
 await wait(()=>document.querySelector('.browser-panel [role=tab][aria-selected=true]'),'active browser');
 const input=document.querySelector('.browser-panel__address input');Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,config.address);input.dispatchEvent(new Event('input',{bubbles:true}));input.closest('form').dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}));
 await wait(()=>document.querySelector('.browser-panel [role=tab][aria-selected=true]')?.textContent.includes('Synthetic native fixture'),'local native navigation');
 await checkpoint('browser',1,document.querySelector('.browser-panel__viewport'));
 stage='connections-open';const entry=document.querySelector('[data-beta-entry]');if(shown(entry)){await click(entry)}else if(config.style==='modern'){
 await click([...document.querySelectorAll('.modern-chrome__menu summary')].find(n=>n.textContent.trim()===(zh?'设置':'Settings')));await click(await wait(()=>[...document.querySelectorAll('[role=menuitem]')].find(n=>n.textContent.trim()===(zh?'账号与连接':'Account & connections')),'connections menu'));
 }else{await click(document.querySelector('.app-chrome__command'));await click(await wait(()=>[...document.querySelectorAll('[role=option]')].find(n=>n.textContent.includes(zh?'账号与连接':'Account & connections')),'connections palette'))}
 await wait(()=>active(),'connections dialog');await checkpoint('account',0);
 const account=document.querySelector('.beta-connections-row');await click(await wait(()=>{const n=button(account,zh?'使用ORCA账号登录':'Sign in with ORCA');return n&&!n.disabled?n:null},'ORCA sign-in'));
 await wait(()=>button(account,zh?'取消登录':'Cancel login'),'login in progress');await checkpoint('signing-in',0);await click(button(account,zh?'取消登录':'Cancel login'));
 await wait(()=>button(account,zh?'使用ORCA账号登录':'Sign in with ORCA'),'login cancelled');
 const web=document.querySelectorAll('.beta-connections-row')[1];stage='web-open';await click(button(web,zh?'登录':'Log in'));
 await wait(()=>document.querySelector('[data-testid=beta-webview]'),'web dialog');await checkpoint('web-login',1,document.querySelector('[data-testid=beta-webview]'));
 await close();await wait(()=>!document.querySelector('[data-testid=beta-webview]'),'web closed');await checkpoint('web-closed',0);
 await click(button(web,zh?'DeepSeek 网页设置':'DeepSeek Web settings'));await wait(()=>document.querySelector('.beta-connections-settings'),'settings');await checkpoint('web-settings',0);
 await click(button(active(),zh?'使用当前登录':'Use current login'));await wait(()=>document.querySelector('[data-web-recovered]'),'account adopted');await checkpoint('account-adopted',0);
 await click(button(active(),zh?'打开网页':'Open webpage'));await wait(()=>document.querySelector('[data-testid=beta-webview]'),'new web dialog');await checkpoint('web-reopened',1,document.querySelector('[data-testid=beta-webview]'));
 await close();await wait(()=>!document.querySelector('[data-testid=beta-webview]'),'new web closed');await wait(()=>shown(document.querySelector('.beta-connections-settings')),'settings restored');await close();await wait(()=>!document.querySelector('.beta-connections-settings'),'settings closed');await close();
 await wait(()=>!document.querySelector('.beta-connections-dialog'),'primary closed');await checkpoint('primary-restored',1,document.querySelector('.browser-panel__viewport'));
 await f.Finish('');
}catch(error){await f.Finish(stage+': '+String(error.stack||error)+' diagnostics='+JSON.stringify({hidden:document.hidden,nativeVisible:document.querySelector('.browser-panel')?.dataset.nativeVisible,viewportCalls}))}
})();`
