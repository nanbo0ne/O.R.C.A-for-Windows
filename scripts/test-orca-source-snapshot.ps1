#requires -Version 7.0
[CmdletBinding()]
param([string]$FixtureRoot)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'orca-source-snapshot.ps1')
if (-not $FixtureRoot) { $FixtureRoot = Join-Path (Join-Path $PSScriptRoot '../.tmp') ('source-snapshot-tests-' + [guid]::NewGuid().ToString('N')) }
$FixtureRoot = New-OrcaOutputDirectory ([IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))) ([IO.Path]::GetFullPath($FixtureRoot))
$script:checks = 0
function Check([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
    $script:checks++
}
function Reject([scriptblock]$Action, [string]$Message) {
    $rejected = $false
    try { & $Action | Out-Null } catch { $rejected = $true }
    Check $rejected $Message
}
function Put([string]$Root, [string]$Path, [string]$Value) {
    $target = Join-Path $Root $Path
    [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($target))
    [IO.File]::WriteAllText($target, $Value, [Text.UTF8Encoding]::new($false))
}
function Approval([string]$Path, [string]$Head, [object[]]$Files) {
    Write-OrcaJson $Path @{schemaVersion=1; baseCommit=$Head; files=$Files}
}
try {
    $repo = Join-Path $FixtureRoot 'repo'
    $empty = Join-Path $FixtureRoot 'empty-git-template'
    [void][IO.Directory]::CreateDirectory($repo); [void][IO.Directory]::CreateDirectory($empty)
    # Only this disposable fixture receives Git writes; no shared checkout index is touched.
    $null = Invoke-OrcaGit $repo @('-c', "init.templateDir=$empty", 'init', '--initial-branch=fixture')
    $tracked = @('.gitignore', 'source.go', 'LICENSE', 'THIRD-PARTY-NOTICES.txt', '.env', 'secrets/private.json', 'release/old-installer.exe', 'desktop/frontend/dist/index.html', 'desktop/build/windows/installer/resources/eula.txt', 'desktop/build/windows/info.json', 'desktop/build/appicon.png', 'docs/legacy-image.png', 'internal/artifact/testdata/legacy.pdf', 'cmd/orca/main.go', 'npm/orca-agent/bin/orca.js', 'docs/releases/desktop-v3.0.13.md')
    foreach ($path in $tracked) { Put $repo $path "synthetic baseline: $path" }
    [IO.File]::WriteAllBytes((Join-Path $repo 'desktop/build/appicon.png'), [Convert]::FromHexString('89504E470D0A1A0A00000000'))
    [IO.File]::WriteAllBytes((Join-Path $repo 'docs/legacy-image.png'), [Convert]::FromHexString('FFD8FFE000104A4649460001FFD9'))
    Put $repo 'internal/artifact/testdata/legacy.pdf' "%PDF-1.4`nsynthetic fixture`n%%EOF"
    Put $repo '.gitignore' "/.tmp/`n/ignored/`n"
    $null = Invoke-OrcaGit $repo (@('add', '--') + $tracked)
    $null = Invoke-OrcaGit $repo @('-c', 'user.name=Synthetic Fixture', '-c', 'user.email=fixture@example.invalid', '-c', 'commit.gpgsign=false', '-c', "core.hooksPath=$empty", 'commit', '-m', 'synthetic fixture only')
    $head = (Invoke-OrcaGit $repo @('rev-parse', 'HEAD')).Trim()
    Put $repo 'source.go' 'synthetic uncommitted working bytes'
    Put $repo 'new-source.go' 'synthetic reviewed new source'
    Put $repo 'ignored/local.txt' 'synthetic ignored sentinel'
    Put $repo '.env.production' 'synthetic secret sentinel'
    Put $repo '.npmrc' 'synthetic credential config sentinel'
    Put $repo '.docker/config.json' 'synthetic credential profile sentinel'
    $plan = Get-OrcaSourcePlan $repo ''
    Check (-not $plan.allowed -and $plan.unexpected.Count -eq 1 -and $plan.unexpected[0].path -eq 'new-source.go') 'Unreviewed new source was not blocked.'
    Check ($plan.git.head -ceq $head -and $plan.git.dirty) 'Dirty-tree provenance is missing.'
    $audit = New-OrcaOutputDirectory $repo (Join-Path $repo '.tmp/audit')
    Write-OrcaSelectionAudit $plan $audit
    $candidateDoc = Get-Content -LiteralPath (Join-Path $audit 'new-source-candidates.json') -Raw | ConvertFrom-Json
    Check (-not $candidateDoc.approved -and $candidateDoc.candidates.Count -eq 1) 'Audit approved unknown files or omitted candidates.'
    Reject { Get-OrcaSourcePlan $repo (Join-Path $audit 'new-source-candidates.json') } 'Unapproved candidate document was accepted as an allowlist.'
    $inventory = Get-Content -LiteralPath (Join-Path $audit 'source-inventory.json') -Raw | ConvertFrom-Json
    Check ($inventory.groups.Count -ge 4 -and @($inventory.groups | Where-Object { -not $_.group }).Count -eq 0) 'Ownership groups are empty or collapsed.'
    $rootGroup = $inventory.groups | Where-Object group -EQ 'root-and-other'
    Check (@($rootGroup.counts | Where-Object { $_.state -eq 'unreviewed' -and $_.count -eq 1 }).Count -eq 1) 'Grouped counts do not match candidate inventory.'
    Check (@($inventory.groups.entries | Where-Object { $_.path -eq 'source.go' -and $_.state -eq 'modified-tracked' }).Count -eq 1) 'Grouped inventory lost dirty tracked status.'
    Check ($plan.resources.trackedFixtures.Count -eq 1 -and $plan.resources.trackedFixtures[0].state -eq 'selected') 'Binary fixture closure missing.'
    foreach ($sum in [IO.File]::ReadAllLines((Join-Path $audit 'AUDIT-SHA256SUMS.txt'))) {
        $parts = $sum.Split(' *', 2)
        Check ((Get-OrcaHash (Join-Path $audit $parts[1])) -ceq $parts[0]) 'Audit hash mismatch.'
    }
    foreach ($path in @('.env', '.env.production', '.npmrc', '.docker/config.json', 'secrets/private.json', 'release/old-installer.exe', 'desktop/frontend/dist/index.html', 'ignored/')) {
        Check (@($plan.excluded | Where-Object path -CEQ $path).Count -eq 1) "Missing exclusion report for $path"
    }
    $blocked = Join-Path $repo '.tmp/blocked'
    Reject { New-OrcaSourceSnapshot $plan $blocked -Zip } 'Unreviewed snapshot succeeded.'
    Check ((Test-Path -LiteralPath (Join-Path $blocked 'selection-report.json')) -and -not (Test-Path -LiteralPath (Join-Path $blocked 'source'))) 'Blocked selection copied source or lost its report.'
    $approval = Join-Path $FixtureRoot 'approved.json'
    $newFile = @{path='new-source.go'; sha256=(Get-OrcaHash (Join-Path $repo 'new-source.go'))}
    Approval $approval $head @($newFile)
    Put $repo 'source.go' "package fixture`n//go:embed desktop/build/appicon.png`nvar icon []byte"
    $embedPlan = Get-OrcaSourcePlan $repo $approval
    Check ($embedPlan.allowed -and $embedPlan.resources.embeds[0].state -eq 'selected') 'Required tracked embed was not retained.'
    Put $repo 'source.go' "package fixture`n//go:embed .env`nvar secret []byte"
    Check (-not (Get-OrcaSourcePlan $repo $approval).allowed) 'Excluded runtime embed was silently omitted.'
    Put $repo 'source.go' "package fixture`n//go:embed assets/*`nvar assets embed.FS"
    Check (-not (Get-OrcaSourcePlan $repo $approval).allowed) 'Unsupported embed pattern did not fail closed.'
    Put $repo 'source.go' 'synthetic uncommitted working bytes'
    $fixtureAudit = Get-OrcaResourceAudit $repo @() @() @(@{path='testdata/example.zip'; tracked=$true; reason='compiled-archive-or-log'})
    Check ($fixtureAudit.errors.Count -eq 1) 'Excluded tracked test fixture was not flagged.'
    $plan = Get-OrcaSourcePlan $repo $approval
    Check $plan.allowed 'Exact approved new source was rejected.'
    $snapshot = New-OrcaSourceSnapshot $plan (Join-Path $repo '.tmp/snapshot') -Zip
    foreach ($file in $snapshot.files) { Check ((Get-OrcaHash (Join-Path $snapshot.source $file.path)) -ceq $file.sha256) "Wrong snapshot bytes: $($file.path)" }
    foreach ($path in @('LICENSE', 'THIRD-PARTY-NOTICES.txt', 'desktop/build/windows/installer/resources/eula.txt', 'desktop/build/windows/info.json', 'desktop/build/appicon.png', 'docs/legacy-image.png', 'internal/artifact/testdata/legacy.pdf', 'cmd/orca/main.go', 'npm/orca-agent/bin/orca.js', 'docs/releases/desktop-v3.0.13.md')) {
        Check ((Get-OrcaHash (Join-Path $repo $path)) -ceq (Get-OrcaHash (Join-Path $snapshot.source $path))) "Baseline license/resource changed: $path"
    }
    Check ([IO.File]::ReadAllText((Join-Path $snapshot.source 'source.go')) -ceq 'synthetic uncommitted working bytes') 'Snapshot used HEAD instead of working bytes.'
    $manifest = Get-Content -LiteralPath $snapshot.manifest -Raw | ConvertFrom-Json
    Check ($manifest.git.head -ceq $head -and $manifest.allowlistSha256 -ceq (Get-OrcaHash $approval) -and $snapshot.manifestSha256 -ceq (Get-OrcaHash $snapshot.manifest)) 'Manifest provenance/hash mismatch.'
    $cli = & (Join-Path $PSScriptRoot 'orca-source-snapshot.ps1') -RepositoryRoot $repo -AllowlistPath $approval -OutputDirectory (Join-Path $repo '.tmp/cli-snapshot') -Snapshot -SourceZip
    Check ((Test-Path -LiteralPath $cli.archive) -and $cli.files.Count -eq $snapshot.files.Count) 'Source-only CLI failed.'
    $zip = [IO.Compression.ZipFile]::OpenRead($snapshot.archive)
    try {
        Check ($zip.Entries.Count -eq $snapshot.files.Count + 3) 'Unexpected source ZIP members.'
        Check (@($zip.Entries | Where-Object FullName -Match '(dist/|\.env|private\.json|old-installer|ignored/)').Count -eq 0) 'Excluded file leaked into source ZIP.'
        Check ($null -ne $zip.GetEntry('source/desktop/build/windows/installer/resources/eula.txt')) 'Baseline installer source resource missing.'
    } finally { $zip.Dispose() }
    $before = Get-OrcaHash $snapshot.archive
    Reject { New-OrcaSourceSnapshot $plan $snapshot.directory -Zip } 'Existing snapshot was overwritten.'
    Check ((Get-OrcaHash $snapshot.archive) -ceq $before) 'Existing ZIP changed.'
    Reject { New-OrcaOutputDirectory $repo (Join-Path $repo 'ordinary-output') } 'Nonexcluded output inside source tree was allowed.'
    Put $repo 'new-source.go' 'changed after approval'
    Check (-not (Get-OrcaSourcePlan $repo $approval).allowed) 'Changed approved hash was accepted.'
    Put $repo 'new-source.go' 'synthetic reviewed new source'
    $stale = Get-OrcaSourcePlan $repo $approval
    Put $repo 'source.go' 'changed after plan'
    Reject { New-OrcaSourceSnapshot $stale (Join-Path $repo '.tmp/stale') -Zip } 'Stale tracked file plan was accepted.'
    Put $repo 'source.go' 'synthetic uncommitted working bytes'
    Approval $approval ('0' * 40) @($newFile)
    Reject { Get-OrcaSourcePlan $repo $approval } 'Wrong base commit was accepted.'
    Approval $approval $head @($newFile, $newFile)
    Reject { Get-OrcaSourcePlan $repo $approval } 'Duplicate approval was accepted.'
    Approval $approval $head @(@{path='../escape.go'; sha256=('0' * 64)})
    Reject { Get-OrcaSourcePlan $repo $approval } 'Traversal approval was accepted.'
    Approval $approval $head @($newFile, @{path='.env'; sha256=(Get-OrcaHash (Join-Path $repo '.env'))})
    Check (-not (Get-OrcaSourcePlan $repo $approval).allowed) 'An allowlist overrode secret exclusions.'
    Approval $approval $head @($newFile)
    Put $repo 'handoff-note.md' 'reviewed but not delivered'
    $note = @{path='handoff-note.md'; sha256=(Get-OrcaHash (Join-Path $repo 'handoff-note.md'))}
    Write-OrcaJson $approval @{schemaVersion=1; baseCommit=$head; files=@($newFile); exclude=@($note)}
    $excludedPlan = Get-OrcaSourcePlan $repo $approval
    Check ($excludedPlan.allowed -and @($excludedPlan.files | Where-Object path -EQ $note.path).Count -eq 0) 'Reviewed non-source exclusion was copied or rejected.'
    Check (@($excludedPlan.excluded | Where-Object reason -EQ 'reviewed-new-file-exclusion').Count -eq 1) 'Reviewed exclusion is absent from report.'
    Put $repo 'handoff-note.md' 'changed after review'
    Check (-not (Get-OrcaSourcePlan $repo $approval).allowed) 'Changed reviewed exclusion was silently ignored.'
    [IO.File]::Move((Join-Path $repo 'handoff-note.md'), (Join-Path $repo '.tmp/handoff-note.md'))
    Approval $approval $head @($newFile)
    Put $repo 'ORCA beta/session.json' 'synthetic profile sentinel'
    Check (@((Get-OrcaSourcePlan $repo $approval).excluded | Where-Object path -EQ 'ORCA beta/session.json').Count -eq 1) 'ORCA profile was not excluded.'
    Put $repo 'disguised.go' "MZ`0synthetic binary"
    Check (-not (Get-OrcaSourcePlan $repo $approval).allowed) 'Disguised binary was accepted.'
    # Keep failed fixtures, but move this input to an already-excluded directory for later cases.
    [IO.File]::Move((Join-Path $repo 'disguised.go'), (Join-Path $repo '.tmp/disguised.go'))
    $outside = Join-Path $FixtureRoot 'outside'
    Put $outside 'private.txt' 'synthetic outside sentinel'
    if ($IsWindows) {
        $link = Join-Path $repo 'redirected'
        $null = New-Item -ItemType Junction -Path $link -Target $outside
        Check (-not (Get-OrcaSourcePlan $repo $approval).allowed) 'Reparse-point input was accepted.'
        Reject { Copy-OrcaVerifiedFiles $repo (Join-Path $link 'output') $plan.files } 'Reparse-point output was accepted.'
        # Retain both the fixture junction and target; no recursive cleanup/move.
        Check ([IO.File]::ReadAllText((Join-Path $outside 'private.txt')) -ceq 'synthetic outside sentinel') 'Outside target was modified.'
    }
    $dist = Join-Path $FixtureRoot 'dist-input'
    Put $dist 'index.html' '<html><script src="./assets/app.js"></script><link rel="stylesheet" href="./assets/app.css"></html>'
    Put $dist 'assets/app.js' 'console.log("synthetic only")'
    Put $dist 'assets/app.css' 'body { color: black; }'
    Put $dist '.gitkeep' ''
    $frontend = Get-OrcaFrontendPlan $dist
    Check ($frontend.files.Count -eq 3) 'Frontend build-only allowlist expanded.'
    $frontendOutput = Join-Path $FixtureRoot 'build-only-dist'
    $frontendManifest = Join-Path $FixtureRoot 'frontend.json'
    Copy-OrcaFrontendBuild $frontend $frontendOutput $frontendManifest $snapshot.manifestSha256
    Check ((Get-OrcaHash (Join-Path $frontendOutput 'assets/app.js')) -ceq (Get-OrcaHash (Join-Path $dist 'assets/app.js'))) 'Frontend copy differs.'
    Check (-not (Test-Path -LiteralPath (Join-Path $snapshot.source 'desktop/frontend/dist'))) 'Build-only dist entered clean source snapshot.'
    Put $dist '.env' 'synthetic secret'
    Reject { Get-OrcaFrontendPlan $dist } 'Private frontend file was accepted.'
    [IO.File]::Move((Join-Path $dist '.env'), (Join-Path $FixtureRoot 'excluded-dist-env'))
    Put $dist 'index.html' '<script src="https://example.invalid/app.js"></script><link href="./assets/app.css">'
    Reject { Get-OrcaFrontendPlan $dist } 'External frontend reference was accepted.'
    Put $dist 'index.html' '<script src="./assets/missing.js"></script><link href="./assets/app.css">'
    Reject { Get-OrcaFrontendPlan $dist } 'Missing frontend asset was accepted.'
    Put $dist 'index.html' '<script>alert(1)</script><link href="./assets/app.js"><link href="./assets/app.css">'
    Reject { Get-OrcaFrontendPlan $dist } 'Inline frontend script was accepted.'
    Put $dist 'index.html' '<script src="./assets/app.js"></script><link href="./assets/app.css">'
    Put $dist 'assets/app.css' 'body { background: url(../../private.png); }'
    Reject { Get-OrcaFrontendPlan $dist } 'Escaping frontend CSS reference was accepted.'
    Put $dist 'assets/app.css' 'body { color: black; }'
    Put $dist 'assets/app.js' "MZ`0synthetic executable"
    Reject { Get-OrcaFrontendPlan $dist } 'Executable disguised as JavaScript was accepted.'
    if ($IsWindows) {
        Put $dist 'assets/app.js' 'synthetic'
        $null = New-Item -ItemType Junction -Path (Join-Path $dist 'redirected') -Target $outside
        Reject { Get-OrcaFrontendPlan $dist } 'Frontend reparse directory was accepted.'
    }
    $buildScript = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'build-orca-beta.ps1'))
    Check ($buildScript -notmatch '(?i)robocopy|Copy-Item[^\r\n]*-Recurse') 'Broad build staging copy remains.'
    Check ($buildScript.Contains('New-OrcaSourceSnapshot $sourcePlan') -and $buildScript.Contains('Build-OrcaSnapshotFrontend $snapshot')) 'Build staging bypasses source/frontend manifests.'
    Check ($buildScript -notmatch 'Copy-OrcaFrontendBuild|Get-OrcaFrontendPlan \(Join-Path \$desktop') 'Build still accepts a stale checkout bundle.'
    Check ($buildScript -notmatch '(?im)^\s*\$package\s*=') 'Package switch is overwritten by a case-insensitive artifact-path variable.'
    Reject { & (Join-Path $PSScriptRoot 'build-orca-beta.ps1') -Package } 'Package started without explicit integration gates.'
    Write-Output "PASS: $script:checks source snapshot/build safety checks. Fixtures retained: $FixtureRoot"
} finally { Write-Host "Synthetic fixtures retained: $FixtureRoot" }
