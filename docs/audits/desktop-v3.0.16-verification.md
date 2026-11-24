# O.R.C.A. Desktop 3.0.16 Verification

## Scope and privacy

This release carries the built-in browser from the beta line into the official build and keeps the WhaleStack account sign-in prepared in 3.0.15. Checks use synthetic credentials against the login service and a local test page for browser control; no real account password, personal conversation, model configuration or service log was read or recorded. The relay key returned by the login service is never written to any document.

## Changes under test

- `browserControlEnabled()` is true for every real channel while `browserPreviewEnabled()` stays false, so the official window keeps its own identity, data directory, window class and single-instance lock.
- The four browser feature gates (`browser_app.go` three sites, `browser_tool.go`) use `browserControlEnabled()`; the account-model gate in `browser_tool.go` still uses `betaEnabled()` because the ORCA account runtime remains beta-scoped.
- Every browser action goes through the approval path before execution; the beta audit `docs/audits/orca-beta-r10-20261116.md` records the sandbox results for notification, tab and popup behaviour.
- `WhaleStackSignIn` posts the entered account to `POST /api/app/login`, decodes `{email, name, baseUrl, apiKey, keyName}`, reads `{baseUrl}/models`, and only then registers the provider and writes the key.
- The login service derives one relay key per account (`orca` + 28 hex characters), creates it once as `ORCA 桌面端`, and reuses it on later sign-ins.
- The Linux sandbox tests compare `bwrap` by base name, so the release gate passes on Linux runners with bubblewrap installed.

## Release evidence

- `go test ./... -count=1 -p=1` in the repository root; `go test . -count=1` in `desktop`.
- Frontend: `npm ci`, `npm run test:all`, `npm run build` in `desktop/frontend`, after `wails generate module`.
- Version metadata: `desktop/wails.json` and `desktop/build/windows/info.json` both read 3.0.16.
- Checksums are generated after the optional signed manifest and before publication; every asset in the release carries a matching `.minisig`.

## Signing status

Windows packages carry no Authenticode publisher signature in this release because no SignPath configuration is present. Minisign signatures cover each artifact and `latest.json`; they verify update integrity, not operating-system publisher trust. macOS packages are not notarized.
