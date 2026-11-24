package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRenameReplaceCheckedValidationFailure(t *testing.T) {
	dir := t.TempDir()
	tmp, dest := filepath.Join(dir, "new.tmp"), filepath.Join(dir, "state.json")
	for path, body := range map[string]string{tmp: "new-complete", dest: "old-complete"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want := &os.PathError{Op: "read", Path: dest, Err: os.ErrPermission}
	checks := 0
	err := RenameReplaceChecked(tmp, dest, func() error { checks++; return want })
	if err != want || checks != 1 {
		t.Fatalf("validation failure retried or hidden: checks=%d error=%v", checks, err)
	}
	for path, body := range map[string]string{tmp: "new-complete", dest: "old-complete"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != body {
			t.Fatalf("rejected publication changed %q: %q, %v", path, got, err)
		}
	}
	if err := RenameReplaceChecked(tmp, dest, func() error {
		got, err := os.ReadFile(dest)
		if err != nil {
			return err
		}
		if string(got) != "old-complete" {
			return errors.New("destination changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "new-complete" {
		t.Fatalf("valid publication failed: %q, %v", got, err)
	}
}
