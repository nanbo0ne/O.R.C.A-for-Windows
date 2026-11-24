#requires -Version 7.0
[CmdletBinding()]
param(
    [switch]$Package,
    [switch]$PrepareFrontend,
    [switch]$PrepareCandidate,
    [switch]$IntegrationGatesPassed,
    [string]$PayloadDirectory,
    [string]$CodeGraphArchive,
    [string]$SourceAllowlist,
    [string]$NodeExecutable,
    [string]$NpmCli,
    [string]$GoExecutable,
    [string]$WailsExecutable,
    [string]$MakensisExecutable,
    [string]$SevenZipExecutable,
    [string]$NpmCacheDirectory,
    [string]$GoModuleCacheDirectory,
    [string]$OutputDirectory
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw 'ORCA beta packaging requires Windows.' }
$version = '3.0.14-beta.2'
$repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$desktop = Join-Path $repo 'desktop'
if (($Package -and $PrepareFrontend) -or ($Package -and $PrepareCandidate) -or ($PrepareFrontend -and $PrepareCandidate)) {
    throw 'Choose exactly one build mode.'
}
if ($PrepareCandidate) {
    if ($IntegrationGatesPassed) { throw 'Candidate mode cannot assert completed acceptance.' }
    if (-not $OutputDirectory) { throw 'Candidate output must be explicitly below this worktree .tmp directory.' }
    $candidateRoot = [IO.Path]::GetFullPath((Join-Path $repo '.tmp')).TrimEnd('\') + '\'
    $candidateOutput = [IO.Path]::GetFullPath($OutputDirectory)
    if (-not $candidateOutput.StartsWith($candidateRoot, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Candidate output must be explicitly below this worktree .tmp directory.'
    }
}
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $repo "dist\orca-beta-$version" }
if (-not $PayloadDirectory) { $PayloadDirectory = Join-Path $desktop 'build\windows\installer-go\payload' }
if (-not $CodeGraphArchive) { $CodeGraphArchive = Join-Path $repo '.tmp\orca-codegraph-cache\codegraph-win32-x64.zip' }
$utf8 = [Text.UTF8Encoding]::new($false)
. (Join-Path $PSScriptRoot 'orca-source-snapshot.ps1') -OutputDirectory $OutputDirectory
. (Join-Path $PSScriptRoot 'orca-frontend-snapshot.ps1')
. (Join-Path $PSScriptRoot 'orca-native-build.ps1')

function Find-Tool([string]$Name, [string]$Explicit, [string[]]$Candidates = @()) {
    # Do not resolve build executables through an inherited PATH.
    $paths = if ($Explicit) { @($Explicit) } else { $Candidates }
    foreach ($candidate in $paths) {
        if ([IO.Path]::IsPathFullyQualified($candidate) -and (Test-Path -LiteralPath $candidate -PathType Leaf)) { return (Assert-OrcaPlainPath $candidate) }
    }
    throw "Required tool not found: $Name"
}

function Assert-Payload([string]$Directory) {
    [void](Assert-OrcaPlainPath $Directory)
    [void](Assert-OrcaPlainPath $CodeGraphArchive)
    if ((Get-FileHash -LiteralPath (Join-Path $Directory 'node.exe') -Algorithm SHA256).Hash -ne '0d0f5e39f9f3d9587bc19f73eab3c2c9c4903fd02d6dbf9c853dd81b3d95fad4') {
        throw 'Pinned Windows amd64 Node runtime is missing or changed.'
    }
    if ((Get-FileHash -LiteralPath $CodeGraphArchive -Algorithm SHA256).Hash -ne 'a5571d3ee54cc1caac76bf09e0f7cb350fc4dd6788a5437217eac33b71fa7a15') {
        throw 'Pinned CodeGraph archive is missing or changed.'
    }
    if (-not (Test-Path -LiteralPath (Join-Path $Directory 'LICENSE.node.txt') -PathType Leaf)) { throw 'Node license is missing.' }
    $tree = Join-Path $Directory 'codegraph'
    $plainFiles = @(Get-OrcaPlainFiles $tree)
    $zip = [IO.Compression.ZipFile]::OpenRead($CodeGraphArchive)
    try {
        $count = 0
        foreach ($entry in $zip.Entries) {
            if ($entry.FullName.EndsWith('/')) { continue }
            $parts = $entry.FullName.Split('/', 2)
            if ($parts.Length -ne 2 -or $parts[1] -match '(^|/)\.\.(/|$)' -or [IO.Path]::IsPathRooted($parts[1])) { throw 'Unsafe archive layout.' }
            $stream = $entry.Open()
            try { $expected = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($stream)) }
            finally { $stream.Dispose() }
            if ((Get-FileHash -LiteralPath (Join-Path $tree $parts[1]) -Algorithm SHA256).Hash -ne $expected) {
                throw "CodeGraph payload differs from pinned archive: $($parts[1])"
            }
            $count++
        }
    if ($plainFiles.Count -ne $count) { throw 'Unexpected CodeGraph files.' }
    } finally { $zip.Dispose() }
}

function Assert-BetaExecutable([string]$Path) {
    if ([IO.Path]::GetFileName($Path) -cne 'OrcaBeta.exe') { throw 'Unexpected beta executable name.' }
    $details = [Diagnostics.FileVersionInfo]::GetVersionInfo($Path)
    if ($details.ProductName -ne 'ORCA beta' -or $details.ProductVersion -ne $version -or
        $details.FileVersion -ne '3.0.14.2' -or $details.OriginalFilename -ne 'OrcaBeta.exe') {
        throw 'Unexpected beta executable identity.'
    }
    $buildInfo = Invoke-OrcaNativeStep $native 'inspect-executable' 'go' @('version', '-m', '-json', $Path) $stagedDesktop | ConvertFrom-Json
    if ($buildInfo.Path -cne 'github.com/nanbo0ne/O.R.C.A-for-Windows/desktop' -or
        @($buildInfo.Settings | Where-Object { $_.Key -ceq '-trimpath' -and $_.Value -ceq 'true' }).Count -ne 1) {
        throw 'Unexpected Go application identity/path privacy settings.'
    }
    Assert-OrcaExecutableBuildInfo $native $Path $version
}

function Get-BetaPortableFiles([string]$Executable, [string]$Payload, [string]$LicenseRoot) {
    $files = @{
        'OrcaBeta.exe' = $Executable
        'node.exe' = (Join-Path $Payload 'node.exe')
        'LICENSE.node.txt' = (Join-Path $Payload 'LICENSE.node.txt')
        'LICENSE' = (Join-Path $LicenseRoot 'LICENSE')
        'THIRD-PARTY-NOTICES.txt' = (Join-Path $LicenseRoot 'THIRD-PARTY-NOTICES.txt')
    }
    # Assert-Payload has verified this entire CodeGraph tree against the pinned
    # archive, including its licenses. No other checkout/payload files belong here.
    $tree = Join-Path $Payload 'codegraph'
    foreach ($file in Get-ChildItem -LiteralPath $tree -File -Recurse -Force) {
        $relative = [IO.Path]::GetRelativePath($tree, $file.FullName).Replace('\', '/')
        $files.Add('codegraph/' + $relative, $file.FullName)
    }
    return $files
}

function Write-BetaPortableArchive([string]$Archive, [hashtable]$Files) {
    $required = @('OrcaBeta.exe', 'node.exe', 'LICENSE.node.txt', 'LICENSE', 'THIRD-PARTY-NOTICES.txt')
    foreach ($name in $required) {
        if (-not $Files.ContainsKey($name)) { throw "Missing portable member: $name" }
    }
    if (@($Files.Keys | Where-Object { $_.StartsWith('codegraph/', [StringComparison]::Ordinal) }).Count -eq 0) {
        throw 'CodeGraph runtime is missing from portable files.'
    }
    $hashes = @{}
    foreach ($name in $Files.Keys) {
        if (($required -cnotcontains $name -and -not $name.StartsWith('codegraph/', [StringComparison]::Ordinal)) -or
            $name -match '(^/|\\|:|(^|/)\.{1,2}(/|$)|//|/$)') { throw "Unexpected portable member: $name" }
        $path = [IO.Path]::GetFullPath($Files[$name])
        for ($p = $path; $p; $p = [IO.Path]::GetDirectoryName($p)) {
            if ((Get-Item -LiteralPath $p -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) {
                throw "Portable input is redirected: $p"
            }
        }
        $hashes[$name] = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash
    }
    # Create mode refuses to overwrite an earlier artifact.
    $zip = [IO.Compression.ZipFile]::Open($Archive, [IO.Compression.ZipArchiveMode]::Create)
    try {
        foreach ($name in ($Files.Keys | Sort-Object)) {
            [void][IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, $Files[$name], $name, [IO.Compression.CompressionLevel]::Optimal)
        }
    } finally { $zip.Dispose() }
    Assert-BetaPortableArchive $Archive $hashes
    return $hashes
}

function Assert-BetaPortableArchive([string]$Archive, [hashtable]$Hashes) {
    $zip = [IO.Compression.ZipFile]::OpenRead($Archive)
    try {
        if ($zip.Entries.Count -ne $Hashes.Count) { throw 'Portable member count mismatch.' }
        $seen = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
        foreach ($entry in $zip.Entries) {
            if (-not $seen.Add($entry.FullName) -or @($Hashes.Keys) -cnotcontains $entry.FullName) {
                throw "Unexpected/duplicate portable entry: $($entry.FullName)"
            }
            $stream = $entry.Open()
            try { $hash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($stream)) }
            finally { $stream.Dispose() }
            if ($hash -cne $Hashes[$entry.FullName]) { throw "Portable hash mismatch: $($entry.FullName)" }
        }
    } finally { $zip.Dispose() }
}

if (-not $Package -and -not $PrepareCandidate) {
  if (-not $PrepareFrontend) {
    $selection = Get-OrcaSourcePlan $repo $SourceAllowlist
    [pscustomobject]@{
        Mode = 'Preflight only; no builds or packaging'
        Version = $version; Channel = 'beta'; Executable = 'OrcaBeta.exe'
        Artifacts = @("ORCA-beta-$version-windows-amd64-installer.exe", "ORCA-beta-$version-windows-amd64.zip", "ORCA-beta-$version-source.zip")
        SourceSelectionAllowed = $selection.allowed
        UnreviewedNewSource = $selection.unexpected
        SourceSelectionErrors = $selection.errors
        ExcludedSource = $selection.excluded
        FrontendPolicy = 'Mandatory fresh offline snapshot rebuild; checkout dist is never an input'
        PayloadPresent = Test-Path -LiteralPath (Join-Path $PayloadDirectory 'node.exe')
        CodeGraphArchivePresent = Test-Path -LiteralPath $CodeGraphArchive
        OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
    }
    return
  }
}
if ($Package) {
    if (-not $IntegrationGatesPassed) { throw 'Packaging requires explicit -IntegrationGatesPassed after parent integration gates.' }
}
$sourcePlan = Get-OrcaSourcePlan $repo $SourceAllowlist
if (-not $sourcePlan.allowed) { throw 'Source selection is not approved. Run the selection audit and obtain a reviewed allowlist first.' }
$programFiles = [Environment]::GetFolderPath('ProgramFiles')
$programFilesX86 = [Environment]::GetFolderPath('ProgramFilesX86')
$go = Find-Tool 'go.exe' $GoExecutable @((Join-Path $programFiles 'Go/bin/go.exe'))
$wails = Find-Tool 'wails.exe' $WailsExecutable @((Join-Path ([Environment]::GetFolderPath('UserProfile')) 'go/bin/wails.exe'))
if (-not $NodeExecutable -or -not $NpmCli -or -not $NpmCacheDirectory -or -not $GoModuleCacheDirectory) { throw 'Specify Node 24, npm CLI, and explicit offline npm/Go cache paths for a fresh frontend build.' }
if ($Package -or $PrepareCandidate) {
    $makensis = Find-Tool 'makensis.exe' $MakensisExecutable @((Join-Path $programFilesX86 'NSIS/makensis.exe'))
    $sevenZip = Find-Tool '7z.exe' $SevenZipExecutable @((Join-Path $programFiles '7-Zip/7z.exe'))
    Assert-Payload $PayloadDirectory
}

# Snapshot source into a unique directory. Never rewrite the shared checkout's
# wails.json, info.json, frontend, generated bindings, or resource object files.
$stage = Join-Path $repo ('.tmp\orca-beta-build\' + [guid]::NewGuid().ToString('N'))
if ($PrepareFrontend) { $stage = [IO.Path]::GetFullPath($OutputDirectory) }
$source = Join-Path $stage 'source'
$stagedDesktop = Join-Path $source 'desktop'
$build = Join-Path $stagedDesktop 'build'
$windows = Join-Path $build 'windows'
$installer = Join-Path $windows 'installer'
$output = Assert-OrcaPlainPath $OutputDirectory
foreach ($inputRoot in @($PayloadDirectory, [IO.Path]::GetDirectoryName([IO.Path]::GetFullPath($CodeGraphArchive)))) {
    $inputRoot = [IO.Path]::GetFullPath($inputRoot).TrimEnd('\')
    if ($output.Equals($inputRoot, [StringComparison]::OrdinalIgnoreCase) -or $output.StartsWith($inputRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Output must not modify the read-only payload/archive input directories.'
    }
}
if (Test-Path -LiteralPath $output) { throw 'Output directory already exists; select a new directory to preserve prior artifacts.' }
try {
    # Keep the pristine source/ZIP separate from mutable beta build identity.
    $snapshot = New-OrcaSourceSnapshot $sourcePlan (Join-Path $stage 'snapshot') -Zip
    $frontendBuild = Build-OrcaSnapshotFrontend $snapshot $source (Join-Path $stage 'frontend-verification') @{node=$NodeExecutable; npm=$NpmCli; go=$go; wails=$wails} $NpmCacheDirectory $GoModuleCacheDirectory
    $frontendManifest = $frontendBuild.manifest
    Assert-OrcaBuiltFrontend $frontendBuild
    if ($PrepareFrontend) {
        [pscustomobject]@{Mode='Source snapshot and fresh frontend verified; no application or installer built'; SourceManifest=$snapshot.manifest; FrontendManifest=$frontendManifest; SourceZip=$snapshot.archive}
        return
    }
    [void][IO.Directory]::CreateDirectory((Join-Path $windows 'installer-go'))
    $payloadFiles = @('node.exe', 'LICENSE.node.txt') + @(Get-OrcaPlainFiles (Join-Path $PayloadDirectory 'codegraph') | ForEach-Object { [IO.Path]::GetRelativePath($PayloadDirectory, $_).Replace('\', '/') })
    $payloadInputs = @($payloadFiles | ForEach-Object { @{path=$_; sha256=(Get-OrcaHash (Join-Path $PayloadDirectory $_))} })
    Copy-OrcaVerifiedFiles $PayloadDirectory (Join-Path $windows 'installer-go\payload') $payloadInputs
    Assert-Payload (Join-Path $windows 'installer-go\payload')
    $native = New-OrcaNativeBuildContext (Join-Path $stage 'native-verification') @{node=$NodeExecutable; npm=$NpmCli; go=$go; wails=$wails; nsis=$makensis; sevenZip=$sevenZip} $NpmCacheDirectory $GoModuleCacheDirectory
    $frontendInfo = Get-Content -LiteralPath $frontendManifest -Raw | ConvertFrom-Json -AsHashtable
    foreach ($key in @('node', 'npm', 'go', 'wails')) {
        if ($native.pins[$key].sha256 -cne $frontendInfo.tools[$key].sha256) { throw "Tool changed since frontend verification: $key" }
    }
    $nsisRoot = [IO.Path]::GetDirectoryName($makensis)
    foreach ($dir in @('Bin', 'Include', 'Contrib', 'Plugins', 'Stubs')) { Add-OrcaNativeInputTree $native "nsis/$dir" (Join-Path $nsisRoot $dir) }
    $sevenZipLibrary = Join-Path ([IO.Path]::GetDirectoryName($sevenZip)) '7z.dll'
    if (Test-Path -LiteralPath $sevenZipLibrary) { Add-OrcaNativeInput $native 'sevenzip/7z.dll' $sevenZipLibrary }
    foreach ($file in $payloadInputs) { Add-OrcaNativeInput $native ('payload/' + $file.path) (Join-Path $windows ('installer-go/payload/' + $file.path)) }
    Add-OrcaNativeInput $native 'codegraph-archive' $CodeGraphArchive
    Add-OrcaNativeInput $native 'metadata/source-manifest' $snapshot.manifest
    Add-OrcaNativeInput $native 'metadata/frontend-manifest' $frontendManifest
    $config = Get-Content -LiteralPath (Join-Path $stagedDesktop 'wails.json') -Raw | ConvertFrom-Json -AsHashtable
    $config.name = 'orca-beta'; $config.outputfilename = 'OrcaBeta'
    $config.info.productName = 'ORCA beta'; $config.info.productVersion = '3.0.14'
    $config.info.fileAssociations = @(); $config.info.protocols = @()
    $config['frontend:install'] = ''; $config['frontend:build'] = ''
    [IO.File]::WriteAllText((Join-Path $stagedDesktop 'wails.json'), ($config | ConvertTo-Json -Depth 30), $utf8)
    $info = Get-Content -LiteralPath (Join-Path $windows 'info.json') -Raw | ConvertFrom-Json -AsHashtable
    $info.fixed.file_version = '3.0.14.2'; $info.fixed.product_version = '3.0.14.2'
    foreach ($language in $info.info.Values) {
        $language.FileVersion = '3.0.14.2'; $language.ProductVersion = $version
        $language.ProductName = 'ORCA beta'; $language.FileDescription = 'ORCA beta'
        $language.InternalName = 'OrcaBeta'; $language.OriginalFilename = 'OrcaBeta.exe'
    }
    [IO.File]::WriteAllText((Join-Path $windows 'info.json'), ($info | ConvertTo-Json -Depth 30), $utf8)
    # Pin the effective sources, including only these two staged identity changes.
    foreach ($file in $snapshot.files) {
        $sourceInput = Join-Path $source $file.path
        if ($file.path -cnotin @('desktop/wails.json', 'desktop/build/windows/info.json') -and (Get-OrcaHash $sourceInput) -cne $file.sha256) { throw "Staged source differs from snapshot: $($file.path)" }
        Add-OrcaNativeInput $native ('source/' + $file.path) $sourceInput
    }
    foreach ($file in $frontendBuild.bundle.files) { Add-OrcaNativeInput $native ('frontend/' + $file.path) (Join-Path $frontendBuild.bundle.root $file.path) }
    $buildInputs = [ordered]@{
        schemaVersion=1; version=$version; channel='beta'; baseCommit=$sourcePlan.git.head
        sourceManifestSha256=$snapshot.manifestSha256; sourceSelectionSha256=$sourcePlan.selectionSha256
        frontendManifestSha256=(Get-OrcaHash $frontendManifest)
        stagedIdentity=@{ 'desktop/wails.json'=(Get-OrcaHash (Join-Path $stagedDesktop 'wails.json')); 'desktop/build/windows/info.json'=(Get-OrcaHash (Join-Path $windows 'info.json')) }
    }
    $null = Invoke-OrcaNativeStep $native 'verify-root-modules' 'go' @('mod', 'verify') $source
    $null = Invoke-OrcaNativeStep $native 'verify-desktop-modules' 'go' @('mod', 'verify') $stagedDesktop
    $moduleInfo = Invoke-OrcaNativeStep $native 'locate-wails-module' 'go' @('list', '-m', '-json', 'github.com/wailsapp/wails/v2') $stagedDesktop | ConvertFrom-Json
    if ($moduleInfo.Path -cne 'github.com/wailsapp/wails/v2' -or $moduleInfo.PSObject.Properties['Replace']) { throw 'Unexpected/replaced Wails module.' }
    $module = Assert-OrcaPlainPath $moduleInfo.Dir
    $moduleRelative = [IO.Path]::GetRelativePath($GoModuleCacheDirectory, $module).Replace('\', '/')
    if ([IO.Path]::IsPathRooted($moduleRelative) -or $moduleRelative -eq '..' -or $moduleRelative.StartsWith('../')) { throw 'Wails module is outside the explicit cache.' }
    $template = Join-Path $module 'pkg/buildassets/build/windows/installer/wails_tools.nsh'
    $bootstrapper = Join-Path $module 'internal/webview2runtime/MicrosoftEdgeWebview2Setup.exe'
    Add-OrcaNativeInput $native 'wails/installer-template' $template
    Add-OrcaNativeInput $native 'wails/webview2-bootstrapper' $bootstrapper
    $null = Invoke-OrcaNativeStep $native 'wails-build' 'wails' @('build', '-s', '-m', '-nosyncgomod', '-skipbindings', '-trimpath', '-nocolour', '-platform', 'windows/amd64', '-o', 'OrcaBeta.exe', '-ldflags', "-X main.version=$version -X main.channel=beta") $stagedDesktop
    $guard = Join-Path $installer 'orca-beta-guard.exe'
    $null = Invoke-OrcaNativeStep $native 'build-guard' 'go' @('build', '-trimpath', '-o', $guard, './build/windows/installer/beta_guard') $stagedDesktop
    Add-OrcaNativeInput $native 'generated/guard' $guard
    $rendered = Join-Path $installer 'wails_tools.nsh'
    $null = Invoke-OrcaNativeStep $native 'render-installer-tools' 'go' @('run', './build/windows/installer/render_beta_tools', $template, 'wails.json', $rendered) $stagedDesktop
    Add-OrcaNativeInput $native 'generated/installer-tools' $rendered
    Assert-OrcaBuiltFrontend $frontendBuild
    $app = Join-Path $build 'bin\OrcaBeta.exe'
    Add-OrcaNativeInput $native 'generated/application' $app
    Assert-BetaExecutable $app
    $appHash = (Get-FileHash -LiteralPath $app -Algorithm SHA256).Hash
    if ($PrepareCandidate) {
        # A runnable internal candidate is necessary for real login acceptance.
        # It never creates installer/portable/source delivery archives.
        if (Test-Path -LiteralPath $output) { throw 'Candidate output appeared during the build.' }
        [void](Assert-OrcaPlainPath $output)
        [void][IO.Directory]::CreateDirectory($output)
        $candidateFiles = Get-BetaPortableFiles $app (Join-Path $windows 'installer-go/payload') $source
        $candidateHashes = @{}
        foreach ($name in $candidateFiles.Keys) {
            $destination = Join-Path $output $name
            [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($destination))
            Copy-Item -LiteralPath $candidateFiles[$name] -Destination $destination
            $expected = Get-OrcaHash $candidateFiles[$name]
            if ((Get-OrcaHash $destination) -cne $expected) { throw "Candidate copy hash mismatch: $name" }
            $candidateHashes[$name] = $expected
        }
        $nativeManifest = Write-OrcaNativeManifest $native $snapshot.manifestSha256 (Get-OrcaHash $frontendManifest)
        Write-OrcaJson (Join-Path $output 'candidate.json') @{
            schemaVersion=1; version=$version; channel='beta'; status='awaiting-real-acceptance'
            files=$candidateHashes; sourceManifestSha256=$snapshot.manifestSha256
            frontendManifestSha256=(Get-OrcaHash $frontendManifest)
            nativeManifestSha256=(Get-OrcaHash $nativeManifest); staging=$stage
        }
        [pscustomobject]@{Mode='Internal candidate only; not accepted or packaged'; Directory=$output; ExecutableSha256=$appHash}
        return
    }
    [void][IO.Directory]::CreateDirectory((Join-Path $installer 'tmp'))
    $stagedBootstrapper = Join-Path $installer 'tmp/MicrosoftEdgeWebview2Setup.exe'
    [IO.File]::Copy($bootstrapper, $stagedBootstrapper, $false)
    if ((Get-OrcaHash $stagedBootstrapper) -cne $native.inputs['wails/webview2-bootstrapper'].sha256) { throw 'Bootstrapper copy hash mismatch.' }
    Add-OrcaNativeInput $native 'generated/webview2-bootstrapper' $stagedBootstrapper
    $null = Invoke-OrcaNativeStep $native 'compile-installer' 'nsis' @('/NOCONFIG', '/V2', '/INPUTCHARSET', 'UTF8', "/DARG_WAILS_AMD64_BINARY=$app", 'beta.nsi') $installer
    $installerArtifact = Join-Path $build "bin\ORCA-beta-$version-windows-amd64-installer.exe"
    Add-OrcaNativeInput $native 'generated/installer' $installerArtifact
    $null = Invoke-OrcaNativeStep $native 'check-installer' 'go' @('run', './cmd/nsischeck', $installerArtifact) $stagedDesktop
    $null = Invoke-OrcaNativeStep $native 'test-installer' 'sevenZip' @('t', $installerArtifact) $stagedDesktop
    $portable = Join-Path $build "bin\ORCA-beta-$version-windows-amd64.zip"
    $stagedPayload = Join-Path $windows 'installer-go\payload'
    Assert-Payload $stagedPayload
    Assert-BetaExecutable $app
    $portableFiles = Get-BetaPortableFiles $app $stagedPayload $source
    $portableHashes = Write-BetaPortableArchive $portable $portableFiles
    if ($portableHashes['OrcaBeta.exe'] -cne $appHash) { throw 'Beta executable changed between installer and portable packaging.' }
    $null = Invoke-OrcaNativeStep $native 'test-portable' 'sevenZip' @('t', $portable) $stagedDesktop
    Add-OrcaNativeInput $native 'generated/portable' $portable
    Assert-OrcaSnapshotBytes $snapshot $snapshot.source
    $nativeManifest = Write-OrcaNativeManifest $native $snapshot.manifestSha256 (Get-OrcaHash $frontendManifest)
    $buildInputs['nativeManifestSha256'] = Get-OrcaHash $nativeManifest
    if (Test-Path -LiteralPath $output) { throw 'Output appeared during packaging; refusing to overwrite it.' }
    [void](Assert-OrcaPlainPath $output)
    [void][IO.Directory]::CreateDirectory($output)
    Copy-Item -LiteralPath $installerArtifact -Destination $output
    Copy-Item -LiteralPath $portable -Destination $output
    $sourceArchive = Join-Path $output "ORCA-beta-$version-source.zip"
    Copy-Item -LiteralPath $snapshot.archive -Destination $sourceArchive
    if ((Get-OrcaHash $sourceArchive) -cne (Get-OrcaHash $snapshot.archive)) { throw 'Source ZIP output hash mismatch.' }
    Copy-Item -LiteralPath $snapshot.manifest -Destination (Join-Path $output 'source-manifest.json')
    Copy-Item -LiteralPath $frontendManifest -Destination (Join-Path $output 'frontend-build-input.json')
    Copy-Item -LiteralPath $nativeManifest -Destination (Join-Path $output 'native-build-input.json')
    Write-OrcaJson (Join-Path $output 'build-inputs.json') $buildInputs
    $checksums = foreach ($artifact in @($installerArtifact, $portable)) {
        $name = [IO.Path]::GetFileName($artifact)
        $expected = (Get-FileHash -LiteralPath $artifact -Algorithm SHA256).Hash
        if ((Get-FileHash -LiteralPath (Join-Path $output $name) -Algorithm SHA256).Hash -cne $expected) { throw "Output hash mismatch: $name" }
        "$($expected.ToLowerInvariant()) *$name"
    }
    $checksums += @("$(Get-OrcaHash $sourceArchive) *$([IO.Path]::GetFileName($sourceArchive))")
    foreach ($name in @('source-manifest.json', 'frontend-build-input.json', 'native-build-input.json', 'build-inputs.json')) { $checksums += "$(Get-OrcaHash (Join-Path $output $name)) *$name" }
    [IO.File]::WriteAllText((Join-Path $output 'SHA256SUMS.txt'), (($checksums -join "`n") + "`n"), $utf8)
    Get-FileHash -LiteralPath (Join-Path $output ([IO.Path]::GetFileName($installerArtifact))), (Join-Path $output ([IO.Path]::GetFileName($portable))) -Algorithm SHA256
} finally {
    Write-Host "Build staging retained: $stage"
}
