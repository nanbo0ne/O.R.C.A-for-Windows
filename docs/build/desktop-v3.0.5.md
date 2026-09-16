# O.R.C.A. Desktop 3.0.5 Release Runbook

Published on 2026-09-17 (UTC+8) after all required workflow gates passed. Windows follows the existing unsigned policy; Minisign, SHA-256, package-integrity, and installer gates passed. The update signature is not an operating-system publisher signature.

## Release Contract

- Stable tag/version: `desktop-v3.0.5` / `v3.0.5`; Wails `3.0.5`; Windows resources `3.0.5.0`.
- Local delivery contains the verified CI assets; nothing was installed over the user's application.
- CI built the stable draft from the source SHA below; publication followed native, race, installer, integrity, and server staging checks.
- Official model: `deepseek/deepseek-flash`, DeepSeek V4.1 Flash, native vision, 1M context, up to 384K output, tools/JSON; old Flash/Vision names are aliases. Pro remains text-only.
- Official defaults, roles, and saved session model choices upgrade once; custom endpoints/credentials and subsequent Pro choices remain intact. Effort `auto=high`, with `low/high/max`.
- Managed local AI and Computer Use stay disabled on every platform. No DPI work or legacy-brand duplicate assets. Preserve historical releases, user data, and `site/src/data/community.json`.

## Final Evidence

Release source and immutable tag target: `db3b3b5c9a4ca81cc310dd8dfc9a42188df87e75`.
Workflow: [35131853347](https://github.com/nanbo0ne/O.R.C.A-for-Windows/actions/runs/35131853347), **success**.
The documentation-only final evidence update does not change that tag or any signed asset.

| Check | Status / evidence |
| --- | --- |
| Direct live API | **PASS**: low/high/max and direct synthetic ASCII/chart vision; local evidence `.tmp/v305-live-results-funded2.json` |
| Live tool history and Pro | **PASS**: `.tmp/v305-live-results-tool-pro.json`; captured empty final reasoning remains distinct from absent reasoning |
| Chinese image main/child route | **PASS**: `.tmp/v305-live-results-child.json`, `control.RunTurn -> task(images) -> visionChild -> parent`; 3 upstream requests, Chinese text/nonce and snapshot bytes verified, 1 task dispatch and 3 usage receipts |
| Full config migration tests | **PASS**: `.tmp/v305-config-final.log`, including strict atomic writes and unreadable-source protection |
| Final root Go suite | **PASS**: `go test ./...`, 56 passing packages; `.tmp/v305-root-final.log` |
| Local workflow/version/baseline contracts | **PASS**: 9 focused tests, including the mandatory config-race gate contract |
| Local race | **NOT RUN**: no CGO compiler available |
| Linux CI race | **PASS**: agent, control, billing, local AI, and config on the release SHA |
| Final frontend checks | **PASS**: full suite 578 PASS lines, build; 224 rendered cases, 1,148 assertions, zero browser errors |
| Final local desktop checks | **PASS**: full suite repeated after startup-failure protection, 27.710 seconds |
| CI/native platform checks | **PASS**: Windows amd64, macOS universal, Linux amd64 builds and native tests |
| Real Windows installation/upgrade/uninstall | **PASS**: hosted runner, pinned 3.0.4 baseline, retained installation path, explicit new directory, payload hashes, synthetic config/session/model retention |
| Final Windows installer | **PASS**: 88,804,245 bytes; NSIS CRC `2c072443`; 7-Zip integrity; executable version `3.0.5.0` |
| Release integrity | **PASS**: 17 GitHub size/digest matches, 16 checksum records, eight Minisign verifications, seven archive tests; installer/portable executable bytes match |
| Actual updater download | **PASS**: detects 3.0.5 from 3.0.4, downloads full installer from Mac, verifies signature and SHA-256; 70.529 seconds for transfer and verification |
| Public delivery | **PASS**: public manifest/signature equal release assets; four unique update packages downloaded in full and verified; missing update/package URLs return 404 |
| Documentation/site checks | **PASS**: 3-page Astro build, 16 README price rows, built docs pricing, 40 local links, and diff checks |
| Preserved data/history | Community hash unchanged; historical notes, audits, and old changelog entries unchanged |

Live API validation is complete; no further API calls are needed. Ledger: cumulative 17 requests and 3.321856 CNY reserved, including the initial 402 and assertion reruns. This is a conservative reservation; estimated usage cost is much lower and is not an authoritative invoice. Native race/install results above come from the completed workflow, not only source-contract tests.

## Windows Baseline

- Published baseline: `desktop-v3.0.4`.
- Installer: `O.R.C.A-for-Windows-windows-amd64-installer.exe`, 88,758,125 bytes.
- SHA-256: `3bb58aab89011e36210521b28ac8620bb6a4a372759db5df4a94aa1d843519a2`.
- Filename, size, and digest matched GitHub metadata on 2026-09-17. The hosted acceptance test also downloaded and verified the actual baseline binary before installing it.
- `scripts/test-desktop-installer.ps1` requires both the pinned hash and published `SHA256SUMS.txt`. Run only on a clean GitHub-hosted Windows X64 runner, after integrity checks and before Minisign; preserve its runner/path guards.

## Delivered Files and Deployment

- [GitHub Release](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.5), published at `2026-09-16T18:24:53Z`, marked latest; 17 assets, no legacy-brand duplicates.
- Local directory: `D:\AI-Reasonix\dist\desktop-v3.0.5\`.
- Windows installer: `O.R.C.A-for-Windows-windows-amd64-installer.exe`.
- Installer SHA-256: `ab824268dcf6b01807022ef3c606db67f11f32c72871069eac86dbe75c50bca3`.
- Portable ZIP SHA-256: `00d576d0dccbadb3bd9f34a5b7619e2ca11b77eac0bc0df9dccb3ddb9d35c2ab`.
- Signed manifest SHA-256: `42b0a439bcaf0368ccbdc8fdf83bf5f6845b61f809d15ee10f464e757177c141`.
- [Download site](https://orca.aichat.diy/) and [stable manifest](https://orca.aichat.diy/updates/stable/latest.json).
- Mac deployment: `20260917-orca305/orca`, staged and hash-checked before atomic symlink activation. The previous `20260908-orca304/orca` deployment remains available for rollback; other sites were not changed.
- Public site HTML SHA-256 matches the committed-source build: `7d8b80699083a710eadbc611021d7c63586259e93086d38c582fe782f118237d`.

The public updater probe exercised the application's actual manifest fetch,
version evaluation, package transfer, and signature verification using an isolated
profile and directory. It did not launch the installer, restart the application,
or modify user data. The full 88,804,245-byte Mac-source installer transfer and
verification took 70.529 seconds (about 1.20 MiB/s); this is one local network
observation, not a guaranteed download speed.

Local release evidence: `.tmp/release-v305/windows-ci.log`, `core-ci.log`,
`updater-public.log`, `delivery-verification.json`, and `public-delivery.log`.

## Limits and Reproduction

Windows has no Authenticode publisher signature; macOS is unnotarized. Native
installation retention was checked on a clean hosted Windows runner using
synthetic data, not by installing over the user's real copy. Browser checks use
mock data at DPR 1 and are distinct from native/live API tests. No DPI testing or
Computer Use/managed-local-AI validation is claimed. The frontend main bundle
size warning remains (991.96 kB; 284.26 kB gzip).

The GitHub API delivered all 17 assets successfully. A later anonymous check of
the GitHub browser-download URLs hit connection resets/timeouts from both the
Windows workstation and Mac server; that path's post-publication Range check
could not be completed. The primary Mac public downloads passed in full. Do not
interpret successful asset/signature verification as guaranteed GitHub network
availability from every connection.

To recheck downloaded release files without installing:

```powershell
go run ./cmd/sign validate D:\AI-Reasonix\dist\desktop-v3.0.5 v3.0.5 desktop-v3.0.5
go run ./cmd/nsischeck D:\AI-Reasonix\dist\desktop-v3.0.5\O.R.C.A-for-Windows-windows-amd64-installer.exe
```

Run those commands from `desktop`. Do not repoint the release tag, rewrite its
signed manifest, or overwrite an existing public asset. A future binary change
requires a new patch release.
