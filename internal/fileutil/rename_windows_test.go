package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// The baseline os.Rename cannot replace a file held by an ordinary Go reader.
func TestRenameBaselineOpenReader(t *testing.T) {
	dir := t.TempDir()
	tmp, dest := filepath.Join(dir, "new.tmp"), filepath.Join(dir, "state.json")
	for path, body := range map[string]string{tmp: "new-complete", dest: "old-complete"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })
	err = os.Rename(tmp, dest)
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("baseline rename with open reader = %v", err)
	}
	for path, want := range map[string]string{tmp: "new-complete", dest: "old-complete"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("failed rename changed %q: %q, %v", path, got, err)
		}
	}
}

func TestRenameReplaceRetryPolicy(t *testing.T) {
	for _, code := range []error{windows.ERROR_SHARING_VIOLATION, windows.ERROR_LOCK_VIOLATION, windows.ERROR_ACCESS_DENIED, windows.ERROR_FILE_NOT_FOUND} {
		for _, transient := range []bool{false, true} {
			calls := 0
			var waited time.Duration
			wrapped := &os.LinkError{Op: "rename", Old: "tmp", New: "dest", Err: code}
			err := renameReplaceWithRetry("tmp", "dest", func(from, to string) error {
				if from != "tmp" || to != "dest" {
					t.Fatal("retry changed file names")
				}
				calls++
				if transient && calls == 3 {
					return nil
				}
				return wrapped
			}, func(d time.Duration) { waited += d }, nil)
			switch {
			case code == windows.ERROR_FILE_NOT_FOUND:
				if err != wrapped || calls != 1 || waited != 0 {
					t.Fatalf("non-sharing error retried: calls=%d waited=%s err=%v", calls, waited, err)
				}
			case transient:
				if err != nil || calls != 3 || waited != 30*time.Millisecond {
					t.Fatalf("transient retry: calls=%d waited=%s err=%v", calls, waited, err)
				}
			default:
				if err != wrapped || calls != 7 || waited != 630*time.Millisecond {
					t.Fatalf("permanent failure masked/unbounded: calls=%d waited=%s err=%v", calls, waited, err)
				}
			}
		}
	}
}

func TestRenameReplaceReaderReleaseAndPermanentFailure(t *testing.T) {
	for _, release := range []bool{true, false} {
		t.Run(map[bool]string{true: "released", false: "held"}[release], func(t *testing.T) {
			dir := t.TempDir()
			tmp, dest := filepath.Join(dir, "new.tmp"), filepath.Join(dir, "state.json")
			for path, body := range map[string]string{tmp: "new-complete", dest: "old-complete"} {
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			reader, err := os.Open(dest)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { reader.Close() })
			waits := 0
			err = renameReplaceWithRetry(tmp, dest, os.Rename, func(time.Duration) {
				waits++
				for path, want := range map[string]string{tmp: "new-complete", dest: "old-complete"} {
					got, readErr := os.ReadFile(path)
					if readErr != nil || string(got) != want {
						t.Fatalf("before retry %q = %q, %v", path, got, readErr)
					}
				}
				if release && waits == 1 {
					if err := reader.Close(); err != nil {
						t.Fatal(err)
					}
				}
			}, nil)
			if release {
				if err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(dest)
				if err != nil || string(got) != "new-complete" {
					t.Fatalf("published %q, %v", got, err)
				}
				if _, err := os.Stat(tmp); !os.IsNotExist(err) {
					t.Fatalf("rename did not consume temporary file: %v", err)
				}
			} else if err == nil || waits != 6 {
				t.Fatalf("held reader failure hidden: waits=%d err=%v", waits, err)
			} else {
				for path, want := range map[string]string{tmp: "new-complete", dest: "old-complete"} {
					got, err := os.ReadFile(path)
					if err != nil || string(got) != want {
						t.Fatalf("permanent failure changed %q: %q, %v", path, got, err)
					}
				}
			}
		})
	}
}

func TestRenameReplaceRevalidatesBeforeRetry(t *testing.T) {
	for _, validationErr := range []error{errors.New("snapshot changed"), windows.ERROR_ACCESS_DENIED} {
		checks, renames, waits := 0, 0, 0
		err := renameReplaceWithRetry("tmp", "dest", func(_, _ string) error {
			renames++
			return windows.ERROR_SHARING_VIOLATION
		}, func(time.Duration) { waits++ }, func() error {
			checks++
			if checks > 1 {
				return validationErr
			}
			return nil
		})
		if err != validationErr || checks != 2 || renames != 1 || waits != 1 {
			t.Fatalf("revalidation failure ignored/retried: checks=%d renames=%d waits=%d error=%v", checks, renames, waits, err)
		}
	}
}
