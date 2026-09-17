# O.R.C.A. Desktop 3.0.6 Release Runbook

Published 2026-09-17 at 13:21:30 UTC+8. Release source and immutable tag target:
`058c9840899fe8abc4357e3759dabb35924ae893`.
[Workflow 35183322814](https://github.com/nanbo0ne/O.R.C.A-for-Windows/actions/runs/35183322814)
completed successfully before publication. A later documentation-only evidence
commit does not change the tag or signed files.

## Version and Baseline

- Target tag/version: `desktop-v3.0.6` / `v3.0.6`; Wails `3.0.6`; Windows resources
  `3.0.6.0`.
- Windows upgrade baseline: published `desktop-v3.0.5`, installer
  `O.R.C.A-for-Windows-windows-amd64-installer.exe`, 88,804,245 bytes.
- Baseline SHA-256: `ab824268dcf6b01807022ef3c606db67f11f32c72871069eac86dbe75c50bca3`.
- The hosted installer test verifies metadata, size and digest before installing.
  Synthetic configuration, sessions and model files must survive upgrade and
  normal uninstall. Never run that harness against the user's real installation.
- No repeat DeepSeek migration, DPI work, old-brand duplicate packages, or
  re-enabling managed local AI/Computer Use. Preserve dirty community data.

## Gate Status

| Gate | Status |
| --- | --- |
| Root, frontend and desktop suites | PASS; see [verification](../audits/desktop-v3.0.6-verification.md) |
| Real local model, route failure and tool continuation | PASS; isolated service-authenticated probe |
| Modern/Classic layout and provider editor | PASS; 127 layout cases/733 assertions and 27 editor assertions |
| Native Windows/macOS/Linux builds and tests | PASS on the release SHA |
| Linux race tests | PASS: agent, control, billing, local AI, config |
| Windows 3.0.5 upgrade/retention and uninstall | PASS: pinned baseline, recorded installation path, explicit alternate directory, payload and synthetic data retention |
| Package integrity, SHA-256, Minisign, NSIS CRC | PASS: 17 asset digests, 16 checksum records, eight signatures, seven archives; CRC `f01d7746` |
| Actual public updater | PASS: 3.0.5 detects 3.0.6; full Mac installer download and verification, 83.136 seconds |
| Public download matrix | PASS: signed manifest identical to release; full Windows installer/ZIP, macOS DMG and Linux DEB match; missing update/package paths return 404 |

## Publication Procedure

1. Review and commit only this patch, excluding `site/src/data/community.json` and
   all local credentials/evidence. Match workflow `headSha` to that source commit.
2. Dispatch `release-desktop.yml` on main with `channel=stable`,
   `tag=desktop-v3.0.6`, `allow_unsigned_windows=true`. Wait for all jobs, including
   race, native desktop, and Windows installer acceptance. This creates a draft.
3. Download every asset to `D:\AI-Reasonix\dist\desktop-v3.0.6\`. Verify size,
   digest, archive, NSIS CRC, product version and every Minisign signature. Validate
   the signed manifest; do not rewrite it after signing.
4. Stage those exact packages and the committed-source website in a new Mac
   release directory. Verify server-side SHA-256 before changing its public link.
5. Publish the verified GitHub draft, then atomically activate the Mac directory.
   Preserve the previous release for rollback and leave other sites untouched.
6. Check public HTML and manifest/signature hashes, missing-path 404s, complete
   package downloads and the actual application updater. Record URLs, source SHA,
   workflow ID and integrity evidence below. Never repoint a published tag.

The Windows publisher-signing and macOS notarization limitations are unchanged.
Minisign must not be described as either. A later documentation-only evidence
commit may update this runbook without changing the release tag or assets.

## Final Delivery

- [GitHub Release](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.6), 17 assets, marked latest.
- Local delivery: `D:\AI-Reasonix\dist\desktop-v3.0.6\`.
- Windows installer: `O.R.C.A-for-Windows-windows-amd64-installer.exe`, 88,807,663 bytes, executable version `3.0.6.0`.
- Installer SHA-256: `6bcdd6ceb9f8ec8b836ec5f245ca46e7a2b7ac33fc940ae4475d88c0bace3156`.
- Portable ZIP SHA-256: `6017f2f63955858d9ae76d9364fc7685b45a551c4241c44c8a88bb05869504a9`.
- Signed manifest SHA-256: `60a48011c4d7ddbb32f3aae9cb4c60aa18a238b89b72529248731916ebd16856`.
- [Download site](https://orca.aichat.diy/) and [stable manifest](https://orca.aichat.diy/updates/stable/latest.json).
- Mac directory: `20260917-orca306/orca`; previous `20260917-orca305/orca` retained for rollback. Public site HTML SHA-256: `753969e9c3bf76e33b90bb32967dfaaa9e9c1852d6e7eccefeb418e55d0010e0`.

Direct GitHub asset transfer on the workstation stalled with incomplete files.
The same assets were retrieved through the Mac's existing per-command proxy,
verified against GitHub digests there, then copied locally and verified again.
No machine/system proxy settings changed. Do not claim that GitHub direct-route
availability has been fixed. The app's full Mac-source installer transfer and
verification passed in 83.136 seconds (about 1.02 MiB/s), a single observation,
not a promised download speed.

Local test evidence lives under `.tmp/release-v306/`: native CI logs,
`local-model-live.log`, browser reports, `updater-public.log`, and
`delivery-verification.json`. No secrets or user screenshots are publication
assets. The front-end main bundle warning remains 991.99 kB / 284.26 kB gzip.
