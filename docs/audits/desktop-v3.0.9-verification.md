# O.R.C.A. Desktop 3.0.9 Verification

## Scope and Current Status

This release restores the standard NSIS/MUI2 wizard and replaces the installer
process check with a native, installation-path-scoped helper. Main application
Modern/Classic layouts and the existing feature-disable policies are unchanged.

The Windows preview passed hosted acceptance in
[run 35320328576](https://github.com/nanbo0ne/O.R.C.A-for-Windows/actions/runs/35320328576),
source `4626f7047aa76132eac8e1dd87ab2a64fb760be6`. Production packages are built
separately without the preview caption and must repeat the release gates.
Formal publication and public-download evidence are recorded below.

## Verified Preview Behavior

- No-process checks return immediately: 36-39 ms locally, 5 ms on the hosted
  runner. These timings exclude unpacking and WebView2 installation.
- Normal exit, self-exit, unresponsive background processes, cancellation,
  another installation's process, path/creation-time identity, live ancestry,
  invalid manifests, junctions and external file locks have regression coverage.
- The new current-user shutdown channel requests a real exit rather than
  close-to-background, flushes drafts/attachments and snapshots sessions.
- Native status publishing tolerates a short-lived Windows INI reader and
  cannot report success after a final status-write failure.
- Preview installation, 3.0.8 upgrade with a running synthetic process, Chinese
  paths, directory precedence and uninstall passed. 689 payload files matched;
  the final uninstall retained all 17 synthetic user-data markers.
- Frontend tests, desktop tests, helper/IPC tests, NSIS CRC, archive integrity
  and Minisign verification passed. The UI was inspected in native NSIS windows,
  not a browser recreation.

## Boundaries

The screenshot's original incident cannot be reconstructed without its logs.
The fixes address confirmed wait, shutdown and diagnostic problems rather than
claiming the exact historical cause. A memory-mapped file can escape a
nondestructive preflight check; actual NSIS extraction still forbids skipping
files. Forced termination of an unresponsive old application cannot recover
unsaved in-memory content it never persisted.

Welcome, license, directory, options, external-lock and cancel pages were
inspected. Progress/finish/uninstall pages were not all manually captured.
Installation automation runs only in disposable Windows, not on the user's
workstation. A complete manual Wails in-app upgrade with a real unsent draft
has not been performed; the draft handshake is covered separately by tests.
No system-DPI project or Windows ARM64 installation claim is included.

Minisign authenticates update files; it is not Windows Authenticode or Apple
notarization. Local race tests lack a C compiler; the production workflow's
existing Linux concurrency gate remains mandatory.

## Formal Release: 2026-09-18

- Source and tag `desktop-v3.0.9` resolve to
  `d96ff0637ad601c8394aca23e6d4ae992d597da0`.
- [Production run 35324032695](https://github.com/nanbo0ne/O.R.C.A-for-Windows/actions/runs/35324032695)
  passed core, Linux race, frontend, three-platform packaging and native desktop
  tests. Windows repeated fresh installation, upgrade from the pinned 3.0.8
  package with a synthetic background process, Chinese paths, payload hashes,
  uninstall and synthetic-data retention against the formal installer.
- The formal installer is 91,192,109 bytes, NSIS CRC `29bb7c57`, SHA-256
  `89efd5e03de9848988189901ce90fe0ec761a980c64b16e5bda0c2b9c14a7723`.
  The packaged application reports product/file version `3.0.9.0`.
- All 17 GitHub assets match local sizes and SHA-256 digests. Manifest validation,
  all eight payload/manifest Minisign verifications, Windows/macOS ZIP integrity,
  and server-side checksum validation passed. No preview or old-brand duplicate
  installer was published.
- GitHub release ID `391323685` became public at `2026-09-18T08:42:26Z`.
  Mac packages were verified in a new directory before the atomic site switch;
  the prior `20260918-orca308/orca` directory remains available for rollback.
- The signed public manifest reports `v3.0.9`. Missing update/package paths return
  404. Full public downloads of the Windows installer, portable ZIP, Linux DEB
  and universal DMG matched expected sizes, digests and signature files.
  The actual application updater evaluated a 3.0.8 client, downloaded the
  complete Windows installer from the Mac source and verified signature and
  SHA-256. Transfer plus verification took 54.081 seconds in this test; this is
  not a bandwidth guarantee or a manual in-app installation test.
- The Mac-hosted download page now links to 3.0.9. An additional GitHub Pages
  dispatch, run `35325853911`, built successfully but its deployment API returned
  404 (Pages unavailable/not enabled). No Pages settings were changed. This does
  not affect GitHub Releases or the Mac-hosted website/update service.

Local release files: `D:\AI-Reasonix\dist\desktop-v3.0.9`.
Detailed transfer logs and probe results are retained in the implementation
worktree under `.tmp/release-v309`; signing credentials are not included.
The manual-testing and publisher-signing boundaries above still apply.
