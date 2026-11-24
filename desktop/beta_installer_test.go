package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/proc"
)

func TestBetaInstallerIdentityAndNonTerminatingContract(t *testing.T) {
	if betaVersion != "3.0.14-beta.2" {
		t.Fatalf("unexpected beta version: %s", betaVersion)
	}
	raw, err := os.ReadFile("build/windows/installer/beta.nsi")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, want := range []string{
		`!define UNINST_KEY_NAME "ORCA beta"`, `!define PRODUCT_EXECUTABLE "OrcaBeta.exe"`,
		`InstallDir "$LOCALAPPDATA\Programs\ORCA beta"`, `WriteUninstaller "$INSTDIR\uninstall-beta.exe"`,
		`WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "3.0.14-beta.2"`,
		`VIProductVersion "3.0.14.2"`, `VIFileVersion "3.0.14.2"`,
		`VIAddVersionKey "ProductVersion" "3.0.14-beta.2"`, `VIAddVersionKey "FileVersion" "3.0.14.2"`,
		`Caption "ORCA beta 3.0.14-beta.2"`, `OutFile "..\..\bin\ORCA-beta-3.0.14-beta.2-windows-${ARCH}-installer.exe"`,
		`Call CheckBetaTarget`, `Call un.CheckBetaTarget`, `"$INSTDIR\.orca-beta-install"`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("missing installer isolation: %s", want)
		}
	}
	previous := -1
	for _, page := range []string{"!insertmacro MUI_PAGE_WELCOME", "!insertmacro MUI_PAGE_LICENSE", "!insertmacro MUI_PAGE_DIRECTORY", "Page custom InstallOptionsPage InstallOptionsPageLeave", "!insertmacro MUI_PAGE_INSTFILES", "!insertmacro MUI_PAGE_FINISH"} {
		at := strings.Index(source, page)
		if at <= previous {
			t.Fatalf("standard wizard missing/reordered: %s", page)
		}
		previous = at
	}
	for _, forbidden := range []string{"installer_guard.nsh", "taskkill", "TerminateProcess", "RequestShutdown", "LEGACY_UNINST_KEY", "associateCustomProtocols", "associateFiles", `RMDir /r "$AppData`, `RMDir /r "$Profile`, `RMDir /r "$INSTDIR"`} {
		if strings.Contains(source, forbidden) {
			t.Errorf("unsafe beta installer action: %s", forbidden)
		}
	}
	guard, err := os.ReadFile("build/windows/installer/beta_guard/main_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"TerminateProcess", "RequestShutdown", "os.Remove", "os.WriteFile", "exec.Command", "OpenProcess"} {
		if strings.Contains(string(guard), forbidden) {
			t.Errorf("beta guard may only inspect: %s", forbidden)
		}
	}
}

func TestBetaBuildScriptStagesIdentityAndRequiresGates(t *testing.T) {
	raw, err := os.ReadFile("../scripts/build-orca-beta.ps1")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, want := range []string{
		"[switch]$Package", "[switch]$IntegrationGatesPassed", "if (-not $Package -and -not $PrepareCandidate)", "if (-not $IntegrationGatesPassed)",
		"$version = '3.0.14-beta.2'", "-X main.channel=beta", "'-skipbindings'", "'-nosyncgomod'",
		"$details.FileVersion -ne '3.0.14.2'", "$info.fixed.file_version = '3.0.14.2'", "$info.fixed.product_version = '3.0.14.2'", "$language.FileVersion = '3.0.14.2'",
		"$stagedDesktop", "Assert-Payload", "'beta.nsi'", "'OrcaBeta.exe'",
		"Get-BetaPortableFiles $app $stagedPayload $source", "Write-BetaPortableArchive $portable $portableFiles",
		"Assert-BetaExecutable $app", "$portableHashes['OrcaBeta.exe'] -cne $appHash", "'SHA256SUMS.txt'",
		"Output must not modify the read-only payload/archive input directories.",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("missing packaging gate/identity: %s", want)
		}
	}
	for _, forbidden := range []string{"Start-Process", "SetEnvironmentVariable($key, $savedEnvironment[$key], 'User')", "build-installer-preview.ps1", "desktop-build.sh"} {
		if strings.Contains(source, forbidden) {
			t.Errorf("unexpected side effect: %s", forbidden)
		}
	}
}

func TestBetaBuildModeBranchesKeepPackageGate(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("beta build entry point requires Windows")
	}
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("PowerShell 7 is required")
	}
	source, err := os.ReadFile("../scripts/build-orca-beta.ps1")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	scripts := filepath.Join(root, "scripts")
	if err := os.Mkdir(scripts, 0o700); err != nil {
		t.Fatal(err)
	}
	const selectionReached = "SYNTHETIC_SOURCE_SELECTION_REACHED_NO_BUILD"
	// Execute the real entry-point branches, but stop before source selection or
	// tool discovery. Even an accidentally bypassed gate cannot build artifacts.
	fixtures := map[string][]byte{
		"build-orca-beta.ps1": source,
		"orca-source-snapshot.ps1": []byte("param([string]$OutputDirectory)\n" +
			"function Get-OrcaSourcePlan { throw '" + selectionReached + "' }\n"),
		"orca-frontend-snapshot.ps1": []byte("# Synthetic helper: no frontend commands.\n"),
		"orca-native-build.ps1":      []byte("# Synthetic helper: no native commands.\n"),
	}
	for name, data := range fixtures {
		if err := os.WriteFile(filepath.Join(scripts, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"package-without-gate", []string{"-Package"}, "Packaging requires explicit -IntegrationGatesPassed after parent integration gates."},
		{"package-false-gate", []string{"-Package", "-IntegrationGatesPassed:$false"}, "Packaging requires explicit -IntegrationGatesPassed after parent integration gates."},
		{"package-candidate-conflict", []string{"-Package", "-PrepareCandidate"}, "Choose exactly one build mode."},
		{"package-frontend-conflict", []string{"-Package", "-PrepareFrontend"}, "Choose exactly one build mode."},
		{"candidate-cannot-assert-acceptance", []string{"-PrepareCandidate", "-IntegrationGatesPassed"}, "Candidate mode cannot assert completed acceptance."},
		{"candidate-without-release-gate-stops-at-selection", []string{"-PrepareCandidate"}, selectionReached},
		{"package-explicit-gate-stops-at-selection", []string{"-Package", "-IntegrationGatesPassed"}, selectionReached},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			args := []string{"-NoProfile", "-NonInteractive", "-File", filepath.Join(scripts, "build-orca-beta.ps1")}
			args = append(args, tc.args...)
			args = append(args, "-OutputDirectory", filepath.Join(root, ".tmp", "synthetic-output"))
			cmd := exec.CommandContext(ctx, pwsh, args...)
			cmd.Dir = root
			proc.HideWindow(cmd)
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("branch check timed out: %v\n%s", ctx.Err(), output)
			}
			if err == nil || !strings.Contains(string(output), tc.want) {
				t.Fatalf("wrong gate branch: %v\n%s", err, output)
			}
			if tc.want != selectionReached && strings.Contains(string(output), selectionReached) {
				t.Fatal("rejected build mode reached source selection")
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "scripts" {
				t.Fatal("branch check created staging or artifact output")
			}
		})
	}
}
