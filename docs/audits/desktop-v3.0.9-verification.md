# O.R.C.A. Desktop 3.0.9 Verification

## Scope and Current Status

This release restores the standard NSIS/MUI2 wizard and replaces the installer
process check with a native, installation-path-scoped helper. Main application
Modern/Classic layouts and the existing feature-disable policies are unchanged.

The Windows preview passed hosted acceptance in
[run 35320328576](https://github.com/nanbo0ne/O.R.C.A-for-Windows/actions/runs/35320328576),
source `4626f7047aa76132eac8e1dd87ab2a64fb760be6`. Production packages are built
separately without the preview caption and must repeat the release gates.
Final publication and public-download evidence will be appended after validation.

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
