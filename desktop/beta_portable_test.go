package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/proc"
)

func TestBetaPortableArchiveSyntheticFixtures(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("PowerShell 7 is required for the beta build-script fixture")
	}
	script, err := filepath.Abs("../scripts/build-orca-beta.ps1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Extract helper definitions only. Never invoke the gated build/package body.
	cmd := exec.CommandContext(ctx, pwsh, "-NoProfile", "-NonInteractive", "-Command", betaPortableFixture)
	cmd.Env = append(os.Environ(), "ORCA_BETA_SCRIPT_FIXTURE="+script, "ORCA_BETA_ARCHIVE_FIXTURE="+t.TempDir())
	proc.HideWindow(cmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synthetic portable fixture: %v\n%s", err, output)
	}
}

const betaPortableFixture = `
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$tokens = $null
$parseErrors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile($env:ORCA_BETA_SCRIPT_FIXTURE, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count) { throw 'Build script does not parse.' }
$wanted = @('Get-BetaPortableFiles', 'Write-BetaPortableArchive', 'Assert-BetaPortableArchive', 'Assert-BetaExecutable')
$functions = $ast.FindAll({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -in $wanted }, $false)
if ($functions.Count -ne $wanted.Count) { throw 'Missing portable helper.' }
foreach ($function in $functions) { . ([scriptblock]::Create($function.Extent.Text)) }
function Must-Reject([scriptblock]$Action) {
    $rejected = $false
    try { & $Action | Out-Null } catch { $rejected = $true }
    if (-not $rejected) { throw 'Unsafe archive operation was accepted.' }
}
$root = $env:ORCA_BETA_ARCHIVE_FIXTURE
$payload = Join-Path $root 'payload'
$licenses = Join-Path $root 'licenses'
[void][IO.Directory]::CreateDirectory((Join-Path $payload 'codegraph/bin'))
[void][IO.Directory]::CreateDirectory($licenses)
$exe = Join-Path $root 'OrcaBeta.exe'
$version = '3.0.14-beta.2'
[IO.File]::WriteAllText($exe, 'synthetic; not an executable')
foreach ($name in @('node.exe', 'LICENSE.node.txt', 'codegraph/bin/codegraph.cmd', 'codegraph/LICENSE')) {
    [IO.File]::WriteAllText((Join-Path $payload $name), "synthetic $name")
}
foreach ($name in @('LICENSE', 'THIRD-PARTY-NOTICES.txt')) {
    [IO.File]::WriteAllText((Join-Path $licenses $name), "synthetic $name")
}
# Unrelated source/payload files must not enter the map or ZIP.
[IO.File]::WriteAllText((Join-Path $payload 'do-not-include.env'), 'synthetic secret sentinel')
[IO.File]::WriteAllText((Join-Path $licenses 'source.go'), 'not a license')
$files = Get-BetaPortableFiles $exe $payload $licenses
if ($files.Count -ne 7 -or $files.ContainsKey('source.go') -or $files.ContainsKey('do-not-include.env')) { throw 'Portable allowlist expanded.' }
$archive = Join-Path $root 'synthetic.zip'
$hashes = Write-BetaPortableArchive $archive $files
if ($hashes['OrcaBeta.exe'] -cne (Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash) { throw 'Executable bytes changed.' }
$before = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash
Must-Reject { Write-BetaPortableArchive $archive $files }
if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash -cne $before) { throw 'Existing archive overwritten.' }
foreach ($name in @('Orca.exe', 'uninstall-beta.exe', 'source.go', 'codegraph/../escape', '/absolute', 'codegraph/C:stream', 'codegraph//duplicate')) {
    $invalid = $files.Clone()
    $invalid[$name] = $exe
    Must-Reject { Write-BetaPortableArchive (Join-Path $root 'invalid.zip') $invalid }
    if (Test-Path -LiteralPath (Join-Path $root 'invalid.zip')) { throw 'Invalid input wrote an archive.' }
}
$missing = $files.Clone()
$missing.Remove('OrcaBeta.exe')
Must-Reject { Write-BetaPortableArchive (Join-Path $root 'missing.zip') $missing }
Must-Reject { Assert-BetaExecutable $exe }
Must-Reject { Assert-BetaExecutable (Join-Path $payload 'node.exe') }
# Same name/count but changed bytes must fail verification.
$zip = [IO.Compression.ZipFile]::Open($archive, [IO.Compression.ZipArchiveMode]::Update)
try {
    $zip.GetEntry('OrcaBeta.exe').Delete()
    $entry = $zip.CreateEntry('OrcaBeta.exe')
    $writer = [IO.StreamWriter]::new($entry.Open())
    try { $writer.Write('tampered synthetic executable') } finally { $writer.Dispose() }
} finally { $zip.Dispose() }
Must-Reject { Assert-BetaPortableArchive $archive $hashes }
# Duplicate member names must fail even with a trusted hash map.
$duplicate = Join-Path $root 'duplicate.zip'
$hashes = Write-BetaPortableArchive $duplicate $files
$zip = [IO.Compression.ZipFile]::Open($duplicate, [IO.Compression.ZipArchiveMode]::Update)
try { [void]$zip.CreateEntry('OrcaBeta.exe') } finally { $zip.Dispose() }
Must-Reject { Assert-BetaPortableArchive $duplicate $hashes }
`
