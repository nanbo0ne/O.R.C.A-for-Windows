//go:build windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestBetaGuardDedicatedTarget(t *testing.T) {
	dir := t.TempDir()
	if err := checkTarget(filepath.Join(dir, "new-beta")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Orca.exe"), []byte("official sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkTarget(dir); err == nil {
		t.Fatal("official directory accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, markerName), []byte(markerValue), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkTarget(dir); err == nil {
		t.Fatal("beta marker must not authorize official executable")
	}
	body, err := os.ReadFile(filepath.Join(dir, "Orca.exe"))
	if err != nil || string(body) != "official sentinel" {
		t.Fatal("official file changed")
	}
}

func TestBetaGuardLockedPayload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "OrcaBeta.exe")
	for name, body := range map[string]string{markerName: markerValue, "OrcaBeta.exe": "beta sentinel"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkTarget(dir); err != nil {
		t.Fatal(err)
	}
	name, _ := syscall.UTF16PtrFromString(path)
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTarget(dir); err == nil {
		t.Error("locked beta accepted")
	}
	if err := syscall.CloseHandle(h); err != nil {
		t.Fatal(err)
	}
	if err := checkTarget(dir); err != nil {
		t.Fatal(err)
	}
	if err := checkTarget("relative"); err == nil {
		t.Fatal("relative target accepted")
	}
}
