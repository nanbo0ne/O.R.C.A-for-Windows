#requires -Version 7.0
[CmdletBinding()]
param([string]$FixtureRoot)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'orca-source-snapshot.ps1')
. (Join-Path $PSScriptRoot 'orca-frontend-snapshot.ps1')
. (Join-Path $PSScriptRoot 'orca-native-build.ps1')
if (-not $FixtureRoot) { $FixtureRoot = Join-Path (Join-Path $PSScriptRoot '../.tmp') ('build-info-tests-' + [guid]::NewGuid().ToString('N')) }
$FixtureRoot = New-OrcaOutputDirectory ([IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))) ([IO.Path]::GetFullPath($FixtureRoot))
$script:checks = 0
function Check([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message }; $script:checks++ }
function Reject([scriptblock]$Action, [string]$Message) { $failed = $false; try { & $Action | Out-Null } catch { $failed = $true }; Check $failed $Message }
function Put([string]$Path, [string]$Text) { [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($Path)); [IO.File]::WriteAllText($Path, $Text, [Text.UTF8Encoding]::new($false)) }
$tools = @{go=(Join-Path ([Environment]::GetFolderPath('ProgramFiles')) 'Go/bin/go.exe')}
foreach ($key in @('wails', 'nsis', 'sevenZip')) { $tools[$key] = Join-Path $PSHOME 'pwsh.exe' }
foreach ($key in @('node', 'npm')) { $tools[$key] = Join-Path $FixtureRoot "tools/$key.exe"; Put $tools[$key] "synthetic $key" }
$npmCache = Join-Path $FixtureRoot 'npm-cache'; $goCache = Join-Path $FixtureRoot 'go-cache'
[void][IO.Directory]::CreateDirectory($npmCache); [void][IO.Directory]::CreateDirectory($goCache)
$module = Join-Path $FixtureRoot 'module'
Put (Join-Path $module 'go.mod') "module synthetic.invalid/buildidentity`n`ngo 1.20`n"
Copy-Item -LiteralPath (Join-Path $PSScriptRoot '../desktop/build_info.go') -Destination (Join-Path $module 'build_info.go')
Put (Join-Path $module 'main.go') @'
package main
import ("fmt"; "os"; "time")
var channel = "stable"
var version = "dev"
func main() {
    if os.Getenv("ORCA_BUILD_INFO_PRIVATE_SENTINEL") != "" || os.Getenv("GOENV") != "off" || os.Getenv("GOFLAGS") != "-mod=readonly -buildvcs=false" { os.Exit(8) }
    cwd, _ := os.Getwd()
    if cwd != os.Getenv("TEMP") || os.Getenv("HOME") == "" || os.Getenv("APPDATA") == "" { os.Exit(9) }
    if raw, err := os.ReadFile("synthetic-response.txt"); err == nil {
        switch string(raw) {
        case "hang": time.Sleep(30*time.Second); return
        case "exit": os.Exit(7)
        case "stderr": fmt.Fprintln(os.Stderr, "unexpected diagnostic")
        default: fmt.Print(string(raw)); return
        }
    }
    if handled, err := writeBuildInfoJSON(os.Args[1:], os.Stdout); handled {
        if err != nil { os.Exit(2) }; return
    }
    os.Exit(3)
}
'@
$compile = New-OrcaNativeBuildContext (Join-Path $FixtureRoot 'compile') $tools $npmCache $goCache
foreach ($name in @('go.mod', 'main.go', 'build_info.go')) { Add-OrcaNativeInput $compile "source/$name" (Join-Path $module $name) }
$expectedVersion = '3.0.14-beta.2'
$contexts = @{}
foreach ($case in @(
    @{name='beta'; flags="-X main.channel=beta -X main.version=$expectedVersion"},
    @{name='stable'; flags="-X main.channel=stable -X main.version=$expectedVersion"},
    @{name='wrong-version'; flags='-X main.channel=beta -X main.version=3.0.14-beta.1'},
    @{name='default'; flags=''}
)) {
    $exe = Join-Path $FixtureRoot ($case.name + '.exe')
    $null = Invoke-OrcaNativeStep $compile ('build-' + $case.name) 'go' @('build', '-trimpath', '-ldflags', ("-s -w -H windowsgui " + $case.flags), '-o', $exe, '.') $module
    $context = New-OrcaNativeBuildContext (Join-Path $FixtureRoot ('probe-' + $case.name)) $tools $npmCache $goCache
    Add-OrcaNativeInput $context 'generated/application' $exe
    $contexts[$case.name] = $context
}
$beta = $contexts.beta
$exe = $beta.inputs['generated/application'].path
$info = Invoke-OrcaNativeStep $compile 'inspect-trimmed-fixture' 'go' @('version', '-m', '-json', $exe) $module | ConvertFrom-Json
Check (@($info.Settings | Where-Object Key -CEQ '-ldflags').Count -eq 0) 'Fixture did not reproduce Go trimpath omission.'
Check (@($info.Settings | Where-Object { $_.Key -ceq '-trimpath' -and $_.Value -ceq 'true' }).Count -eq 1) 'Fixture lost path privacy.'
$oldSecret = [Environment]::GetEnvironmentVariable('ORCA_BUILD_INFO_PRIVATE_SENTINEL', 'Process')
try {
    $env:ORCA_BUILD_INFO_PRIVATE_SENTINEL = 'synthetic-do-not-inherit'
    Assert-OrcaExecutableBuildInfo $beta $exe $expectedVersion
    Check ($beta.steps.Count -eq 1 -and $beta.steps[0].timeoutSeconds -eq 10) 'Identity probe lost its bounded step/provenance.'
    Check ($beta.pins.application.sha256 -ceq (Get-OrcaHash $exe)) 'Identity probe was not executable-hash pinned.'
    foreach ($name in @('stable', 'wrong-version', 'default')) {
        $context = $contexts[$name]
        Reject { Assert-OrcaExecutableBuildInfo $context $context.inputs['generated/application'].path $expectedVersion } "Accepted linked $name identity."
    }
    $manifest = Write-OrcaNativeManifest $beta ('1' * 64) ('2' * 64)
    $provenance = Get-Content -LiteralPath $manifest -Raw | ConvertFrom-Json
    Check (@($provenance.tools | Where-Object { $_.tool -ceq 'application' -and $_.sha256 -ceq (Get-OrcaHash $exe) }).Count -eq 1) 'Executable identity hash missing from provenance.'
    Check ($provenance.steps[0].logSha256 -ceq (Get-OrcaHash (Join-Path $beta.directory $provenance.steps[0].log))) 'Identity JSON evidence hash missing.'
    $response = Join-Path $beta.environment.TEMP 'synthetic-response.txt'
    foreach ($bad in @(
        '', 'not-json', '[]', 'null',
        '{"schemaVersion":1,"channel":"beta"}',
        '{"schemaVersion":"1","channel":"beta","version":"3.0.14-beta.2"}',
        '{"schemaVersion":1,"channel":"Beta","version":"3.0.14-beta.2"}',
        '{"schemaVersion":1,"channel":"beta","version":"3.0.14-beta.2","extra":true}',
        '{"schemaVersion":1,"channel":"stable","channel":"beta","version":"3.0.14-beta.2"}',
        '{"schemaVersion":1,"channel":"beta","version":"3.0.14-beta.2"} trailing',
        'stderr'
    )) {
        Put $response $bad
        Reject { Assert-OrcaExecutableBuildInfo $beta $exe $expectedVersion } 'Malformed/noisy executable identity was accepted.'
    }
    # Failed processes do not get successful provenance rows. Use fresh contexts
    # so their preserved failure logs cannot collide with a later test step.
    foreach ($mode in @('exit', 'hang')) {
        $context = New-OrcaNativeBuildContext (Join-Path $FixtureRoot "probe-$mode") $tools $npmCache $goCache
        Add-OrcaNativeInput $context 'generated/application' $exe
        Put (Join-Path $context.environment.TEMP 'synthetic-response.txt') $mode
        $watch = [Diagnostics.Stopwatch]::StartNew()
        Reject { Assert-OrcaExecutableBuildInfo $context $exe $expectedVersion } "Accepted $mode probe."
        Check ($context.steps.Count -eq 0) "Failed $mode probe recorded success."
        if ($mode -ceq 'hang') {
            Check ($watch.Elapsed.TotalSeconds -ge 9 -and $watch.Elapsed.TotalSeconds -lt 20) 'Probe did not enforce its ten-second timeout.'
        }
    }
    $unpinned = New-OrcaNativeBuildContext (Join-Path $FixtureRoot 'unpinned') $tools $npmCache $goCache
    Reject { Assert-OrcaExecutableBuildInfo $unpinned $exe $expectedVersion } 'Unpinned executable was launched.'
    [IO.File]::AppendAllText($exe, 'tamper')
    Reject { Assert-OrcaExecutableBuildInfo $beta $exe $expectedVersion } 'Changed executable was launched.'
    $tokens=$null; $errors=$null
    $ast=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'build-orca-beta.ps1'), [ref]$tokens, [ref]$errors)
    $assert=$ast.Find({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq 'Assert-BetaExecutable'}, $false).Extent.Text
    Check ($assert.Contains('Assert-OrcaExecutableBuildInfo $native $Path $version') -and -not $assert.Contains("'-ldflags'")) 'Packaging still relies on omitted linker metadata.'
    Check ($ast.Extent.Text.Contains("'-trimpath'")) 'Packaging disabled path privacy.'
    Write-Output "PASS: $script:checks build identity checks; linked synthetic Windows GUI probes only. No application/installer build. Fixtures: $FixtureRoot"
} finally {
    [Environment]::SetEnvironmentVariable('ORCA_BUILD_INFO_PRIVATE_SENTINEL', $oldSecret, 'Process')
}
