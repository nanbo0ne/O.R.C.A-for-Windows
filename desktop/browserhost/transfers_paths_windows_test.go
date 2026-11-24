//go:build windows

package browserhost

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDownloadDirectoryPins(t *testing.T) {
	root := t.TempDir()
	ancestor := filepath.Join(root, "ancestor")
	parent := filepath.Join(ancestor, "destination")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	handles, err := pinDownloadParents(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { releaseDownloadDirectories(handles) }()
	for _, dir := range []string{ancestor, parent} {
		if err := os.Rename(dir, dir+"-replaced"); err == nil {
			t.Fatalf("pinned directory was renamed: %s", dir)
		}
	}
	stage := filepath.Join(parent, "stage")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	stageLock, err := pinDownloadDirectory(stage)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if stageLock != 0 {
			_ = windows.CloseHandle(stageLock)
		}
	}()
	if err := os.Rename(stage, stage+"-replaced"); err == nil {
		t.Fatal("pinned staging directory was replaced")
	}
	source := filepath.Join(stage, "file.bin")
	destination := filepath.Join(parent, "file.bin")
	if err := os.WriteFile(source, []byte("approved bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	src, _ := windows.UTF16PtrFromString(source)
	dst, _ := windows.UTF16PtrFromString(destination)
	if err := os.WriteFile(destination, []byte("other file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := windows.MoveFile(src, dst); err == nil {
		t.Fatal("publication overwrote a destination created after approval")
	}
	if contents, err := os.ReadFile(destination); err != nil || string(contents) != "other file" {
		t.Fatalf("existing destination changed: %q %v", contents, err)
	}
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if err := windows.MoveFile(src, dst); err != nil {
		t.Fatalf("directory pins prevented approved publication: %v", err)
	}
	_ = windows.CloseHandle(stageLock)
	stageLock = 0
	if err := os.Remove(stage); err != nil {
		t.Fatal(err)
	}
	releaseDownloadDirectories(handles)
	handles = nil
	if err := os.Rename(parent, parent+"-released"); err != nil {
		t.Fatalf("directory remained locked after release: %v", err)
	}
}

func TestDownloadDirectoryPinsReleaseOnFailure(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "regular-file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if handles, err := pinDownloadParents(file); err == nil {
		releaseDownloadDirectories(handles)
		t.Fatal("regular file accepted as download parent")
	}
	if err := os.Rename(root, root+"-released"); err != nil {
		t.Fatalf("failed guard leaked a directory handle: %v", err)
	}
	// Restore the temp path so testing owns its cleanup.
	if err := os.Rename(root+"-released", root); err != nil {
		t.Fatal(err)
	}
}
