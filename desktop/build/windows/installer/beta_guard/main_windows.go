//go:build windows

// The beta installer deliberately never signals or terminates a process.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const markerName = ".orca-beta-install"
const markerValue = "ORCA beta"

func checkTarget(dir string) error {
	if !filepath.IsAbs(dir) || filepath.Dir(dir) == dir {
		return fmt.Errorf("choose a dedicated absolute beta directory")
	}
	dir = filepath.Clean(dir)
	// Check existing ancestors as well as the target: junctions must not turn a
	// seemingly separate beta directory into an official installation.
	for prefix := dir; ; prefix = filepath.Dir(prefix) {
		info, err := os.Lstat(prefix)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("not a directory: %s", prefix)
			}
			if reparse(info) {
				return fmt.Errorf("reparse points are not allowed: %s", prefix)
			}
		}
		if filepath.Dir(prefix) == prefix {
			break
		}
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	marker, err := os.ReadFile(filepath.Join(dir, markerName))
	if err != nil || strings.TrimSpace(string(marker)) != markerValue {
		return fmt.Errorf("choose an empty directory; this is not an ORCA beta installation")
	}
	allowed := map[string]bool{
		"orcabeta.exe": true, "uninstall-beta.exe": true, markerName: true,
		"node.exe": true, "license.node.txt": true, "third-party-notices.txt": true, "codegraph": true,
	}
	for _, entry := range entries {
		if !allowed[strings.ToLower(entry.Name())] {
			return fmt.Errorf("unowned entry blocks beta installation: %s", entry.Name())
		}
	}
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if reparse(info) {
			return fmt.Errorf("reparse points are not allowed: %s", path)
		}
		if info.IsDir() {
			return nil
		}
		name, err := syscall.UTF16PtrFromString(path)
		if err != nil {
			return err
		}
		h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, 0, 0)
		if err != nil {
			return fmt.Errorf("close ORCA beta and release this file before retrying: %s: %w", path, err)
		}
		return syscall.CloseHandle(h)
	})
}

func reparse(info fs.FileInfo) bool {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return info.Mode()&os.ModeSymlink != 0 || (ok && data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0)
}

func main() {
	if len(os.Args) != 2 {
		os.Exit(23)
	}
	if err := checkTarget(os.Args[1]); err != nil {
		fmt.Println(err)
		os.Exit(21)
	}
}
