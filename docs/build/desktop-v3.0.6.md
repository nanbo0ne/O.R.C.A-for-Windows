# O.R.C.A. Desktop 3.0.6 Release Runbook

Updated 2026-09-17 (UTC+8). Local implementation, synthetic/browser regression,
and real local-model protocol tests passed. Native CI and delivery gates below
remain pending until completed results are recorded.

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
| Native Windows/macOS/Linux builds and tests | Pending CI |
| Linux race tests | Pending CI |
| Windows 3.0.5 upgrade/retention and uninstall | Pending hosted CI |
| Package integrity, SHA-256, Minisign, NSIS CRC | Pending final artifacts |
| Public update and downloads | Pending publication |

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

Pending completion of the gates above. Local test evidence lives under
`.tmp/release-v306/`; no secrets or user screenshots are publication assets.
