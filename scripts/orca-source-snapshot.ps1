#requires -Version 7.0
[CmdletBinding()]
param(
    [string]$RepositoryRoot = (Join-Path $PSScriptRoot '..'),
    [string]$AllowlistPath,
    [Parameter(Mandatory = $false)][string]$OutputDirectory,
    [Alias('Snapshot')][switch]$CreateSnapshot,
    [switch]$SourceZip
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Invoke-OrcaGit([string]$Root, [string[]]$Arguments) {
    $start = [Diagnostics.ProcessStartInfo]::new('git')
    $start.UseShellExecute = $false; $start.CreateNoWindow = $true
    $start.RedirectStandardOutput = $true; $start.RedirectStandardError = $true
    $start.StandardOutputEncoding = [Text.UTF8Encoding]::new($false, $true)
    foreach ($arg in @('--no-optional-locks', '-C', $Root) + $Arguments) { $start.ArgumentList.Add($arg) }
    $process = [Diagnostics.Process]::Start($start)
    try {
        $stdout = $process.StandardOutput.ReadToEndAsync(); $stderr = $process.StandardError.ReadToEndAsync()
        $process.WaitForExit()
        if ($process.ExitCode -ne 0) { throw "Git inventory failed: $($stderr.GetAwaiter().GetResult())" }
        return $stdout.GetAwaiter().GetResult()
    } finally { $process.Dispose() }
}

function Assert-OrcaPlainPath([string]$Path) {
    $full = [IO.Path]::GetFullPath($Path)
    for ($p = $full; $p; $p = [IO.Path]::GetDirectoryName($p)) {
        if ([IO.File]::Exists($p) -or [IO.Directory]::Exists($p)) {
            if ([IO.File]::GetAttributes($p) -band [IO.FileAttributes]::ReparsePoint) { throw "Reparse point refused: $p" }
        } elseif (Test-Path -LiteralPath $p) { throw "Unsupported filesystem entry: $p" }
    }
    return $full
}

function Assert-OrcaRelativePath([string]$Path) {
    if (-not $Path -or $Path -match '[\\:<>"|?*\x00-\x1f]' -or $Path.StartsWith('/') -or $Path.EndsWith('/')) { throw "Unsafe source path: $Path" }
    foreach ($part in $Path.Split('/')) {
        if (-not $part -or $part -in @('.', '..') -or $part -match '[. ]$|^(?i:CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(?:\.|$)') { throw "Unsafe source path: $Path" }
    }
}

function Get-OrcaPathExclusion([string]$Path) {
    # Baseline source entrypoints/documentation, not profile or binary directories.
    # These still pass Git selection, new-source approval and content validation.
    if ($Path -ceq 'cmd/orca/main.go' -or $Path -ceq 'npm/orca-agent/bin/orca.js' -or $Path -cmatch '^docs/releases/[A-Za-z0-9_.-]+\.md$') { return $null }
    $parts = $Path.TrimEnd('/').Split('/')
    $directories = if ($Path.EndsWith('/')) { $parts } elseif ($parts.Length -gt 1) { $parts[0..($parts.Length - 2)] } else { @() }
    foreach ($part in $directories) {
        if ($part -match '^(?i:\.git|\.tmp|tmp|dist|release|releases|bin|node_modules|wailsjs|installer-go|\.cache|cache|caches|\.gocache|\.gopath|\.tools|\.npm|\.npm-cache|\.pnpm-store|\.codegraph|\.astro|\.idea|\.vscode|stage|\.stage)$') { return 'generated-cache-or-release-directory' }
        if ($part -match '^(?i:secrets?|credentials?|profiles?|browser[-_]profiles?|userdata|user data|webview2|EBWebView|AppData|Application Data|ORCA|ORCA beta|O\.R\.C\.A|\.ssh|\.aws|\.azure|\.docker|\.kube|\.gnupg|\.codex|\.config|\.orca|\.orca-beta|\.orca-web-beta|\.orcaaccount|\.deepseek-orca)$') { return 'private-profile-directory' }
    }
    $name = $parts[-1]
    if ($name -in @('.npmrc', '.pypirc', '.netrc', '_netrc', '.git-credentials', '.authinfo')) { return 'private-config-or-secret' }
    if ($name -match '^(?i:\.git|\.env(?:\..*)?|.*\.env(?:\..*)?|credentials?(?:\..*)?|secrets?(?:\..*)?|id_rsa|id_ed25519|Cookies|Login Data|Local State|deepseek-orca\.toml)$' -or $name -match '(?i)\.(pem|key|pfx|p12|local)$') { return 'private-config-or-secret' }
    if ($name -match '(?i)\.(exe|dll|pdb|syso|obj|o|a|lib|so|dylib|wasm|test|msi|msix|appx|dmg|deb|pkg|zip|7z|rar|tar|gz|tgz|jar|class|pyc|db|sqlite3?|bak|log|prof|out|coverprofile)$') { return 'compiled-archive-or-log' }
    if ($name -in @('package.json.md5', 'Thumbs.db', '.DS_Store')) { return 'generated-cache-file' }
    return $null
}

function Get-OrcaHash([string]$Path) {
    $plain = Assert-OrcaPlainPath $Path
    return (Get-FileHash -LiteralPath $plain -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Get-OrcaTextHash([string]$Text) {
    return [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($Text))).ToLowerInvariant()
}

function Assert-OrcaSourceContent([string]$Path, [string]$Relative, [bool]$Tracked) {
    $stream = [IO.File]::Open($Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    try {
        $buffer = [byte[]]::new(8192); $count = $stream.Read($buffer, 0, $buffer.Length)
        $magic = [Convert]::ToHexString($buffer[0..([Math]::Max(0, [Math]::Min($count, 8) - 1))])
        $ext = [IO.Path]::GetExtension($Relative).ToLowerInvariant()
        $resourceMagic = @{ '.png'='89504E470D0A1A0A'; '.jpg'='FFD8FF'; '.jpeg'='FFD8FF'; '.ico'='00000100'; '.gif'='47494638'; '.woff'='774F4646'; '.woff2'='774F4632'; '.ttf'='00010000'; '.otf'='4F54544F'; '.bmp'='424D' }
        if ($resourceMagic.ContainsKey($ext)) {
            if ($Tracked -and $ext -in @('.png', '.jpg', '.jpeg') -and $magic -match '^(89504E470D0A1A0A|FFD8FF)') { return }
            if (-not $magic.StartsWith($resourceMagic[$ext])) { throw "Invalid source resource signature: $Relative" }
            return
        }
        # These existing binary document fixtures are source test inputs, not release payloads.
        if ($Tracked -and $Relative.StartsWith('internal/artifact/testdata/', [StringComparison]::Ordinal) -and $ext -in @('.pdf', '.docx', '.xlsx', '.pptx')) {
            $expected = if ($ext -eq '.pdf') { '25504446' } else { '504B0304' }
            if (-not $magic.StartsWith($expected)) { throw "Invalid document fixture signature: $Relative" }
            return
        }
        if ($magic -match '^(4D5A|7F454C46|504B0304|1F8B|D0CF11E0|FEEDFACE|FEEDFACF|CEFAEDFE|CFFAEDFE|CAFEBABE|52617221)') { throw "Unapproved binary content: $Relative" }
        do {
            if ($count -gt 0 -and [Array]::IndexOf($buffer, [byte]0, 0, $count) -ge 0) { throw "Unapproved binary content: $Relative" }
            $count = $stream.Read($buffer, 0, $buffer.Length)
        } while ($count -gt 0)
    } finally { $stream.Dispose() }
}

function Get-OrcaResourceAudit([string]$Root, [object[]]$Files, [object[]]$Unexpected, [object[]]$Excluded) {
    $selected = @{}; $pending = @{}; $omitted = @{}
    foreach ($file in $Files) { $selected[$file.path] = $file.sha256 }
    foreach ($file in $Unexpected) { $pending[$file.path] = $file.sha256 }
    foreach ($file in $Excluded) { $omitted[$file.path] = $file.reason }
    $resources = [Collections.Generic.List[object]]::new()
    $errors = [Collections.Generic.List[string]]::new()
    # Current directives use literal paths. New glob/quoted/directory patterns
    # require an explicit resolver review instead of guessing their closure.
    foreach ($file in @($Files) + @($Unexpected) | Where-Object { $_.path.EndsWith('.go') }) {
        $absolute = Assert-OrcaPlainPath (Join-Path $Root $file.path)
        $content = [IO.File]::ReadAllText($absolute)
        if ((Get-OrcaHash $absolute) -cne $file.sha256) { $errors.Add("Source changed during embed audit: $($file.path)"); continue }
        foreach ($directive in [regex]::Matches($content, '(?m)^//go:embed[ \t]+([^\r\n]+)')) {
            foreach ($pattern in $directive.Groups[1].Value.Trim().Split([char[]]" `t", [StringSplitOptions]::RemoveEmptyEntries)) {
                $target = $null; $state = 'unresolved-pattern'; $hash = $null
                if ($file.path -ceq 'desktop/main.go' -and $pattern -ceq 'all:frontend/dist') {
                    $target = 'desktop/frontend/dist'; $state = 'required-fresh-build-only'
                } elseif ($pattern -cmatch '^[A-Za-z0-9_./-]+$' -and $pattern -notmatch '(^|/)\.\.?(/|$)') {
                    $parent = [IO.Path]::GetDirectoryName($file.path).Replace('\', '/')
                    $target = if ($parent) { "$parent/$pattern" } else { $pattern }
                    if ($selected.ContainsKey($target)) { $state = 'selected'; $hash = $selected[$target] }
                    elseif ($pending.ContainsKey($target)) { $state = 'awaiting-review'; $hash = $pending[$target] }
                    elseif ($omitted.ContainsKey($target)) { $state = 'excluded' }
                    else { $state = 'missing-or-directory' }
                }
                $resources.Add([ordered]@{source=$file.path; sourceSelected=$selected.ContainsKey($file.path); pattern=$pattern; path=$target; state=$state; sha256=$hash})
                if ($state -notin @('selected', 'awaiting-review', 'required-fresh-build-only')) { $errors.Add("Embedded resource requires review: $($file.path) -> $pattern ($state)") }
            }
        }
    }
    $fixtures = [Collections.Generic.List[object]]::new()
    foreach ($file in $Files | Where-Object { $_.origin -eq 'git-index' -and $_.path -match '(^|/)testdata/' }) {
        $fixtures.Add([ordered]@{path=$file.path; state='selected'; sha256=$file.sha256})
    }
    foreach ($file in $Excluded | Where-Object { $_.tracked -and $_.path -match '(^|/)testdata/' }) {
        $fixtures.Add([ordered]@{path=$file.path; state='excluded'; reason=$file.reason})
        $errors.Add("Tracked test fixture excluded; explicit policy review required: $($file.path)")
    }
    return [pscustomobject]@{embeds=@($resources.ToArray()); trackedFixtures=@($fixtures.ToArray()); errors=@($errors.ToArray())}
}

function Get-OrcaSourceGroup([string]$Path) {
    switch -Regex ($Path) {
        '^desktop/frontend/' { return 'frontend' }
        '^(desktop/(orca_account|account_)|internal/orcaaccount/)' { return 'account' }
        '^(desktop/(web_beta|webbetahost/)|internal/webbeta/)' { return 'web-beta' }
        '^desktop/(browser_|browserhost/)' { return 'native-browser' }
        '^(scripts/|desktop/(build/|beta_.*(installer|portable|source_snapshot)|release_workflow))' { return 'build-and-resources' }
        '^desktop/' { return 'desktop-other' }
        '^internal/' { return 'core' }
        '^docs/' { return 'documentation' }
        default { return 'root-and-other' }
    }
}

function Write-OrcaSelectionAudit($Plan, [string]$Directory) {
    $rows = [Collections.Generic.List[object]]::new()
    $changed = [Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
    foreach ($path in $Plan.modifiedTracked) { [void]$changed.Add($path) }
    foreach ($file in $Plan.files) {
        $state = if ($file.origin -ne 'git-index') { 'reviewed-new-source' } elseif ($changed.Contains($file.path)) { 'modified-tracked' } else { 'tracked' }
        $rows.Add([pscustomobject][ordered]@{group=(Get-OrcaSourceGroup $file.path); state=$state; path=$file.path; sha256=$file.sha256; bytes=$file.bytes; reason=$null})
    }
    foreach ($file in $Plan.unexpected) { $rows.Add([pscustomobject][ordered]@{group=(Get-OrcaSourceGroup $file.path); state='unreviewed'; path=$file.path; sha256=$file.sha256; bytes=$null; reason=$file.reason}) }
    foreach ($file in $Plan.excluded) { $rows.Add([pscustomobject][ordered]@{group=(Get-OrcaSourceGroup $file.path); state='excluded'; path=$file.path; sha256=$null; bytes=$null; reason=$file.reason}) }
    $groups = @($rows | Group-Object group | Sort-Object Name | ForEach-Object {
        [ordered]@{group=$_.Name; counts=@($_.Group | Group-Object state | Sort-Object Name | ForEach-Object { @{state=$_.Name; count=$_.Count} }); entries=@($_.Group | Sort-Object state,path)}
    })
    Write-OrcaJson (Join-Path $Directory 'selection-report.json') ([ordered]@{schemaVersion=1; allowed=$Plan.allowed; git=$Plan.git; selectionSha256=$Plan.selectionSha256; files=$Plan.files; excluded=$Plan.excluded; unexpected=$Plan.unexpected; errors=$Plan.errors})
    Write-OrcaJson (Join-Path $Directory 'new-source-candidates.json') ([ordered]@{kind='orca-unapproved-source-candidates'; approved=$false; baseCommit=$Plan.git.head; candidates=@($rows | Where-Object state -EQ 'unreviewed')})
    Write-OrcaJson (Join-Path $Directory 'source-inventory.json') ([ordered]@{kind='orca-source-selection-inventory'; baseCommit=$Plan.git.head; selectionSha256=$Plan.selectionSha256; groups=$groups})
    Write-OrcaJson (Join-Path $Directory 'resource-audit.json') $Plan.resources
    $lines = [Collections.Generic.List[string]]::new()
    $lines.Add('# Source Selection Review'); $lines.Add('')
    $lines.Add('Review inventory only. No candidate is approved by this document.'); $lines.Add('')
    $lines.Add("Base commit: ``$($Plan.git.head)``. Selected: $($Plan.files.Count); unreviewed: $($Plan.unexpected.Count); excluded: $($Plan.excluded.Count); errors: $($Plan.errors.Count).")
    $lines.Add(''); $lines.Add('| Group | Tracked | Modified tracked | Reviewed new | Unreviewed | Excluded |'); $lines.Add('| --- | ---: | ---: | ---: | ---: | ---: |')
    foreach ($group in $groups) {
        $counts = @{}; foreach ($count in $group.counts) { $counts[$count.state] = $count.count }
        $lines.Add("| $($group.group) | $([int]$counts['tracked']) | $([int]$counts['modified-tracked']) | $([int]$counts['reviewed-new-source']) | $([int]$counts['unreviewed']) | $([int]$counts['excluded']) |")
    }
    foreach ($group in $groups) {
        $lines.Add(''); $lines.Add("## $($group.group)")
        foreach ($file in $group.entries | Where-Object state -EQ 'unreviewed') { $lines.Add("- UNREVIEWED ``$($file.path)`` SHA256 ``$($file.sha256)``") }
    }
    $lines.Add(''); $lines.Add('## Resource Closure')
    foreach ($embed in $Plan.resources.embeds) { $lines.Add("- $($embed.state): ``$($embed.source)`` -> ``$($embed.pattern)``") }
    $lines.Add("Tracked test fixtures: $($Plan.resources.trackedFixtures.Count). See resource-audit.json for individual hashes/status.")
    $lines.Add(''); $lines.Add('## Exclusions')
    foreach ($file in $Plan.excluded) { $lines.Add("- ``$($file.path)``: $($file.reason)") }
    foreach ($errorMessage in $Plan.errors) { $lines.Add("- ERROR: $errorMessage") }
    [IO.File]::WriteAllLines((Join-Path $Directory 'REVIEW.md'), $lines, [Text.UTF8Encoding]::new($false))
    $sums = foreach ($name in @('selection-report.json', 'new-source-candidates.json', 'source-inventory.json', 'resource-audit.json', 'REVIEW.md')) { "$(Get-OrcaHash (Join-Path $Directory $name)) *$name" }
    [IO.File]::WriteAllLines((Join-Path $Directory 'AUDIT-SHA256SUMS.txt'), $sums, [Text.UTF8Encoding]::new($false))
}

function Get-OrcaSourcePlan([string]$Root, [string]$ApprovedNewFiles) {
    $rootPath = Assert-OrcaPlainPath $Root
    $top = (Invoke-OrcaGit $rootPath @('rev-parse', '--show-toplevel')).Trim()
    if (-not [IO.Path]::GetFullPath($top).Equals($rootPath.TrimEnd([IO.Path]::DirectorySeparatorChar), [StringComparison]::OrdinalIgnoreCase)) { throw 'Source root must be the Git worktree root.' }
    $head = (Invoke-OrcaGit $rootPath @('rev-parse', '--verify', 'HEAD')).Trim()
    $index = Invoke-OrcaGit $rootPath @('ls-files', '--stage', '-z')
    $status = Invoke-OrcaGit $rootPath @('status', '--porcelain=v1', '-z', '--untracked-files=no')
    $approved = [Collections.Generic.Dictionary[string,string]]::new([StringComparer]::OrdinalIgnoreCase)
    $declined = [Collections.Generic.Dictionary[string,string]]::new([StringComparer]::OrdinalIgnoreCase)
    $allowlistHash = $null
    if ($ApprovedNewFiles) {
        $allowlistHash = Get-OrcaHash $ApprovedNewFiles
        $allowlist = Get-Content -LiteralPath $ApprovedNewFiles -Raw -Encoding utf8 | ConvertFrom-Json -AsHashtable
        if ($allowlist.schemaVersion -ne 1 -or $allowlist.baseCommit -cne $head -or -not $allowlist.ContainsKey('files') -or @($allowlist.Keys | Where-Object { $_ -notin @('schemaVersion', 'baseCommit', 'files', 'exclude') }).Count) { throw 'New-source allowlist must match schema 1 and the exact base commit.' }
        foreach ($item in $allowlist.files) {
            Assert-OrcaRelativePath $item.path
            if (@($item.Keys | Where-Object { $_ -notin @('path', 'sha256') }).Count -or $item.sha256 -cnotmatch '^[0-9a-f]{64}$' -or -not $approved.TryAdd($item.path, $item.sha256)) { throw 'Invalid or duplicate new-source allowlist member.' }
        }
        if ($allowlist.ContainsKey('exclude')) {
            foreach ($item in $allowlist.exclude) {
                Assert-OrcaRelativePath $item.path
                if (@($item.Keys | Where-Object { $_ -notin @('path', 'sha256') }).Count -or $item.sha256 -cnotmatch '^[0-9a-f]{64}$' -or $approved.ContainsKey($item.path) -or -not $declined.TryAdd($item.path, $item.sha256)) { throw 'Invalid or duplicate reviewed exclusion.' }
            }
        }
    }
    $files = [Collections.Generic.List[object]]::new()
    $excluded = [Collections.Generic.List[object]]::new()
    $unexpected = [Collections.Generic.List[object]]::new()
    $errors = [Collections.Generic.List[string]]::new()
    $seen = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    $used = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    $declinedUsed = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    $candidates = [Collections.Generic.List[object]]::new()
    foreach ($record in $index.Split([char]0, [StringSplitOptions]::RemoveEmptyEntries)) {
        if ($record -cnotmatch '^([0-9]{6}) ([0-9a-f]+) ([0-3])\t(.+)$') { throw 'Unsupported Git index record.' }
        $candidates.Add(@{ path=$Matches[4]; gitMode=$Matches[1]; indexObject=$Matches[2]; stage=$Matches[3]; tracked=$true })
    }
    foreach ($path in (Invoke-OrcaGit $rootPath @('ls-files', '--others', '--exclude-standard', '-z')).Split([char]0, [StringSplitOptions]::RemoveEmptyEntries)) {
        $candidates.Add(@{ path=$path; gitMode='100644'; indexObject=$null; stage='0'; tracked=$false })
    }
    foreach ($item in ($candidates | Sort-Object { $_.path } -CaseSensitive)) {
        $path = $item.path
        try {
            Assert-OrcaRelativePath $path
            if (-not $seen.Add($path)) { throw "Duplicate/case-colliding or unmerged source path: $path" }
            if ($item.stage -ne '0' -or $item.gitMode -eq '160000') { throw "Unmerged index/submodule cannot be snapshotted: $path" }
            $reason = Get-OrcaPathExclusion $path
            if ($item.gitMode -eq '120000') { $reason = 'git-symlink' }
            if ($reason) { $excluded.Add(@{path=$path; reason=$reason; tracked=$item.tracked}); continue }
            $absolute = Join-Path $rootPath $path
            try { [void](Assert-OrcaPlainPath $absolute) } catch { $excluded.Add(@{path=$path; reason='reparse-point'; tracked=$item.tracked}); $errors.Add($_.Exception.Message); continue }
            if (-not [IO.File]::Exists($absolute)) {
                if ($item.tracked) { $excluded.Add(@{path=$path; reason='deleted-working-file'; tracked=$true}); continue }
                throw "Source file disappeared: $path"
            }
            if (-not $item.tracked -and $declined.ContainsKey($path)) {
                if (@($declined.Keys) -cnotcontains $path -or (Get-OrcaHash $absolute) -cne $declined[$path]) { throw "Reviewed excluded file changed: $path" }
                [void]$declinedUsed.Add($path)
                $excluded.Add(@{path=$path; reason='reviewed-new-file-exclusion'; tracked=$false; sha256=$declined[$path]})
                continue
            }
            Assert-OrcaSourceContent $absolute $path $item.tracked
            $hash = Get-OrcaHash $absolute
            if (-not $item.tracked) {
                if (-not $approved.ContainsKey($path)) { $unexpected.Add(@{path=$path; sha256=$hash; reason='unreviewed-new-source'}); continue }
                if (@($approved.Keys) -cnotcontains $path) { throw "Approved new-source path case differs: $path" }
                if ($approved[$path] -cne $hash) { throw "Approved new-source hash changed: $path" }
                [void]$used.Add($path)
            }
            $files.Add([ordered]@{path=$path; sha256=$hash; bytes=([IO.FileInfo]$absolute).Length; origin=$(if($item.tracked){'git-index'}else{'reviewed-new-source'}); gitMode=$item.gitMode; indexObject=$item.indexObject})
        } catch { $errors.Add($_.Exception.Message) }
    }
    foreach ($path in $approved.Keys) { if (-not $used.Contains($path)) { $errors.Add("Unused, excluded, tracked, missing, or mismatched allowlist entry: $path") } }
    foreach ($path in $declined.Keys) { if (-not $declinedUsed.Contains($path)) { $errors.Add("Unused, tracked, missing, or policy-excluded review exclusion: $path") } }
    foreach ($path in (Invoke-OrcaGit $rootPath @('ls-files', '--others', '--ignored', '--exclude-standard', '--directory', '-z')).Split([char]0, [StringSplitOptions]::RemoveEmptyEntries)) {
        $excluded.Add(@{path=$path; reason='git-ignored-not-traversed'; tracked=$false})
    }
    foreach ($path in (Invoke-OrcaGit $rootPath @('diff', '--no-ext-diff', '--no-textconv', '--name-only', '--diff-filter=D', '--no-renames', '-z', 'HEAD', '--')).Split([char]0, [StringSplitOptions]::RemoveEmptyEntries)) {
        if (-not $seen.Contains($path)) { $excluded.Add(@{path=$path; reason='deleted-from-index'; tracked=$true}) }
    }
    foreach ($path in @('LICENSE', 'THIRD-PARTY-NOTICES.txt')) {
        if (-not @($files | Where-Object { $_.path -ceq $path }).Count) { $errors.Add("Required baseline license missing from selection: $path") }
    }
    $resources = Get-OrcaResourceAudit $rootPath $files.ToArray() $unexpected.ToArray() $excluded.ToArray()
    foreach ($message in $resources.errors) { $errors.Add($message) }
    $modifiedTracked = @((Invoke-OrcaGit $rootPath @('diff', '--no-ext-diff', '--no-textconv', '--name-only', '--no-renames', '-z', 'HEAD', '--')).Split([char]0, [StringSplitOptions]::RemoveEmptyEntries))
    $git = [ordered]@{head=$head; indexSha256=(Get-OrcaTextHash $index); trackedStatusSha256=(Get-OrcaTextHash $status); dirty=($status.Length -gt 0 -or $unexpected.Count -gt 0 -or $declined.Count -gt 0 -or @($files | Where-Object origin -EQ 'reviewed-new-source').Count -gt 0)}
    $identity = [ordered]@{git=$git; allowlistSha256=$allowlistHash; files=@($files.ToArray())}
    return [pscustomobject]@{
        schemaVersion=1; root=$rootPath; allowlistPath=$ApprovedNewFiles; git=$git; allowlistSha256=$allowlistHash
        selectionSha256=(Get-OrcaTextHash ($identity | ConvertTo-Json -Depth 12 -Compress))
        files=@($files.ToArray()); excluded=@($excluded.ToArray()); unexpected=@($unexpected.ToArray()); errors=@($errors.ToArray())
        modifiedTracked=$modifiedTracked; resources=$resources
        allowed=($unexpected.Count -eq 0 -and $errors.Count -eq 0)
    }
}

function New-OrcaOutputDirectory([string]$Root, [string]$Directory) {
    if (-not [IO.Path]::IsPathFullyQualified($Directory)) { throw 'Output must be an absolute fresh directory.' }
    $output = Assert-OrcaPlainPath $Directory
    $relative = [IO.Path]::GetRelativePath($Root, $output).Replace('\', '/')
    if ($relative -eq '.' -or (-not $relative.StartsWith('../') -and -not [IO.Path]::IsPathRooted($relative) -and $relative -notmatch '^(\.tmp|dist)/')) { throw 'In-worktree output is allowed only beneath excluded .tmp/ or dist/.' }
    if (Test-Path -LiteralPath $output) { throw 'Output already exists; refusing to overwrite previous evidence.' }
    [void][IO.Directory]::CreateDirectory($output)
    return $output
}

function Write-OrcaJson([string]$Path, $Value) {
    [IO.File]::WriteAllText($Path, (($Value | ConvertTo-Json -Depth 20) + "`n"), [Text.UTF8Encoding]::new($false))
}

function Get-OrcaPlainFiles([string]$Directory) {
    $pending = [Collections.Generic.Stack[string]]::new()
    $pending.Push((Assert-OrcaPlainPath $Directory))
    while ($pending.Count) {
        foreach ($entry in [IO.Directory]::EnumerateFileSystemEntries($pending.Pop())) {
            [void](Assert-OrcaPlainPath $entry)
            if ([IO.Directory]::Exists($entry)) { $pending.Push($entry) } else { $entry }
        }
    }
}

function Copy-OrcaVerifiedFiles([string]$Root, [string]$Destination, [object[]]$Files) {
    [void](Assert-OrcaPlainPath $Root); [void](Assert-OrcaPlainPath $Destination)
    if (Test-Path -LiteralPath $Destination) { throw 'Copy destination must not already exist.' }
    [void][IO.Directory]::CreateDirectory($Destination)
    foreach ($file in $Files) {
        Assert-OrcaRelativePath $file.path
        $from = Assert-OrcaPlainPath (Join-Path $Root $file.path)
        $to = Assert-OrcaPlainPath (Join-Path $Destination $file.path)
        if ((Get-OrcaHash $from) -cne $file.sha256) { throw "Input changed before copy: $($file.path)" }
        [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($to))
        [IO.File]::Copy($from, $to, $false)
        if ((Get-OrcaHash $to) -cne $file.sha256 -or (Get-OrcaHash $from) -cne $file.sha256) { throw "Input/copy changed: $($file.path)" }
    }
}

function Write-OrcaSourceZip([string]$Archive, [string]$Source, [object[]]$Files, [hashtable]$Metadata) {
    [void](Assert-OrcaPlainPath $Archive)
    if (Test-Path -LiteralPath $Archive) { throw 'Source ZIP already exists.' }
    $members = [ordered]@{}
    foreach ($file in $Files) {
        if (Get-OrcaPathExclusion $file.path) { throw "Excluded source ZIP member: $($file.path)" }
        Assert-OrcaRelativePath $file.path
        $members.Add('source/' + $file.path, @{path=(Join-Path $Source $file.path); sha256=$file.sha256; mode=$file.gitMode})
    }
    foreach ($name in $Metadata.Keys) {
        Assert-OrcaRelativePath $name
        if ($name -cnotin @('manifest.json', 'selection-report.json', 'SHA256SUMS.txt')) { throw 'Invalid source ZIP metadata name.' }
        $members.Add($name, @{path=$Metadata[$name]; sha256=(Get-OrcaHash $Metadata[$name]); mode='100644'})
    }
    $zip = [IO.Compression.ZipFile]::Open($Archive, [IO.Compression.ZipArchiveMode]::Create)
    try {
        foreach ($name in $members.Keys) {
            $member = $members[$name]
            if ((Get-OrcaHash $member.path) -cne $member.sha256) { throw "Snapshot changed before ZIP: $name" }
            $entry = $zip.CreateEntry($name, [IO.Compression.CompressionLevel]::Optimal)
            $entry.LastWriteTime = [DateTimeOffset]::new(2000, 1, 1, 0, 0, 0, [TimeSpan]::Zero)
            $entry.ExternalAttributes = [int]([Convert]::ToInt32($member.mode, 8) -shl 16)
            $inputStream = [IO.File]::OpenRead($member.path); $outputStream = $entry.Open()
            try { $inputStream.CopyTo($outputStream) } finally { $outputStream.Dispose(); $inputStream.Dispose() }
        }
    } finally { $zip.Dispose() }
    $zip = [IO.Compression.ZipFile]::OpenRead($Archive)
    try {
        if ($zip.Entries.Count -ne $members.Count) { throw 'Source ZIP member count mismatch.' }
        $seen = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
        foreach ($entry in $zip.Entries) {
            if (-not $seen.Add($entry.FullName) -or -not $members.Contains($entry.FullName)) { throw 'Unexpected/duplicate source ZIP member.' }
            $stream = $entry.Open()
            try { $hash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($stream)).ToLowerInvariant() } finally { $stream.Dispose() }
            if ($hash -cne $members[$entry.FullName].sha256) { throw "Source ZIP bytes differ: $($entry.FullName)" }
        }
    } finally { $zip.Dispose() }
}

function New-OrcaSourceSnapshot($Plan, [string]$Directory, [switch]$Zip) {
    $output = New-OrcaOutputDirectory $Plan.root $Directory
    $reportPath = Join-Path $output 'selection-report.json'
    Write-OrcaJson $reportPath ([ordered]@{schemaVersion=1; allowed=$Plan.allowed; git=$Plan.git; excluded=$Plan.excluded; unexpected=$Plan.unexpected; errors=$Plan.errors})
    if (-not $Plan.allowed) { throw "Source selection blocked; review $reportPath. No source copied." }
    $source = Join-Path $output 'source'
    Copy-OrcaVerifiedFiles $Plan.root $source $Plan.files
    $after = Get-OrcaSourcePlan $Plan.root $Plan.allowlistPath
    if (-not $after.allowed -or $after.selectionSha256 -cne $Plan.selectionSha256) { throw 'Working tree/index/allowlist changed during snapshot. Partial evidence retained; not deliverable.' }
    $manifestPath = Join-Path $output 'manifest.json'
    Write-OrcaJson $manifestPath ([ordered]@{schemaVersion=1; kind='orca-working-tree-source'; createdUtc=[DateTime]::UtcNow.ToString('o'); git=$Plan.git; allowlistSha256=$Plan.allowlistSha256; selectionSha256=$Plan.selectionSha256; files=$Plan.files})
    $checksumsPath = Join-Path $output 'SHA256SUMS.txt'
    $lines = @($Plan.files | ForEach-Object { "$($_.sha256) *source/$($_.path)" }) + @("$(Get-OrcaHash $manifestPath) *manifest.json", "$(Get-OrcaHash $reportPath) *selection-report.json")
    [IO.File]::WriteAllText($checksumsPath, (($lines -join "`n") + "`n"), [Text.UTF8Encoding]::new($false))
    $archive = $null
    if ($Zip) {
        $archive = Join-Path $output 'source.zip'
        Write-OrcaSourceZip $archive $source $Plan.files @{'manifest.json'=$manifestPath; 'selection-report.json'=$reportPath; 'SHA256SUMS.txt'=$checksumsPath}
        [IO.File]::WriteAllText((Join-Path $output 'source.zip.sha256'), "$(Get-OrcaHash $archive) *source.zip`n", [Text.UTF8Encoding]::new($false))
    }
    return [pscustomobject]@{directory=$output; source=$source; manifest=$manifestPath; manifestSha256=(Get-OrcaHash $manifestPath); files=$Plan.files; archive=$archive}
}

function Get-OrcaFrontendPlan([string]$Directory) {
    $root = Assert-OrcaPlainPath $Directory
    if (-not [IO.Directory]::Exists($root)) { throw 'Frontend dist directory is missing.' }
    $files = [Collections.Generic.List[object]]::new()
    $pending = [Collections.Generic.Stack[string]]::new(); $pending.Push($root)
    while ($pending.Count) {
        $dir = $pending.Pop()
        foreach ($entry in [IO.Directory]::EnumerateFileSystemEntries($dir)) {
            [void](Assert-OrcaPlainPath $entry)
            $relative = [IO.Path]::GetRelativePath($root, $entry).Replace('\', '/')
            Assert-OrcaRelativePath $relative
            if ([IO.Directory]::Exists($entry)) {
                if ($relative -ne 'assets') { throw "Unexpected frontend directory: $relative" }
                $pending.Push($entry); continue
            }
            if ($relative -eq '.gitkeep') { continue }
            if ($relative -cne 'index.html' -and $relative -cnotmatch '^assets/[A-Za-z0-9_.-]+\.(js|css|png|jpg|jpeg|gif|ico|woff|woff2|ttf|otf|svg)$') { throw "Unexpected frontend build file: $relative" }
            Assert-OrcaSourceContent $entry $relative $false
            $files.Add([ordered]@{path=$relative; sha256=(Get-OrcaHash $entry); bytes=([IO.FileInfo]$entry).Length})
        }
    }
    $names = [Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
    foreach ($file in $files) { if (-not $names.Add($file.path)) { throw 'Duplicate frontend file.' } }
    if (-not $names.Contains('index.html')) { throw 'Frontend index.html is missing.' }
    $html = [IO.File]::ReadAllText((Join-Path $root 'index.html'))
    $references = [regex]::Matches($html, '(?i)\b(?:src|href)\s*=\s*["'']([^"'']+)["'']')
    $scripts = 0; $styles = 0
    foreach ($reference in $references) {
        $value = $reference.Groups[1].Value
        if (-not $value.StartsWith('./assets/', [StringComparison]::Ordinal) -or -not $names.Contains($value.Substring(2))) { throw "Nonlocal/missing frontend entry reference: $value" }
        if ($value.EndsWith('.js')) { $scripts++ }; if ($value.EndsWith('.css')) { $styles++ }
    }
    if (-not $scripts -or -not $styles -or $html -match '(?is)<script\b(?![^>]*\bsrc\s*=)[^>]*>') { throw 'Frontend entry needs local scripts/styles and no inline scripts.' }
    foreach ($file in $files | Where-Object { $_.path.EndsWith('.css') }) {
        $css = [IO.File]::ReadAllText((Join-Path $root $file.path))
        foreach ($match in [regex]::Matches($css, 'url\(\s*["'']?([^\s)"'']+)["'']?\s*\)')) {
            $url = $match.Groups[1].Value
            if ($url.StartsWith('data:', [StringComparison]::Ordinal)) { continue }
            $target = 'assets/' + $url.TrimStart('./')
            if ($url.Contains('..') -or $url.Contains(':') -or $url.StartsWith('/') -or -not $names.Contains($target)) { throw "Nonlocal/missing frontend CSS asset: $url" }
        }
    }
    return [pscustomobject]@{root=$root; files=@($files | Sort-Object { $_.path } -CaseSensitive)}
}

function Copy-OrcaFrontendBuild($Plan, [string]$Destination, [string]$ManifestPath, [string]$SourceManifestHash) {
    if ($SourceManifestHash -cnotmatch '^[0-9a-f]{64}$') { throw 'Source manifest SHA256 is required for build provenance.' }
    Copy-OrcaVerifiedFiles $Plan.root $Destination $Plan.files
    $after = Get-OrcaFrontendPlan $Plan.root
    if (($after.files | ConvertTo-Json -Compress) -cne ($Plan.files | ConvertTo-Json -Compress)) { throw 'Frontend build changed during copy.' }
    Write-OrcaJson $ManifestPath ([ordered]@{schemaVersion=1; kind='build-only-frontend-input'; sourceManifestSha256=$SourceManifestHash; note='Validated existing build input, not proof of a reproducible build from this source snapshot.'; files=$Plan.files})
}

if ($MyInvocation.InvocationName -ne '.') {
    if (-not $OutputDirectory) { throw 'Specify a fresh absolute -OutputDirectory for the audit/snapshot.' }
    if ($SourceZip -and -not $CreateSnapshot) { throw '-SourceZip requires -Snapshot.' }
    $plan = Get-OrcaSourcePlan $RepositoryRoot $AllowlistPath
    if ($CreateSnapshot) { New-OrcaSourceSnapshot $plan $OutputDirectory -Zip:$SourceZip }
    else {
        $output = New-OrcaOutputDirectory $plan.root $OutputDirectory
        $report = Join-Path $output 'selection-report.json'
        Write-OrcaSelectionAudit $plan $output
        Write-Output $report
        if (-not $plan.allowed) { throw 'Audit found unreviewed/unsafe source inputs. Nothing copied; report is not an approval allowlist.' }
    }
}
