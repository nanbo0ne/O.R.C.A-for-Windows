# O.R.C.A. Desktop 3.0.19 Verification

## Scope

Changes under verification: the tabbed side dock, the browser host's staleness model, its script-dialog handling, the extended action set, shadow-root and frame observation, and the browser tools staying attached across controller rebuilds.

## Automated checks

- `go build -tags production ./...` in `desktop` — clean.
- `go test . -count=1` in `desktop` — pass (111s), including `TestBrowserActionValidation`, which now asserts that `Enter`, `Tab`, `ArrowDown` and the new `waitfor`/`hover`/`dblclick` kinds are accepted, while free-form key input (`{"kind":"key","key":"a"}`) is still refused.
- `go test ./browserhost/... -count=1` — pass.
- `ORCA_BROWSER_NATIVE_TEST=1 go test ./browserhost -run TestNativeIsolatedBrowser -count=1` — pass, 9 subtests, 2.5s. Includes `mutation-invalidates-input`, which required the new per-target staleness check: removing the old whole-page revision gate without it let a click land on a button whose text had since changed.
- `go test ./browserhost -run TestNativeIsolatedBrowser -count=1` with the dialog handler disabled — the run aborts with `STATUS_HEAP_CORRUPTION` (0xc0000374). That is how the wrong `get_Kind` read (enum read as a wide string, then freed) was found and fixed.
- `ORCA_BROWSER_LIVE_TEST=1 go test ./browserhost -run TestNativeLiveSite -count=1` — pass, 58s, 15 scenarios against `https://the-internet.herokuapp.com/`.
- `desktop/frontend`: `tsc --noEmit` clean; `check-css-syntax` and `check-z-index-tokens` clean; `overview-dock`, `workspace-layout`, `responsive-layout`, `ui-style`, `ui-style-contract`, `settings-panel-contract` all pass.

## Live site evidence

Fixture: `desktop/browserhost/native_live_site_windows_test.go`, opt-in with `ORCA_BROWSER_LIVE_TEST=1`. It drives the shipping host (`New`, `Create`, `Present`, `Observe`, `Execute`) in its own hidden window and temporary profile.

| Scenario | What it proves | Result |
| --- | --- | --- |
| `/` homepage | A large page is observed (20+ elements) | pass |
| `/login` | Typing into a text field; a password field refuses model input; submitting reports the page's own validation | pass |
| `/forgot_password` | Full round trip: type, click, page-level confirmation text | pass |
| `/dropdown` | `select` by option value | pass |
| `/checkboxes` | `click` toggles state (verified against the pre-click value) | pass |
| `/add_remove_elements/` | Click inserts an element; the new element is observed; clicking it removes it | pass |
| `/key_presses` | `key` Escape reaches the page: "You entered: ESCAPE" | pass |
| `/javascript_alerts` | `alert` is auto-accepted, the page continues and reports success | pass |
| `/nested_frames` | 3+ frames counted, frame elements observed, frame document readable | pass |
| `/iframe` | TinyMCE's editor iframe is counted and observed | pass |
| `/shadowdom` | 2 shadow roots counted and readable | pass |
| shadow control (injected) | A control inside an open shadow root is observed and typed into | pass |
| `/dynamic_loading/1` | `waitfor` text after a click that starts an async load | pass |
| `/hovers` | `hover` reveals the caption (`opacity != 0`) | pass |
| `/tables` | Table content is present in the observed text | pass |

## Manual checks

- Side dock: opening a file, the changed-files list and a browser page from a conversation produces three tabs; closing one activates its neighbour; the `+` menu adds a tab; collapsing the dock restores the overview. Opening a page from the conversation does not replace an existing tab of another kind.
- Prompting: with full access selected, browser actions run without per-action prompts; switching to a restricted mode restores them.

## Known limitations

- Closed shadow roots stay invisible to page JavaScript; screenshots that would include one are refused rather than shared. The native host reports this as a blocked screenshot, not an empty one.
- Password, file and one-time-code fields are outside what the model may type into in every permission mode; the live `/login` case asserts that refusal.
- A page that replaces its own `<iframe>`/`<frame>` documents between observation and action surfaces the stale error and a re-read; that is intended.
- Windows builds are unsigned; macOS builds are not notarized.
