#requires -Version 7.0
[CmdletBinding()]
param([string]$FixtureRoot)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'orca-source-snapshot.ps1')
. (Join-Path $PSScriptRoot 'orca-frontend-snapshot.ps1')
. (Join-Path $PSScriptRoot 'orca-native-build.ps1')
if (-not $FixtureRoot) { $FixtureRoot = Join-Path (Join-Path $PSScriptRoot '../.tmp') ('native-build-tests-' + [guid]::NewGuid().ToString('N')) }
$FixtureRoot = New-OrcaOutputDirectory ([IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))) ([IO.Path]::GetFullPath($FixtureRoot))
$script:checks = 0
function Check([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message }; $script:checks++ }
function Reject([scriptblock]$Action, [string]$Message) { $failed = $false; try { & $Action | Out-Null } catch { $failed = $true }; Check $failed $Message }
function Put([string]$Path, [string]$Text) { [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($Path)); [IO.File]::WriteAllText($Path, $Text, [Text.UTF8Encoding]::new($false)) }
$go = Join-Path ([Environment]::GetFolderPath('ProgramFiles')) 'Go/bin/go.exe'
$pwsh = Join-Path $PSHOME 'pwsh.exe'
$tools = @{go=$go; wails=$pwsh; nsis=$pwsh; sevenZip=$pwsh}
foreach ($key in @('node', 'npm')) { $tools[$key] = Join-Path $FixtureRoot "tools/$key.exe"; Put $tools[$key] "synthetic pinned $key" }
$npmCache = Join-Path $FixtureRoot 'npm-cache'; $goCache = Join-Path $FixtureRoot 'go-cache'
[void][IO.Directory]::CreateDirectory($npmCache); [void][IO.Directory]::CreateDirectory($goCache)
$module = Join-Path $FixtureRoot 'module'
Put (Join-Path $module 'go.mod') "module synthetic.invalid/overlayprobe`n`ngo 1.20`n"
Put (Join-Path $module 'probe.go') "package overlayprobe`n"
$replacement = Join-Path $FixtureRoot 'injected.go'
Put $replacement "package overlayprobe`nimport _ `"synthetic.invalid/injected`"`n"
$overlay = Join-Path $FixtureRoot 'overlay.json'
Write-OrcaJson $overlay @{Replace=@{(Join-Path $module 'probe.go')=$replacement}}
$overlayFlags = '"-overlay=' + $overlay + '"'
$userConfig = Join-Path $FixtureRoot 'user-go-env'
Put $userConfig "GOFLAGS=$overlayFlags`n"
$old = @{}
foreach ($key in @('GOFLAGS', 'GOENV', 'GOTOOLCHAIN', 'GOROOT', 'GO111MODULE', 'GOCACHEPROG', 'NSISDIR', 'NSISCONFDIR', 'NODE_OPTIONS', 'PATH')) { $old[$key] = [Environment]::GetEnvironmentVariable($key, 'Process') }
try {
    $env:GOFLAGS=$overlayFlags; $env:GOENV=$userConfig; $env:GOTOOLCHAIN='synthetic-invalid'; $env:GOROOT='synthetic-invalid'
    $env:GO111MODULE='off'; $env:GOCACHEPROG='synthetic-invalid'; $env:NSISDIR='synthetic-invalid'; $env:NSISCONFDIR='synthetic-invalid'
    $env:NODE_OPTIONS='--require=synthetic-invalid'; $env:PATH=Join-Path $FixtureRoot 'hostile-path'
    $context = New-OrcaNativeBuildContext (Join-Path $FixtureRoot 'native') $tools $npmCache $goCache
    Check ($env:GOFLAGS -ceq $overlayFlags -and $env:GOENV -ceq $userConfig) 'Build mutated the parent environment.'
    foreach ($key in @('GOROOT', 'GO111MODULE', 'GOCACHEPROG', 'NSISDIR', 'NSISCONFDIR', 'NODE_OPTIONS')) { Check (-not $context.environment.ContainsKey($key)) "Inherited $key was allowed." }
    Check ($context.environment.GOTOOLCHAIN -ceq 'local' -and $context.environment.GOENV -ceq 'off') 'Toolchain/user config was not pinned.'
    Add-OrcaNativeInput $context 'source/probe.go' (Join-Path $module 'probe.go')
    Add-OrcaNativeInput $context 'source/go.mod' (Join-Path $module 'go.mod')
    Put (Join-Path $FixtureRoot 'resources/fixture.nsh') 'synthetic NSIS input'
    Add-OrcaNativeInputTree $context 'nsis/Include' (Join-Path $FixtureRoot 'resources')
    $plain = Invoke-OrcaNativeStep $context 'go-list' 'go' @('list', '-e', '-json', '.') $module | ConvertFrom-Json
    Check (-not $plain.PSObject.Properties['Imports']) 'Inherited overlay changed native Go source selection.'
    $goEnv = Invoke-OrcaNativeStep $context 'go-environment' 'go' @('env', '-json', 'GOFLAGS', 'GOENV', 'GOTOOLCHAIN', 'GOPROXY') $module | ConvertFrom-Json
    # `go env GOENV` reports an empty path when the process GOENV is "off".
    Check ($goEnv.GOFLAGS -ceq '-mod=readonly -buildvcs=false' -and $goEnv.GOENV -ceq '' -and $goEnv.GOPROXY -ceq 'off') 'Actual native Go process consumed inherited config/flags.'
    # Positive controls prove this overlay and user config really replace source
    # when deliberately supplied; no compilation or network access is involved.
    $bad = $context.environment.Clone(); $bad.GOFLAGS = $overlayFlags
    $injected = Invoke-OrcaBuildStep $go @('list', '-e', '-json', '.') $module $bad (Join-Path $FixtureRoot 'overlay-control.log') | ConvertFrom-Json
    Check ($injected.Imports -contains 'synthetic.invalid/injected') 'Overlay positive control did not inject its fixture.'
    $bad.Remove('GOFLAGS'); $bad.GOENV = $userConfig
    $injected = Invoke-OrcaBuildStep $go @('list', '-e', '-json', '.') $module $bad (Join-Path $FixtureRoot 'goenv-control.log') | ConvertFrom-Json
    Check ($injected.Imports -contains 'synthetic.invalid/injected') 'User GOENV positive control did not inject its fixture.'
    foreach ($role in @('wails', 'nsis', 'sevenZip')) {
        $probe = 'if ($env:GOENV -cne "off" -or $env:GOFLAGS -cne "-mod=readonly -buildvcs=false" -or $env:GOROOT -or $env:GOCACHEPROG -or $env:NSISDIR -or $env:NSISCONFDIR -or $env:NODE_OPTIONS) { exit 9 }; Write-Output "isolated"'
        $result = Invoke-OrcaNativeStep $context ("probe-$role".ToLowerInvariant()) $role @('-NoProfile', '-NonInteractive', '-Command', $probe) $module
        Check ($result -ceq 'isolated') "Native/resource/NSIS process inherited unsafe settings: $role"
    }
    $manifest = Write-OrcaNativeManifest $context ('1' * 64) ('2' * 64)
    $info = Get-Content -LiteralPath $manifest -Raw | ConvertFrom-Json
    Check ($info.steps.Count -eq 5 -and $info.tools.Count -eq 6 -and $info.inputs.Count -eq 3) 'Native provenance omitted steps/tools/inputs.'
    Check ($info.sourceManifestSha256 -ceq ('1' * 64) -and $info.frontendManifestSha256 -ceq ('2' * 64)) 'Native provenance lost source/frontend identity.'
    Check (-not ([IO.File]::ReadAllText($manifest)).Contains($FixtureRoot.Replace('\', '\\'))) 'Published native provenance leaks absolute private paths.'
    $context.environment.GOFLAGS = $overlayFlags
    Reject { Invoke-OrcaNativeStep $context 'bad-environment' 'go' @('version') $module } 'Changed pinned environment was accepted.'
    $context.environment.GOFLAGS = '-mod=readonly -buildvcs=false'
    Put $tools.node 'tampered tool'
    Reject { Invoke-OrcaNativeStep $context 'bad-tool' 'go' @('version') $module } 'Changed pinned tool was accepted.'
    Put $tools.node 'synthetic pinned node'
    Put (Join-Path $module 'probe.go') 'tampered source'
    Reject { Invoke-OrcaNativeStep $context 'bad-source' 'go' @('version') $module } 'Changed native source input was accepted.'
    Put (Join-Path $module 'probe.go') "package overlayprobe`n"
    Put (Join-Path $FixtureRoot 'resources/new.nsh') 'unreviewed resource'
    Reject { Invoke-OrcaNativeStep $context 'bad-resource-tree' 'go' @('version') $module } 'Added toolkit resource was not detected.'
    $tokens=$null; $parseErrors=$null
    $ast=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'build-orca-beta.ps1'), [ref]$tokens, [ref]$parseErrors)
    Check ($parseErrors.Count -eq 0) 'Build script does not parse.'
    $calls=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst]}, $true))
    Check (@($calls | Where-Object { $_.InvocationOperator -eq [Management.Automation.Language.TokenKind]::Ampersand }).Count -eq 0) 'Build script still invokes a tool with inherited process environment.'
    $source=$ast.Extent.Text
    Check ($source -notmatch '\$env:[A-Za-z_]+\s*=|savedEnvironment|Invoke-Checked|Get-Command') 'Native packaging still mutates/inherits process environment or searches inherited PATH.'
    foreach ($name in @('wails-build', 'build-guard', 'locate-wails-module', 'render-installer-tools', 'inspect-executable', 'compile-installer', 'check-installer', 'test-installer', 'test-portable')) {
        Check (@($calls | Where-Object { $_.GetCommandName() -eq 'Invoke-OrcaNativeStep' -and $_.Extent.Text.Contains("'$name'") }).Count -eq 1) "Native stage bypasses the isolated runner: $name"
    }
    Check ($source.Contains("'/NOCONFIG'") -and $source.Contains("'native-build-input.json'") -and $source.Contains('nativeManifestSha256')) 'NSIS config boundary/native provenance missing.'
    Check ($source.Contains("'verify-root-modules'") -and $source.Contains("'verify-desktop-modules'")) 'Go cache integrity validation missing.'
    Write-Output "PASS: $script:checks native build isolation checks; actual Go overlay/GOENV controls and synthetic tool probes only. No application/installer build. Fixtures: $FixtureRoot"
} finally {
    foreach ($key in $old.Keys) { [Environment]::SetEnvironmentVariable($key, $old[$key], 'Process') }
}
