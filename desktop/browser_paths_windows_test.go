//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBrowserUploadAndDownloadRejectJunctionParents(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	junction := filepath.Join(root, "junction")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	// All paths are synthetic. Environment values avoid shell interpolation of
	// path characters while exercising a real Windows directory reparse point.
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "New-Item -ErrorAction Stop -ItemType Junction -Path $env:ORCA_TEST_JUNCTION -Target $env:ORCA_TEST_TARGET | Out-Null")
	cmd.Env = append(os.Environ(), "ORCA_TEST_JUNCTION="+junction, "ORCA_TEST_TARGET="+target)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create synthetic junction: %v: %s", err, out)
	}
	defer os.Remove(junction)
	if err := os.WriteFile(filepath.Join(target, "fixture.txt"), []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBrowserUploadSource(filepath.Join(junction, "fixture.txt"), nil); err == nil {
		t.Fatal("upload accepted junction ancestor")
	}
	if err := browserDownloadPath(filepath.Join(junction, "download.txt")); err == nil {
		t.Fatal("download accepted junction ancestor")
	}
}
