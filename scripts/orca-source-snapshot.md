# ORCA Source Snapshot

This helper selects current working bytes, not a clean `HEAD` archive. It never stages, commits, installs dependencies, builds an application, or runs an installer. The build script uses the same selection and keeps pristine source separate from mutable beta build identity.

## Review First

Run an audit into a fresh directory (PowerShell 7 and Git only):

```powershell
./scripts/orca-source-snapshot.ps1 -RepositoryRoot (Get-Location).Path -OutputDirectory "$((Get-Location).Path)/.tmp/source-audit-unique"
```

A blocked audit exits nonzero but retains `selection-report.json`, `new-source-candidates.json`, grouped `source-inventory.json`, `resource-audit.json`, readable `REVIEW.md`, and `AUDIT-SHA256SUMS.txt`. Candidates carry current hashes but explicitly remain unapproved. The candidate document deliberately is NOT a valid approval-manifest schema. Inventory distinguishes unchanged tracked, modified tracked, reviewed new, unreviewed, and excluded paths. Review the actual source and explicitly prepare a separate allowlist, preferably in excluded `.tmp/`. Never turn all untracked files into an approval list automatically.

```json
{
  "schemaVersion": 1,
  "baseCommit": "EXACT_GIT_HEAD_COMMIT",
  "files": [
    { "path": "internal/example/new_source.go", "sha256": "EXACT_64_CHARACTER_LOWERCASE_SHA256" }
  ]
}
```

Only exact, nonignored, currently untracked paths belong in this list. No glob/directory entries, duplicates, stale hashes, different commit, or policy-exclusion overrides are accepted. Tracked files come from the Git index path set but use current worktree bytes, including unstaged edits. Deleted tracked files are reported. Unmerged indexes and submodules are refused. Root licenses are required.

An optional `exclude` array uses the same `{ "path": "...", "sha256": "..." }` schema for individually reviewed untracked notes that must not be delivered. Exclusions cannot overlap `files`, hide tracked inputs, or acknowledge entire directories. Changed/missing exclusions block the next snapshot; truly unexpected files still block. Policy-excluded secrets/generated paths need no extra approval and cannot be overridden.

## Create Source Only

```powershell
./scripts/orca-source-snapshot.ps1 -RepositoryRoot (Get-Location).Path -AllowlistPath .tmp/reviewed-new-source.json -OutputDirectory "$((Get-Location).Path)/.tmp/source-delivery-unique" -Snapshot -SourceZip
```

Output contains `source/`, `manifest.json`, `selection-report.json`, `SHA256SUMS.txt`, `source.zip`, and `source.zip.sha256`. The ZIP mirrors the first four members, so its extracted checksums use the same paths. Hashes are verified after copying and after ZIP creation. The Git/index/selection is rechecked after copying; changes fail closed. A failed operation retains partial evidence and is not a deliverable. Only a successful command plus the final ZIP hash file constitutes a completed ZIP.

The manifest records the exact base commit, SHA256 of the NUL-delimited index inventory and tracked status, dirty-tree flag, reviewed-allowlist hash, stable selection hash, each file's SHA256/size, Git mode/index object, and tracked-versus-reviewed origin. It contains no Git history or credential-bearing remote URLs. This is provenance of captured working bytes, not a claim that the uncommitted changes have been committed, reviewed for correctness, or reproducibly built.

## Exclusion Boundary

- Environment files, including `.env.example`, private config/key names, known account/browser/profile directories, dependencies, caches, generated bindings, all `dist/`, releases, executable/archive/object/database payloads, and logs are excluded and reported even when tracked.
- Ignored paths are reported as files or directory summaries without traversing ignored trees. Ignored files cannot be approved as new source.
- Selected input/output paths cannot traverse reparse points. Git symlinks are excluded. New filesystem redirection blocks creation. Paths must be portable relative paths with no traversal, alternate streams, case collisions, control characters, or device names.
- Tracked source icons/images/fonts and the existing `internal/artifact/testdata/` document fixtures are legitimate binary source resources, not compiled release payloads. Signatures are checked. Existing tracked JPEG/PNG images with mismatched image extensions retain their original bytes. Licenses and installer resource sources are copied exactly, never rewritten.
- Exact baseline source entrypoints `cmd/orca/main.go` and `npm/orca-agent/bin/orca.js`, plus single-file Markdown release notes under `docs/releases/`, are not profile/binary payloads despite their directory names. They remain subject to Git selection, explicit review for new files, and content validation; this exception does not admit profile directories or release binaries.
- Resource closure checks current literal Go embed directives against selected and awaiting-review paths. Missing/excluded embeds, new unsupported glob/quoted/directory patterns, or excluded tracked `testdata/` fixtures block selection pending policy review. This conservative check does not override secret exclusions. `desktop/main.go`'s `all:frontend/dist` is the sole generated-resource exception, requiring a fresh build in the mutable stage. Web Beta JavaScript remains subject to the same explicit new-source review as its Go owner.
- This is a path/type/hash selection boundary, not a semantic secret scanner. Tracked source and explicitly approved new text still require human review for accidentally embedded credentials. No unknown untracked file is silently selected.

## Build-Only Inputs

`build-orca-beta.ps1 -SourceAllowlist ...` reports the source gate during preflight. `-Package` still requires explicit `-IntegrationGatesPassed`; do not package while integration blockers remain. `-PrepareFrontend` is a separate source-plus-frontend verification mode, not permission to build an application or installer. It also requires the parent's reviewed allowlist.

Both modes create a pristine source ZIP and copy verified source into a fresh mutable build directory. The checkout's existing `dist`, `wailsjs`, and `node_modules` are never build inputs. The frontend helper creates an empty build-only Go embed placeholder, regenerates Wails bindings from snapshot Go code, runs `npm ci --offline --ignore-scripts`, the full frontend unit/typecheck suite and beta unit suite, then the production build using Node 24. Explicit offline npm/Go caches are required; missing caches/dependencies or failed steps stop the operation without fallback. No dependencies are downloaded by these commands.

Each child gets a cleared, explicitly constructed environment with fresh HOME/USERPROFILE/APPDATA and empty npm configuration. Parent credentials, `NODE_OPTIONS`, arbitrary `VITE_*`, and user Go configuration are not inherited. A staged npm shim pins nested scripts to Node 24 too. Build steps have bounded deadlines and retained logs; timeout cleanup targets only the owned child process tree. This environment boundary is not an OS network sandbox or a substitute for reviewing approved build scripts.

Before and after each build step the selected source bytes must match the source manifest. Fresh bundle file set, regular-file/reparse boundary, signatures, local index references, and CSS resource references are validated. Source maps, unknown files/subdirectories, secret files, and external/missing entry assets fail closed. `frontend-build-input.json` records `rebuilt-frontend-from-snapshot`, source-manifest SHA256, package-lock hash, tool versions/hashes, generated binding hashes, passed-step log hashes, and exact output hashes. Packaging rechecks the bundle after the native build. Pinned runtime payload files are copied by exact verified entries, not recursively by directory.

After allowlist review, run source/frontend verification into a new excluded directory. Supply an authorized, already populated npm cache, not a profile directory:

```powershell
./scripts/build-orca-beta.ps1 -PrepareFrontend -SourceAllowlist .tmp/reviewed-new-source.json `
  -OutputDirectory "$((Get-Location).Path)/.tmp/snapshot-frontend-unique" `
  -NodeExecutable "$env:USERPROFILE/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node.exe" `
  -NpmCli 'C:/Program Files/nodejs/node_modules/npm/bin/npm-cli.js' `
  -NpmCacheDirectory D:/AI-Reasonix/.tmp/approved-npm-cache `
  -GoModuleCacheDirectory "$env:USERPROFILE/go/pkg/mod"
```

The npm cache path above is an explicit placeholder, not a claim that this cache has been provisioned. Final delivery remains blocked until this command actually succeeds against the reviewed snapshot. Synthetic pipeline tests, the existing 554-assertion UI evidence, and rebuilding the shared checkout are NOT proof of snapshot-to-bundle correspondence.

Final packaging emits source/frontend/build-input provenance alongside installer/portable/source checksums. Beta `wails.json` and version-resource overrides are recorded separately and never change the pristine snapshot or shared checkout. Baseline licenses/resources remain unchanged. No build or installer is executed by the source-only helper/tests.

## Tests

```powershell
./scripts/test-orca-source-snapshot.ps1
./scripts/test-orca-frontend-snapshot.ps1
go -C desktop test -run '^TestBeta(SourceSnapshotSyntheticFixtures|FrontendSnapshotSyntheticFixtures|PortableArchiveSyntheticFixtures|BuildScriptStagesIdentityAndRequiresGates)$' -count=1 -v
```

The same packaging fixtures can also run independently of unrelated desktop package compilation, from `desktop/`:

```powershell
go test beta_source_snapshot_test.go beta_portable_test.go beta_installer_test.go -run '^TestBeta(SourceSnapshotSyntheticFixtures|FrontendSnapshotSyntheticFixtures|PortableArchiveSyntheticFixtures|BuildScriptStagesIdentityAndRequiresGates)$' -count=2 -v
```

Tests create disposable repositories, explicitly stage/commit only enumerated synthetic fixture files there, and exercise dirty bytes, reviewed new files, exclusions, stale approvals, output preservation, ZIP hashes, Windows junctions, and frontend build-only validation. They never write to the real checkout's Git index or run packaging. Direct PowerShell runs retain fixtures below `.tmp`; Go test temporary fixtures follow Go's normal cleanup lifecycle.

Frontend-pipeline fixtures use synthetic tools for generation/install/build, plus real isolated PowerShell child-process environment, exit-code and timeout checks. They cover Node-version refusal, no prebuilt inputs, missing-cache/install failure, source mutation, output mutation, and provenance. They do not claim a real snapshot frontend rebuild.
