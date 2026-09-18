# O.R.C.A. Desktop 3.0.10 Release Runbook

完成其余发布检查后，继续构建并公开发布到 GitHub 和 Mac 更新源，已有授权保持有效。
原生交互验收因 computer-use 暂停而暂缓，保留未验证说明，不将其写成已通过。

Build and public GitHub/Mac release remain authorized after the other release
checks succeed. Native interaction is deferred while computer-use is paused;
retain this coverage gap without claiming acceptance.
Record actual evidence in the [verification report](../audits/desktop-v3.0.10-verification.md).

Retain the standard NSIS/MUI2 installer restored in 3.0.9 without implementation
changes. Production builds omit ORCA_PREVIEW; do not rename preview outputs and
claim production acceptance. Effort persistence, send locking, failure reporting,
turn-aware cancellation and lifecycle isolation are implemented. Five live provider
cases and the native Wails build passed; see the verification report for coverage.
Full regression and release gates remain pending. Native interaction is deferred
and untested while computer-use is paused so the computer remains available.
Headless renders cannot substitute for native interaction.

After source review and checks, dispatch release-desktop from the reviewed main
commit with stable channel, tag `desktop-v3.0.10`, and the existing explicit
unsigned-Windows policy. Require Minisign; it is not Authenticode or Apple
notarization. Keep the generated GitHub Release a draft until validation passes.

Require core/race/frontend tests, three native platform builds and desktop tests,
and production Windows installer acceptance against:

- Baseline: `desktop-v3.0.9`.
- Asset: `O.R.C.A-for-Windows-windows-amd64-installer.exe`.
- Size: `91192109` bytes.
- SHA-256: `89efd5e03de9848988189901ce90fe0ec761a980c64b16e5bda0c2b9c14a7723`.

Check the pinned values and published SHA256SUMS. Run installation acceptance
only on disposable GitHub-hosted Windows X64, never the user's workstation.
Cover fresh install, upgrade, uninstall, payload hashes and retained user data.

Save verified assets to `D:\AI-Reasonix\dist\desktop-v3.0.10`. Verify GitHub asset
sizes/digests, SHA256SUMS, signatures with the application key, NSIS CRC, archive
integrity and product versions. Retry transfers using the exact accepted outputs.

Stage packages and the rebuilt website in a new Mac release directory. Verify
hashes before atomically switching the site symlink and signed stable manifest.
Retain 3.0.9 and its directory for rollback. Check public JSON/signature/package
downloads, missing-path 404 and the actual updater. Do not alter other sites,
old releases/tags, user data, or `site/src/data/community.json`. Build the site
with `npx astro build` to avoid the community-refresh prebuild hook.

3.0.10 production CI, installer acceptance and deployment results remain pending.
Headless render verification is in progress; its result is pending. A successful
local native build is not installed-app or multi-platform production acceptance.
Keep deferred native interaction and real-window screenshots documented as
coverage gaps while proceeding with the other authorized release checks.
