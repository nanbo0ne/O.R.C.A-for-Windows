package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Compile the production guard macro into a harmless probe. Its bootstrap only
// extracts temporary helpers; it never calls the installer's InitGuard, which
// creates logs under the real user profile. Process ownership is tested in
// internal/installguard; this test covers NSIS/helper protocol and write gating.
func TestWindowsInstallerNativeGuardProbe(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("requires Windows amd64")
	}
	makensis := windowsInstallerMakensis(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "target with spaces \u4e2d\u6587")
	marker := filepath.Join(dir, "reached-write.txt")
	status := filepath.Join(dir, "guard.ini")
	logPath := filepath.Join(dir, "guard.jsonl")
	manifest := filepath.Join(dir, "install-files.txt")
	const relativePayload = `codegraph\assets\guard-probe.dat`
	if err := os.WriteFile(manifest, []byte("Orca.exe\r\nnode.exe\r\n"+relativePayload+"\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	build := func(output, source string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags=-H=windowsgui", "-o", output, source)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOARCH=amd64", "GOOS=windows", "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build probe helper: %v\n%s", err, out)
		}
	}
	helper := filepath.Join(dir, "orca-install-guard.exe")
	build(helper, "./cmd/orca-install-guard")

	guardSource := readWindowsInstallerGuardSource(t)
	guard := windowsInstallerBlock(t, guardSource, "!macro orca.guard PREFIX", "!macroend")
	var variables []string
	for _, line := range strings.Split(guardSource, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "Var" {
			variables = append(variables, line)
		}
	}
	if len(variables) == 0 {
		t.Fatal("native guard variables are missing")
	}
	installerDir, err := filepath.Abs(filepath.Join("build", "windows", "installer"))
	if err != nil {
		t.Fatal(err)
	}
	makeProbe := func(name, helperPath string) string {
		t.Helper()
		probe := filepath.Join(dir, name+".exe")
		// Only replace compile-time helper and manifest sources. /NOCD keeps
		// the macro's remaining File inputs relative to the actual installer.
		selected := guard
		for source, replacement := range map[string]string{
			`"..\installer-go\orca-install-guard.exe"`: helperPath,
			`"..\installer-go\install-files.txt"`:      manifest,
		} {
			if strings.Count(selected, source) != 1 {
				t.Fatalf("expected one guard File source %s", source)
			}
			selected = strings.ReplaceAll(selected, source, `"`+windowsInstallerNSISPath(replacement)+`"`)
		}
		nsi := fmt.Sprintf(`Unicode true
SilentInstall silent
AutoCloseWindow true
RequestExecutionLevel user
OutFile "%s"
!include "LogicLib.nsh"
%s
%s
!insertmacro orca.guard ""

Function .onInit
    InitPluginsDir
    File /oname=$PLUGINSDIR\orca-install-guard.exe "%s"
    File /oname=$PLUGINSDIR\install-files.txt "%s"
    StrCpy $INSTDIR "%s"
    StrCpy $GuardStatus "%s"
    StrCpy $GuardLog "%s"
    System::Call 'kernel32::GetCurrentProcessId() i.s'
    Pop $GuardParent
FunctionEnd

Section
    Call orca.closeTargetProcesses
    FileOpen $0 "%s" w
    FileWrite $0 "guard passed"
    FileClose $0
SectionEnd
`, windowsInstallerNSISPath(probe), strings.Join(variables, "\n"), selected,
			windowsInstallerNSISPath(helperPath), windowsInstallerNSISPath(manifest),
			windowsInstallerNSISPath(target), windowsInstallerNSISPath(status),
			windowsInstallerNSISPath(logPath), windowsInstallerNSISPath(marker))
		path := filepath.Join(dir, name+".nsi")
		if err := os.WriteFile(path, []byte("\ufeff"+nsi), 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		compile := exec.CommandContext(ctx, makensis, "/V1", "/NOCD", "/INPUTCHARSET", "UTF8", path)
		compile.Dir = installerDir
		if out, err := compile.CombinedOutput(); err != nil {
			t.Fatalf("compile guard probe: %v\n%s", err, out)
		}
		return probe
	}
	probe := makeProbe("native-guard-probe", helper)
	run := func(t *testing.T, probe, invalidStatus string, want int) time.Duration {
		t.Helper()
		for _, path := range []string{marker, logPath, status + ".helper-ran"} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, probe, "/S")
		cmd.Env = append(os.Environ(), "ORCA_NSIS_TEST_STATUS="+invalidStatus)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		started := time.Now()
		out, err := cmd.CombinedOutput()
		elapsed := time.Since(started)
		if ctx.Err() != nil {
			t.Fatalf("probe timed out: %v\n%s", err, out)
		}
		code := 0
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else {
				t.Fatalf("start probe: %v\n%s", err, out)
			}
		}
		if code != want {
			log, logErr := os.ReadFile(logPath)
			t.Fatalf("guard exit=%d, want %d\n%s\nhelper log (read error=%v):\n%s", code, want, out, logErr, log)
		}
		body, err := os.ReadFile(marker)
		if want == 0 {
			if err != nil || string(body) != "guard passed" {
				t.Fatalf("successful guard did not reach the write marker: body=%q err=%v", body, err)
			}
		} else if !os.IsNotExist(err) {
			t.Fatalf("failed guard must not reach the write marker: body=%q err=%v", body, err)
		}
		t.Logf("silent guard exit=%d, elapsed=%s, write marker=%t", code, elapsed, want == 0)
		return elapsed
	}

	t.Run("no_process_proceeds_immediately", func(t *testing.T) {
		if elapsed := run(t, probe, "", 0); elapsed >= 4*time.Second {
			t.Fatalf("no-process path incurred a shutdown wait: %s", elapsed)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("preflight must not create the install target: %v", err)
		}
	})

	payload := filepath.Join(target, relativePayload)
	if err := os.MkdirAll(filepath.Dir(payload), 0700); err != nil {
		t.Fatal(err)
	}
	const original = "synthetic payload must remain unchanged"
	if err := os.WriteFile(payload, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	t.Run("external_file_lock_blocks_writes", func(t *testing.T) {
		path, err := syscall.UTF16PtrFromString(payload)
		if err != nil {
			t.Fatal(err)
		}
		lock, err := syscall.CreateFile(path, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer syscall.CloseHandle(lock)
		run(t, probe, "", 21)
	})
	t.Run("retry_after_unlock_proceeds", func(t *testing.T) {
		run(t, probe, "", 0)
		body, err := os.ReadFile(payload)
		if err != nil || string(body) != original {
			t.Fatalf("lock/retry changed the payload: body=%q err=%v", body, err)
		}
	})

	// A clean helper exit is insufficient without a matching final status.
	invalidSource := filepath.Join(dir, "invalid-helper.go")
	if err := os.WriteFile(invalidSource, []byte(`package main
import (
    "os"
    "strings"
)
func main() {
    result := os.Getenv("ORCA_NSIS_TEST_STATUS")
    for _, arg := range os.Args[1:] {
        if path, ok := strings.CutPrefix(arg, "--status="); ok {
            if err := os.WriteFile(path+".helper-ran", []byte("ok"), 0600); err != nil {
                os.Exit(91)
            }
            if result == "missing" { return }
            if err := os.WriteFile(path, []byte("[guard]\r\ncode="+result+"\r\n"), 0600); err != nil {
                os.Exit(91)
            }
        }
    }
}
`), 0600); err != nil {
		t.Fatal(err)
	}
	invalidHelper := filepath.Join(dir, "invalid-helper.exe")
	build(invalidHelper, invalidSource)
	invalidProbe := makeProbe("invalid-result-probe", invalidHelper)
	for _, tc := range []struct{ name, status string }{
		{"missing_result_fails_closed", "missing"},
		{"mismatched_result_fails_closed", "21"},
		{"malformed_result_fails_closed", "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run(t, invalidProbe, tc.status, 23)
			if body, err := os.ReadFile(status + ".helper-ran"); err != nil || string(body) != "ok" {
				t.Fatalf("invalid-result helper was not executed: body=%q err=%v", body, err)
			}
		})
	}
}

func TestWindowsInstallerCompilesWithMakensis(t *testing.T) {
	makensis := windowsInstallerMakensis(t)
	dir := t.TempDir()
	resultPath := filepath.Join(dir, "parameters.txt")
	probe := filepath.Join(dir, "parameters-probe.exe")
	script := fmt.Sprintf(`Unicode true
SilentInstall silent
AutoCloseWindow true
RequestExecutionLevel user
InstallDir "$TEMP\orca-nsis-sentinel"
OutFile "%s"
!include "FileFunc.nsh"
Function .onInit
    ${GetParameters} $0
    FileOpen $1 "%s" w
    FileWrite $1 "CMDLINE=$CMDLINE$\r$\n"
    FileWrite $1 "PARAMS=$0$\r$\n"
    FileWrite $1 "INSTDIR=$INSTDIR$\r$\n"
    FileClose $1
FunctionEnd
Section
SectionEnd
`, windowsInstallerNSISPath(probe), windowsInstallerNSISPath(resultPath))
	scriptPath := filepath.Join(dir, "parameters-probe.nsi")
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	compile := exec.CommandContext(ctx, makensis, "/V1", "/NOCD", "/INPUTCHARSET", "UTF8", scriptPath)
	compile.Dir = dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("synthetic makensis probe failed: %v\n%s", err, out)
	}
	explicit := filepath.Join(dir, "explicit target with spaces")
	cmd := exec.CommandContext(ctx, probe)
	// NSIS requires /D last and unquoted even when the directory has spaces.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CmdLine: syscall.EscapeArg(probe) + " /S /D=" + explicit}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("parameter probe failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("NSIS probe did not write %s: %v", resultPath, err)
	}
	got := string(raw)
	if strings.Contains(got, "/D=") {
		t.Fatalf("NSIS exposed /D in $CMDLINE or GetParameters: %q", got)
	}
	if !strings.Contains(got, "INSTDIR="+explicit+"\r\n") {
		t.Fatalf("NSIS did not preserve explicit /D path: %q", got)
	}
	if _, err := os.Stat(explicit); !os.IsNotExist(err) {
		t.Fatalf("parameter probe must not create the target: %v", err)
	}
}
