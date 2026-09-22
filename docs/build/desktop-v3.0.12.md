# O.R.C.A. Desktop 3.0.12 Release Runbook

Use `desktop-v3.0.12` and the source at its exact commit. See `desktop/README.md` for platform dependencies. Install the Go version in `go.mod`, Node.js 22, npm, and Wails v2.12.0. Windows packaging needs NSIS; macOS DMG and Linux DEB are built on native CI runners.

Run root `go test ./... -p=1 -count=1`; in `desktop`, run `go test . -count=1`; in `desktop/frontend`, run `npm ci`, `npm run test:all`, and `npm run build`. Run the race suite in CI with a supported C compiler. `scripts/desktop-build.sh` builds release payloads; `.github/workflows/release-desktop.yml` defines native platform builds, signature checks and isolated Windows installation tests.

Upgrade acceptance pins the official 3.0.11 installer, checks its SHA-256, installs it on an isolated hosted Windows runner, then upgrades to 3.0.12 and checks synthetic retained data. Do not run that script on a personal workstation.

Prepare a draft release first. Verify all payload sizes, archive integrity, SHA-256 and Minisign before publication or switching the Mac stable manifest. Keep published 3.0.11 files unchanged. Never publish a local unsigned preview as a signed release payload.

Source archives must come from `git archive` of the release commit, not the working directory. See `docs/SOURCE_REUSE.md`. No `.git`, local data, build cache, untracked diagnostic reports or credentials belong in the archive.
