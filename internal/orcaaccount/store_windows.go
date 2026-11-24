//go:build windows

package orcaaccount

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"golang.org/x/sys/windows"
)

var dpapiHeader = []byte("ORCA-DPAPI-USER-v1\x00")

type systemStore struct{ path string }

func (*systemStore) String() string     { return "orcaaccount.SystemStore{current-user DPAPI}" }
func (s *systemStore) GoString() string { return s.String() }

// NewSystemStore uses DPAPI current-user scope, never machine scope or plaintext.
// The parent supplies a path in its isolated beta user-data directory.
func NewSystemStore(path string) (Store, error) {
	if err := config.ValidateProfileDataPath(path); err != nil {
		return nil, ErrStorage
	}
	if !filepath.IsAbs(path) || filepath.Base(path) == "." || filepath.Base(path) == string(filepath.Separator) {
		return nil, ErrInvalid
	}
	return &systemStore{path: filepath.Clean(path)}, nil
}

func protect(data []byte, decrypt bool) ([]byte, error) {
	if len(data) == 0 || len(data) > maxResponse {
		return nil, ErrStorage
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	var err error
	// Omitting CRYPTPROTECT_LOCAL_MACHINE is intentional: another OS account
	// must not be able to decrypt the credential file. No interactive prompts.
	if decrypt {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	runtime.KeepAlive(data)
	if err != nil {
		return nil, ErrStorage
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	if out.Data == nil || out.Size == 0 || out.Size > maxResponse {
		return nil, ErrStorage
	}
	view := unsafe.Slice(out.Data, int(out.Size))
	defer clear(view)
	return bytes.Clone(view), nil
}

func (s *systemStore) Load(ctx context.Context) ([]byte, error) {
	if err := config.ValidateProfileDataPath(s.path); err != nil {
		return nil, ErrStorage
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, ErrStorage
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxResponse+1))
	if err != nil || len(b) > maxResponse || !bytes.HasPrefix(b, dpapiHeader) {
		return nil, ErrStorage
	}
	plain, err := protect(b[len(dpapiHeader):], true)
	if ctx.Err() != nil {
		clear(plain)
		return nil, ctx.Err()
	}
	return plain, err
}

func (s *systemStore) Save(ctx context.Context, plain []byte) error {
	if err := config.ValidateProfileDataPath(s.path); err != nil {
		return ErrStorage
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	encrypted, err := protect(plain, false)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return ErrStorage
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".orca-dpapi-*")
	if err != nil {
		return ErrStorage
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return ErrStorage
	}
	if _, err = f.Write(dpapiHeader); err != nil {
		return ErrStorage
	}
	if _, err = f.Write(encrypted); err != nil {
		return ErrStorage
	}
	if err = f.Sync(); err != nil {
		return ErrStorage
	}
	if err = f.Close(); err != nil {
		return ErrStorage
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	from, e1 := windows.UTF16PtrFromString(f.Name())
	to, e2 := windows.UTF16PtrFromString(s.path)
	if e1 != nil || e2 != nil {
		return ErrStorage
	}
	if windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH) != nil {
		return ErrStorage
	}
	return nil
}

func (s *systemStore) Delete(ctx context.Context) error {
	if err := config.ValidateProfileDataPath(s.path); err != nil {
		return ErrStorage
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := os.Remove(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrStorage
	}
	return nil
}
