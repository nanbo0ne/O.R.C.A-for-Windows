# O.R.C.A. Desktop 3.0.15 Release Runbook

This is the build and release checklist for `desktop-v3.0.15`. Release only from the reviewed final commit, using tag `desktop-v3.0.15` and `.github/workflows/release-desktop.yml`.

## Required gates

- Root `go test ./... -count=1 -p=1`; in `desktop`, `go test . -count=1`. The `internal/sandbox` package must pass on Linux with `bwrap` installed as well as on Windows without it.
- In `desktop/frontend`, `npm ci`, `npm run test:all`, and `npm run build`. Generate Wails bindings before the frontend tests (`wails generate module`).
- The Windows hosted-runner installer test upgrades the published 3.0.13 installer. Do not run it on a personal workstation.
- Verify package/archive integrity, version metadata (`wails.json`, `build/windows/info.json`), SHA-256 sums, Minisign signatures, and the manifest through the workflow and `desktop/cmd/sign`.
- Windows requires SignPath by default; an unsigned build needs an explicit `workflow_dispatch` with `allow_unsigned_windows: true` and must be disclosed. Report macOS signing/notarization based on actual results.
- Review the draft release and every asset before publishing. Do not mutate historical releases or overwrite prior immutable assets.
- Keep version references consistent: `wails.json`, `build/windows/info.json`, `README.md`, `README.en.md`, `CHANGELOG.md`, `site/src/pages/index.astro`, `site/src/pages/docs.astro`, and the pinned expectation in `desktop/release_workflow_test.go`.

## Acceptance boundary

WhaleStack sign-in depends on `login.aichat.diy` and `router.aichat.diy` being reachable from the user's machine. Release checks use synthetic credentials only; no production account sign-in is performed on a user's personal computer. Windows remains unsigned and macOS unnotarized unless the actual release configuration provides those credentials; Minisign is not a substitute for either.
