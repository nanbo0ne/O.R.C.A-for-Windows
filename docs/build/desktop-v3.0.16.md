# O.R.C.A. Desktop 3.0.16 Release Runbook

This is the build and release checklist for `desktop-v3.0.16`. Release only from the reviewed final commit, using tag `desktop-v3.0.16` and `.github/workflows/release-desktop.yml`.

## Required gates

- Root `go test ./... -count=1 -p=1`; in `desktop`, `go test . -count=1`. The `internal/sandbox` package must pass on Linux with `bwrap` installed as well as on Windows without it.
- In `desktop/frontend`, `npm ci`, `npm run test:all`, and `npm run build`. Generate Wails bindings before the frontend tests (`wails generate module`).
- Verify package/archive integrity, version metadata (`wails.json`, `build/windows/info.json`), SHA-256 sums, Minisign signatures, and the manifest through the workflow and `desktop/cmd/sign`.
- Windows requires SignPath by default. This release has no SignPath configuration, so dispatch stable from `main` with `allow_unsigned_windows: true`, and disclose the missing Authenticode signature in the release notes.
- A tag push cannot pass the Windows signing guard on its own; the stable channel is published through a manual dispatch from `main` whose commit is the tagged commit.
- Review the draft release and every asset before publishing. Do not mutate historical releases or overwrite prior immutable assets.
