# O.R.C.A. Desktop 3.0.10 Verification

## Scope and Current Status

Release candidate source: `f052bde37b287e883a13acf74214e28882aeae31`.
Workflow: [35363513580](https://github.com/nanbo0ne/O.R.C.A-for-Windows/actions/runs/35363513580) (success).

## CI Acceptance

- Core full tests, changed-package race checks, frontend full tests and build: passed.
- Windows amd64, macOS Universal and Linux amd64 native builds and desktop tests: passed.
- Linux desktop race checks for cancellation, effort persistence, runtime rebuild
  and work admission: passed.
- Windows standard installer and portable archive checks: passed. Hosted Windows
  fresh installation, pinned 3.0.9 upgrade, custom path, default uninstall,
  current payload hashes and synthetic configuration/session/draft/model marker
  retention passed (`Installer acceptance: passed`, 2026-09-18 15:51:57 UTC).
- GitHub release contains 17 uploaded assets from the candidate source. Local
  sizes, SHA-256 records and GitHub asset digests match. Mac staging checked all
  files before an atomic activation; public download verification follows below.

Latest pre-release checkpoint: the actual App passed 18/18 headless Modern/Classic
scenarios and 176 assertions, plus 6 polling checks, after fixing three browser
findings (cancel-error false idle, pending-submit draft loss, and missing mouse
Stop with a draft). No external requests or browser errors. These tests use a
controlled bridge and do not establish native-window behavior. The first CI run
35362216984 passed core/race gates but was deliberately cancelled before packaging
to include these fixes; it produced no release. Frontend full tests passed again.

思考强度保存/请求生效、当前回合取消与生命周期隔离修复已实现。五项真实
供应商请求通过；本机原生 Wails 构建和三平台 CI 通过。
标准 3.0.9 安装器和 Modern / Classic 保持；原生窗口交互暂缓。

Effort persistence/request behavior, current-turn cancellation and lifecycle
isolation fixes are implemented. Five live provider cases and the local native
Wails build passed, as did production CI. Native-window interaction remains deferred.

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
- Headless rendering verification: 18/18 scenarios and 176 assertions passed in
  Modern and Classic, with 6 additional polling checks. It does not establish
  native Wails interaction or installed-app acceptance.
- Cancellation after visible approval/Ask, paused work, late replies, silent
  streams before and after their first chunk, and two active tabs passed focused
  backend regressions. Completed tool results remain in resumable history.

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
- Frontend application/test TypeScript checks and final full frontend tests passed.
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

## Release Integrity and Deployment

- Published `desktop-v3.0.10` points to `f052bde37b287e883a13acf74214e28882aeae31`.
- Installer: `O.R.C.A-for-Windows-windows-amd64-installer.exe`, 91,205,028 bytes.
- Installer SHA-256: `a021db82626246a6895abbdd53128f0264f37aa40c21cc56cf9a81399879e979`.
- NSIS CRC: `eee2c2cc`, verified locally. Portable archive integrity and package
  Minisign signatures passed; the signed manifest validates five platform entries.
- Final bilingual release notes were inserted into the manifest and re-signed
  before publication. Binary assets and source commit were unchanged.
- All 17 GitHub asset sizes/digests match local delivery. Mac staging and final
  release directories passed all 16 SHA-256 records; stable manifest switched
  atomically from 3.0.9 to 3.0.10. Local HTTP matches the signed manifest and
  missing update paths return 404. Public-path checks are recorded below.

## Public Update Probe (2026-09-19, Asia/Shanghai)

- The production updater functions fetched and verified the public signed
  manifest, detected the 3.0.9-to-3.0.10 update, and downloaded the complete
  91,205,028-byte installer from the Mac public source. SHA-256 and Minisign
  verification passed. Download/verification took 13m7.822s (about 113 KiB/s).
  This isolated test downloaded only; it did not launch or install the package.
- The public site shows 3.0.10. Stable manifest and signature match local files;
  missing manifest and package paths return 404.
- Portable ZIP, Linux DEB and macOS DMG public first-64-KiB bytes, advertised
  total lengths and signature files match the fully verified local packages.
  These three packages were sampled, not fully downloaded again over public HTTP.
- Network limitations remain: an independent full curl transfer stalled;
  a Mac-origin public curl probe timed out after 180s with 13,879,611 bytes.
  Two portable-ZIP tail-range probes returned correct 206/Content-Range headers
  but timed out without body bytes. These are not recorded as successful full
  downloads. No claim is made that public download throughput was improved.
- Evidence: `.tmp/release-v310/updater-public-result.log` and
  `.tmp/release-v310/delivery-verification.json`. GitHub and server full-file
  digests remain independently verified; network sampling is not a substitute.

## Remaining Coverage Limits

Native-window effort/cancellation interaction is deferred at the user's request;
no GUI automation was used while the workstation was needed. Headless tests and
hosted Windows installer acceptance are separate evidence, not substitutes for
native button interaction. Local race testing required an unavailable C compiler;
Linux CI core and desktop race checks passed. The historical Token Lens 404 was
not reproduced and is not claimed fixed. Earlier [3.0.9 evidence](desktop-v3.0.9-verification.md)
remains historical and is not relabeled as 3.0.10 acceptance.
