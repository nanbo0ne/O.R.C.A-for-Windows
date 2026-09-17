# O.R.C.A. Desktop 3.0.7 Verification

Release source: `ee0c92f6cb9888b12f312937d4ac3a195cc981c5`.
[Native workflow 35221616155](https://github.com/nanbo0ne/O.R.C.A-for-Windows/actions/runs/35221616155)
passed core, race, frontend, all three native desktop suites and builds, and full
Windows installer upgrade/data-retention/uninstall acceptance. The Windows log
records `Installer acceptance: passed` at 2026-09-17T12:45:00Z.

Local desktop `go test ./... -count=1` passed (desktop package: 83.149 seconds).
All 17 delivered files match GitHub size/digest metadata and checksums. All eight
Minisign signatures verify against the embedded updater public key; seven package
archives, including NSIS, pass integrity checks. Installer and portable app hashes
match and Windows PE resources report `3.0.7.0`.

Installer: 88,808,325 bytes; NSIS CRC `8571612f`; SHA-256
`8a2fbb9d46ef63759e24cbc869c7ce947004d77432de95ee0c34b98e2b510d4f`.

Public updater test: the actual application updater evaluates current `v3.0.6`,
discovers `v3.0.7`, downloads the complete installer from Mac, and validates its
signature and SHA-256. Observed download time: 225.940 seconds, not a bandwidth
guarantee. The user's installed application was not modified for this test.

Public delivery verification completed at 2026-09-17T21:38:03+08:00: the signed
manifest and signatures match release assets, nonexistent package/manifest URLs
return 404, and complete Windows installer/ZIP, macOS DMG and Linux DEB downloads
match their expected sizes and SHA-256 values. The final check reuses completed
public downloads and revalidates them, rather than downloading them repeatedly.

Network limitations remain: a GitHub direct-route download stalled, and curl
public-package transfer was interrupted after a stall. The Mac staging transfer
used bounded Range resume; actual updater downloads completed and verified.
No claim is made that all network routes or connection speeds are fixed.

Evidence is stored locally under `.tmp/release-v307/`: `all-ci.log`,
`windows-ci.log`, `updater-public.log`, `public-other-final.log`, and
`delivery-verification.json`. No credentials or user data are included.

Local regression exercises the actual NSIS helper using isolated 64-bit,
windowless processes. Cases cover automatic target termination, preservation of
an unrelated same-name process, unreadable paths under redirected PowerShell,
an external exclusive file lock, and retry after the lock is released.

Hosted Windows acceptance installs the pinned public 3.0.5 baseline, substitutes
a synthetic background executable in the owned installation directory, then
performs a full upgrade while that executable is running. It checks installed
payload hashes, metadata, synthetic user data retention, an explicit alternate
install directory, and uninstall. This is not a live-user-task recovery test.

The installer is not transactional: disk exhaustion or a new file lock during
copy can still abort a partially completed installation. Files cannot be skipped;
retry the installer after resolving the cause. Preflight checks do not constitute
a guarantee against filesystem changes after checking.

Manual upgrades request normal window closure and allow 30 seconds before
terminating remaining exact-path processes. This cannot guarantee preservation
of an unsaved active task; save tasks first. In-app updates separately require
idle work and persist session snapshots before quitting.

Do not execute the hosted installer acceptance harness on a user workstation.
Do not overwrite published 3.0.6 packages, signatures, or tags.
