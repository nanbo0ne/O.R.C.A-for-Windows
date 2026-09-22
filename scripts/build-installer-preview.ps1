#requires -Version 7.0
[CmdletBinding()]
param(
    [ValidateSet('amd64', 'arm64')]
    [string]$Architecture = 'amd64',
    [switch]$SkipAppBuild,
    [string]$OutputDirectory = 'D:\AI-Reasonix\dist\desktop-v3.0.12-preview'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw 'The installer preview requires Windows.' }

$version = '3.0.12'
$repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$desktop = Join-Path $repo 'desktop'
$output = [IO.Path]::GetFullPath($OutputDirectory)
$appDirectory = Join-Path $output "app\$Architecture"
$app = Join-Path $appDirectory 'Orca.exe'
$preview = Join-Path $output "O.R.C.A-for-Windows-v$version-windows-$Architecture-preview-installer.exe"
$utf8 = [Text.UTF8Encoding]::new($false)

function Find-Tool([string]$Name, [string[]]$Candidates = @()) {
    $command = Get-Command $Name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($command) { return $command.Source }
    foreach ($candidate in $Candidates) {
        if ($candidate -and (Test-Path -LiteralPath $candidate -PathType Leaf)) { return $candidate }
    }
    throw "Required tool not found: $Name"
}

function Invoke-Checked([string]$Tool, [string[]]$Arguments) {
    & $Tool @Arguments 2>&1 | Tee-Object -FilePath (Join-Path $stage 'build.log') -Append | Out-Host
    if ($LASTEXITCODE -ne 0) { throw "$Tool failed with exit code $LASTEXITCODE" }
}

function Assert-PE([string]$Path, [int]$Machine, [int]$Subsystem) {
    $reader = [IO.BinaryReader]::new([IO.File]::OpenRead($Path))
    try {
        if ($reader.ReadUInt16() -ne 0x5a4d) { throw "Invalid executable: $Path" }
        $reader.BaseStream.Position = 0x3c
        $offset = $reader.ReadInt32()
        $reader.BaseStream.Position = $offset
        if ($reader.ReadUInt32() -ne 0x4550 -or $reader.ReadUInt16() -ne $Machine) {
            throw "Wrong executable architecture: $Path"
        }
        $reader.BaseStream.Position = $offset + 24 + 68
        if ($reader.ReadUInt16() -ne $Subsystem) { throw "Wrong executable subsystem: $Path" }
    } finally { $reader.Dispose() }
}

function Assert-Version([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "Missing executable: $Path" }
    $info = [Diagnostics.FileVersionInfo]::GetVersionInfo($Path)
    foreach ($actual in @($info.FileVersion, $info.ProductVersion)) {
        if ($actual -notmatch ('^' + [regex]::Escape($version) + '(?:\.0)?$')) {
            throw "Wrong version '$actual' in $Path; rebuild the $version preview without -SkipAppBuild."
        }
    }
}

function Assert-App([string]$Path) {
    Assert-Version $Path
    Assert-PE $Path $machine 2
    $buildInfoJSON = & $go version -m -json $Path
    if ($LASTEXITCODE -ne 0) { throw "Cannot read Go build information: $Path" }
    $buildInfo = $buildInfoJSON | ConvertFrom-Json
    $ldflags = @($buildInfo.Settings | Where-Object Key -EQ '-ldflags' | Select-Object -ExpandProperty Value) -join ' '
    if ($ldflags -notmatch ('(?:^|\s)-X main\.version=v' + [regex]::Escape($version) + '(?:\s|$)')) {
        throw "The application lacks the v$version Go version marker: $Path"
    }
}

$go = Find-Tool 'go.exe'
$git = Get-Command git.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
$bashCandidates = @("$env:ProgramFiles\Git\bin\bash.exe")
if ($git) { $bashCandidates += Join-Path (Split-Path (Split-Path $git.Source)) 'bin\bash.exe' }
$bash = Find-Tool 'bash.exe' $bashCandidates
$makensis = Find-Tool 'makensis.exe' @("${env:ProgramFiles(x86)}\NSIS\makensis.exe", "$env:ProgramFiles\NSIS\makensis.exe")
$sevenZip = Find-Tool '7z.exe' @("$env:ProgramFiles\7-Zip\7z.exe")
$machine = if ($Architecture -eq 'amd64') { 0x8664 } else { 0xaa64 }
if ($SkipAppBuild) {
    Assert-App $app
} else {
    $wails = Find-Tool 'wails.exe' @("$env:USERPROFILE\go\bin\wails.exe")
    [void](Find-Tool 'node.exe')
    [void](Find-Tool 'npm.cmd')
    if (-not (Test-Path -LiteralPath (Join-Path $desktop 'frontend\node_modules\.bin\vite.cmd'))) {
        throw 'Cached frontend dependencies are required; this preview script does not install them.'
    }
}

# Wails reads wails.json from its working directory, but supports an explicit
# projectdir/build:dir. Only these disposable copies receive preview metadata.
$stageParent = Join-Path $repo '.tmp\installer-preview'
$stage = Join-Path $stageParent ([guid]::NewGuid().ToString('N'))
$stageDesktop = Join-Path $stage 'desktop'
$build = Join-Path $stageDesktop 'build'
$windows = Join-Path $build 'windows'
$installer = Join-Path $windows 'installer'
$savedPrepOnly = $env:ORCA_PACKAGING_PREP_ONLY
try {
    [void][IO.Directory]::CreateDirectory($installer)
    [void][IO.Directory]::CreateDirectory((Join-Path $build 'bin'))
    [void][IO.Directory]::CreateDirectory($appDirectory)
    Write-Host "Retained preview staging directory: $stage"

    $env:ORCA_PACKAGING_PREP_ONLY = '1'
    Push-Location $repo
    try { Invoke-Checked $bash @('--noprofile', '--norc', 'scripts/desktop-build.sh', "windows/$Architecture", "v$version", 'stable') }
    finally { Pop-Location; $env:ORCA_PACKAGING_PREP_ONLY = $savedPrepOnly }

    $sidecar = Join-Path $desktop 'build\windows\installer-go'
    Assert-PE (Join-Path $sidecar 'orca-install-guard.exe') $machine 2
    Copy-Item -LiteralPath $sidecar -Destination (Join-Path $windows 'installer-go') -Recurse
    Copy-Item -LiteralPath (Join-Path $repo 'THIRD-PARTY-NOTICES.txt') -Destination $stage
    Copy-Item -LiteralPath (Join-Path $desktop 'build\appicon.png') -Destination $build
    Copy-Item -LiteralPath (Join-Path $desktop 'build\windows\icon.ico') -Destination $windows
    Copy-Item -LiteralPath (Join-Path $desktop 'build\windows\installer\project.nsi') -Destination $installer
    foreach ($include in Get-ChildItem -LiteralPath (Join-Path $desktop 'build\windows\installer') -Filter '*.nsh' -File) {
        if ($include.Name -ne 'wails_tools.nsh') {
            Copy-Item -LiteralPath $include.FullName -Destination (Join-Path $installer $include.Name)
        }
    }
    Copy-Item -LiteralPath (Join-Path $desktop 'build\windows\installer\resources') -Destination $installer -Recurse

    $config = Get-Content -LiteralPath (Join-Path $desktop 'wails.json') -Raw | ConvertFrom-Json -AsHashtable
    $config.info.productVersion = $version
    $config['projectdir'] = $desktop
    $config['build:dir'] = $build
    $config['frontend:dir'] = Join-Path $desktop 'frontend'
    $config['frontend:install'] = ''
    [IO.File]::WriteAllText((Join-Path $stageDesktop 'wails.json'), ($config | ConvertTo-Json -Depth 30), $utf8)
    $info = Get-Content -LiteralPath (Join-Path $desktop 'build\windows\info.json') -Raw | ConvertFrom-Json -AsHashtable
    $info.fixed.file_version = "$version.0"
    $info.fixed.product_version = "$version.0"
    foreach ($language in $info.info.Values) {
        $language.FileVersion = "$version.0"
        $language.ProductVersion = "$version.0"
    }
    [IO.File]::WriteAllText((Join-Path $windows 'info.json'), ($info | ConvertTo-Json -Depth 30), $utf8)

    if (-not $SkipAppBuild) {
        Push-Location $desktop
        try { Invoke-Checked $wails @('generate', 'module', '-nocolour') }
        finally { Pop-Location }
        Push-Location $stageDesktop
        try {
            Invoke-Checked $wails @('build', '-platform', "windows/$Architecture", '-m', '-nosyncgomod', '-skipbindings',
                '-webview2', 'embed', '-nocolour', '-o', 'Orca.exe', '-ldflags', "-X main.version=v$version -X main.channel=stable")
        } finally { Pop-Location }
        $builtApp = Join-Path $build 'bin\Orca.exe'
        Assert-App $builtApp
        Copy-Item -LiteralPath $builtApp -Destination $app -Force
    }
    Assert-App $app

    # Use the project's pinned Wails module for NSIS macros and the embedded
    # WebView2 bootstrapper, even on a checkout without generated installer files.
    Push-Location $desktop
    try {
        $wailsModule = & $go list -m -f '{{.Dir}}' github.com/wailsapp/wails/v2
        if ($LASTEXITCODE -ne 0 -or -not $wailsModule) { throw 'Cannot locate the cached Wails module.' }
    } finally { Pop-Location }
    # Render with Go's template engine, including Wails' association/protocol
    # loops. Copying the raw module template would leave invalid NSIS commands.
    $renderer = Join-Path $stage 'render-wails-tools.go'
    [IO.File]::WriteAllText($renderer, @'
package main

import (
    "encoding/json"
    "os"
    "text/template"
)

func check(err error) { if err != nil { panic(err) } }

func main() {
    var project struct {
        Name string
        Info struct {
            CompanyName, ProductName, ProductVersion, Copyright string
            FileAssociations []struct { Ext, Name, Description, IconName, Role string }
            Protocols []struct { Scheme, Description, Role string }
        }
    }
    config, err := os.ReadFile(os.Args[2]); check(err)
    check(json.Unmarshal(config, &project))
    source, err := os.ReadFile(os.Args[1]); check(err)
    parsed, err := template.New("wails_tools").Option("missingkey=error").Parse(string(source)); check(err)
    out, err := os.Create(os.Args[3]); check(err)
    check(parsed.Execute(out, project))
    check(out.Close())
}
'@, $utf8)
    Invoke-Checked $go @('run', $renderer,
        (Join-Path $wailsModule 'pkg\buildassets\build\windows\installer\wails_tools.nsh'),
        (Join-Path $stageDesktop 'wails.json'), (Join-Path $installer 'wails_tools.nsh'))
    [void][IO.Directory]::CreateDirectory((Join-Path $installer 'tmp'))
    Copy-Item -LiteralPath (Join-Path $wailsModule 'internal\webview2runtime\MicrosoftEdgeWebview2Setup.exe') -Destination (Join-Path $installer 'tmp')
    $nsisArgs = @('/V2', '/INPUTCHARSET', 'UTF8', '/DORCA_PREVIEW', "/DINFO_PRODUCTVERSION=$version",
        "/DINFO_PROJECTNAME=$($config.name)", "/DINFO_COMPANYNAME=$($config.info.companyName)",
        "/DINFO_PRODUCTNAME=$($config.info.productName)", "/DINFO_COPYRIGHT=$($config.info.copyright)",
        "/DARG_WAILS_$($Architecture.ToUpperInvariant())_BINARY=$app", 'project.nsi')
    Write-Host "NSIS working directory: $installer"
    Write-Host ('NSIS command: & ''' + $makensis + ''' ' + (($nsisArgs | ForEach-Object { "'" + $_.Replace("'", "''") + "'" }) -join ' '))
    Push-Location $installer
    try { Invoke-Checked $makensis $nsisArgs }
    finally { Pop-Location }

    $compiled = Join-Path $build "bin\O.R.C.A-for-Windows-windows-$Architecture-installer.exe"
    Assert-Version $compiled
    Invoke-Checked $go @('-C', $desktop, 'run', './cmd/nsischeck', $compiled)
    Invoke-Checked $sevenZip @('t', $compiled)
    Copy-Item -LiteralPath $compiled -Destination $preview -Force
    $sourceHash = (Get-FileHash -LiteralPath $compiled -Algorithm SHA256).Hash
    $previewHash = (Get-FileHash -LiteralPath $preview -Algorithm SHA256).Hash
    if ($sourceHash -cne $previewHash) { throw 'Preview installer copy failed SHA-256 verification.' }
    Write-Host "Preview installer: $preview"
    Write-Host "Application: $app"
    Write-Host "SHA256: $($previewHash.ToLowerInvariant())"
} finally {
    $env:ORCA_PACKAGING_PREP_ONLY = $savedPrepOnly
    Write-Host "Preview staging retained for NSIS iteration: $stage"
}
