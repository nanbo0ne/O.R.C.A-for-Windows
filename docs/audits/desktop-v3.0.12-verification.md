# O.R.C.A. Desktop 3.0.12 Verification

## Fixes and local evidence

Synthetic Agent regressions reproduced false terminal readiness errors before the fix. Five cases now cover recovered actions, rejected checklist updates, rejected sign-off, and the latter two following an earlier action failure. Each checks request counts, one final answer, preserved pending todos and rejection of unsupported completion claims. Existing tests retain terminal errors for real new failed verification.

HTTP/SSE regressions cover delayed consumers, saturated delivery, byte activity in fragmented lines, cancellation, and terminal chunks on a channel left open. Three synthetic Agent turns exercise six HTTP requests with reasoning and split tool arguments. Sustained consumer blockage reports an error without replay and a subsequent request remains usable.

Before release metadata changes, root Go, desktop Go, frontend all-tests and Windows Wails production build passed. The local 3.0.11 preview also passed NSIS CRC, archive extraction and exact embedded-application SHA-256 comparison. This evidence is not a substitute for the new version's CI package checks.

## Release gates

Release validation completed on 2026-09-22. [CI run 35709000387](https://github.com/nanbo0ne/O.R.C.A-for-Windows/actions/runs/35709000387) passed core, race, frontend, all three native desktop platform builds/tests, Windows package integrity, actual 3.0.11 installer upgrade/data retention, and package/manifest signatures. GitHub published `desktop-v3.0.12` at 09:39:12 UTC, targeting `6af5c50608dae122c7ab298b55794e27c3fbe7ba`. The historical tag and packages for 3.0.11 remain unchanged.

All 17 release assets downloaded locally matched GitHub sizes/digests and `SHA256SUMS.txt`; all eight payload/manifest Minisign signatures verified with the embedded update public key. NSIS CRC is `3feb5d32`. Archives passed integrity checks. The installer and portable archive contain the same `Orca.exe`, version 3.0.12.0, SHA-256 `aa63177cb8ef46e1476f889f1e487482451492e9b7a97b33a70a171330dbffc3`.

The Mac origin verified complete staged files before atomically switching the static site root. Public `latest.json` matches GitHub exactly, reports v3.0.12 and verifies with the downloaded signature. The public home page matches the clean source build. Missing update JSON returns 404. Windows installer/portable, macOS DMG, Linux DEB and source ZIP HEAD checks return 200 with correct lengths. A public 1 MiB installer Range response returns 206 and matches the verified package prefix. This is not a full public installer download or native updater UI test.

The separately delivered `O.R.C.A-v3.0.12-source.zip` is 8,361,247 bytes, SHA-256 `13dd57a95ed240111316bb37cfcec7e6a5316839183049f34b06f8d1ae92a150`. Its 1,244 files match Git blobs at the release commit byte-for-byte. Only the tracked top-level legacy `release/` binary directory is excluded. Source ZIP integrity, isolated Agent/evidence/provider tests, generated Wails bindings, frontend build and static site build passed. A complete source ZIP downloaded from the public Mac URL matches this digest. `SOURCE-DELIVERY.md` labels the archive and explains reuse.

Delivery: `D:\AI-Reasonix\dist\desktop-v3.0.12\`. Large GitHub downloads repeatedly stalled on this network; bounded Range retries completed and full hashes/signatures were verified. No general download-speed improvement is claimed. Source archives have SHA-256 delivery records; they are not represented as Minisign-signed updater payloads. Windows remains without Authenticode and macOS without notarization.

No user conversations, configuration, production model prompts or service logs were read for this repair. No Computer Use or native workstation interaction was performed. Full native-window interaction, manual multi-display checks, and production-provider certification are not claimed. Platform/installer CI evidence must be distinguished from interactive desktop acceptance.
