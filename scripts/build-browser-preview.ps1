param(
    [string]$OutputDirectory = 'D:\AI-Reasonix\dist\desktop-v3.0.14-browser-preview.1'
)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$desktop = Join-Path $root 'desktop'
$infoPath = Join-Path $desktop 'build\windows\info.json'
$wailsPath = Join-Path $desktop 'wails.json'
$infoBytes = [IO.File]::ReadAllBytes($infoPath)
$wailsBytes = [IO.File]::ReadAllBytes($wailsPath)
$version = '3.0.14-browser-preview.1'
$output = [IO.Path]::GetFullPath($OutputDirectory)
$payload = Join-Path $desktop 'build\windows\installer-go\payload'
$codegraphZip = Join-Path $root '.tmp\orca-codegraph-cache\codegraph-win32-x64.zip'
# Match the immutable Windows runtime pins in desktop-build.sh/checksums.go.
if ((Get-FileHash -LiteralPath (Join-Path $payload 'node.exe') -Algorithm SHA256).Hash -ne '0d0f5e39f9f3d9587bc19f73eab3c2c9c4903fd02d6dbf9c853dd81b3d95fad4') {
    throw 'Pinned Node runtime is missing or differs from the normal Windows payload.'
}
if ((Get-FileHash -LiteralPath $codegraphZip -Algorithm SHA256).Hash -ne 'a5571d3ee54cc1caac76bf09e0f7cb350fc4dd6788a5437217eac33b71fa7a15') {
    throw 'Pinned CodeGraph archive verification failed.'
}
Add-Type -AssemblyName System.IO.Compression.FileSystem
function Test-CodeGraphTree([string]$directory) {
    $zip = [IO.Compression.ZipFile]::OpenRead($codegraphZip)
    try {
        $count = 0
        foreach ($entry in $zip.Entries) {
            if ($entry.FullName.EndsWith('/')) { continue }
            $parts = $entry.FullName.Split('/', 2)
            if ($parts.Length -ne 2 -or $parts[1] -match '(^|/)\.\.(/|$)') { throw 'Unexpected runtime archive layout.' }
            $file = Join-Path $directory $parts[1]
            $inputStream = $entry.Open()
            $sha = [Security.Cryptography.SHA256]::Create()
            try { $expected = [BitConverter]::ToString($sha.ComputeHash($inputStream)).Replace('-', '') }
            finally { $inputStream.Dispose(); $sha.Dispose() }
            if ((Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash -ne $expected) { throw "Runtime file differs from pinned archive: $($parts[1])" }
            $count++
        }
        if (@(Get-ChildItem -LiteralPath $directory -File -Recurse).Count -ne $count) { throw 'Unexpected files in runtime tree.' }
    } finally { $zip.Dispose() }
}
Test-CodeGraphTree (Join-Path $payload 'codegraph')
if (!(Test-Path -LiteralPath (Join-Path $desktop 'frontend\dist\index.html'))) {
    throw 'Build and verify the current frontend before packaging the preview.'
}
$null = New-Item -ItemType Directory -Path $output -Force
Push-Location $desktop
try {
    # Wails reads these generated resource inputs while compiling. Restore the
    # original bytes even after failure; stable release metadata stays unchanged.
    $info = [Text.Encoding]::UTF8.GetString($infoBytes) | ConvertFrom-Json
    $info.fixed.file_version = '3.0.14.0'
    $info.fixed.product_version = '3.0.14.0'
    $info.info.'0409'.FileVersion = '3.0.14.0'
    $info.info.'0409'.ProductVersion = $version
    $info.info.'0409'.ProductName = 'O.R.C.A. Browser Preview'
    $info.info.'0409'.FileDescription = 'O.R.C.A. Browser Preview (local test build)'
    $info.info.'0409'.InternalName = 'OrcaBrowserPreview'
    $info.info.'0409'.OriginalFilename = 'OrcaBrowserPreview.exe'
    $utf8 = New-Object Text.UTF8Encoding($false)
    [IO.File]::WriteAllText($infoPath, ($info | ConvertTo-Json -Depth 8), $utf8)
    $wails = [Text.Encoding]::UTF8.GetString($wailsBytes) | ConvertFrom-Json
    $wails.info.productVersion = '3.0.14'
    $wails.info.productName = 'O.R.C.A. Browser Preview'
    [IO.File]::WriteAllText($wailsPath, ($wails | ConvertTo-Json -Depth 8), $utf8)
    & wails build -s -m -nosyncgomod -skipbindings -trimpath -nocolour -o OrcaBrowserPreview.exe -ldflags "-X main.version=$version -X main.channel=browser-preview"
    if ($LASTEXITCODE -ne 0) { throw "Preview build failed ($LASTEXITCODE)." }
    $binary = Join-Path $desktop 'build\bin\OrcaBrowserPreview.exe'
    $details = (Get-Item -LiteralPath $binary).VersionInfo
    if ($details.FileVersion -ne '3.0.14.0' -or $details.ProductVersion -ne $version) {
        throw "Unexpected executable metadata: $($details.FileVersion), $($details.ProductVersion)"
    }
    Copy-Item -LiteralPath $binary -Destination (Join-Path $output 'OrcaBrowserPreview.exe')
    foreach ($name in @('node.exe', 'LICENSE.node.txt')) {
        Copy-Item -LiteralPath (Join-Path $payload $name) -Destination (Join-Path $output $name) -Force
    }
    Copy-Item -LiteralPath (Join-Path $root 'THIRD-PARTY-NOTICES.txt') -Destination $output -Force
    Copy-Item -LiteralPath (Join-Path $payload 'codegraph') -Destination $output -Recurse -Force
    Test-CodeGraphTree (Join-Path $output 'codegraph')
    Get-FileHash -LiteralPath (Join-Path $output 'OrcaBrowserPreview.exe') -Algorithm SHA256
} finally {
    [IO.File]::WriteAllBytes($infoPath, $infoBytes)
    [IO.File]::WriteAllBytes($wailsPath, $wailsBytes)
    Pop-Location
}
