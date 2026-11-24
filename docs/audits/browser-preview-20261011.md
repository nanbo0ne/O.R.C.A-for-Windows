# O.R.C.A. 3.0.14 Browser Preview: Local Acceptance

Date: 2026-10-11. Local Windows preview only; no release, upload, tag, installer,
or stable update-feed change is authorized by this delivery.

## Implementation

- The Globe action opens an owned browser tab in the existing workspace dock.
  The browser is an independent WebView2 environment/controller, not an iframe
  and not navigation of the Wails application view.
- A dedicated native STA owns WebView2 calls. Parent lookup uses the test/build
  process and `OrcaBrowserPreviewWindow`, never the foreground window. There is
  no remote debugging listener or system mouse/keyboard injection.
- Tabs, navigation, back/forward, reload/stop, zoom, find, copy URL, external
  opening, popup tabs, upload approval, download progress/cancel and profile
  clearing are implemented. Browser metadata uses stable conversation identity;
  project conversations share a profile, independent conversations do not.
- `browser_task`, `browser_status` and `browser_stop` reuse parent turn/session
  ownership. The dedicated child inherits the current model unless the existing
  `agent.subagent_models["browser"]` configuration names an explicit model.
  It exposes only structured actions, not arbitrary JS/CDP or filesystem tools.
- DOM references are scoped to the current isolated-world observation. Canvas
  clicks use full screenshot pixels converted once to CSS coordinates. Real
  hit-testing rejects covered, cross-frame, shadow-root and protected targets.
- Website content cannot authorize actions. Origin/model exposure, screenshots
  and mutating actions require explicit consent. Uploads use validated immutable
  snapshots and downloads are not opened/executed automatically.
- A project profile has one AI lease. Cancellation, takeover, tab closure and
  parent completion invalidate it. A new observation is required after every
  action. An uncertain input is never replayed automatically.
- The main conversation pause gate now cancels its active browser child before
  acknowledging pause. It waits for in-flight native dispatch to return, blocks
  late actions and leaves the old parent turn revoked. Resume does not silently
  restart browser work. Already-dispatched website side effects cannot be undone.
- Completion uses a new observation and a separate verdict, including trusted
  execution counts/receipts and restrictions. Native dispatch success alone is
  not evidence that a website task succeeded.
- Screenshots stay in the child observation, not the main conversation history.
  Protected inputs, frames and closed shadow roots conservatively block visual
  inspection. No fallback to a different model/provider is silent.
- Preview startup redirects configuration, browser profiles and workspace into
  an independent preview home before constructing the application. Stable
  configuration/history is not imported, updater actions are disabled, and the
  preview has its own single-instance identity and executable name.

## Actual Model Tests

Tests used an isolated Wails host, synthetic localhost pages, the real Orca main
Agent, a dedicated browser child and native WebView2 input. The harness set up
pages and independently checked events; it did not substitute clicks for the
model. User history, screenshots and work files were not sent to either model.

The configured LAN API model `lens-main` was verified as Qwen3.8-27B. No model
service was restarted or reconfigured. DeepSeek used `deepseek-flash` at the
official endpoint. Secrets are absent from reports and packages.

| Model | Scenario | Latest result | Native actions | Verified page outcomes | Elapsed |
| --- | --- | --- | ---: | ---: | ---: |
| Qwen3.8-27B | DOM button | Pass | 1 | 1 | 54.8 s |
| Qwen3.8-27B | Chinese form and selection | Pass | 3 | 1 | 37.4 s |
| Qwen3.8-27B | Visual-only Canvas | Pass | 1 | 1 | 51.0 s |
| DeepSeek Flash | DOM button | Pass | 1 | 1 | 7.1 s |
| DeepSeek Flash | Chinese form and selection | Pass | 3 | 1 | 7.6 s |
| DeepSeek Flash | Visual-only Canvas | Pass | 1 | 1 | 7.4 s |

Each row is one successful run, not a three-run stability or performance claim.
Times include delegation, probes, native work and verification. Earlier failed
Canvas attempts are retained in the audit ledger and are not counted as passes.
The fixes reject Canvas-container IDs as buttons and state image-coordinate
semantics explicitly; the latest runs each produced exactly one correct click.

### Cumulative Live Budget

| Evidence file | HTTP attempts | DeepSeek conservative reservation, CNY |
| --- | ---: | ---: |
| live-click-initial.json | 0 | 0 |
| live-click-v2.json | 8 | 0 |
| live-deepseek-click.json | 6 | 0.609705 |
| live-core-v3.json | 30 | 1.741842 |
| live-canvas-v3.json | 8 | 0 |
| live-deepseek-canvas.json | 10 | 1.547874 |
| live-qwen-canvas-v4.json | 7 | 0 |
| live-deepseek-canvas-v4.json | 7 | 0.928305 |
| Total | **76 / 80** | **4.827726 / 5** |

The reservation uses conservative token/output estimates and does not refund
failed attempts. It is not an invoice or current advertised pricing. Remaining
budget is insufficient for a complete new scenario, so live tests stopped.
Three repeats per scenario, live scroll/filter and cross-page workflows remain
uncovered. Offline safety tests do not replace those model runs.

## Native Verification

`ORCA_BROWSER_NATIVE_TEST=1 go test ./browserhost -count=1 -timeout=4m -v`
passed (69.419 s), including:

- Real native Chinese fill/click, mutation invalidation, fixed DOM references,
  blocked overlays and focus redirection, closed-shadow screenshot denial.
- Project-cookie sharing, separate-profile isolation and cookie/localStorage
  removal; stale navigation fencing, native zoom and explicit network errors.
- iframe/closed-shadow/protected-descendant rejection and ordinary descendant
  acceptance, partial-input errors never classified as safely replayable.
- Bounded nonblocking native event delivery; overflow fails closed rather than
  silently losing takeover/navigation/permission events.
- Six download cases: denied, approved with independently checked file hash,
  active cancel, approval timeout, pending/context cancel and tab closure.
- Eight upload cases: unapproved picker, approved snapshot, DOM mutation,
  observation change, multiple files, cancellation, cross-process iframe denial
  and tab closure.

The minimal native Wails fixture and the production frontend fixture have
separate evidence and must not be conflated. See
`browser-preview-acceptance.md` for exact hosting-test boundaries.

## Known Preview Restrictions

- There is no claim of full browser compatibility. Frames, shadow components,
  CAPTCHA, passwords and unsupported focused key input require manual handling.
  AI form fill is a guarded DOM value/change operation, not OS keystroke typing.
- Native JavaScript alert/confirm dialogs are disabled in this preview to avoid
  unowned blocking modal windows. Sites depending on them may need another
  browser. Authentication, CAPTCHA and payments are never automated here.
- A stopped/taken-over task cannot resume in the same parent turn. The user must
  start a new turn, which obtains a new observation and approval.
- An unapproved cross-origin redirect stops the task; it is not silently replayed
  after permission. Exact origin/model and mutation confirmations are intentionally
  conservative, even when the main conversation has broad permission.
- On this machine a 248 KiB local download received all bytes in under a second,
  but WebView2 reported Completed after about 60 seconds. The UI reports the real
  finalizing phase and allows cancellation. The engine-side cause is unconfirmed;
  no security checking was disabled or fake completion added.
- Screenshot evidence from the native browser is not automatically evidence of
  the whole application chrome. Synthetic UI images and actual native images
  are labeled separately.
- Linux desktop cross-compilation passed; browserhost also builds for macOS.
  A full macOS desktop cross-build from this Windows toolchain is blocked by the
  existing native systray/CGO dependency. No macOS/Linux browser UI is exposed.
- Race testing needs a C toolchain unavailable on this machine. Repeated ordinary
  tests and ownership/fault tests are not presented as a race-detector run.
- No real personal account login, production-site compatibility sweep, real
  manual takeover, system DPI, or upgrade/install validation was performed.
  This package does not install over or upgrade the formal application.
- Browser child request usage is captured by the live-test ledger, but is not
  yet emitted into the production token/fee ledger. The main `browser_task`
  lifecycle is observable; its displayed main-task cost is not the complete
  browser task cost. This is an explicit preview integration gap.

## Local Verification Commands

From the repository root: `go test ./... -p=2 -count=1`.
From `desktop`: `go test . -count=1` (latest pass 44.336 s), native tests above,
and opt-in production Wails tests. The live fixture is build-tagged
`windows,production,browserlive`; do not run it without the cumulative budget.

The final delivery report records frontend, production-window and executable
hash results after the final bundle has been built. No release is implied by
this audit document.

## Final Local Gate

- Root Go suite passed. Desktop Go suite passed again after pause integration
  (46.221 s). The native suite after the ABI fix passed (68.946 s); targeted
  native/adversarial scenarios passed three times. Pointer/GC regression tests
  passed 100 repetitions in the independent review and a focused main rerun.
- Frontend `npm run test:all` including pre/post suites and `check:css` passed.
  The production frontend build passed. Synthetic browser layout/interaction
  suite: 118 assertions, 19 cases, zero failures or console/page errors. Matrix:
  Modern/Classic, English/Chinese, 760/1024/1366/1920 pixels, with real hit-tests
  for the Globe control. A Classic search-area overlap was fixed and retested.
- The production Wails fixture passed repeated native geometry/navigation and
  Settings-overlay checks, followed by a true first-run Skip/Globe flow. The
  frozen final frontend bundle passed that flow again. Parent `PrintWindow`
  captures remain stale/blank and are not accepted as whole-window rendering
  evidence; native WebView2 page PNGs and synthetic layout PNGs remain separate.
- JS: `index-VFhgQtMK.js`, SHA-256
  `f704fdc33ee4e66d059136983085d41eb206e15dfe44704beead40d892dcd71a`.
- CSS: `index-CTl9hVVy.css`, SHA-256
  `671ebc1680deccd0f45feb1844c57aea3f1d5acb65bb4cb44f4f5429aeac67f0`.
- Windows Wails production build passed. Executable: `OrcaBrowserPreview.exe`,
  23,971,840 bytes, resource version `3.0.14.0`, product version
  `3.0.14-browser-preview.1`. SHA-256:
  `315d2838b49f752e4179a235f5172655ecd522026d1efb9a3c45244f5b3e2515`.
- An isolated executable startup smoke remained alive for eight seconds with
  the preview title and its own generated home. Only that test-owned process
  was terminated afterward; this is not a normal-shutdown or user-task test.
- Pinned Node runtime and every CodeGraph runtime file were verified against
  the existing immutable Windows release inputs. Node/CodeGraph licenses and
  third-party notices accompany the portable build. Stable resource input files
  were restored byte-for-byte; there is no installer or update-feed mutation.
- Base revision is `d23890928acf8bf6aede558959670027445829ac`; preview source
  modifications are local working-tree changes, not a published commit/tag.

## Native ABI Finding During Final UI Gate

Two production-window runs exposed a nil browser interface during controller
creation, despite earlier minimal/native scenarios passing. The COM forwarding
helpers accepted pointer-valued `uintptr` arguments without escape directives;
compiler escape analysis confirmed the out variables remained on the moving Go
stack. The wrappers now use `//go:uintptrescapes`, retain typed object pointers
through the call and retain UTF-16 pointers until the actual call site. Empty
interfaces return a recoverable error instead of dereferencing nil. The compiler
now reports both affected out variables moved to the heap. The final native and
production-window reruns are required after this change; earlier passes are not
substitutes. The pointer-lifetime rule follows the
[Go compiler directives](https://pkg.go.dev/cmd/compile#hdr-Compiler_Directives).
