//go:build !windows

package orcaaccount

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUnsupportedStorageHasNoPlaintextFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-fallback", "session")
	store, err := NewSystemStore(path)
	if store != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("unsafe non-Windows storage")
	}
	if _, err = os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsupported storage created files")
	}
}
