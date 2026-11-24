//go:build windows

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

func browserBetaJunction(t *testing.T, link, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "New-Item -ErrorAction Stop -ItemType Junction -Path $env:ORCA_TEST_JUNCTION -Target $env:ORCA_TEST_TARGET | Out-Null")
	cmd.Env = append(os.Environ(), "ORCA_TEST_JUNCTION="+link, "ORCA_TEST_TARGET="+target)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create synthetic junction: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
}

func TestBrowserBetaMetadataRejectsJunctionBeforeReadOrWrite(t *testing.T) {
	for _, atRoot := range []bool{true, false} {
		name := "metadata-parent"
		if atRoot {
			name = "browser-root"
		}
		t.Run(name, func(t *testing.T) {
			_, root := withBetaProfile(t)
			official := filepath.Join(os.Getenv("APPDATA"), "orca", "browser-profiles")
			if err := initializeBeta(); err != nil {
				t.Fatal(err)
			}
			storage := filepath.Join(root, "orca", "browser-profiles")
			fake := &browserFake{tabs: map[string]browserhost.Tab{}, profiles: map[string]string{}, generation: map[string]uint64{}}
			s := newBrowserService(fake, storage, nil)
			defer s.shutdown()
			key := browserProfileKey("owner", "global", "")
			v := browserMetadata{Version: 2, Owner: "owner", StorageKey: "owner", Profile: key,
				Tabs: []browserhost.Tab{{ID: browserID(), URL: "https://synthetic.invalid/private", Title: "official metadata sentinel"}}}
			data, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			link, target := filepath.Join(storage, "tab-metadata"), official
			sentinel := filepath.Join(target, filepath.Base(s.metadataPath("owner")))
			if atRoot {
				link = storage
				sentinel = filepath.Join(target, "tab-metadata", filepath.Base(s.metadataPath("owner")))
			}
			writeBetaFixture(t, sentinel, string(data))
			browserBetaJunction(t, link, target)
			if err := s.register("owner", key); err != nil {
				t.Fatal(err)
			}
			if state := s.state("owner"); len(state.Tabs) != 0 || state.Error == "" {
				t.Fatalf("redirected official metadata was read: %+v", state)
			}
			s.saveMetadata("owner")
			after, err := os.ReadFile(sentinel)
			if err != nil || string(after) != string(data) {
				t.Fatalf("official sentinel changed: %v", err)
			}
			entries, err := os.ReadDir(filepath.Dir(sentinel))
			if err != nil || len(entries) != 1 {
				t.Fatal("metadata created an external temporary file", err)
			}
		})
	}
}

func TestBrowserBetaUploadSnapshotRejectsRedirectedStorage(t *testing.T) {
	base, root := withBetaProfile(t)
	official := filepath.Join(os.Getenv("APPDATA"), "orca", "sentinel")
	writeBetaFixture(t, official, "unchanged")
	source := filepath.Join(base, "selected", "ordinary.txt")
	writeBetaFixture(t, source, "selected external bytes")
	if err := initializeBeta(); err != nil {
		t.Fatal(err)
	}
	storage := filepath.Join(root, "orca", "browser-profiles")
	browserBetaJunction(t, filepath.Join(storage, "upload-snapshots"), filepath.Dir(official))
	if _, err := newBrowserUploadSnapshot(source, storage); err == nil {
		t.Fatal("snapshot wrote through junction")
	}
	entries, err := os.ReadDir(filepath.Dir(official))
	if err != nil || len(entries) != 1 {
		t.Fatalf("snapshot created external state: %v %v", entries, err)
	}
	if data, err := os.ReadFile(official); err != nil || string(data) != "unchanged" {
		t.Fatal("official sentinel changed", err)
	}
}

func TestBrowserBetaTransfersExcludeOfficialDataButAllowExternalSelection(t *testing.T) {
	base, root := withBetaProfile(t)
	official := filepath.Join(os.Getenv("APPDATA"), "orca")
	blocked := []string{
		filepath.Join(official, "config.toml"), filepath.Join(official, "sessions", "old.jsonl"),
		filepath.Join(official, "browser-profiles", "cookies.bin"), filepath.Join(base, "project", ".orca", "private.txt"),
		filepath.Join(base, "project", ".deepseek-orca", "private.txt"), filepath.Join(base, "project", ".env"),
		filepath.Join(base, "project", "orca.toml"),
	}
	for _, path := range blocked {
		writeBetaFixture(t, path, "official sentinel")
	}
	if err := initializeBeta(); err != nil {
		t.Fatal(err)
	}
	for _, path := range blocked {
		if _, err := readBrowserUploadSource(path, nil); err == nil {
			t.Errorf("read protected upload %s", path)
		}
		if err := browserDownloadPath(path); err == nil {
			t.Errorf("accepted protected download %s", path)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != "official sentinel" {
			t.Fatal("official sentinel changed", err)
		}
	}
	if err := browserDownloadPath(filepath.Join(official, "new-file.txt")); err == nil {
		t.Fatal("accepted a new destination under official profile")
	}
	external := filepath.Join(base, "selected", "ordinary.txt")
	writeBetaFixture(t, external, "selected external bytes")
	if data, err := readBrowserUploadSource(external, nil); err != nil || string(data) != "selected external bytes" {
		t.Fatal("ordinary external upload rejected", err)
	}
	if err := browserDownloadPath(filepath.Join(filepath.Dir(external), "download.txt")); err != nil {
		t.Fatal("ordinary external download rejected", err)
	}
	snap, err := newBrowserUploadSnapshot(external, filepath.Join(root, "orca", "browser-profiles"))
	if err != nil {
		t.Fatal("ordinary external file did not snapshot", err)
	}
	defer snap.remove()
	if !snap.valid() {
		t.Fatal("owned beta snapshot invalid")
	}
	// Shared non-isolated behavior is unchanged, even for these synthetic names.
	if err := config.SetIsolatedProfileHome(""); err != nil {
		t.Fatal(err)
	}
	if _, err := readBrowserUploadSource(blocked[0], nil); err != nil {
		t.Fatal("non-isolated upload behavior changed", err)
	}
	if err := browserDownloadPath(filepath.Join(official, "new-file.txt")); err != nil {
		t.Fatal("non-isolated download behavior changed", err)
	}
}
