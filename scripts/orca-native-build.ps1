#requires -Version 7.0
# Uses the source snapshot and cleared process runner shared with frontend builds.
Set-StrictMode -Version Latest

function New-OrcaNativeBuildContext([string]$Directory, [hashtable]$Tools, [string]$NpmCache, [string]$GoModuleCache) {
    [void](Assert-OrcaPlainPath $Directory)
    if (Test-Path -LiteralPath $Directory) { throw 'Native build evidence must be fresh.' }
    $pins = @{}
    foreach ($key in @('node', 'npm', 'go', 'wails', 'nsis', 'sevenZip')) {
        if (-not $Tools.ContainsKey($key) -or -not [IO.Path]::IsPathFullyQualified($Tools[$key])) { throw "Absolute native tool path required: $key" }
        $path = Assert-OrcaPlainPath $Tools[$key]
        $pins[$key] = @{path=$path; sha256=(Get-OrcaHash $path)}
    }
    [void][IO.Directory]::CreateDirectory($Directory)
    $environment = New-OrcaBuildEnvironment (Join-Path $Directory 'environment') $Tools $NpmCache $GoModuleCache
    return [pscustomobject]@{
        directory=$Directory; pins=$pins; environment=$environment
        environmentSha256=(Get-OrcaTextHash ($environment | ConvertTo-Json -Compress))
        inputs=@{}; trees=@{}; steps=[Collections.Generic.List[object]]::new()
    }
}

function Add-OrcaNativeInput($Context, [string]$Name, [string]$Path) {
    Assert-OrcaRelativePath $Name
    if ($Context.inputs.ContainsKey($Name)) { throw "Duplicate native input: $Name" }
    $plain = Assert-OrcaPlainPath $Path
    $Context.inputs[$Name] = @{path=$plain; sha256=(Get-OrcaHash $plain)}
}

function Add-OrcaNativeInputTree($Context, [string]$Name, [string]$Root) {
    Assert-OrcaRelativePath $Name
    if ($Context.trees.ContainsKey($Name)) { throw 'Duplicate native resource tree.' }
    $plain = Assert-OrcaPlainPath $Root
    $files = @(Get-OrcaPlainFiles $plain | ForEach-Object { [IO.Path]::GetRelativePath($plain, $_).Replace('\', '/') } | Sort-Object -CaseSensitive)
    if (-not $files.Count) { throw "Empty native resource tree: $Name" }
    $Context.trees[$Name] = @{root=$plain; files=$files}
    foreach ($file in $files) { Add-OrcaNativeInput $Context "$Name/$file" (Join-Path $plain $file) }
}

function Assert-OrcaNativeInputs($Context) {
    if ((Get-OrcaTextHash ($Context.environment | ConvertTo-Json -Compress)) -cne $Context.environmentSha256) { throw 'Native build environment changed after pinning.' }
    if ($Context.environment.GOENV -cne 'off' -or $Context.environment.GOFLAGS -cne '-mod=readonly -buildvcs=false') { throw 'Unsafe native Go environment.' }
    foreach ($key in $Context.pins.Keys) {
        if ((Get-OrcaHash $Context.pins[$key].path) -cne $Context.pins[$key].sha256) { throw "Pinned native tool changed: $key" }
    }
    foreach ($name in $Context.inputs.Keys) {
        if ((Get-OrcaHash $Context.inputs[$name].path) -cne $Context.inputs[$name].sha256) { throw "Pinned native input changed: $name" }
    }
    foreach ($name in $Context.trees.Keys) {
        $tree = $Context.trees[$name]
        $now = @(Get-OrcaPlainFiles $tree.root | ForEach-Object { [IO.Path]::GetRelativePath($tree.root, $_).Replace('\', '/') } | Sort-Object -CaseSensitive)
        if (($now | ConvertTo-Json -Compress) -cne ($tree.files | ConvertTo-Json -Compress)) { throw "Native resource tree membership changed: $name" }
    }
}

function Invoke-OrcaNativeStep($Context, [string]$Name, [string]$Tool, [string[]]$Arguments, [string]$Directory, [ValidateRange(1, 600)][int]$TimeoutSeconds = 600) {
    if ($Name -cnotmatch '^[a-z0-9-]+$' -or -not $Context.pins.ContainsKey($Tool)) { throw 'Invalid native step/tool.' }
    Assert-OrcaNativeInputs $Context
    $logName = '{0:d2}-{1}.log' -f $Context.steps.Count, $Name
    $log = Join-Path $Context.directory $logName
    if (Test-Path -LiteralPath $log) { throw 'Native step log already exists.' }
    $result = Invoke-OrcaBuildStep $Context.pins[$Tool].path $Arguments (Assert-OrcaPlainPath $Directory) $Context.environment $log $TimeoutSeconds
    Assert-OrcaNativeInputs $Context
    $Context.steps.Add([ordered]@{name=$Name; tool=$Tool; toolSha256=$Context.pins[$Tool].sha256; argumentsSha256=(Get-OrcaTextHash ($Arguments | ConvertTo-Json -Compress)); timeoutSeconds=$TimeoutSeconds; log=$logName; logSha256=(Get-OrcaHash $log); passed=$true})
    return $result
}

function Assert-OrcaExecutableBuildInfo($Context, [string]$Path, [string]$ExpectedVersion) {
    $plain = Assert-OrcaPlainPath $Path
    if (-not $Context.inputs.ContainsKey('generated/application') -or
        $Context.inputs['generated/application'].path -cne $plain) { throw 'Application must be pinned before its identity probe.' }
    Assert-OrcaNativeInputs $Context
    $pin = $Context.inputs['generated/application']
    if (-not $Context.pins.ContainsKey('application')) {
        $Context.pins.application = @{path=$plain; sha256=$pin.sha256}
    }
    if ($Context.pins.application.path -cne $plain -or $Context.pins.application.sha256 -cne $pin.sha256) { throw 'Application probe pin mismatch.' }
    # Go omits -ldflags with -trimpath. Query the linked values, not build logs.
    $json = Invoke-OrcaNativeStep $Context 'inspect-build-identity' 'application' @('--orca-build-info-json') $Context.environment.TEMP 10
    $document = [Text.Json.JsonDocument]::Parse([string]$json)
    try {
        $root = $document.RootElement
        if ($root.ValueKind -ne [Text.Json.JsonValueKind]::Object) { throw 'Invalid executable build identity object.' }
        $fields = @($root.EnumerateObject())
        if ($fields.Count -ne 3 -or (@($fields.Name | Sort-Object -CaseSensitive) -join ',') -cne 'channel,schemaVersion,version') { throw 'Invalid executable build identity fields.' }
        $schema = $root.GetProperty('schemaVersion'); $channel = $root.GetProperty('channel'); $version = $root.GetProperty('version')
        if ($schema.ValueKind -ne [Text.Json.JsonValueKind]::Number -or $schema.GetRawText() -cne '1' -or
            $channel.ValueKind -ne [Text.Json.JsonValueKind]::String -or $channel.GetString() -cne 'beta' -or
            $version.ValueKind -ne [Text.Json.JsonValueKind]::String -or $version.GetString() -cne $ExpectedVersion) {
            throw 'Unexpected executable channel/version identity.'
        }
    } finally { $document.Dispose() }
}

function Write-OrcaNativeManifest($Context, [string]$SourceManifestHash, [string]$FrontendManifestHash) {
    foreach ($hash in @($SourceManifestHash, $FrontendManifestHash)) { if ($hash -cnotmatch '^[0-9a-f]{64}$') { throw 'Native provenance requires source/frontend hashes.' } }
    Assert-OrcaNativeInputs $Context
    $tools = @($Context.pins.Keys | Sort-Object | ForEach-Object { @{tool=$_; file=([IO.Path]::GetFileName($Context.pins[$_].path)); sha256=$Context.pins[$_].sha256} })
    $inputs = @($Context.inputs.Keys | Sort-Object -CaseSensitive | ForEach-Object { @{name=$_; sha256=$Context.inputs[$_].sha256} })
    $manifest = Join-Path $Context.directory 'native-build-input.json'
    if (Test-Path -LiteralPath $manifest) { throw 'Native provenance already exists.' }
    Write-OrcaJson $manifest ([ordered]@{
        schemaVersion=1; kind='isolated-native-build'; sourceManifestSha256=$SourceManifestHash; frontendManifestSha256=$FrontendManifestHash
        environmentSha256=$Context.environmentSha256; environmentKeys=@($Context.environment.Keys | Sort-Object)
        policy=@{inheritedEnvironment='cleared'; GOENV='off'; GOFLAGS='-mod=readonly -buildvcs=false'; GOTOOLCHAIN='local'; GOPROXY='off'; GOWORK='off'; nsisConfig='disabled with /NOCONFIG'; profile='fresh build-only directories'}
        tools=$tools; inputs=$inputs; steps=@($Context.steps.ToArray())
    })
    return $manifest
}
