package sandbox

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/proc"
)

func TestWindowsBashCandidatesUnique(t *testing.T) {
	t.Setenv("ProgramFiles", `C:\synthetic`)
	t.Setenv("ProgramW6432", `c:\SYNTHETIC`)
	t.Setenv("ProgramFiles(x86)", `C:\synthetic`)
	t.Setenv("LOCALAPPDATA", "")
	if got := windowsBashCandidates(); len(got) != 2 {
		t.Fatalf("duplicate roots should not repeat slow probes: %v", got)
	}
}

func TestResolveShellSkipsWindowsAppsBeforeProbe(t *testing.T) {
	const alias = `C:\synthetic\Microsoft\WindowsApps\bash.exe`
	lookPath := func(name string) (string, error) {
		if name == "bash" {
			return alias, nil
		}
		return "", exec.ErrNotFound
	}
	probe := func(path string) bool {
		t.Fatalf("WSL alias was probed: %q", path)
		return true
	}
	sh := resolveShell("windows", lookPath, func(string) bool { return false }, nil, probe, isWindowsWSLBash)
	if sh.Kind != ShellPowerShell || sh.Path != "powershell" {
		t.Fatalf("WSL alias selected as fallback: %+v", sh)
	}
}

func TestNativeShellWorkspace(t *testing.T) {
	dir := t.TempDir()
	started := time.Now()
	sh := ResolveShell()
	t.Logf("resolved shell=%+v in %s", sh, time.Since(started))
	if isWindowsWSLBash(sh.Path) {
		t.Fatalf("native workspace selected WSL: %q", sh.Path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	argv := sh.argv("pwd")
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	proc.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), filepath.Base(dir)) {
		t.Fatalf("shell=%+v cwd output=%q err=%v", sh, out, err)
	}
}
