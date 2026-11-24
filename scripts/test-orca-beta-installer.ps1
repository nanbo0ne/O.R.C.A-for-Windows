#requires -Version 7.0
[CmdletBinding()]
param(
    [ValidateSet('Preflight', 'ExtractOnly', 'Execute')][string]$Mode = 'Preflight',
    [string]$ArtifactManifest,
    [string]$ManifestSha256,
    [string]$TestRoot,
    [string]$InstallRoot,
    [string]$SevenZipPath,
    [switch]$ArtifactsReady,
    [switch]$DisposableVM,
    [switch]$AllowShellArtifacts,
    [ValidateRange(10, 600)][int]$TimeoutSeconds = 120
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Assert-AcceptancePath([string]$Path) {
    if ($Path -notmatch '^[A-Za-z]:[\\/]' -or $Path.Substring(2) -match '[:"\r\n]' -or $Path -match '[\\/]$') {
        throw "A plain absolute local path is required: $Path"
    }
    foreach ($part in $Path.Substring(3).Split([char[]]'\/')) {
        if (-not $part -or $part -in @('.', '..') -or $part -match '[ .]$|[<>|?*\x00-\x1f]' -or $part -match '^(?i:CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(?:\.|$)') {
            throw "Unsafe path component: $Path"
        }
    }
    $full = [IO.Path]::GetFullPath($Path)
    # EvalSymlinks/Resolve-Path alone do not protect a missing child of a junction.
    for ($p = $full; $p; $p = [IO.Path]::GetDirectoryName($p)) {
        try { $attributes = [IO.File]::GetAttributes($p) }
        catch [IO.FileNotFoundException] { continue }
        catch [IO.DirectoryNotFoundException] { continue }
        if ($attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Redirected path: $p" }
    }
    return $full
}

function Test-AcceptanceWithin([string]$Path, [string]$Root) {
    return $Path.Equals($Root, [StringComparison]::OrdinalIgnoreCase) -or $Path.StartsWith($Root.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase)
}

function Assert-AcceptanceRoots([string]$Root, [string]$Install, [string[]]$ProtectedRoots) {
    $rootPath = Assert-AcceptancePath $Root
    $installPath = Assert-AcceptancePath $Install
    if ([IO.Path]::GetFileName($rootPath) -notmatch '^orca-beta-installer-acceptance-[a-f0-9]{32}$') { throw 'TestRoot must end in orca-beta-installer-acceptance-<32 lowercase hex characters>.' }
    if ((Test-Path -LiteralPath $rootPath) -or (Test-Path -LiteralPath $installPath)) { throw 'TestRoot and InstallRoot must not exist.' }
    if (-not (Test-Path -LiteralPath ([IO.Path]::GetDirectoryName($rootPath)) -PathType Container)) { throw 'TestRoot parent must already exist.' }
    if (-not ([IO.Path]::GetDirectoryName($installPath)).Equals((Join-Path $rootPath 'install'), [StringComparison]::OrdinalIgnoreCase) -or [IO.Path]::GetFileName($installPath) -notmatch '[\u3400-\u9fff]') { throw 'InstallRoot must be a Chinese-named direct child of TestRoot/install.' }
    foreach ($protected in $ProtectedRoots) {
        if ($protected -and ((Test-AcceptanceWithin $rootPath $protected) -or (Test-AcceptanceWithin $protected $rootPath))) {
            throw 'TestRoot overlaps a real profile, system, or program directory.'
        }
    }
    return @{root=$rootPath; install=$installPath}
}

function Get-AcceptanceHash([string]$Path) {
    $path = Assert-AcceptancePath $Path
    return (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Assert-AcceptanceHash([string]$Path, [string]$Hash) {
    if ($Hash -cnotmatch '^[a-f0-9]{64}$' -or (Get-AcceptanceHash $Path) -cne $Hash) { throw "Hash mismatch: $Path" }
}

function Write-AcceptanceNewFile([string]$Path, [string]$Text) {
    [void](Assert-AcceptancePath $Path)
    [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($Path))
    $stream = [IO.File]::Open($Path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try { $bytes = [Text.Encoding]::UTF8.GetBytes($Text); $stream.Write($bytes) } finally { $stream.Dispose() }
}

function Assert-AcceptanceExecuteGate([bool]$Ready, [bool]$Disposable, [bool]$ShellConsent, $Computer) {
    if (-not ($Ready -and $Disposable -and $ShellConsent)) { throw 'Execute requires -ArtifactsReady -DisposableVM -AllowShellArtifacts; parent approval is still required.' }
    $vm = ($Computer.Manufacturer -eq 'Microsoft Corporation' -and $Computer.Model -eq 'Virtual Machine') -or
        $Computer.Manufacturer -match 'VMware|innotek|QEMU' -or $Computer.Model -match '^(VirtualBox|VMware Virtual Platform|KVM|QEMU.*)$'
    if (-not $vm) { throw 'Actual installation is restricted to a disposable VM/account. Use ExtractOnly on a host.' }
}

function Get-AcceptanceRegistry([string]$Hive, [string]$View, [string]$Key) {
    $base = [Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::$Hive, [Microsoft.Win32.RegistryView]::$View)
    try {
        $entry = $base.OpenSubKey($Key, $false)
        if ($null -eq $entry) { return $null }
        try {
            $values = [ordered]@{}
            foreach ($name in ($entry.GetValueNames() | Sort-Object)) {
                $values[$name] = @{kind=$entry.GetValueKind($name).ToString(); value=$entry.GetValue($name, $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)}
            }
            return ($values | ConvertTo-Json -Depth 8 -Compress)
        } finally { $entry.Dispose() }
    } finally { $base.Dispose() }
}

function Get-AcceptanceBetaRegistrations {
    $records = @{}
    foreach ($hive in @('CurrentUser', 'LocalMachine')) {
        foreach ($view in @('Registry32', 'Registry64')) {
            $record = Get-AcceptanceRegistry $hive $view 'Software\Microsoft\Windows\CurrentVersion\Uninstall\ORCA beta'
            if ($null -ne $record) { $records["$hive/$view"] = $record }
        }
    }
    return $records
}

function Get-AcceptanceShellPaths {
    $paths = @{}
    foreach ($folder in @('DesktopDirectory', 'Programs', 'CommonDesktopDirectory', 'CommonPrograms')) {
        $directory = [Environment]::GetFolderPath([Environment+SpecialFolder]::$folder)
        if (-not $directory) { throw "Known folder unavailable: $folder" }
        [void](Assert-AcceptancePath $directory)
        $names = @('ORCA beta.lnk')
        if ($folder -match 'Programs') { $names += 'Uninstall ORCA beta.lnk' }
        foreach ($name in $names) { $paths["$folder/$name"] = Join-Path $directory $name }
    }
    return $paths
}

function Assert-AcceptanceVacant($Registrations, [hashtable]$ShellPaths) {
    if ($Registrations.Count) { throw 'A beta uninstall registration already exists; refusing to adopt or modify it.' }
    foreach ($path in $ShellPaths.Values) {
        [void](Assert-AcceptancePath $path)
        if (Test-Path -LiteralPath $path) { throw "A beta shortcut already exists: $path" }
    }
}

function Assert-AcceptanceWebViewPresent {
    $id = '{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
    foreach ($probe in @(@('LocalMachine', "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\$id"), @('CurrentUser', "Software\Microsoft\EdgeUpdate\Clients\$id"))) {
        $record = Get-AcceptanceRegistry $probe[0] 'Registry64' $probe[1]
        if ($record) {
            $values = $record | ConvertFrom-Json -AsHashtable
            if ($values.ContainsKey('pv') -and $values.pv.value -match '^\d+\.\d+\.\d+\.\d+$' -and $values.pv.value -ne '0.0.0.0') { return }
        }
    }
    throw 'Existing WebView2 registration is required; this harness never authorizes runtime bootstrap/install.'
}

function Assert-AcceptanceNoOrcaProcesses {
    foreach ($process in [Diagnostics.Process]::GetProcesses()) {
        try {
            if ($process.ProcessName -match '^(?i:Orca|OrcaBeta|deepseek-orca-desktop)$') { throw "Existing ORCA process blocks acceptance; it will not be closed (PID $($process.Id))." }
        } finally { $process.Dispose() }
    }
}

function Get-AcceptanceVMFixtureRoots {
    return @(
        (Join-Path ([Environment]::GetFolderPath('UserProfile')) '.orca'),
        (Join-Path ([Environment]::GetFolderPath('ApplicationData')) 'ORCA beta'),
        (Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'Programs/O.R.C.A for Windows')
    )
}

function New-AcceptanceSentinelDirectory([string]$Path, [string]$RunID) {
    $path = Assert-AcceptancePath $Path
    if (Test-Path -LiteralPath $path) { throw 'Synthetic profile directory already exists; refusing adoption.' }
    $null = New-Item -ItemType Directory -Path $path -ErrorAction Stop
    Write-AcceptanceNewFile (Join-Path $path 'acceptance-owner.txt') $RunID
}

function Assert-AcceptancePristineVMAccount {
    foreach ($name in @('O.R.C.A for Windows', 'DeepSeek-Orca')) {
        foreach ($hive in @('CurrentUser', 'LocalMachine')) {
            foreach ($view in @('Registry32', 'Registry64')) {
                if ($null -ne (Get-AcceptanceRegistry $hive $view "Software\Microsoft\Windows\CurrentVersion\Uninstall\$name")) { throw 'Use an empty disposable VM/account, not an actual official installation.' }
            }
        }
    }
    $homePath = [Environment]::GetFolderPath('UserProfile')
    $roaming = [Environment]::GetFolderPath('ApplicationData')
    $local = [Environment]::GetFolderPath('LocalApplicationData')
    foreach ($path in (@(Get-AcceptanceVMFixtureRoots) + @((Join-Path $homePath '.deepseek-orca'), (Join-Path $local 'Programs/ORCA beta'), (Join-Path $local 'Programs/DeepSeek-Orca')))) {
        [void](Assert-AcceptancePath $path)
        if (Test-Path -LiteralPath $path) { throw 'Existing ORCA state blocks actual acceptance; use a fresh VM/account.' }
    }
}

function Get-AcceptanceNSISArguments([string]$Directory, [switch]$Uninstall) {
    $directory = Assert-AcceptancePath $Directory
    # NSIS requires the final /D= or _?= value to be unquoted, even with spaces.
    if ($Uninstall) { return '/S _?=' + $directory }
    return '/S /D=' + $directory
}

function Set-AcceptanceChildEnvironment([Diagnostics.ProcessStartInfo]$Start, [hashtable]$ProfileEnvironment) {
    # Do not pass inherited API keys, app overrides, PATH tools, or WebView2
    # profile/argument overrides into an installer or its descendants.
    $windows = [Environment]::GetFolderPath('Windows')
    $system = [Environment]::GetFolderPath('System')
    $Start.Environment.Clear()
    $Start.Environment['SystemRoot']=$windows; $Start.Environment['WINDIR']=$windows
    $Start.Environment['SystemDrive']=[IO.Path]::GetPathRoot($windows).TrimEnd('\')
    $Start.Environment['ComSpec']=Join-Path $system 'cmd.exe'
    $Start.Environment['PATH']="$system;$windows"
    $Start.Environment['PATHEXT']='.COM;.EXE;.BAT;.CMD'
    $Start.Environment['OS']='Windows_NT'
    foreach ($key in $ProfileEnvironment.Keys) { $Start.Environment[$key]=$ProfileEnvironment[$key] }
    $Start.WorkingDirectory=$ProfileEnvironment['TEMP']
}

function Invoke-AcceptanceProcess([string]$File, [string]$RawArguments, [string[]]$Arguments, [hashtable]$ChildEnvironment, [int]$Timeout = 120) {
    [void](Assert-AcceptancePath $File)
    $start = [Diagnostics.ProcessStartInfo]::new($File)
    $start.UseShellExecute = $false; $start.CreateNoWindow = $true
    $start.RedirectStandardOutput = $true; $start.RedirectStandardError = $true
    if ($RawArguments) { $start.Arguments = $RawArguments } else { foreach ($arg in $Arguments) { $start.ArgumentList.Add($arg) } }
    Set-AcceptanceChildEnvironment $start $ChildEnvironment
    $process = [Diagnostics.Process]::Start($start)
    $output = $process.StandardOutput.ReadToEndAsync(); $errors = $process.StandardError.ReadToEndAsync()
    try {
        if (-not $process.WaitForExit($Timeout * 1000)) {
            throw "Timed out; PID $($process.Id) was NOT killed. Do not clean this run while it is active; discard the disposable VM after inspection."
        }
        if (-not $output.Wait(5000) -or -not $errors.Wait(5000)) { throw 'Child output pipes did not close; retain the VM and inspect, do not continue cleanup.' }
        $text = $output.GetAwaiter().GetResult() + $errors.GetAwaiter().GetResult()
        if ($process.ExitCode -ne 0) { throw "Child failed (exit $($process.ExitCode)): $text" }
        return $text
    } finally { $process.Dispose() }
}

function Assert-AcceptanceMember([string]$Name) {
    if (-not $Name -or $Name -match '(^[\\/]|[:\\\x00-\x1f]|[ .]$)' -or $Name.Contains('//')) { throw "Unsafe archive member: $Name" }
    foreach ($part in $Name.TrimEnd('/').Split('/')) {
        if (-not $part -or $part -in @('.', '..') -or $part -match '[ .]$|[<>"|?*]' -or $part -match '^(?i:CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(?:\.|$)') { throw "Unsafe archive member: $Name" }
    }
}

function Get-AcceptancePortableMap([string]$Archive, [string]$Destination) {
    [void](Assert-AcceptancePath $Archive)
    $zip = [IO.Compression.ZipFile]::OpenRead($Archive)
    try {
        $seen = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
        $files = @{}; [long]$total = 0
        if ($zip.Entries.Count -gt 20000) { throw 'Portable archive has too many members.' }
        foreach ($entry in $zip.Entries) {
            $name = $entry.FullName
            Assert-AcceptanceMember $name
            if (-not $seen.Add($name.TrimEnd('/')) -or (($entry.ExternalAttributes -shr 16) -band 0xF000) -eq 0xA000) { throw 'Duplicate/link portable member.' }
            if ($name.EndsWith('/')) { continue }
            if ($name -cnotin @('OrcaBeta.exe', 'node.exe', 'LICENSE.node.txt', 'LICENSE', 'THIRD-PARTY-NOTICES.txt') -and -not $name.StartsWith('codegraph/', [StringComparison]::Ordinal)) { throw "Unexpected portable member: $name" }
            $total += $entry.Length
            if ($entry.Length -gt 512MB -or $total -gt 2GB) { throw 'Portable archive exceeds acceptance size limits.' }
            $stream = $entry.Open()
            try { $files[$name] = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($stream)).ToLowerInvariant() } finally { $stream.Dispose() }
        }
        foreach ($name in @('OrcaBeta.exe', 'node.exe', 'LICENSE.node.txt', 'LICENSE', 'THIRD-PARTY-NOTICES.txt')) {
            if (-not $files.ContainsKey($name)) { throw "Missing portable member: $name" }
        }
        if (-not @($files.Keys | Where-Object { $_.StartsWith('codegraph/') }).Count) { throw 'Missing CodeGraph payload.' }
        if ($Destination) {
            [void](Assert-AcceptancePath $Destination)
            if (Test-Path -LiteralPath $Destination) { throw 'Extraction destination already exists.' }
            foreach ($entry in $zip.Entries) {
                if ($entry.FullName.EndsWith('/')) { continue }
                $target = Join-Path $Destination $entry.FullName
                [void](Assert-AcceptancePath $target)
                [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($target))
                [IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $target, $false)
                Assert-AcceptanceHash $target $files[$entry.FullName]
            }
        }
        return $files
    } finally { $zip.Dispose() }
}

function Get-AcceptanceSevenZipMembers([string]$Listing) {
    $seen = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    $paths = [Collections.Generic.List[string]]::new()
    foreach ($line in ($Listing -split '\r?\n')) {
        if ($line -match '^(Symbolic Link|Hard Link) = .+' -or $line -match '^Attributes = .*\bL') { throw 'Link in installer archive.' }
        if ($line.StartsWith('Path = ')) {
            $name = $line.Substring(7).Replace('\', '/')
            Assert-AcceptanceMember $name
            if (-not $seen.Add($name.TrimEnd('/'))) { throw 'Duplicate installer archive member.' }
            $paths.Add($name)
        }
    }
    if (-not $paths.Count -or $paths.Count -gt 20000) { throw 'Unrecognized/oversized installer archive listing.' }
    return $paths.ToArray()
}

function Assert-AcceptanceIdentity([string]$Path, [string]$Version, [switch]$Installer) {
    if ($Version -notmatch '^(\d+)\.(\d+)\.(\d+)-beta\.(\d+)$') { throw 'Expected a beta version.' }
    $fileVersion = "$($Matches[1]).$($Matches[2]).$($Matches[3]).$($Matches[4])"
    $info = [Diagnostics.FileVersionInfo]::GetVersionInfo($Path)
    if ($info.ProductName -cne 'ORCA beta' -or $info.ProductVersion -cne $Version -or $info.FileVersion -cne $fileVersion) { throw "Wrong beta identity/version: $Path" }
    if ($Installer) {
        if ($info.FileDescription -cne 'ORCA beta Installer') { throw 'Wrong installer identity.' }
    } elseif ([IO.Path]::GetFileName($Path) -cne 'OrcaBeta.exe' -or $info.OriginalFilename -cne 'OrcaBeta.exe') { throw 'Wrong executable identity.' }
}

function Assert-AcceptanceArtifact($Artifact) {
    foreach ($field in @('version', 'installer', 'portable', 'executableSha256')) { if (-not $Artifact.ContainsKey($field)) { throw "Missing artifact field: $field" } }
    if ($Artifact.version -notmatch '^\d+\.\d+\.\d+-beta\.\d+$' -or $Artifact.executableSha256 -cnotmatch '^[a-f0-9]{64}$') { throw 'Invalid artifact metadata.' }
    foreach ($kind in @('installer', 'portable')) { Assert-AcceptanceHash $Artifact[$kind].path $Artifact[$kind].sha256 }
    Assert-AcceptanceIdentity $Artifact.installer.path $Artifact.version -Installer
    $map = Get-AcceptancePortableMap $Artifact.portable.path ''
    if ($map['OrcaBeta.exe'] -cne $Artifact.executableSha256) { throw 'Portable executable does not match approved executable hash.' }
    return $map
}

function Get-AcceptanceUpgradeKind($Candidate, $Baseline) {
    if ($null -eq $Baseline) { return 'same-artifact-reinstall; cross-version-upgrade NOT_RUN' }
    $new = [version]$Candidate.version.Replace('-beta.', '.')
    $old = [version]$Baseline.version.Replace('-beta.', '.')
    if ($old -ge $new) { throw 'Baseline must be a distinct older beta version.' }
    return 'cross-version-upgrade'
}

function Get-AcceptancePlainFiles([string]$Root) {
    [void](Assert-AcceptancePath $Root)
    $queue = [Collections.Generic.Queue[string]]::new(); $queue.Enqueue($Root)
    while ($queue.Count) {
        foreach ($item in Get-ChildItem -LiteralPath $queue.Dequeue() -Force) {
            [void](Assert-AcceptancePath $item.FullName)
            if ($item.PSIsContainer) { $queue.Enqueue($item.FullName) } else { $item.FullName }
        }
    }
}

function Expand-AcceptanceArtifact($Artifact, [string]$Directory, [string]$SevenZip, [hashtable]$Environment, [int]$Timeout) {
    $portable = Join-Path $Directory 'portable'
    $map = Get-AcceptancePortableMap $Artifact.portable.path $portable
    Assert-AcceptanceIdentity (Join-Path $portable 'OrcaBeta.exe') $Artifact.version
    $listing = Invoke-AcceptanceProcess $SevenZip '' @('l', '-slt', '-ba', '--', $Artifact.installer.path) $Environment $Timeout
    $members = @(Get-AcceptanceSevenZipMembers $listing)
    $extracted = Join-Path $Directory 'nsis'
    if (Test-Path -LiteralPath $extracted) { throw 'Installer extraction destination already exists.' }
    [void](Invoke-AcceptanceProcess $SevenZip '' @('x', '-y', "-o$extracted", '--', $Artifact.installer.path) $Environment $Timeout)
    $files = @(Get-AcceptancePlainFiles $extracted)
    $executables = @($files | Where-Object { [IO.Path]::GetFileName($_) -ceq 'OrcaBeta.exe' })
    if ($executables.Count -ne 1) { throw 'Installer must contain exactly one OrcaBeta.exe.' }
    $payload = [IO.Path]::GetDirectoryName($executables[0])
    foreach ($name in $map.Keys) {
        if ($name -ceq 'LICENSE') { continue } # beta.nsi currently ships notices and the runtime licenses, not this top-level license.
        Assert-AcceptanceHash (Join-Path $payload $name) $map[$name]
    }
    Assert-AcceptanceIdentity $executables[0] $Artifact.version
    return @{files=$map; installerMembers=$members.Count; executableSha256=$map['OrcaBeta.exe']}
}

function Get-AcceptanceOwnedShortcuts([hashtable]$ShellPaths, [string]$Install) {
    $owned = @{}
    $shell = New-Object -ComObject WScript.Shell
    try {
        foreach ($key in $ShellPaths.Keys) {
            $path = $ShellPaths[$key]
            [void](Assert-AcceptancePath $path)
            if ($key.StartsWith('Common')) {
                if (Test-Path -LiteralPath $path) { throw 'Installer wrote an all-users shortcut.' }
                continue
            }
            if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Missing beta shortcut: $path" }
            $shortcut = $shell.CreateShortcut($path)
            try {
                $target = Join-Path $Install $(if ($key -match '/Uninstall ') { 'uninstall-beta.exe' } else { 'OrcaBeta.exe' })
                if (-not ([IO.Path]::GetFullPath($shortcut.TargetPath)).Equals($target, [StringComparison]::OrdinalIgnoreCase) -or $shortcut.Arguments) { throw 'Shortcut target/arguments are not test-owned.' }
            } finally { [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($shortcut) }
            $owned[$path] = Get-AcceptanceHash $path
        }
    } finally { [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell) }
    return $owned
}

function Remove-AcceptanceOwnedFile([string]$Path, [string]$Hash, [string[]]$AllowedPaths) {
    if ($AllowedPaths -notcontains $Path) { throw 'Cleanup path is not an exact owned artifact.' }
    Assert-AcceptanceHash $Path $Hash
    Remove-Item -LiteralPath $Path -Force
}

function Assert-AcceptanceInstalled($Artifact, [hashtable]$Files, [string]$Install, [hashtable]$ShellPaths) {
    foreach ($name in $Files.Keys) { if ($name -cne 'LICENSE') { Assert-AcceptanceHash (Join-Path $Install $name) $Files[$name] } }
    $allowed = @($Files.Keys | Where-Object { $_ -cne 'LICENSE' }) + @('.orca-beta-install', 'uninstall-beta.exe')
    foreach ($file in Get-AcceptancePlainFiles $Install) {
        if ([IO.Path]::GetRelativePath($Install, $file).Replace('\', '/') -cnotin $allowed) { throw "Unexpected installed file: $file" }
    }
    if ([IO.File]::ReadAllText((Join-Path $Install '.orca-beta-install')) -cne 'ORCA beta') { throw 'Wrong install ownership marker.' }
    Assert-AcceptanceIdentity (Join-Path $Install 'OrcaBeta.exe') $Artifact.version
    $registrations = Get-AcceptanceBetaRegistrations
    if (-not $registrations.ContainsKey('CurrentUser/Registry32') -or @($registrations.Keys | Where-Object { $_.StartsWith('LocalMachine/') }).Count) { throw 'Missing per-user registration or unexpected machine registration.' }
    foreach ($raw in $registrations.Values) {
        $values = $raw | ConvertFrom-Json -AsHashtable
        if ($values.DisplayName.value -cne 'ORCA beta' -or $values.DisplayVersion.value -cne $Artifact.version -or
            -not ([string]$values.InstallLocation.value).Equals($Install, [StringComparison]::OrdinalIgnoreCase) -or
            $values.UninstallString.value -cne ('"' + (Join-Path $Install 'uninstall-beta.exe') + '"') -or
            $values.QuietUninstallString.value -cne ('"' + (Join-Path $Install 'uninstall-beta.exe') + '" /S')) { throw 'Unexpected beta uninstall registration.' }
    }
    return @{registrations=$registrations; shortcuts=(Get-AcceptanceOwnedShortcuts $ShellPaths $Install); uninstallerSha256=(Get-AcceptanceHash (Join-Path $Install 'uninstall-beta.exe'))}
}

function Assert-AcceptanceReceipt($Receipt) {
    $current = Get-AcceptanceBetaRegistrations
    if ($current.Count -ne $Receipt.registrations.Count) { throw 'Registration ownership changed.' }
    foreach ($key in $Receipt.registrations.Keys) { if (-not $current.ContainsKey($key) -or $current[$key] -cne $Receipt.registrations[$key]) { throw 'Registration ownership changed.' } }
    foreach ($path in $Receipt.shortcuts.Keys) { Assert-AcceptanceHash $path $Receipt.shortcuts[$path] }
}

function Assert-AcceptanceSentinels([hashtable]$Files, [Diagnostics.Process]$Process) {
    foreach ($path in $Files.Keys) { Assert-AcceptanceHash $path $Files[$path] }
    if ($Process -and ($Process.HasExited -or $Process.ProcessName -ine 'Orca')) { throw 'Synthetic official-name process was closed or has the wrong identity.' }
}

# No installer, extractor, file, registry, or environment writes before these gates.
if (-not $IsWindows) { throw 'Windows is required.' }
if ($Mode -eq 'Execute') {
    if (-not ($ArtifactsReady -and $DisposableVM -and $AllowShellArtifacts)) { throw 'Execute requires all three explicit approval switches.' }
    Assert-AcceptanceExecuteGate $ArtifactsReady $DisposableVM $AllowShellArtifacts (Get-CimInstance Win32_ComputerSystem)
}
if (-not $ArtifactManifest) {
    if ($Mode -ne 'Preflight') { throw 'A hash-approved artifact manifest is required.' }
    [pscustomobject]@{mode='Preflight'; writes=$false; execution='Disposable VM/account only, after parent artifact approval'; required=@('ArtifactManifest', 'ManifestSha256', 'TestRoot', 'InstallRoot with Chinese characters', 'SevenZipPath'); gaps=@('No installer/portable tested', 'No wizard/native application launch tested')}
    return
}
Assert-AcceptanceHash $ArtifactManifest $ManifestSha256
$manifest = Get-Content -LiteralPath $ArtifactManifest -Raw | ConvertFrom-Json -AsHashtable
if ($manifest.schemaVersion -ne 1 -or $manifest.candidate.version -cne '3.0.14-beta.2') { throw 'Unsupported acceptance manifest/version.' }
$baseline = $null
if ($manifest.ContainsKey('baseline')) { $baseline = $manifest.baseline }
$upgradeKind = Get-AcceptanceUpgradeKind $manifest.candidate $baseline
$artifacts = @($manifest.candidate)
if ($baseline) { $artifacts += $baseline }
foreach ($artifact in $artifacts) { [void](Assert-AcceptanceArtifact $artifact) }
$shellPaths = Get-AcceptanceShellPaths
Assert-AcceptanceVacant (Get-AcceptanceBetaRegistrations) $shellPaths
$protected = @('UserProfile', 'Windows', 'ProgramFiles', 'ProgramFilesX86', 'CommonApplicationData') | ForEach-Object { [Environment]::GetFolderPath([Environment+SpecialFolder]::$_) }
$roots = Assert-AcceptanceRoots $TestRoot $InstallRoot $protected
foreach ($artifact in $artifacts) {
    foreach ($kind in @('installer', 'portable')) {
        if (Test-AcceptanceWithin ([IO.Path]::GetFullPath($artifact[$kind].path)) $roots.root) { throw 'Artifacts must be outside the disposable test root.' }
    }
}
[void](Assert-AcceptancePath $SevenZipPath)
if ([IO.Path]::GetFileName($SevenZipPath) -ine '7z.exe' -or -not (Test-Path -LiteralPath $SevenZipPath -PathType Leaf)) { throw 'Specify an already-installed trusted 7z.exe; no tools are installed by this script.' }
if ($Mode -eq 'Execute') { Assert-AcceptanceWebViewPresent; Assert-AcceptanceNoOrcaProcesses; Assert-AcceptancePristineVMAccount }
if ($Mode -eq 'Preflight') {
    [pscustomobject]@{mode=$Mode; writes=$false; version=$manifest.candidate.version; upgrade=$upgradeKind; root=$roots.root; install=$roots.install; shellArtifacts=$shellPaths; executionReady=$false}
    return
}

[void](Assert-AcceptancePath $roots.root)
$null = New-Item -ItemType Directory -Path $roots.root -ErrorAction Stop
$runID = [guid]::NewGuid().ToString('N')
Write-AcceptanceNewFile (Join-Path $roots.root 'acceptance-owner.txt') $runID
$environment = @{}
foreach ($pair in @{HOME='profiles/home'; USERPROFILE='profiles/home'; APPDATA='profiles/roaming'; LOCALAPPDATA='profiles/local'; TEMP='temp'; TMP='temp'}.GetEnumerator()) {
    $environment[$pair.Key] = Join-Path $roots.root $pair.Value
    [void][IO.Directory]::CreateDirectory($environment[$pair.Key])
}
$report = [ordered]@{schemaVersion=1; runID=$runID; mode=$Mode; manifestSha256=$ManifestSha256; installerExecuted=$false; status='running'; upgrade=$upgradeKind; stages=@(); gaps=@('Wizard UI and actual application startup are not exercised', 'Coexistence fixtures are synthetic, never real official user data')}
$sentinels = @{}; $officialProcess = $null; $receipt = $null; $failure = $null
try {
    $candidate = Expand-AcceptanceArtifact $manifest.candidate (Join-Path $roots.root 'candidate') $SevenZipPath $environment $TimeoutSeconds
    $report.stages += 'candidate installer/portable member hashes and PE identity verified'
    $firstArtifact = $manifest.candidate; $first = $candidate
    if ($baseline) { $firstArtifact = $baseline; $first = Expand-AcceptanceArtifact $baseline (Join-Path $roots.root 'baseline') $SevenZipPath $environment $TimeoutSeconds }
    if ($Mode -eq 'Execute') {
        Assert-AcceptancePristineVMAccount
        # These are NEW synthetic directories in the disposable VM's actual
        # shell-resolved profile. Environment-only fixtures cannot cover them.
        foreach ($directory in Get-AcceptanceVMFixtureRoots) {
            New-AcceptanceSentinelDirectory $directory $runID
            foreach ($name in @('config.json', 'sessions/acceptance-sentinel.jsonl')) {
                $path = Join-Path $directory $name
                Write-AcceptanceNewFile $path "synthetic known-folder sentinel $runID"
                $sentinels[$path] = Get-AcceptanceHash $path
            }
            $marker = Join-Path $directory 'acceptance-owner.txt'
            $sentinels[$marker] = Get-AcceptanceHash $marker
        }
        foreach ($relative in @('profiles/home/.orca/config.json', 'profiles/home/.orca/sessions/sentinel.jsonl', 'profiles/roaming/ORCA beta/account/sentinel.json', 'profiles/roaming/ORCA beta/chats/sentinel.jsonl', 'official/Programs/O.R.C.A for Windows/sentinel.txt')) {
            $path = Join-Path $roots.root $relative
            Write-AcceptanceNewFile $path "synthetic acceptance sentinel $runID"
            $sentinels[$path] = Get-AcceptanceHash $path
        }
        # A copy of the OS command interpreter supplies ONLY an official-name
        # process sentinel. No real ORCA executable or user application runs.
        $sentinelExe = Join-Path $roots.root 'official/Orca.exe'
        $cmd = Join-Path ([Environment]::GetFolderPath('System')) 'cmd.exe'
        [IO.File]::Copy($cmd, $sentinelExe, $false)
        $sentinels[$sentinelExe] = Get-AcceptanceHash $cmd
        $start = [Diagnostics.ProcessStartInfo]::new($sentinelExe, '/d /q /k')
        $start.UseShellExecute=$false; $start.CreateNoWindow=$true; $start.RedirectStandardInput=$true
        Set-AcceptanceChildEnvironment $start $environment
        $officialProcess = [Diagnostics.Process]::Start($start)
        Assert-AcceptanceSentinels $sentinels $officialProcess
        Assert-AcceptanceVacant (Get-AcceptanceBetaRegistrations) $shellPaths
        Assert-AcceptanceWebViewPresent
        Assert-AcceptanceHash $firstArtifact.installer.path $firstArtifact.installer.sha256
        $report.installerExecuted = $true
        [void](Invoke-AcceptanceProcess $firstArtifact.installer.path (Get-AcceptanceNSISArguments $roots.install) @() $environment $TimeoutSeconds)
        $receipt = Assert-AcceptanceInstalled $firstArtifact $first.files $roots.install $shellPaths
        Assert-AcceptanceSentinels $sentinels $officialProcess
        $report.stages += 'silent first install verified; synthetic data/process retained'
        Assert-AcceptanceReceipt $receipt
        Assert-AcceptanceWebViewPresent
        Assert-AcceptanceHash $manifest.candidate.installer.path $manifest.candidate.installer.sha256
        [void](Invoke-AcceptanceProcess $manifest.candidate.installer.path (Get-AcceptanceNSISArguments $roots.install) @() $environment $TimeoutSeconds)
        $receipt = Assert-AcceptanceInstalled $manifest.candidate $candidate.files $roots.install $shellPaths
        Assert-AcceptanceSentinels $sentinels $officialProcess
        $report.stages += "verified $upgradeKind; synthetic data/process retained"
        Assert-AcceptanceReceipt $receipt
        $uninstaller = Join-Path $roots.install 'uninstall-beta.exe'
        Assert-AcceptanceHash $uninstaller $receipt.uninstallerSha256
        $copy = Join-Path $roots.root 'owned-uninstall-beta.exe'
        [IO.File]::Copy($uninstaller, $copy, $false)
        Assert-AcceptanceHash $copy $receipt.uninstallerSha256
        [void](Invoke-AcceptanceProcess $copy (Get-AcceptanceNSISArguments $roots.install -Uninstall) @() $environment $TimeoutSeconds)
        Assert-AcceptanceVacant (Get-AcceptanceBetaRegistrations) $shellPaths
        if (Test-Path -LiteralPath $roots.install) { throw 'Uninstall left the installation directory behind.' }
        Assert-AcceptanceSentinels $sentinels $officialProcess
        Remove-AcceptanceOwnedFile $copy $receipt.uninstallerSha256 @($copy)
        $report.stages += 'silent uninstall removed only installation/registration/shortcuts; data/process retained'
        $report.status = 'PASS_SCOPED'
    } else {
        $report.status = 'EXTRACTION_ONLY_NOT_INSTALL_ACCEPTANCE'
        $report.gaps += 'No install, upgrade, uninstall, registry, shell shortcut, or running-process coexistence acceptance'
    }
} catch {
    $failure = $_; $report.status = 'FAILED'; $report['error'] = $_.Exception.Message
    # Never guess ownership after a partial installer failure. The disposable VM
    # is retained/discarded by its operator, not repaired using global deletion.
    $report.gaps += 'Failure artifacts retained; no automatic uninstaller or registry cleanup attempted'
} finally {
    if ($officialProcess) {
        try {
            if (-not $officialProcess.HasExited) {
                $officialProcess.StandardInput.WriteLine('exit')
                $officialProcess.StandardInput.Close()
                if (-not $officialProcess.WaitForExit(5000)) { $report.gaps += 'Owned process sentinel did not exit; discard VM, do not kill user processes' }
            }
        } finally { $officialProcess.Dispose() }
    }
    if ($receipt) { $report['lastOwnedReceipt'] = $receipt }
    $report['syntheticSentinels'] = $sentinels
    Write-AcceptanceNewFile (Join-Path $roots.root 'acceptance-report.json') ($report | ConvertTo-Json -Depth 12)
}
if ($failure) { throw $failure }
[pscustomobject]$report
