# O.R.C.A. Desktop 3.0.5 Release Runbook

Release and deployment are authorized once the required checks pass; keep the release draft until workflow gates pass. Use the existing unsigned Windows policy (`allow_unsigned_windows=true`); retain Minisign, SHA-256, package-integrity, and installer gates.

## Release Contract

- Stable tag/version: `desktop-v3.0.5` / `v3.0.5`; Wails `3.0.5`; Windows resources `3.0.5.0`.
- Local candidate: Windows `v3.0.5`, built without installing over the user's application.
- CI builds the same stable version into a draft release. Publish only after all native, race, installer and integrity gates pass. A canary dispatch remains available for isolated diagnostics.
- Official model: `deepseek/deepseek-flash`, DeepSeek V4.1 Flash, native vision, 1M context, up to 384K output, tools/JSON; old Flash/Vision names are aliases. Pro remains text-only.
- Official defaults, roles, and saved session model choices upgrade once; custom endpoints/credentials and subsequent Pro choices remain intact. Effort `auto=high`, with `low/high/max`.
- Managed local AI and Computer Use stay disabled on every platform. No DPI work or legacy-brand duplicate assets. Preserve historical releases, user data, and `site/src/data/community.json`.

## Evidence and Blockers

Evidence updated 2026-09-17. Record the exact candidate SHA with each final result.

| Check | Status / evidence |
| --- | --- |
| Direct live API | **PASS**: low/high/max and direct synthetic ASCII/chart vision; local evidence `.tmp/v305-live-results-funded2.json` |
| Live tool history and Pro | **PASS**: `.tmp/v305-live-results-tool-pro.json`; captured empty final reasoning remains distinct from absent reasoning |
| Chinese image main/child route | **PASS**: `.tmp/v305-live-results-child.json`, `control.RunTurn -> task(images) -> visionChild -> parent`; 3 upstream requests, Chinese text/nonce and snapshot bytes verified, 1 task dispatch and 3 usage receipts |
| Full config migration tests | **PASS**: `.tmp/v305-config-final.log`, including strict atomic writes and unreadable-source protection |
| Final root Go suite | **PASS**: `go test ./...`, 56 passing packages; `.tmp/v305-root-final.log` |
| Local workflow/version/baseline contracts | **PASS**: 9 focused tests, including the mandatory config-race gate contract |
| Local race | **NOT RUN**: no CGO compiler available |
| Linux CI race | **PENDING**: must include `internal/config` and all existing race packages |
| Final frontend checks | **PASS**: full suite 578 PASS lines, build; 224 rendered cases, 1,148 assertions, zero browser errors |
| Final local desktop checks | **PASS**: full suite repeated after startup-failure protection, 27.710 seconds |
| CI/native platform checks | **PENDING**: record exact candidate results for Windows, macOS, and Linux |
| Real Windows installation/upgrade/uninstall | **PENDING**: hosted-runner acceptance and retention evidence required |
| Local Windows candidate | **PASS**: Wails/NSIS, 88,803,847-byte installer, NSIS CRC `b9203280`, 7-Zip integrity; final signed CI assets pending |
| Documentation/site checks | **PASS**: 3-page Astro build, 16 README price rows, built docs pricing, 40 local links, and diff checks |
| Preserved data/history | Community hash unchanged; historical notes, audits, and old changelog entries unchanged |

Live API validation is complete; no further API calls are needed. Ledger: cumulative 17 requests and 3.321856 CNY reserved, including the initial 402 and assertion reruns. This is a conservative reservation; estimated usage cost is much lower and is not an authoritative invoice. Source-contract tests establish workflow wiring only; record actual CI race/install results, run IDs and candidate SHA in pending rows.

## Windows Baseline

- Published baseline: `desktop-v3.0.4`.
- Installer: `O.R.C.A-for-Windows-windows-amd64-installer.exe`, 88,758,125 bytes.
- SHA-256: `3bb58aab89011e36210521b28ac8620bb6a4a372759db5df4a94aa1d843519a2`.
- Filename, size, and digest matched live GitHub metadata on 2026-09-17; this was not a fresh binary download/hash check.
- `scripts/test-desktop-installer.ps1` requires both the pinned hash and published `SHA256SUMS.txt`. Run only on a clean GitHub-hosted Windows X64 runner, after integrity checks and before Minisign; preserve its runner/path guards.

## Remote Snapshot

Read-only snapshot: 2026-09-17, approximately 01:24 UTC+8; refresh refs immediately before dispatch.

- Remote main and local HEAD: `742a97a230513b5c859f5482e92d04f31d915e39`; local branch `codex/v3.0.0` still has candidate changes.
- `desktop-v3.0.5` and `v3.0.5` refs were absent; authenticated lookup found no 3.0.5 Release.
- Desktop workflow `294595327` was active; running/queued/waiting queries each returned zero.
- Latest run `34241804936` failed at Windows SignPath on a 3.0.4 tag push. Successful manual run `34238633086` is historical evidence only.
- The new config-race inclusion is local and must reach the reviewed staging/main revision before CI. Use the authorized unsigned override; no additional release approval gate is required.

## Execution Order

1. Build the local candidate in an isolated checkout. The build script writes fixed filenames into that checkout's `dist`; the RC suffix alone does not isolate files. Do not install over the user's real installation.

```sh
bash scripts/desktop-build.sh windows/amd64 v3.0.5-rc.1 canary
```

2. Root and real API checks passed; complete the startup-failure handling test, frontend fixes and final native checks. Re-run root tests after relevant code changes. Linux race with CGO remains mandatory before native CI builds; independent staging can continue.

```sh
go test ./... -count=1 -p=1
go test -race ./internal/agent ./internal/control ./internal/billing ./internal/localai ./internal/config -count=1 -p=1
```

3. Review and commit the candidate, excluding unrelated shared-tree data, then push the reviewed staging branch. Dispatch artifact-only CI staging; match the run's `headSha` to the reviewed commit.

```powershell
$releaseRepo = 'nanbo0ne/O.R.C.A-for-Windows'
$stagingRef = '<reviewed-staging-branch>'
gh workflow run release-desktop.yml --repo $releaseRepo --ref $stagingRef -f channel=canary -f base_version=3.0.5 -F allow_unsigned_windows=true
gh run list --repo $releaseRepo --workflow release-desktop.yml --branch $stagingRef --event workflow_dispatch --limit 5 --json databaseId,headSha,status,conclusion,url
$releaseRun = '<matching-run-id>'
gh run watch $releaseRun --repo $releaseRepo --exit-status
gh run view $releaseRun --repo $releaseRepo --json headSha,status,conclusion,jobs,url
```

4. Download `canary-dist` to a new isolated directory. Verify payload/manifest signatures, version/channel, checksums, and hosted installation evidence. Record Windows as unsigned and actual macOS signing/notarization status.

5. Once remaining frontend/desktop, CI race, native-build and installer checks pass, integrate the reviewed candidate into main. Use the finalized release notes for the stable build: the workflow embeds them in the signed manifest. Recheck main SHA and tag/Release absence; a network/auth error is not proof of absence.

```powershell
gh api "repos/$releaseRepo/branches/main" --jq .commit.sha
gh api "repos/$releaseRepo/git/ref/tags/desktop-v3.0.5"
gh release view desktop-v3.0.5 --repo $releaseRepo --json tagName,isDraft,targetCommitish
gh workflow run release-desktop.yml --repo $releaseRepo --ref main -f channel=stable -f tag=desktop-v3.0.5 -F allow_unsigned_windows=true
```

6. Wait for the exact stable run, verify its SHA and all gates, then inspect draft assets, checksums, signatures, installer evidence, and notes. The workflow creates a draft at its run SHA; do not pre-push a second tag trigger or clobber an existing release.

```powershell
gh release edit desktop-v3.0.5 --repo $releaseRepo --draft=false
```

7. Deploy the same verified website/packages and signed stable update pointer; check served hashes. Never rewrite a signed manifest after signing. Keep 3.0.4 available for rollback and preserve user data.
