# O.R.C.A. Desktop 3.0.8 Verification

Verification date: 2026-09-18 (Asia/Shanghai).
Source: `0b2d30a7d48a675b45024a13bb73c729e99c9034`.
Release: `desktop-v3.0.8`.

## UI and Regression Tests

Modern layout verification: 362 browser cases, 3990 assertions, 38 screenshots,
zero failures. The 60 Classic comparisons have identical geometry and computed
styles. Attachment layout adds 24 passing assertions. See the
[UI verification](2026-09-18-modern-chat-polish.md) for scope, screenshots and limits.

Frontend full tests, CSS checks, production build and the local Windows Wails
review build passed. The release CI also passed core tests, the selected race
tests, frontend tests/build, and all three native desktop builds and test suites.

[CI run 35254508087](https://github.com/nanbo0ne/O.R.C.A-for-Windows/actions/runs/35254508087)
failed only in the final GitHub upload step with HTTP 400. It is not reported as
an entirely successful workflow. Its completed build artifacts were recovered,
checked against the GitHub artifact SHA-256 values and unpacked without rebuilding
the application. Existing successfully uploaded assets were retained and checked.
The manifest was regenerated with the same release tool and signed locally with
the existing Minisign key; the client's embedded public key verified it.
The final installer upload completed in
[recovery run 35261905511](https://github.com/nanbo0ne/O.R.C.A-for-Windows/actions/runs/35261905511).
That job downloaded the original Windows artifact, verified the exact installer
digest, and uploaded it without rebuilding. Only incomplete entries in this
unpublished draft were removed during recovery.

## Installer and Package Integrity

- Windows hosted acceptance passed installation, upgrade from its pinned 3.0.5
  baseline, retention of synthetic configuration/session/model data and uninstall.
  The harness did not run against the user's installed application.
- NSIS CRC: `21b42bfb`; installer size: `88808549` bytes.
- Installer SHA-256:
  `708a94d7a97690ea1e5abd8e9fa6c0fcd4a0edf334ac5eb6f2c11b8e593c13b0`.
- Installer and portable archive contain identical `Orca.exe` bytes, PE version
  `3.0.8.0`, product name `O.R.C.A. for Windows`.
- Executable SHA-256:
  `369f1a966856017ac373d1d46f835fe48479eaabac76707e8106879a251668f4`.
- All seven package signatures and the manifest signature verify against the
  updater's embedded public key. Package/archive checks and the release validator
  passed. The manifest has five platform entries and four unique update payloads.
- The 17-file distribution contains seven packages, eight detached signatures,
  `latest.json` and `SHA256SUMS.txt`. No old-brand compatibility copies were added.

Local distribution: `D:\AI-Reasonix\dist\desktop-v3.0.8\`.

## Delivery

- Published at 2026-09-18 03:01:40 +08:00:
  [GitHub Release](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.8).
  The tag resolves to the source commit above. All 17 assets have matching local
  size and GitHub SHA-256 digests; no incomplete upload entries remain.
- Mac website and signed stable manifest were atomically switched to
  `/usr/local/aichat/srv/site-gateway/releases/20260918-orca308/orca`.
  The previous 3.0.7 directory remains available for rollback. Other sites were
  unchanged. Public website, manifest bytes and detached signature match the
  staged 3.0.8 files. Missing manifest and package paths return HTTP 404.
- The actual updater, using current version `v3.0.7`, discovered `v3.0.8` and
  downloaded the full 88,808,549-byte installer from the public Mac source.
  SHA-256 and Minisign passed; package transfer/verification took 29.792 seconds,
  and the complete test took 32.34 seconds. No GitHub fallback was used. This is
  one local network measurement, not a guaranteed download speed.
- All other unique manifest payloads (Windows portable ZIP, macOS Universal DMG
  and Linux DEB) were downloaded from the public Mac source and verified with the
  updater's signature and digest checks. The additional test passed in 44.34
  seconds. Public detached signatures also match the local files.
- GitHub's public installer endpoint returned HTTP 206 for a 16 KiB Range request;
  every returned byte matches the local installer. This was a link/Range probe,
  not a second full installer download from GitHub.
- Final delivery verification passed at 2026-09-18 03:04:25 +08:00. Both README
  languages, release notes and the live download site describe 3.0.8 consistently.

## Limits

Native startup, maximize and dock layout were reviewed using isolated profiles.
The test profile once reported a `desktop-tabs.json` atomic replacement access
error. It did not recur after restart, and 30 additional migration/save test
rounds passed. The cause is unconfirmed; no persistence fix is claimed.

The UI suite uses a simulated provider/controller. It does not establish real
model-service behavior; this patch does not change provider protocols. Native
menu interaction and system DPI are not claimed as comprehensive acceptance.
Classic retains its existing design. Managed local AI and Computer Use remain
disabled. Windows Authenticode signing and macOS notarization are unavailable;
Minisign verifies update authenticity, not OS publisher identity.

Detailed release evidence is in the worktree's `.tmp/release-v308/`, including
CI logs, original artifact digests, archive checks, staging logs and transfer logs.
No historical release asset, tag or signature was replaced.
