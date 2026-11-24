# ORCA Beta Installer Acceptance

Status: preparation only. No actual install, upgrade, uninstall, portable launch,
or wizard acceptance has been executed by this workstream. Do not run Execute
until the parent provides approved artifacts and explicitly opens that gate.

## Owned Files

- `scripts/test-orca-beta-installer.ps1`: read-only preflight, extraction-only
  inspection, and gated disposable-VM silent installer acceptance.
- `scripts/test-orca-beta-installer.Tests.ps1`: offline synthetic helper/gate tests.
- This document. No installer source or build-script changes are required.

## Execution Boundary

Actual execution is VM-only, not host-account testing. Windows shell folders and
HKCU cannot be isolated by changing APPDATA, LOCALAPPDATA, HOME, or USERPROFILE.
The script does not modify shell-folder registry settings or user/global environment
variables. Child-only synthetic profile variables are not claimed as shell-folder
redirection.

Child environments are rebuilt from a minimal Windows environment plus the
synthetic profile/temp paths. Inherited API credentials, custom PATH entries,
application overrides, and WebView2 overrides are not forwarded. The caller's
process/user/global environment is unchanged.

Required human conditions:

1. A throwaway VM and fresh account, with a rollback snapshot. `-DisposableVM`
   explicitly attests this. Manufacturer/model detection rejects ordinary physical
   hosts but does NOT prove the VM/account contains no personal data.
2. No real official ORCA installation, processes, or personal profiles in the VM.
   Do not map personal host directories, share the host profile, or expose writable
   host drives. Copy approved artifacts in, or mount their input directory read-only.
3. Existing trusted PowerShell 7, 7-Zip, and WebView2. The harness installs no tools
   and refuses the runtime-missing path that could invoke the WebView2 bootstrapper.
   Disable VM network access where possible. Runtime bootstrap is not acceptance.
4. Parent-approved installer/portable hashes and executable hash, in a separately
   hash-approved manifest. A checksum file shipped beside an arbitrary executable
   is not itself approval.
5. The parent explicitly permits the temporary VM account's three beta shortcuts
   and beta HKCU uninstall registration, through `-AllowShellArtifacts`. The
   disposable-account authorization also covers NEW synthetic sentinel directories
   at its actual `%USERPROFILE%\.orca`, shell-resolved roaming `ORCA beta`, and
   local `Programs\O.R.C.A for Windows` locations. Any existing directory blocks
   execution. These fixtures are never created in a host account by this harness.

No VM launchers were found on PATH during preparation. This is not a complete
inventory of installed virtualization products. No tools or VMs were installed.

The script refuses ANY preexisting beta uninstall key in HKCU/HKLM, 32/64-bit
views, and ANY exact beta shortcut on current/all-users Desktop or Programs. It
never adopts an existing installation. Execute also rejects real official ORCA
registrations/process names and known existing ORCA profile locations. It never
reads existing official chat/config contents or closes a user application.

## Manifest

Supply absolute plain local paths. SHA-256 strings are lowercase hex. Candidate
version is fixed to `3.0.14-beta.2`; app identity is `ORCA beta` / `OrcaBeta.exe` /
file version `3.0.14.2`. Every artifact is checked before execution.

```json
{
  "schemaVersion": 1,
  "candidate": {
    "version": "3.0.14-beta.2",
    "installer": {
      "path": "C:\\ApprovedArtifacts\\ORCA-beta-3.0.14-beta.2-windows-amd64-installer.exe",
      "sha256": "PARENT_APPROVED_64_LOWERCASE_HEX"
    },
    "portable": {
      "path": "C:\\ApprovedArtifacts\\ORCA-beta-3.0.14-beta.2-windows-amd64.zip",
      "sha256": "PARENT_APPROVED_64_LOWERCASE_HEX"
    },
    "executableSha256": "PARENT_APPROVED_ORCABETA_EXE_64_LOWERCASE_HEX"
  }
}
```

Optional `baseline` has the same structure and must identify a distinct, older
beta version. Without it, the script performs same-artifact reinstall and reports
`cross-version-upgrade NOT_RUN`. Never relabel that result as version-upgrade
acceptance. Never substitute an official ORCA installer for the baseline.

## Commands For Later

These are instructions, not authorization to run an installer now.

Read-only conditions, with no artifacts or filesystem/registry writes:

```powershell
pwsh -NoProfile -File scripts/test-orca-beta-installer.ps1
```

Prepare arguments inside the disposable VM after the parent opens the gate.
`C:\Acceptance` must already exist. TestRoot must not exist; its leaf must have
the exact GUID-based prefix. InstallRoot must be a Chinese-named direct child of
`TestRoot\install`. The character construction keeps these instructions ASCII.

```powershell
$root = Join-Path 'C:\Acceptance' ('orca-beta-installer-acceptance-' + [guid]::NewGuid().ToString('N'))
$chinese = -join ([char[]]@(0x6D4B, 0x8BD5, 0x5B89, 0x88C5))
$acceptance = @{
    ArtifactManifest = 'C:\ApprovedArtifacts\acceptance-manifest.json'
    ManifestSha256 = 'PARENT_APPROVED_MANIFEST_HASH'
    TestRoot = $root
    InstallRoot = Join-Path $root "install\$chinese ORCA beta"
    SevenZipPath = 'C:\Program Files\7-Zip\7z.exe'
}
& scripts/test-orca-beta-installer.ps1 @acceptance -Mode Preflight
# Only after the parent explicitly authorizes actual installation:
& scripts/test-orca-beta-installer.ps1 @acceptance -Mode Execute `
    -ArtifactsReady -DisposableVM -AllowShellArtifacts
```

For host-side inspection without any installer execution:

```powershell
& scripts/test-orca-beta-installer.ps1 @acceptance -Mode ExtractOnly
```

ExtractOnly requires an already-installed trusted `7z.exe`; it does not acquire
one. It validates archive paths and hashes, extracts into a new owned test root,
and compares NSIS payload bytes with portable members. It returns
`EXTRACTION_ONLY_NOT_INSTALL_ACCEPTANCE`. It does not establish install, upgrade,
uninstall, registration, shortcut, runtime, or process-coexistence acceptance.

## What Execute Checks

- Parent-approved installer and portable SHA-256; portable member allowlist,
  traversal/link/duplicate rejection, sizes, all member hashes, and executable PE
  identity. NSIS extraction must match portable runtime/license bytes, except the
  portable's top-level `LICENSE`, which the current installer does not ship.
- Real silent NSIS install at the explicit Chinese path, never a default location.
  `/D=` is the final unquoted parameter, as NSIS requires even for paths with spaces.
- Actual installed payload hashes and exact membership; marker, executable name,
  product/file versions, HKCU install/uninstall strings, and shortcut targets.
- A synthetic official-name `Orca.exe` process, made from a copy of Windows
  `cmd.exe` with `/d /q /k`, stays alive across all operations. It never runs ORCA
  code, reads official profiles, navigates, or makes network requests. Only its
  owned stdin receives `exit` at fixture teardown; no process-kill API exists.
- Synthetic official config/chat, beta account/chat, and official-installation
  sentinels retain identical hashes after install, upgrade/reinstall, and uninstall.
  These cover both private child-environment roots and the VM's real known-folder
  locations; the latter cannot be redirected by APPDATA/HOME environment changes.
  This is NOT a claim about compatibility with a real official application's state.
- Before upgrade and uninstall, every registration value and owned shortcut hash
  must match the last verified checkpoint. An unrelated change aborts the run.
- Uninstall uses a hash-verified private copy and final unquoted `_?=<InstallRoot>`
  so the harness waits for the actual uninstaller, not its temporary launcher.
  InstallRoot, beta registration, and the three beta shortcuts must be absent;
  synthetic data and the official-name process must remain. The private uninstaller
  copy is removed only after exact-path and recorded-hash checks.

## Failure And Cleanup

Evidence is retained at `TestRoot\acceptance-report.json`. There is no recursive
cleanup, registry deletion, global process termination, automatic failure-time
uninstall, or attempt to restore unknown contents. Successful NSIS uninstall is
allowed only after the ownership/hash checkpoint passes. Preexisting shortcuts
are refused, not backed up and overwritten.

After partial failure, retain the VM for diagnosis or revert its snapshot. Do not
reuse the failed root/account or manually delete an ambiguous shortcut/key. A
timeout reports the owned child PID and does not kill it or begin cleanup. A
separate outstanding installer may still be running; the operator must inspect
or discard that disposable VM. No failed run is reported as a pass.

## Automated Tests And Gaps

```powershell
pwsh -NoProfile -NonInteractive -File scripts/test-orca-beta-installer.Tests.ps1
```

Tests load function definitions through the PowerShell AST, use synthetic files,
ZIPs, junctions and registry maps, and exercise the default/no-approval entry
points. They do not execute an installer, launch an app, create real shortcuts,
or write registry keys. Fixtures are retained under the worktree's `.tmp`.

Preparation verification on 2026-11-06: 68 synthetic checks passed. The final
fixture directory is `.tmp/installer-harness-tests-46d44dd1379644b7a74f6357c73bf4fb`.
This result is harness verification only, not an installer acceptance result.

Not yet validated: actual artifact extraction layout, silent NSIS execution,
cross-version upgrade (requires an older approved beta artifact), real registry
and shortcut effects, wizard localization/flow, portable/app startup, or a real
official installation running beside beta. The standard installer wizard source
is unchanged; silent testing cannot certify its visible UI.

References: [NSIS command-line options](https://nsis.sourceforge.io/Docs/Chapter3.html)
and [shell-folder context](https://nsis.sourceforge.io/Reference/SetShellVarContext).
