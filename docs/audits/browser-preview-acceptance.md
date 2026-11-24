# Browser Preview Acceptance Boundaries

`desktop/browser_wails_acceptance_test.go` is an opt-in Windows integration test for the native hosting boundary. It starts a minimal Wails application with a fixture-only frontend, assigns the Wails window the production browser preview window class, and configures a WebView2 user-data directory under the test temporary root. The browserhost child is created under that Wails window and uses a separate temporary profile.

Run from `desktop`:

```powershell
$env:ORCA_BROWSER_WAILS_TEST = '1'
go test -tags desktop,production -run '^TestBrowserPreviewInWailsWindow$' -count=1 -timeout 90s
```

The test source itself is constrained to `windows && production`. This is intentional: Wails v2.12.0 selects a Windows stub for `!dev && !production && !bindings`, whose `Run` does not create a window or reach `OnDomReady`. Do not use plain `go test` or the `dev` tag as evidence of live Wails hosting. The `production` tag selects Wails' actual desktop frontend; `desktop` is retained in the command as the repository's desktop build tag.

The test runs the Wails fixture in a dedicated Go test subprocess. The child has a 60-second fixture deadline; the parent imposes a 75-second hard limit, terminates only that child if necessary, waits for process exit, then allows its own `testing.T.TempDir` cleanup to remove both WebView2 profiles. Normal completion and fixture failures request Wails shutdown with the lifecycle context and close only the browserhost-owned tab/host. PNG evidence and both profiles live under the parent's temporary root by default; an absolute `ORCA_BROWSER_EVIDENCE_DIR` under the repository `.tmp` directory may nominate a persistent evidence directory for the PNG only. Evidence files use unique names, and the test never deletes a nominated directory.

No user configuration, AppData contents, or existing browser profile is read. The Wails window's WebView2 user-data directory and the browserhost profile are both explicit test-owned paths.

This establishes that a real Wails window can host the real browserhost child using the expected native window class and isolated profiles, and that the browserhost observation/action/presentation primitives work in that fixture. It does **not** establish production frontend integration, browser panel layout or address-slot behavior, app bindings/event wiring, personal configuration isolation in the production entrypoint, multi-tab ownership policy, or full application startup/shutdown. Those require the production UI and separate desktop acceptance coverage; this fixture is not a substitute for it.

## Production UI Gate

`desktop/browser_wails_ui_test.go` is the opt-in production frontend integration gate. It is compiled only with `windows && production && browserui`; run it from `desktop` with the exact production-assets build:

```powershell
$env:ORCA_BROWSER_WAILS_UI_TEST = '1'
$env:ORCA_BROWSER_UI_EVIDENCE_DIR = 'D:\AI-Reasonix\.tmp\v2.1.3-worktree\.tmp\browser-preview-evidence\wails-ui'
go test -tags desktop,production,browserui -run '^TestBrowserPreviewInProductionUI$' -count=1 -timeout 70s -v
```

The fixture starts the real Wails production asset server and binds the real `App` in a dedicated test subprocess. It uses an isolated AppData/profile root, a synthetic owner tab, and only a local `httptest` page. `WindowExecJS` drives the fixture's own Wails frontend; it does not send OS input. The gate first verifies the real first-run onboarding dialog and visible Skip button, hit-tests its screen position, clicks that UI control (which invokes the real `CompleteOnboarding` binding), and waits until the modal is gone before hit-testing/clicking Globe. The resulting onboarding-complete config is written only under the fixture's temporary root. It then checks BrowserPanel navigation/address/title, browserService tab and owner state, DOM viewport against native child bounds, and that a Settings overlay hides the native child. It saves the browserhost's own page PNG when that native capture succeeds. It does not capture the desktop or any unrelated window.

An additional best-effort `PrintWindow(PW_RENDERFULLCONTENT=2)` helper attempts a PNG of only the fixture's top-level `OrcaBrowserPreviewWindow` HWND. It runs in a bounded helper process and verifies the target HWND belongs to the isolated Wails fixture PID and has the expected class before capture. A failed, invalid, or wholly uniform result is reported as unavailable and is never replaced by a DOM screenshot or composited image. A valid parent-window PNG can still contain a stale/blank client surface; the helper does not assert child-WebView composition, which is reported separately based on inspection.

Before the native ABI fix, two consecutive runs on the previous frontend bundle failed (2026-10-11 15:52:41 and 15:54:01) in the browserhost create callback:

```text
browserhost/internal/webviewabi.(*ICoreWebView2).GetSettings
browserhost/internal/webviewabi/abi_windows.go:86  ICoreWebView2.GetSettings
browserhost/native_windows.go:482  Host.configure
browserhost/native_windows.go:318  Host.Create callback
```

The panic was a nil-pointer dereference at `t.view.GetSettings()` (`native_windows.go:482`), dispatching through the wrapper at `abi_windows.go:86`. The proposed cause was missing `//go:uintptrescapes` / object liveness across uintptr-based COM calls. The subsequent native fix added escape/liveness handling and nil-interface errors; this acceptance work did not edit native implementation.

After the native fix, the final frontend bundle was tested three times sequentially without rebuilding or changing assets. JS: `desktop/frontend/dist/assets/index-B4-ZIX6Z.js`, SHA-256 `8889FA33DE81DF9F98309361C7D3D5B9506E6DFAD71FA69D2DBBF281278A083F`; CSS: `desktop/frontend/dist/assets/index-IjieTC-j.css`, SHA-256 `7A30ABE2336693C8B85E65B1377212440384000ADCCE30CED8B308C96B475828`; both LastWriteTime `2026-10-11 15:57:32` Asia/Shanghai. **Result: 3/3 PASS for native browser geometry/navigation/overlay checks only.** Those runs did not exercise the first-run onboarding path; their parent screenshots revealed the onboarding prompt, so the DOM-driven Globe click could have bypassed the modal. Do not treat those three passes as first-user-flow acceptance. Run durations were 8.57s, 8.45s, and 8.30s. They created and navigated the native host to the local `httptest` page, passed DOM/native child bounds and overlay-hide assertions, and saved native browser page PNGs:

- `.tmp/browser-preview-evidence/wails-ui/browser-preview-ui-1249724002.png`
- `.tmp/browser-preview-evidence/wails-ui/browser-preview-ui-1004904211.png`
- `.tmp/browser-preview-evidence/wails-ui/browser-preview-ui-3602113778.png`

Each run also successfully used `PrintWindow(PW_RENDERFULLCONTENT=2)` on only its fixture Wails HWND after verifying the exact fixture PID and `OrcaBrowserPreviewWindow` class. The three 1240x820 parent-window PNGs are `.tmp/browser-preview-evidence/wails-ui/browser-preview-window-1289847547.png`, `browser-preview-window-3119326656.png`, and `browser-preview-window-684133348.png`. These are genuine captures of the named Wails parent HWND, not desktop screenshots. Visual inspection shows the onboarding dialog in those pre-follow-up captures, not the browser panel; they do not prove native child composition.

After correcting the onboarding blind spot, the same unchanged bundle passed the actual first-run flow twice (test durations 9.65s at 16:04:25 and 9.51s at 16:05:37 Asia/Shanghai). Test logs confirm the visible Skip control label `暂不配置，进入应用`, its viewport/hit-test checks, successful modal dismissal, and modal absence before Globe activation. The Globe control also passed an unobstructed hit-test. The second run additionally verified `app.NeedsOnboarding()` is false from the isolated test config after Skip; it does not just hide the dialog in the DOM. Fresh native browser page evidence:

- Native browser page: `.tmp/browser-preview-evidence/wails-ui/browser-preview-ui-832361872.png` (450x786, local `httptest` content).
- Native browser page after persisted-onboarding assertion: `.tmp/browser-preview-evidence/wails-ui/browser-preview-ui-728115412.png` (450x786, local `httptest` content).
- Parent HWND capture: `.tmp/browser-preview-evidence/wails-ui/browser-preview-window-1105759393.png` (1240x820).
- Parent HWND capture after persisted-onboarding assertion: `.tmp/browser-preview-evidence/wails-ui/browser-preview-window-594593828.png` (1240x820).

Both post-onboarding parent PNGs have identical SHA-256 `550914C06B826FB1FE58E50A039BA96ADAEC3C017D5AF25B83406D9DF4742F24`. They contain the Wails title/chrome and a blank white client region rather than the browser panel page. Treat them as genuine captures of the named parent HWND but stale/blank Wails/WebView client frames; they do not prove that the native browser child appears in the full-window capture. The separate browserhost page PNGs prove native page rendering. No PNGs were stitched or substituted. Across these runs the fixture used only temporary config/profile paths and the local HTTP test server; it made no real-model calls and loaded no user configuration or credentials.

Final frozen-dist retest (2026-10-11 16:14 Asia/Shanghai): one additional production UI gate run passed in 9.55s. The test verified the real onboarding Skip path and persisted completion before Globe activation, then passed browser navigation, viewport/native bounds, and overlay-hide checks. Asset hashes were checked immediately before and after the test and remained unchanged: JS `index-VFhgQtMK.js` SHA-256 `F704FDC33EE4E66D059136983085D41EB206E15DFE44704BEEAD40D892DCD71A`; CSS `index-CTl9hVVy.css` SHA-256 `671EBC1680DECCD0F45FEB1844C57AEA3F1D5ACB65BB4CB44F4F5429AEAC67F0`. Fresh native browser page evidence is `.tmp/browser-preview-evidence/wails-ui/browser-preview-ui-2472828170.png` (450x786). The fresh parent capture is `.tmp/browser-preview-evidence/wails-ui/browser-preview-window-2386677297.png` (1240x820); it has the same SHA-256 as the prior blank/stale client captures and remains parent-HWND-only evidence, not proof of child-WebView composition. The frontend bundle was not modified.
