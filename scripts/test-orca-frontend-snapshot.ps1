#requires -Version 7.0
[CmdletBinding()]
param(
    [string]$FixtureRoot,
    [string]$NodeExecutable = (Join-Path ([Environment]::GetFolderPath('UserProfile')) '.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node.exe'),
    [string]$NpmCli = (Join-Path ([Environment]::GetFolderPath('ProgramFiles')) 'nodejs/node_modules/npm/bin/npm-cli.js')
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'orca-source-snapshot.ps1')
. (Join-Path $PSScriptRoot 'orca-frontend-snapshot.ps1')
if (-not $FixtureRoot) { $FixtureRoot = Join-Path (Join-Path $PSScriptRoot '../.tmp') ('frontend-snapshot-tests-' + [guid]::NewGuid().ToString('N')) }
$FixtureRoot = New-OrcaOutputDirectory ([IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))) ([IO.Path]::GetFullPath($FixtureRoot))
$script:checks = 0
function Check([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message }; $script:checks++ }
function Reject([scriptblock]$Action, [string]$Message) { $failed = $false; try { & $Action | Out-Null } catch { $failed = $true }; Check $failed $Message }
function Put([string]$Path, [string]$Text) { [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($Path)); [IO.File]::WriteAllText($Path, $Text, [Text.UTF8Encoding]::new($false)) }
$pristine = Join-Path $FixtureRoot 'pristine'
$names = @('LICENSE', 'desktop/frontend/package-lock.json', 'desktop/frontend/src/source.ts', 'desktop/wails.json')
foreach ($name in $names) { Put (Join-Path $pristine $name) "synthetic snapshot source $name" }
$files = @($names | ForEach-Object { @{path=$_; sha256=(Get-OrcaHash (Join-Path $pristine $_)); bytes=([IO.FileInfo](Join-Path $pristine $_)).Length; gitMode='100644'} })
$manifest = Join-Path $FixtureRoot 'manifest.json'
Write-OrcaJson $manifest @{schemaVersion=1; files=$files}
$snapshot = [pscustomobject]@{source=$pristine; manifest=$manifest; manifestSha256=(Get-OrcaHash $manifest); files=$files}
$tools = @{}
foreach ($key in @('node', 'npm', 'go', 'wails')) { $tools[$key] = Join-Path $FixtureRoot "tools/$key.exe"; Put $tools[$key] "synthetic tool $key" }
$npmCache = Join-Path $FixtureRoot 'npm-cache'; $goCache = Join-Path $FixtureRoot 'go-mod-cache'
[void][IO.Directory]::CreateDirectory($npmCache); [void][IO.Directory]::CreateDirectory($goCache)
$environment = New-OrcaBuildEnvironment (Join-Path $FixtureRoot 'env-test') $tools $npmCache $goCache
Check ($environment.GOPROXY -eq 'off' -and $environment.GOENV -eq 'off' -and $environment.npm_config_offline -eq 'true') 'Offline boundaries missing.'
Check ($environment.USERPROFILE.StartsWith($FixtureRoot) -and $environment.APPDATA.StartsWith($FixtureRoot)) 'Build uses a real profile.'
Check (-not $environment.ContainsKey('VITE_SYNTHETIC_SECRET') -and -not $environment.ContainsKey('SYNTHETIC_TOKEN')) 'Environment allowlist expanded.'
$runner = ${function:Invoke-OrcaBuildStep}
$oldSecret = $env:VITE_SYNTHETIC_SECRET
try {
    $env:VITE_SYNTHETIC_SECRET = 'synthetic-do-not-inherit'
    $result = Invoke-OrcaBuildStep (Join-Path $PSHOME 'pwsh.exe') @('-NoProfile', '-NonInteractive', '-Command', 'if ($env:VITE_SYNTHETIC_SECRET) { exit 9 }; Write-Output "isolated"') $FixtureRoot $environment (Join-Path $FixtureRoot 'child.log')
    Check ($result -eq 'isolated') 'Child inherited arbitrary parent environment.'
    Reject { Invoke-OrcaBuildStep (Join-Path $PSHOME 'pwsh.exe') @('-NoProfile', '-Command', 'exit 7') $FixtureRoot $environment (Join-Path $FixtureRoot 'failed-child.log') } 'Child failure was ignored.'
    Reject { Invoke-OrcaBuildStep (Join-Path $PSHOME 'pwsh.exe') @('-NoProfile', '-Command', 'Start-Sleep -Seconds 20') $FixtureRoot $environment (Join-Path $FixtureRoot 'timeout-child.log') 1 } 'Child deadline was ignored.'
} finally { $env:VITE_SYNTHETIC_SECRET = $oldSecret }

# Exercise the real npm configuration loader before replacing the process runner
# with synthetic build tools. No packages, network, or existing profiles are used.
foreach ($path in @($NodeExecutable, $NpmCli)) { Check ([IO.File]::Exists((Assert-OrcaPlainPath $path))) 'Real Node/npm is required for the configuration regression.' }
$actualTools = $tools.Clone(); $actualTools.node = $NodeExecutable; $actualTools.npm = $NpmCli
$actualEnvironment = New-OrcaBuildEnvironment (Join-Path $FixtureRoot 'real-npm-environment') $actualTools $npmCache $goCache
$npmProbe = Join-Path $FixtureRoot 'real-npm-probe'
Put (Join-Path $npmProbe 'package.json') '{"name":"synthetic-npm-probe","version":"1.0.0","private":true,"scripts":{"probe":"npm --version"}}'
Put (Join-Path $npmProbe 'package-lock.json') '{"name":"synthetic-npm-probe","version":"1.0.0","lockfileVersion":3,"packages":{"":{"name":"synthetic-npm-probe","version":"1.0.0"}}}'
$nodeVersion = Invoke-OrcaBuildStep $NodeExecutable @('--version') $npmProbe $actualEnvironment (Join-Path $FixtureRoot 'real-node-version.log')
Check ($nodeVersion -cmatch '^v24\.') 'Configuration regression must use Node 24.'
$npmVersion = Invoke-OrcaBuildStep $NodeExecutable @($NpmCli, '--version') $npmProbe $actualEnvironment (Join-Path $FixtureRoot 'real-npm-version.log')
$npmPackage = Get-Content -LiteralPath (Join-Path ([IO.Path]::GetDirectoryName($NpmCli)) '../package.json') -Raw | ConvertFrom-Json
Check ($npmVersion -ceq $npmPackage.version) 'Real npm --version failed under the isolated environment.'
Check ($actualEnvironment.npm_config_userconfig -cne $actualEnvironment.npm_config_globalconfig) 'npm user/global configuration paths must be distinct.'
foreach ($kind in @('userconfig', 'globalconfig')) {
    $configPath = $actualEnvironment["npm_config_$kind"]
    Check ($configPath.StartsWith($FixtureRoot) -and [IO.File]::ReadAllText($configPath).Length -eq 0) "npm $kind must be a fresh empty fixture file."
    $selected = Invoke-OrcaBuildStep $NodeExecutable @($NpmCli, 'config', 'get', $kind) $npmProbe $actualEnvironment (Join-Path $FixtureRoot "real-npm-$kind.log")
    Check ($selected -ceq $configPath) "npm did not use the explicit $kind file."
}
$brokenEnvironment = $actualEnvironment.Clone()
$brokenEnvironment.npm_config_globalconfig = $brokenEnvironment.npm_config_userconfig
$negativeLog = Join-Path $FixtureRoot 'real-npm-duplicate-config.log'
Reject { Invoke-OrcaBuildStep $NodeExecutable @($NpmCli, '--version') $npmProbe $brokenEnvironment $negativeLog } 'Duplicate npm config negative control did not fail.'
Check ([IO.File]::ReadAllText($negativeLog).Contains('double-loading config')) 'Negative control did not reproduce the actual packaging failure.'
$null = Invoke-OrcaBuildStep $NodeExecutable @($NpmCli, 'ci', '--offline', '--ignore-scripts', '--no-audit', '--no-fund') $npmProbe $actualEnvironment (Join-Path $FixtureRoot 'real-npm-empty-ci.log')
Check ($true) 'Empty offline npm ci completed.'
$nestedVersion = Invoke-OrcaBuildStep $NodeExecutable @($NpmCli, 'run', 'probe', '--silent') $npmProbe $actualEnvironment (Join-Path $FixtureRoot 'real-npm-nested.log')
Check ($nestedVersion -ceq $npmVersion) 'Nested npm shim did not use the isolated configuration.'

$script:mode = 'pass'; $script:steps = [Collections.Generic.List[string]]::new()
function Invoke-OrcaBuildStep([string]$Tool, [string[]]$Arguments, [string]$Directory, [hashtable]$Environment, [string]$Log, [int]$TimeoutSeconds = 600) {
    $name = [IO.Path]::GetFileNameWithoutExtension($Log)
    $script:steps.Add($name)
    Check ($Environment.npm_config_offline -eq 'true' -and -not $Environment.ContainsKey('NODE_OPTIONS')) 'Build child environment was not isolated.'
    $text = 'synthetic passed'
    if ($name -eq 'node-version') { $text = if ($script:mode -eq 'old-node') { 'v18.0.0' } else { 'v24.0.0' } }
    if ($name -eq 'bindings') { Put (Join-Path $Directory 'frontend/wailsjs/go/main/App.js') 'synthetic generated from current backend' }
    if ($name -eq 'npm-ci') {
        Check ($Arguments -contains '--offline' -and $Arguments -contains '--ignore-scripts') 'Dependency install permits network/lifecycle scripts.'
        if ($script:mode -eq 'install-failure') { throw 'synthetic missing offline dependency' }
        [void][IO.Directory]::CreateDirectory((Join-Path $Directory 'node_modules'))
    }
    if ($name -eq 'build') {
        if ($script:mode -eq 'mutated-source') { Put (Join-Path $Directory 'src/source.ts') 'unexpected mutation' }
        Put (Join-Path $Directory 'dist/index.html') '<script src="./assets/app.js"></script><link href="./assets/app.css">'
        Put (Join-Path $Directory 'dist/assets/app.js') ([IO.File]::ReadAllText((Join-Path $Directory 'src/source.ts')))
        Put (Join-Path $Directory 'dist/assets/app.css') 'body {color: black}'
    }
    Put $Log $text
    return $text
}
try {
    $buildRoot = Join-Path $FixtureRoot 'build-source'
    $build = Build-OrcaSnapshotFrontend $snapshot $buildRoot (Join-Path $FixtureRoot 'evidence') $tools $npmCache $goCache
    Assert-OrcaBuiltFrontend $build
    $provenance = Get-Content -LiteralPath $build.manifest -Raw | ConvertFrom-Json
    Check ($provenance.kind -eq 'rebuilt-frontend-from-snapshot' -and $provenance.sourceManifestSha256 -ceq $snapshot.manifestSha256) 'Fresh-build provenance missing.'
    Check ($provenance.steps.Count -eq 5 -and $provenance.bindings.Count -eq 1) 'Build skipped bindings/install/tests/build.'
    Check ([IO.File]::ReadAllText((Join-Path $buildRoot 'desktop/frontend/dist/assets/app.js')) -ceq [IO.File]::ReadAllText((Join-Path $pristine 'desktop/frontend/src/source.ts'))) 'Bundle did not derive from snapshot input.'
    Check (-not (Test-Path -LiteralPath (Join-Path $pristine 'desktop/frontend/dist'))) 'Pristine source snapshot was polluted.'
    Reject { Build-OrcaSnapshotFrontend $snapshot $buildRoot (Join-Path $FixtureRoot 'reused') $tools $npmCache $goCache } 'Existing build output was reused.'
    Put (Join-Path $buildRoot 'desktop/frontend/dist/assets/app.js') 'stale or changed output'
    Reject { Assert-OrcaBuiltFrontend $build } 'Changed bundle passed final verification.'
    foreach ($mode in @('old-node', 'install-failure', 'mutated-source')) {
        $script:mode = $mode
        $evidence = Join-Path $FixtureRoot "$mode-evidence"
        Reject { Build-OrcaSnapshotFrontend $snapshot (Join-Path $FixtureRoot "$mode-source") $evidence $tools $npmCache $goCache } "Accepted $mode."
        Check (-not (Test-Path -LiteralPath (Join-Path $evidence 'frontend-build-input.json'))) "Published success provenance after $mode."
    }
    Reject { New-OrcaBuildEnvironment (Join-Path $FixtureRoot 'missing-cache-env') $tools (Join-Path $FixtureRoot 'missing') $goCache } 'Missing offline cache accepted.'
    $badFile = @{path='desktop/frontend/dist/index.html'; sha256=Get-OrcaTextHash 'stale'; bytes=5; gitMode='100644'}
    Put (Join-Path $pristine $badFile.path) 'stale'
    $bad = [pscustomobject]@{source=$pristine; manifest=$manifest; manifestSha256=$snapshot.manifestSha256; files=@($files) + @($badFile)}
    Reject { Build-OrcaSnapshotFrontend $bad (Join-Path $FixtureRoot 'stale-source') (Join-Path $FixtureRoot 'stale-evidence') $tools $npmCache $goCache } 'Prebuilt snapshot bundle accepted.'
    Write-Output "PASS: $script:checks frontend snapshot checks. Synthetic execution only; not a production rebuild. Fixtures: $FixtureRoot"
} finally { Set-Item Function:Invoke-OrcaBuildStep $runner }
