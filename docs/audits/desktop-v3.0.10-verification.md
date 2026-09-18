# O.R.C.A. Desktop 3.0.10 Verification

## Scope and Current Status

思考强度保存/请求生效、当前回合取消与生命周期隔离修复已实现。五项真实
供应商请求通过；本机原生 Wails 构建通过。完整发布验收仍未完成，
不将局部通过写成整版通过。标准 3.0.9 安装器和 Modern / Classic 保持。

Effort persistence/request behavior, current-turn cancellation and lifecycle
isolation fixes are implemented. Five live provider cases and the local native
Wails build passed. Full release acceptance remains incomplete.

## Current Integration Checkpoint

Verified locally before the release workflow:

- `go test ./... -p=1 -count=1`: passed. The first parallel run hit a CLI
  shell timeout; focused rerun and the serial full run both passed.
- Desktop `go test . -count=1`: passed; disk-save rollback regression also passed.
- Frontend `npm run test:all` and `npm run build`: passed, including CSS checks.
- `git diff --check`: passed. Windows lacks a C compiler for `-race`; the
  release workflow performs the core and desktop concurrency tests on Linux.

- Native Wails build: passed. Actual native-window interaction: deferred and
  NOT tested while computer-use is paused so the computer remains available.
- Headless rendering verification: running in a separate task; result pending.
  It does not establish native Wails interaction or installed-app acceptance.
- Full core rerun with `-p 1`: ongoing. The initial parallel run hit a CLI
  timeout; that focused CLI rerun passed. The complete core suite is not yet
  recorded as passing.
- Full frontend: initial source-contract failures were corrected; final full
  rerun result remains pending. Earlier focused/typecheck passes are not a
  substitute for this final rerun.

## Actual Provider Verification

Read evidence: `.tmp/release-v310/live-provider-verification.md` and its sanitized
JSON, executed 2026-09-18 15:08:42 UTC. All five cases passed using the actual
provider/config-capability pipeline, with one upstream POST each and no retries.

| Route / selection | HTTP | Serialized reasoning_effort | Result |
| --- | --- | --- | --- |
| Token Lens / xhigh | 200 | xhigh | Completed, stop |
| Token Lens / auto | 200 | omitted | Completed, stop |
| Official DeepSeek / low | 200 | low | Completed, stop |
| Official DeepSeek / auto | 200 | high | Completed, stop |
| Official DeepSeek / low, cancel | 200 | low | Canceled after first reasoning delta |

The harness used a loopback relay to inspect serialized requests and forward
unchanged JSON upstream. It is not an uninstrumented production transport test.
Cancellation closed the local provider output channel in less than 1 ms; this
does not prove remote compute/billing termination. Token Lens cancellation,
native stop-button behavior, installed binaries, full config-loader boot,
retries, concurrent workloads and the historical gateway 404 were not tested.
Acceptance of xhigh does not establish upstream reasoning depth or quality.

## Observed Focused Evidence

- EffortSwitcher: 15 rendered-component tests passed for saving/running locks,
  accessible busy state, effective/omitted auto defaults, and EN/ZH hints.
- Frontend application and test TypeScript checks passed at that UI checkpoint.
  These do not verify subsequent parent integration, send blocking, failure
  reporting, cancellation, browser interactions, or screenshots.
- Complete `internal/event` and `internal/control` suites passed after lifecycle
  isolation changes, including the formerly failing delayed-completion successor
  regression and synchronous RunTurn success/failure/cancelled outcomes.
- Installer/packaging/release contracts passed after updating the additional
  installer source-contract pins to verified 3.0.9.
- Release workflow, pinned-baseline, version metadata and documentation tests
  passed: `go test . -run 'Test(Release|StableManualDispatch|InstallerAcceptanceUsesPublished309Baseline|NextDesktopRelease)' -count=1`
  from `desktop`.
- Local 3.0.9 baseline installer size and SHA-256 match the pins below. This is
  a local integrity check, not a fresh public download or upgrade acceptance.
- PowerShell acceptance-script syntax parsing and scoped `git diff --check`
  passed. The installer harness was not executed on this workstation.
- Astro built all three static pages successfully with the bundled Node runtime,
  calling `node node_modules/astro/bin/astro.mjs build` directly. Default Node
  18.20.0 was rejected by Astro (requires >=22.12.0); the bundled runtime retry
  passed. The community-refresh prebuild hook was not run. Build success is not
  browser screenshot or public deployment acceptance.

## Pinned Upgrade Baseline

- Release: `desktop-v3.0.9`.
- Asset: `O.R.C.A-for-Windows-windows-amd64-installer.exe`.
- Size: `91192109` bytes.
- SHA-256: `89efd5e03de9848988189901ce90fe0ec761a980c64b16e5bda0c2b9c14a7723`.
- The acceptance harness must verify the release metadata, actual file size,
  pinned digest, and published `SHA256SUMS.txt`; require a candidate newer than
  3.0.9. Execute installation only on disposable GitHub-hosted Windows X64.

## Pending Release Gates

- Final complete core/frontend/native-desktop and race-check results on the
  reviewed source. Local `-race` was unavailable with CGO disabled and no C
  compiler on PATH; this must not be recorded as a race-detector pass.
- Downstream aggregate/status handling of stale terminal events requires its
  own identity checks; lifecycle passthrough preserves historical receipts.
- Production Windows/macOS/Linux builds and version/packaging checks.
- Production installer fresh-install, 3.0.9 upgrade, default uninstall, payload
  integrity and configuration/session/draft/model retention acceptance.
- Headless render results remain pending. Native-window effort/cancellation
  interaction is deferred and untested while computer-use is paused. This is a
  documented coverage gap, not an additional release authorization gate.
- Asset sizes/digests, archive integrity, NSIS CRC and Minisign verification.
- GitHub publication and Mac staged deployment, public downloads, signed stable
  manifest, missing-path 404, and actual updater validation.

No production CI, verified platform release assets, completed headless screenshot
report, native interaction or public update results for 3.0.10 are claimed here. Earlier [3.0.9 evidence](desktop-v3.0.9-verification.md)
is historical and must not be relabeled as 3.0.10 acceptance. User authorization
for building and public GitHub/Mac release remains effective after the other
release checks pass. Deferred native interaction must remain disclosed.
