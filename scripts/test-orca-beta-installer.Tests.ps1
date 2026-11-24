#requires -Version 7.0
[CmdletBinding()]
param([string]$FixtureRoot)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw 'Windows fixture tests required.' }
$scriptPath = Join-Path $PSScriptRoot 'test-orca-beta-installer.ps1'
$tokens = $null; $parseErrors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile($scriptPath, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count) { throw ($parseErrors | Out-String) }
# Definitions only: neither the main body nor any real installer is invoked.
foreach ($function in $ast.FindAll({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] }, $false)) {
    . ([scriptblock]::Create($function.Extent.Text))
}
if (-not $FixtureRoot) { $FixtureRoot = Join-Path (Join-Path $PSScriptRoot '../.tmp') ('installer-harness-tests-' + [guid]::NewGuid().ToString('N')) }
$FixtureRoot = Assert-AcceptancePath ([IO.Path]::GetFullPath($FixtureRoot))
if (Test-Path -LiteralPath $FixtureRoot) { throw 'FixtureRoot must be new.' }
$null = New-Item -ItemType Directory -Path $FixtureRoot
$script:checks = 0
function Check([bool]$Value, [string]$Why) { if (-not $Value) { throw $Why }; $script:checks++ }
function Reject([scriptblock]$Action, [string]$Why) {
    $rejected = $false
    try { & $Action | Out-Null } catch { $rejected = $true }
    Check $rejected $Why
}
function Put([string]$Path, [string]$Text) { Write-AcceptanceNewFile $Path $Text }
function Make-Zip([string]$Path, [hashtable]$Files, [string]$Extra = '') {
    $zip = [IO.Compression.ZipFile]::Open($Path, [IO.Compression.ZipArchiveMode]::Create)
    try {
        foreach ($name in $Files.Keys) {
            $entry = $zip.CreateEntry($name)
            $writer = [IO.StreamWriter]::new($entry.Open())
            try { $writer.Write($Files[$name]) } finally { $writer.Dispose() }
        }
        if ($Extra) { [void]$zip.CreateEntry($Extra) }
    } finally { $zip.Dispose() }
}

try {
    $root = Join-Path $FixtureRoot ('orca-beta-installer-acceptance-' + ('a' * 32))
    $chinese = -join ([char[]]@(0x6D4B, 0x8BD5, 0x5B89, 0x88C5))
    $install = Join-Path $root "install/$chinese ORCA beta"
    $paths = Assert-AcceptanceRoots $root $install @()
    Check ($paths.install -eq $install.Replace('/', '\')) 'Valid Chinese path rejected or changed.'
    Check ((Get-AcceptanceNSISArguments $install) -ceq ('/S /D=' + $paths.install)) 'Incorrect NSIS /D ordering/quoting.'
    Check ((Get-AcceptanceNSISArguments $install -Uninstall) -ceq ('/S _?=' + $paths.install)) 'Incorrect NSIS _? ordering/quoting.'
    $realHome = $env:USERPROFILE
    $start = [Diagnostics.ProcessStartInfo]::new()
    $start.Environment['SYNTHETIC_API_KEY']='must not inherit'
    $start.Environment['WEBVIEW2_USER_DATA_FOLDER']='must not inherit'
    Set-AcceptanceChildEnvironment $start @{HOME=$FixtureRoot; USERPROFILE=$FixtureRoot; TEMP=$FixtureRoot; TMP=$FixtureRoot}
    Check (-not $start.Environment.ContainsKey('SYNTHETIC_API_KEY') -and -not $start.Environment.ContainsKey('WEBVIEW2_USER_DATA_FOLDER')) 'Inherited secret/profile override leaked to child environment.'
    Check ($start.Environment['USERPROFILE'] -ceq $FixtureRoot -and $env:USERPROFILE -ceq $realHome) 'Child isolation changed the caller environment.'
    foreach ($bad in @('C:\', 'relative\file', '\\server\share\file', 'C:\safe\..\other', 'C:\safe\file:stream', 'C:\safe\AUX.txt', 'C:\safe\trailing.', "C:\bad`nargument")) {
        Reject { Assert-AcceptancePath $bad } "Unsafe path accepted: $bad"
    }
    Reject { Assert-AcceptanceRoots $root (Join-Path $root "profiles/$chinese") @() } 'Install overlaps synthetic profiles.'
    Reject { Assert-AcceptanceRoots $root $install @($FixtureRoot) } 'Protected root accepted.'
    Reject { Assert-AcceptanceRoots (Join-Path $FixtureRoot 'ordinary') $install @() } 'Unscoped root accepted.'
    $physical = @{Manufacturer='Example PC'; Model='Physical workstation'}
    $virtual = @{Manufacturer='Microsoft Corporation'; Model='Virtual Machine'}
    Reject { Assert-AcceptanceExecuteGate $false $true $true $virtual } 'Unready artifact permitted.'
    Reject { Assert-AcceptanceExecuteGate $true $false $true $virtual } 'Missing disposable attestation permitted.'
    Reject { Assert-AcceptanceExecuteGate $true $true $false $virtual } 'Missing shell consent permitted.'
    Reject { Assert-AcceptanceExecuteGate $true $true $true $physical } 'Physical host execution permitted.'
    Assert-AcceptanceExecuteGate $true $true $true $virtual
    $script:checks++
    $candidate = @{version='3.0.14-beta.2'}
    Check ((Get-AcceptanceUpgradeKind $candidate $null) -match 'NOT_RUN') 'Reinstall reported as upgrade.'
    Check ((Get-AcceptanceUpgradeKind $candidate @{version='3.0.14-beta.1'}) -eq 'cross-version-upgrade') 'Valid older beta rejected.'
    Reject { Get-AcceptanceUpgradeKind $candidate $candidate } 'Same version claimed as cross-version upgrade.'
    Reject { Get-AcceptanceUpgradeKind $candidate @{version='3.0.15-beta.1'} } 'Newer baseline accepted.'
    $shortcutPath = Join-Path $FixtureRoot 'shell/ORCA beta.lnk'
    $shellPaths = @{ 'DesktopDirectory/ORCA beta.lnk'=$shortcutPath }
    Assert-AcceptanceVacant @{} $shellPaths
    $script:checks++
    Reject { Assert-AcceptanceVacant @{ 'CurrentUser/Registry32'='synthetic existing registration' } $shellPaths } 'Existing beta registration accepted.'
    Put $shortcutPath 'existing synthetic shortcut, not a real LNK'
    Reject { Assert-AcceptanceVacant @{} $shellPaths } 'Existing beta shortcut accepted.'
    $hash = Get-AcceptanceHash $shortcutPath
    Reject { Remove-AcceptanceOwnedFile $shortcutPath $hash @() } 'Unowned cleanup accepted.'
    Reject { Remove-AcceptanceOwnedFile $shortcutPath ('0' * 64) @($shortcutPath) } 'Changed shortcut cleanup accepted.'
    Check ((Get-AcceptanceHash $shortcutPath) -ceq $hash) 'Rejected cleanup changed sentinel.'
    $script:fakeRegistry = @{ 'CurrentUser/Registry32'='synthetic owned record' }
    function Get-AcceptanceBetaRegistrations { return $script:fakeRegistry }
    $receipt = @{registrations=$script:fakeRegistry.Clone(); shortcuts=@{} }
    $receipt.shortcuts[$shortcutPath] = $hash
    Assert-AcceptanceReceipt $receipt
    $script:checks++
    $script:fakeRegistry['CurrentUser/Registry32'] = 'changed by another owner'
    Reject { Assert-AcceptanceReceipt $receipt } 'Changed registration allowed upgrade/uninstall.'
    $script:fakeRegistry = $receipt.registrations.Clone()
    $script:fakeRegistry['LocalMachine/Registry64'] = 'unexpected machine record'
    Reject { Assert-AcceptanceReceipt $receipt } 'Unexpected registration allowed upgrade/uninstall.'
    $script:fakeRegistry = $receipt.registrations.Clone()
    $receipt.shortcuts[$shortcutPath] = '0' * 64
    Reject { Assert-AcceptanceReceipt $receipt } 'Changed shortcut allowed upgrade/uninstall.'
    Remove-AcceptanceOwnedFile $shortcutPath $hash @($shortcutPath)
    Check (-not (Test-Path -LiteralPath $shortcutPath)) 'Owned hash-checked cleanup failed.'
    $outside = Join-Path $FixtureRoot 'outside'
    Put (Join-Path $outside 'sentinel') 'not owned by redirected test path'
    $junction = Join-Path $FixtureRoot 'junction'
    $null = New-Item -ItemType Junction -Path $junction -Target $outside
    Reject { Assert-AcceptancePath (Join-Path $junction 'missing/child') } 'Junction with missing child accepted.'
    Reject { Remove-AcceptanceOwnedFile (Join-Path $junction 'sentinel') (Get-AcceptanceHash (Join-Path $outside 'sentinel')) @((Join-Path $junction 'sentinel')) } 'Redirected cleanup accepted.'
    Check ([IO.File]::ReadAllText((Join-Path $outside 'sentinel')) -ceq 'not owned by redirected test path') 'Outside sentinel changed.'
    $profileFixture = Join-Path $FixtureRoot 'fresh-synthetic-profile'
    New-AcceptanceSentinelDirectory $profileFixture 'synthetic-run-id'
    Check ([IO.File]::ReadAllText((Join-Path $profileFixture 'acceptance-owner.txt')) -ceq 'synthetic-run-id') 'Fresh synthetic profile lacks ownership marker.'
    Reject { New-AcceptanceSentinelDirectory $profileFixture 'other-run' } 'Existing synthetic profile adopted.'
    Reject { New-AcceptanceSentinelDirectory (Join-Path $junction 'new-profile') 'other-run' } 'Redirected synthetic profile created.'
    foreach ($name in @('../escape', '/absolute', 'C:/absolute', 'codegraph/../escape', 'codegraph/file:stream', 'codegraph/NUL.txt', 'codegraph/trailing.', 'codegraph//double', 'codegraph\backslash')) {
        Reject { Assert-AcceptanceMember $name } "Unsafe member accepted: $name"
    }
    $members = @(Get-AcceptanceSevenZipMembers "Path = OrcaBeta.exe`nSize = 10`n`nPath = `$PLUGINSDIR/System.dll`n")
    Check ($members.Count -eq 2) 'Safe NSIS plugin listing rejected.'
    Reject { Get-AcceptanceSevenZipMembers "Path = ../escape`n" } 'Unsafe NSIS listing accepted.'
    Reject { Get-AcceptanceSevenZipMembers "Path = a`nSymbolic Link = target`n" } 'NSIS link accepted.'
    Reject { Get-AcceptanceSevenZipMembers "Path = a`nPath = A`n" } 'Case-duplicate NSIS member accepted.'
    $files = @{'OrcaBeta.exe'='not executable'; 'node.exe'='synthetic node'; 'LICENSE'='license'; 'LICENSE.node.txt'='node license'; 'THIRD-PARTY-NOTICES.txt'='notices'; 'codegraph/LICENSE'='codegraph license'}
    $archive = Join-Path $FixtureRoot 'portable.zip'
    Make-Zip $archive $files
    $map = Get-AcceptancePortableMap $archive (Join-Path $FixtureRoot 'portable')
    Check ($map.Count -eq 6) 'Portable map has wrong membership.'
    Assert-AcceptanceHash (Join-Path $FixtureRoot 'portable/OrcaBeta.exe') $map['OrcaBeta.exe']
    $script:checks++
    Reject { Assert-AcceptanceIdentity (Join-Path $FixtureRoot 'portable/OrcaBeta.exe') '3.0.14-beta.2' } 'Synthetic file accepted as actual beta executable.'
    Reject { Get-AcceptancePortableMap $archive (Join-Path $FixtureRoot 'portable') } 'Existing extraction directory adopted.'
    Reject { Assert-AcceptanceHash $archive ('0' * 64) } 'Unapproved artifact hash accepted.'
    $badZip = Join-Path $FixtureRoot 'duplicate.zip'
    Make-Zip $badZip $files 'orcabeta.exe'
    Reject { Get-AcceptancePortableMap $badZip '' } 'Duplicate portable name accepted.'
    $badZip = Join-Path $FixtureRoot 'traversal.zip'
    Make-Zip $badZip $files '../escape'
    Reject { Get-AcceptancePortableMap $badZip (Join-Path $FixtureRoot 'invalid-extraction') } 'Portable traversal accepted.'
    Check (-not (Test-Path -LiteralPath (Join-Path $FixtureRoot 'invalid-extraction'))) 'Invalid ZIP wrote before complete validation.'
    $badZip = Join-Path $FixtureRoot 'unexpected.zip'
    Make-Zip $badZip $files '.env'
    Reject { Get-AcceptancePortableMap $badZip '' } 'Unexpected source/secret portable member accepted.'
    $main = [IO.File]::ReadAllText($scriptPath)
    Check ($main -notmatch '(?i)Stop-Process|taskkill|\.Kill\(|Remove-Item[^\r\n]*-Recurse|DeleteSubKey') 'Harness contains unbounded process/registry/tree cleanup.'
    Check ($main.Contains('Assert-AcceptanceReceipt $receipt') -and $main.Contains('Assert-AcceptanceVacant (Get-AcceptanceBetaRegistrations) $shellPaths')) 'Install/uninstall ownership gates missing.'
    # These two main-body invocations stop before all OS side effects.
    Reject { & $scriptPath -Mode Execute } 'Execute ran without authorization gates.'
    $preflight = & $scriptPath
    Check ($preflight.mode -eq 'Preflight' -and $preflight.writes -eq $false) 'Default invocation is not read-only preflight.'
    Write-Output "PASS: $script:checks synthetic installer-harness checks. No installer, registry writes, shell shortcut creation, or native application execution."
} finally { Write-Host "Synthetic fixtures retained: $FixtureRoot" }
