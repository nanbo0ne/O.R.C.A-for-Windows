# O.R.C.A. Desktop 3.0.19 Release Runbook

This is the build and release checklist for `desktop-v3.0.19`. Release only from the reviewed final commit, using tag `desktop-v3.0.19` and `.github/workflows/release-desktop.yml`.

## Required gates

- Root `go test ./... -count=1 -p=1`; in `desktop`, `go test . -count=1`. The `internal/sandbox` package must pass on Linux with `bwrap` installed as well as on Windows without it.
- In `desktop/frontend`, `npm ci`, `npm run test:all`, and `npm run build`. Generate Wails bindings before the frontend tests (`wails generate module`).
- Browser host: `go test ./browserhost/... -count=1` on Windows. The two native fixtures are opt-in because they own a real hidden WebView2 window: `ORCA_BROWSER_NATIVE_TEST=1` for the isolated local fixture, and `ORCA_BROWSER_LIVE_TEST=1` for the public practice site (needs network).
- Verify package/archive integrity, version metadata (`wails.json`, `build/windows/info.json`), SHA-256 sums, Minisign signatures, and the manifest through the workflow and `desktop/cmd/sign`.
- Windows requires SignPath by default. This release has no SignPath configuration, so dispatch stable from `main` with `allow_unsigned_windows: true`, and disclose the missing Authenticode signature in the release notes.
- A tag push cannot pass the Windows signing guard on its own; the stable channel is published through a manual dispatch from `main` whose commit is the tagged commit.
- Review the draft release and every asset before publishing. Do not mutate historical releases or overwrite prior immutable assets.

## This release touches

- `desktop/browserhost/native_windows.go` — script-dialog handler, per-target staleness signature, extended action dispatch.
- `desktop/browserhost/world_windows.go` — approved-hit test now admits open shadow roots inside the approved target.
- `desktop/browserhost/internal/webviewabi/abi_windows.go` — `ICoreWebView2ScriptDialogOpeningEventArgs`.
- `desktop/browser_tool.go` — action validation and the `browser_action` schema.
- `desktop/app.go`, `desktop/settings_app.go` — window owner id passed to every controller rebuild so the browser tools stay attached.
- `desktop/frontend/src/App.tsx`, `styles.css`, `locales/{zh,en}.ts` — the tabbed side dock.

ABI note: the dialog args interface sits between `AddScriptDialogOpening` and `AddPermissionRequested`; if the vtable is ever reordered, the native fixture `TestNativeIsolatedBrowser/native-zoom-and-script-dialog-suppression` fails loudly instead of corrupting the heap (a wrong `get_Kind` read does exactly that).
