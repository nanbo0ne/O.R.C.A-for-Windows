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

// Exercise the real NSIS helper in a harmless probe, not the installer. Only
// this test's synthetic background process and temporary marker files are used.
func TestWindowsInstallerDetects64BitBackgroundProcess(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("requires Windows amd64")
	}
	makensis := os.Getenv("MAKENSIS")
	if makensis == "" {
		makensis = `C:\Program Files (x86)\NSIS\makensis.exe`
	}
	if _, err := os.Stat(makensis); err != nil {
		t.Skip("NSIS compiler unavailable")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target with spaces")
	other := filepath.Join(dir, "other install")
	for _, path := range []string{target, other} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	fixtureSource := filepath.Join(dir, "fixture.go")
	if err := os.WriteFile(fixtureSource, []byte("package main\nimport \"time\"\nfunc main(){for{time.Sleep(time.Hour)}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(target, "Orca.exe")
	build := exec.Command("go", "build", "-ldflags=-H=windowsgui", "-o", exe, fixtureSource)
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOARCH=amd64", "GOOS=windows", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, out)
	}
	body, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	otherExe := filepath.Join(other, "Orca.exe")
	if err := os.WriteFile(otherExe, body, 0700); err != nil {
		t.Fatal(err)
	}
	start := func(path string) *exec.Cmd {
		cmd := exec.Command(path)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		return cmd
	}
	owned := start(exe)
	unrelated := start(otherExe)
	time.Sleep(150 * time.Millisecond)
	source := readWindowsInstallerSource(t)
	startAt := strings.Index(source, "Function orca.closeTargetProcesses\n")
	if startAt < 0 {
		source = strings.ReplaceAll(source, "\r\n", "\n")
		startAt = strings.Index(source, "Function orca.closeTargetProcesses\n")
	}
	if startAt < 0 {
		t.Fatal("missing process guard")
	}
	end := strings.Index(source[startAt:], "FunctionEnd")
	if end < 0 {
		t.Fatal("unterminated process guard")
	}
	guard := source[startAt : startAt+end+len("FunctionEnd")]
	makeProbe := func(name, checkDir string, legacy bool) string {
		probe := filepath.Join(dir, name+".exe")
		selected := guard
		if legacy {
			selected = strings.ReplaceAll(selected, `StrCpy $2 "$WINDIR\Sysnative\WindowsPowerShell\v1.0\powershell.exe"`, `StrCpy $2 "$SYSDIR\WindowsPowerShell\v1.0\powershell.exe"`)
		}
		marker := filepath.Join(dir, name+".reached-write")
		nsi := fmt.Sprintf("Unicode true\nSilentInstall silent\nAutoCloseWindow true\nRequestExecutionLevel user\n!define INFO_PRODUCTNAME \"Installer regression fixture\"\nOutFile \"%s\"\n%s\nSection\nStrCpy $INSTDIR \"%s\"\nCall orca.closeTargetProcesses\nFileOpen $0 \"%s\" w\nFileWrite $0 \"guard passed\"\nFileClose $0\nSectionEnd\n", probe, selected, checkDir, marker)
		path := filepath.Join(dir, name+".nsi")
		if err := os.WriteFile(path, []byte(nsi), 0600); err != nil {
			t.Fatal(err)
		}
		compile := exec.Command(makensis, "/V1", "/INPUTCHARSET", "UTF8", path)
		if out, err := compile.CombinedOutput(); err != nil {
			t.Fatalf("compile probe: %v\n%s", err, out)
		}
		return probe
	}
	run := func(probe string) int {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, probe, "/S")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		out, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("probe timed out: %v\n%s", err, out)
		}
		if err == nil {
			return 0
		}
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		t.Fatalf("probe start: %v\n%s", err, out)
		return -1
	}
	if code := run(makeProbe("legacy", target, true)); code != 66 {
		t.Fatalf("unreadable 64-bit paths must fail closed even with redirected PowerShell, exit=%d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "legacy.reached-write")); !os.IsNotExist(err) {
		t.Fatal("unreadable process paths allowed writes")
	}
	if code := run(makeProbe("fixed", target, false)); code != 0 {
		t.Fatalf("upgrade must stop background target before writes, got exit=%d", code)
	}
	done := make(chan error, 1)
	go func() { done <- owned.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("guard passed without stopping target")
	}
	if _, err := os.Stat(filepath.Join(dir, "fixed.reached-write")); err != nil {
		t.Fatal("upgrade did not proceed after stopping target")
	}
	// A running process has the STILL_ACTIVE exit code; query the owned handle.
	var exitCode uint32
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(unrelated.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(handle)
	if err := syscall.GetExitCodeProcess(handle, &exitCode); err != nil || exitCode != 259 {
		t.Fatalf("unrelated process was affected: exit=%d err=%v", exitCode, err)
	}
	if code := run(makeProbe("different-folder", filepath.Join(dir, "empty target"), false)); code != 0 {
		t.Fatalf("unrelated same-name processes must not block install: %d", code)
	}
	// Retry after automatic shutdown must also pass.
	path, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := syscall.CreateFile(path, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer syscall.CloseHandle(lock)
		if code := run(makeProbe("locked", target, false)); code != 66 {
			t.Fatalf("external file lock must block writes: %d", code)
		}
		if _, err := os.Stat(filepath.Join(dir, "locked.reached-write")); !os.IsNotExist(err) {
			t.Fatal("locked executable allowed writes")
		}
	}()
	if code := run(makeProbe("retry", target, false)); code != 0 {
		t.Fatalf("retry after target exits failed: %d", code)
	}
}
