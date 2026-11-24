package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func assertNoModelStateTemps(t *testing.T, dir string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, ".model-upgrade-*.tmp"))
	if err != nil || len(paths) != 0 {
		t.Fatalf("model-state temporary files leaked: %v, %v", paths, err)
	}
}

func TestDesktopModelStateRetriesWindowsReaders(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing-destination", true: "fresh-temporary"}[fresh], func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tabsFileName)
			old, next := []byte(`{"tabs":[],"old":true}`), []byte(`{"tabs":[],"deepseekModelVersion":41}`)
			var reader *os.File
			var err error
			if !fresh {
				if err := os.WriteFile(path, old, 0o600); err != nil {
					t.Fatal(err)
				}
				reader, err = os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				if reader != nil {
					reader.Close()
				}
			})
			released := make(chan error, 1)
			checks := 0
			err = writeDesktopModelState(path, next, func() error {
				checks++
				current, err := readDesktopModelState(path)
				if err != nil {
					return err
				}
				if fresh && current != nil || !fresh && !bytes.Equal(current, old) {
					return errors.New("destination changed before publication")
				}
				if checks == 1 {
					if fresh {
						paths, err := filepath.Glob(filepath.Join(dir, ".model-upgrade-*.tmp"))
						if err != nil || len(paths) != 1 {
							return errors.New("missing completed temporary file")
						}
						reader, err = os.Open(paths[0])
						if err != nil {
							return err
						}
					}
					go func() {
						time.Sleep(100 * time.Millisecond)
						released <- reader.Close()
					}()
				}
				return nil
			})
			select {
			case closeErr := <-released:
				if closeErr != nil {
					t.Fatal(closeErr)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("reader was not released")
			}
			if err != nil {
				t.Fatalf("temporary Windows reader prevented model-state publication: %v", err)
			}
			if checks < 2 {
				t.Fatal("snapshot was not validated again before retry")
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, next) {
				t.Fatalf("completed state not published: %s, %v", got, err)
			}
			assertNoModelStateTemps(t, dir)
		})
	}
}

func TestDesktopModelStateRetryRejectsConcurrentEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, tabsFileName)
	old, edited := []byte(`{"tabs":[],"owner":"original"}`), []byte(`{"tabs":[],"owner":"concurrent"}`)
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	changed := errors.New("snapshot changed during retry")
	checks := 0
	updated := make(chan error, 1)
	err = writeDesktopModelState(path, []byte(`{"tabs":[],"owner":"migration"}`), func() error {
		checks++
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, old) {
			return changed
		}
		if checks == 1 {
			go func() {
				time.Sleep(100 * time.Millisecond)
				err := os.WriteFile(path, edited, 0o600)
				updated <- errors.Join(err, reader.Close())
			}()
		}
		return nil
	})
	select {
	case updateErr := <-updated:
		if updateErr != nil {
			t.Fatal(updateErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent edit did not complete")
	}
	if !errors.Is(err, changed) || checks < 2 {
		t.Fatalf("concurrent edit not rejected by revalidation: checks=%d error=%v", checks, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, edited) {
		t.Fatalf("concurrent state overwritten: %s, %v", got, err)
	}
	assertNoModelStateTemps(t, dir)
}

func TestDesktopModelStatePersistentReaderAndValidationFailure(t *testing.T) {
	for _, validationFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "held-reader", true: "validation-access-denied"}[validationFailure], func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tabsFileName)
			old := []byte(`{"tabs":[],"preserve":true}`)
			if err := os.WriteFile(path, old, 0o600); err != nil {
				t.Fatal(err)
			}
			reader, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			checks := 0
			err = writeDesktopModelState(path, []byte(`{"tabs":[]}`), func() error {
				checks++
				if validationFailure {
					return windows.ERROR_ACCESS_DENIED
				}
				return nil
			})
			if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
				t.Fatalf("persistent failure was hidden: %v", err)
			}
			if validationFailure && checks != 1 || !validationFailure && checks > 7 {
				t.Fatalf("unknown write/validation retried or rename unbounded: %d", checks)
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(got, old) {
				t.Fatalf("failed publication changed existing state: %s, %v", got, readErr)
			}
			assertNoModelStateTemps(t, dir)
		})
	}
}

func TestDesktopTabFallbackSaveWindowsReader(t *testing.T) {
	for _, outcome := range []string{"released", "changed", "held"} {
		t.Run(outcome, func(t *testing.T) {
			isolateDesktopUserDirs(t)
			dir := desktopConfigDir()
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, tabsFileName)
			old, _ := json.Marshal(desktopTabsFile{ActiveTab: "original"})
			edited, _ := json.Marshal(desktopTabsFile{ActiveTab: "concurrent"})
			if err := os.WriteFile(path, old, 0o600); err != nil {
				t.Fatal(err)
			}
			reader, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			released := make(chan error, 1)
			if outcome != "held" {
				go func() {
					deadline := time.Now().Add(3 * time.Second)
					for {
						temps, _ := filepath.Glob(filepath.Join(dir, ".model-upgrade-*.tmp"))
						_, statErr := os.Stat(path + ".tmp")
						if len(temps) > 0 || statErr == nil {
							break
						}
						if time.Now().After(deadline) {
							released <- errors.Join(errors.New("save did not create a temporary file"), reader.Close())
							return
						}
						time.Sleep(time.Millisecond)
					}
					time.Sleep(100 * time.Millisecond)
					var editErr error
					if outcome == "changed" {
						editErr = os.WriteFile(path, edited, 0o600)
					}
					released <- errors.Join(editErr, reader.Close())
				}()
			}
			a := &App{deepSeekModelVersion: 41}
			err = a.saveTabsLocked()
			if outcome != "held" {
				if releaseErr := <-released; releaseErr != nil {
					t.Fatal(releaseErr)
				}
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			switch outcome {
			case "released":
				var state desktopTabsFile
				if decodeErr := json.Unmarshal(got, &state); err != nil || decodeErr != nil || state.DeepSeekModelVersion != 41 {
					t.Fatalf("fallback save failed after reader release: %v", err)
				}
			case "changed":
				if err == nil || !strings.Contains(err.Error(), "changed during") || !bytes.Equal(got, edited) {
					t.Fatalf("fallback save did not reject/preserve concurrent edit: %s, %v", got, err)
				}
			case "held":
				if err == nil || !bytes.Equal(got, old) {
					t.Fatalf("held-reader failure hidden or state changed: %s, %v", got, err)
				}
			}
			assertNoModelStateTemps(t, dir)
			if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
				t.Fatalf("fallback temporary file leaked: %v", err)
			}
		})
	}
}
