package main

import (
	"os/exec"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/sandbox"
)

func requireNativeBash(t *testing.T) string {
	t.Helper()
	sh := sandbox.ResolveShell()
	if sh.Kind != sandbox.ShellBash {
		t.Skip("native bash is required for the script fixture")
	}
	path, err := exec.LookPath(sh.Path)
	if err != nil {
		t.Skipf("native bash is unavailable: %v", err)
	}
	return path
}
