//go:build windows

package orcaaccount

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDPAPICurrentUserRoundTripAndEncryptedFilesOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "isolated-beta", "account.dpapi")
	s, err := NewSystemStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = s.Load(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	for _, secret := range [][]byte{[]byte(`{"refresh_token":"SYNTHETIC_SECRET_ONE"}`), []byte(`{"refresh_token":"SYNTHETIC_SECRET_TWO"}`)} {
		if err = s.Save(ctx, secret); err != nil {
			t.Fatal(err)
		}
		encoded, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.HasPrefix(encoded, dpapiHeader) || bytes.Contains(encoded, []byte("SYNTHETIC_SECRET")) || bytes.Contains(encoded, []byte("refresh_token")) {
			t.Fatal("plaintext persisted")
		}
		got, e := s.Load(ctx)
		if e != nil || !bytes.Equal(got, secret) {
			t.Fatalf("DPAPI round trip: %v", e)
		}
		entries, e := os.ReadDir(filepath.Dir(path))
		if e != nil || len(entries) != 1 {
			t.Fatal("temporary file retained")
		}
	}
	if err = s.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx); err != nil {
		t.Fatal("delete not idempotent")
	}
}

func TestDPAPIRejectsPlaintextCorruptAndFailedWriteWithoutFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.dpapi")
	s, err := NewSystemStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range [][]byte{[]byte(`{"refresh_token":"SYNTHETIC"}`), append(bytes.Clone(dpapiHeader), []byte("corrupt ciphertext")...)} {
		if err = os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Load(context.Background()); !errors.Is(err, ErrStorage) {
			t.Fatalf("unsafe fallback: %v", err)
		}
	}
	blocked := filepath.Join(dir, "file-not-directory")
	if err = os.WriteFile(blocked, []byte("synthetic blocker"), 0600); err != nil {
		t.Fatal(err)
	}
	failed, err := NewSystemStore(filepath.Join(blocked, "session.dpapi"))
	if err != nil {
		t.Fatal(err)
	}
	if err = failed.Save(context.Background(), []byte("SYNTHETIC_SECRET")); !errors.Is(err, ErrStorage) {
		t.Fatalf("write failure: %v", err)
	}
	got, e := os.ReadFile(blocked)
	if e != nil || string(got) != "synthetic blocker" {
		t.Fatal("fallback overwrote unrelated file")
	}
	if _, err = NewSystemStore("relative.dpapi"); !errors.Is(err, ErrInvalid) {
		t.Fatal("relative store path accepted")
	}
}

func TestDPAPIHonorsCanceledContext(t *testing.T) {
	s, err := NewSystemStore(filepath.Join(t.TempDir(), "session.dpapi"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = s.Save(ctx, []byte("synthetic")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = s.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err = s.Delete(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
