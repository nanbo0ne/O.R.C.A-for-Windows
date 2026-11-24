#requires -Version 7.0
# Dot-source after orca-source-snapshot.ps1. All execution uses a fresh build copy.
Set-StrictMode -Version Latest

function Assert-OrcaSnapshotBytes($Snapshot, [string]$Root) {
    if ((Get-OrcaHash $Snapshot.manifest) -cne $Snapshot.manifestSha256) { throw 'Source manifest changed.' }
    foreach ($file in $Snapshot.files) {
        if ((Get-OrcaHash (Join-Path $Root $file.path)) -cne $file.sha256) { throw "Snapshot source changed: $($file.path)" }
    }
}

function Invoke-OrcaBuildStep([string]$Tool, [string[]]$Arguments, [string]$Directory, [hashtable]$Environment, [string]$Log, [int]$TimeoutSeconds = 600) {
    $start = [Diagnostics.ProcessStartInfo]::new($Tool)
    $start.UseShellExecute = $false; $start.CreateNoWindow = $true
    $start.WorkingDirectory = $Directory
    $start.RedirectStandardOutput = $true; $start.RedirectStandardError = $true
    $start.Environment.Clear()
    foreach ($key in $Environment.Keys) { $start.Environment[$key] = $Environment[$key] }
    foreach ($argument in $Arguments) { $start.ArgumentList.Add($argument) }
    $process = [Diagnostics.Process]::Start($start)
    try {
        $stdout = $process.StandardOutput.ReadToEndAsync(); $stderr = $process.StandardError.ReadToEndAsync()
        $finished = $process.WaitForExit($TimeoutSeconds * 1000)
        if (-not $finished) { $process.Kill($true); $process.WaitForExit() }
        $text = $stdout.GetAwaiter().GetResult() + $stderr.GetAwaiter().GetResult()
        [IO.File]::WriteAllText($Log, $text, [Text.UTF8Encoding]::new($false))
        if (-not $finished) { throw "Build step timed out; owned process terminated. See $Log" }
        if ($process.ExitCode -ne 0) { throw "Build step exited $($process.ExitCode). See $Log" }
        return $text.Trim()
    } finally { $process.Dispose() }
}

function New-OrcaBuildEnvironment([string]$Directory, [hashtable]$Tools, [string]$NpmCache, [string]$GoModuleCache) {
    foreach ($path in @($NpmCache, $GoModuleCache)) {
        if (-not $path -or -not [IO.Path]::IsPathFullyQualified($path) -or -not [IO.Directory]::Exists((Assert-OrcaPlainPath $path))) { throw 'Explicit existing offline npm and Go module caches are required.' }
    }
    if (Test-Path -LiteralPath $Directory) { throw 'Build environment must be fresh.' }
    [void](Assert-OrcaPlainPath $Directory)
    foreach ($name in @('home', 'appdata', 'local', 'temp', 'gocache', 'gopath', 'tools')) { [void][IO.Directory]::CreateDirectory((Join-Path $Directory $name)) }
    # npm rejects loading one file as both user and global configuration.
    $userConfig = Join-Path $Directory 'user.npmrc'
    $globalConfig = Join-Path $Directory 'global.npmrc'
    foreach ($config in @($userConfig, $globalConfig)) { [IO.File]::WriteAllText($config, '', [Text.UTF8Encoding]::new($false)) }
    foreach ($path in @($Tools.node, $Tools.npm)) {
        if ($path -match '[%!*?^&|<>"\r\n]') { throw 'Tool path cannot be represented safely by the isolated npm shim.' }
    }
    # Nested npm scripts must also use Node 24, not npm.cmd's adjacent system Node.
    [IO.File]::WriteAllText((Join-Path $Directory 'tools/npm.cmd'), "@`"$($Tools.node)`" `"$($Tools.npm)`" %*`r`n", [Text.UTF8Encoding]::new($false))
    $system = [Environment]::GetFolderPath('System')
    $windows = [IO.Path]::GetDirectoryName($system)
    return @{
        PATH=(@((Join-Path $Directory 'tools'), ([IO.Path]::GetDirectoryName($Tools.node)), ([IO.Path]::GetDirectoryName($Tools.go)), ([IO.Path]::GetDirectoryName($Tools.wails)), $system) -join ';')
        SystemRoot=$windows; WINDIR=$windows; ComSpec=(Join-Path $system 'cmd.exe'); PATHEXT='.COM;.EXE;.BAT;.CMD'
        USERPROFILE=(Join-Path $Directory 'home'); HOME=(Join-Path $Directory 'home'); APPDATA=(Join-Path $Directory 'appdata'); LOCALAPPDATA=(Join-Path $Directory 'local')
        XDG_CONFIG_HOME=(Join-Path $Directory 'appdata'); XDG_CACHE_HOME=(Join-Path $Directory 'local'); XDG_DATA_HOME=(Join-Path $Directory 'local')
        TEMP=(Join-Path $Directory 'temp'); TMP=(Join-Path $Directory 'temp'); GOTMPDIR=(Join-Path $Directory 'temp')
        GOENV='off'; GOTOOLCHAIN='local'; GOPROXY='off'; GOSUMDB='off'; GOWORK='off'; GOFLAGS='-mod=readonly -buildvcs=false'
        GOPATH=(Join-Path $Directory 'gopath'); GOCACHE=(Join-Path $Directory 'gocache'); GOMODCACHE=$GoModuleCache
        GOOS='windows'; GOARCH='amd64'; CGO_ENABLED='0'
        npm_config_cache=$NpmCache; npm_config_userconfig=$userConfig; npm_config_globalconfig=$globalConfig
        npm_config_offline='true'; npm_config_audit='false'; npm_config_fund='false'; npm_config_update_notifier='false'
        CI='true'; NO_COLOR='1'
    }
}

function Build-OrcaSnapshotFrontend($Snapshot, [string]$BuildSource, [string]$Evidence, [hashtable]$Tools, [string]$NpmCache, [string]$GoModuleCache) {
    if (-not $IsWindows) { throw 'Snapshot frontend verification currently requires Windows.' }
    foreach ($key in @('node', 'npm', 'go', 'wails')) {
        if (-not $Tools.ContainsKey($key) -or -not [IO.Path]::IsPathFullyQualified($Tools[$key]) -or -not [IO.File]::Exists((Assert-OrcaPlainPath $Tools[$key]))) { throw "Missing absolute build tool path: $key" }
    }
    if (Test-Path -LiteralPath $Evidence) { throw 'Frontend build evidence must be fresh.' }
    [void](Assert-OrcaPlainPath $Evidence)
    Assert-OrcaSnapshotBytes $Snapshot $Snapshot.source
    # Copy inside this function so a caller cannot inject an old bundle or bindings.
    Copy-OrcaVerifiedFiles $Snapshot.source $BuildSource $Snapshot.files
    $frontend = Join-Path $BuildSource 'desktop/frontend'
    foreach ($path in @('dist', 'node_modules', 'wailsjs')) {
        if (Test-Path -LiteralPath (Join-Path $frontend $path)) { throw "Snapshot contains forbidden prebuilt frontend input: $path" }
    }
    [void][IO.Directory]::CreateDirectory($Evidence)
    $environment = New-OrcaBuildEnvironment (Join-Path $Evidence 'environment') $Tools $NpmCache $GoModuleCache
    $toolInfo = [ordered]@{}
    foreach ($key in @('node', 'npm', 'go', 'wails')) {
        $arguments = switch ($key) { 'node' { @('--version') } 'npm' { @($Tools.npm, '--version') } default { @('version') } }
        $executable = if ($key -eq 'npm') { $Tools.node } else { $Tools[$key] }
        $toolVersion = Invoke-OrcaBuildStep $executable $arguments $frontend $environment (Join-Path $Evidence "$key-version.log")
        if ($key -eq 'node' -and $toolVersion -cnotmatch '^v24\.[0-9]+\.[0-9]+$') { throw 'Node 24 is required; no system-node fallback.' }
        $toolInfo[$key] = @{version=$toolVersion; sha256=(Get-OrcaHash $Tools[$key])}
    }
    $lockHash = Get-OrcaHash (Join-Path $frontend 'package-lock.json')
    [void][IO.Directory]::CreateDirectory((Join-Path $frontend 'dist'))
    [IO.File]::WriteAllText((Join-Path $frontend 'dist/.gitkeep'), '', [Text.UTF8Encoding]::new($false))
    $steps = @(
        @{name='bindings'; tool=$Tools.wails; args=@('generate', 'module', '-nocolour'); cwd=(Join-Path $BuildSource 'desktop')},
        @{name='npm-ci'; tool=$Tools.node; args=@($Tools.npm, 'ci', '--offline', '--ignore-scripts', '--no-audit', '--no-fund'); cwd=$frontend},
        @{name='test-all'; tool=$Tools.node; args=@($Tools.npm, 'run', 'test:all'); cwd=$frontend},
        @{name='test-beta'; tool=$Tools.node; args=@('node_modules/tsx/dist/cli.mjs', 'src/__tests__/beta-connections.test.ts'); cwd=$frontend},
        @{name='build'; tool=$Tools.node; args=@($Tools.npm, 'run', 'build'); cwd=$frontend}
    )
    $stepInfo = @()
    foreach ($step in $steps) {
        Write-Host "Snapshot frontend: $($step.name)"
        $log = Join-Path $Evidence ($step.name + '.log')
        $null = Invoke-OrcaBuildStep $step.tool $step.args $step.cwd $environment $log
        Assert-OrcaSnapshotBytes $Snapshot $BuildSource
        $stepInfo += @{name=$step.name; passed=$true; logSha256=(Get-OrcaHash $log)}
    }
    $bindings = @(Get-OrcaPlainFiles (Join-Path $frontend 'wailsjs') | Sort-Object | ForEach-Object { @{path=([IO.Path]::GetRelativePath($frontend, $_).Replace('\', '/')); sha256=(Get-OrcaHash $_)} })
    if (-not $bindings.Count) { throw 'Fresh Wails bindings are missing.' }
    $bundle = Get-OrcaFrontendPlan (Join-Path $frontend 'dist')
    Assert-OrcaSnapshotBytes $Snapshot $Snapshot.source
    $manifest = Join-Path $Evidence 'frontend-build-input.json'
    Write-OrcaJson $manifest ([ordered]@{
        schemaVersion=1; kind='rebuilt-frontend-from-snapshot'; sourceManifestSha256=$Snapshot.manifestSha256
        packageLockSha256=$lockHash; tools=$toolInfo; steps=$stepInfo; bindings=$bindings; files=$bundle.files
        note='Fresh isolated offline dependency install, generated bindings, tests and production build. No checkout dist input.'
    })
    return [pscustomobject]@{manifest=$manifest; manifestSha256=(Get-OrcaHash $manifest); bundle=$bundle}
}

function Assert-OrcaBuiltFrontend($Build) {
    if ((Get-OrcaHash $Build.manifest) -cne $Build.manifestSha256) { throw 'Frontend build provenance changed.' }
    $current = Get-OrcaFrontendPlan $Build.bundle.root
    if (($current.files | ConvertTo-Json -Compress) -cne ($Build.bundle.files | ConvertTo-Json -Compress)) { throw 'Fresh frontend bundle changed after verification.' }
}
