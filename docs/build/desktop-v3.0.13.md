# O.R.C.A. Desktop 3.0.13 Release Runbook

Preparation checklist; acceptance has not yet been completed.

This is a preparation checklist, not a record of completed acceptance. Release only from the reviewed final commit, using tag `desktop-v3.0.13` and `.github/workflows/release-desktop.yml`.

## Required gates

- Run root `go test ./... -count=1 -p=1`; run the CI race gate, including `internal/monitor`; in `desktop`, run `go test . -count=1`.
- In `desktop/frontend`, run `npm ci`, `npm run test:all`, and `npm run build`. Run the native desktop tests and builds on the workflow's macOS, Windows, and Linux runners.
- The Windows hosted-runner installer test upgrades the published 3.0.12 installer. Its pinned official baseline is `O.R.C.A-for-Windows-windows-amd64-installer.exe`, 91,212,149 bytes, SHA-256 `3bdc8cbd0ae418684829526afc2650b9d870a392002a29b345df5cefb7d95b1b`. The test also checks the published `SHA256SUMS.txt` and synthetic retained data. Do not run it on a personal workstation.
- Verify package/archive integrity, version metadata, SHA-256 sums, Minisign signatures, and manifest with the workflow and `desktop/cmd/sign`. Windows requires SignPath by default; an unsigned override must be an explicit authorized dispatch and clearly disclosed. Report macOS signing/notarization based on actual configuration and results.
- Review the GitHub draft and all assets before publishing. Stage site/update files separately, verify sizes/hashes/signatures and public manifest, then use the established atomic deployment procedure. Do not mutate historical releases or overwrite prior immutable assets.
- Build the source ZIP with `git archive` from the exact final release commit, excluding the top-level historical `release/` directory. Verify file inventory against Git, ZIP integrity, and SHA-256 before replacing any prior delivered source archive. Keep the delivery record outside the archive.

## Acceptance boundary

The visible-history persistence/compaction, monitor privacy and bounds, context usage, guidance, rich image/file paths, paste routing, and permission-dialog changes require regression coverage. Release acceptance is pending until CI, artifact/signature checks, draft review, source archive verification, and deployment checks are recorded in `docs/audits/desktop-v3.0.13-verification.md`.

No production model requests and no native interaction on a user's personal computer are authorized or claimed. Windows remains unsigned and macOS unnotarized unless the actual release configuration provides and verifies those platform credentials; Minisign is not a substitute for either.
