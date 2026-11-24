//go:build windows && production && browserlive

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const browserLiveRootPrefix = "orca-browser-live-fixture-"

func browserLiveFixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", browserLiveRootPrefix)
	if err != nil {
		t.Fatal("fixture_root_create_failed")
	}
	t.Cleanup(func() {
		// WebView2 metrics handles can outlive the fixture process briefly. Retry
		// only this fixture's directory; never search for or terminate processes.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := browserLiveCleanupRoot(ctx, root); err != nil {
			kind := "fixture_profile_cleanup_failed"
			if errors.Is(err, context.DeadlineExceeded) {
				kind = "fixture_profile_cleanup_timeout"
			}
			t.Error(kind)
		}
	})
	return root
}

func browserLiveCleanupRoot(ctx context.Context, root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return errors.New("fixture_cleanup_scope_invalid")
	}
	temp, err := filepath.Abs(os.TempDir())
	if err != nil || !strings.EqualFold(filepath.Dir(root), temp) || !strings.HasPrefix(filepath.Base(root), browserLiveRootPrefix) {
		return errors.New("fixture_cleanup_scope_invalid")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Lstat(root)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("fixture_cleanup_root_invalid")
		}
		path, err := windows.UTF16PtrFromString(root)
		if err != nil {
			return errors.New("fixture_cleanup_root_invalid")
		}
		attrs, err := windows.GetFileAttributes(path)
		if err != nil || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return errors.New("fixture_cleanup_root_invalid")
		}
		if err := os.RemoveAll(root); err == nil {
			return nil
		} else if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_LOCK_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_DIR_NOT_EMPTY) {
			return errors.New("fixture_cleanup_remove_failed")
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func TestBrowserLiveProfileCleanupOffline(t *testing.T) {
	lock := func(t *testing.T, root string) windows.Handle {
		t.Helper()
		path, err := windows.UTF16PtrFromString(filepath.Join(root, "synthetic-BrowserMetrics.pma"))
		if err != nil {
			t.Fatal("synthetic_lock_path_failed")
		}
		h, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.CREATE_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			t.Fatal("synthetic_lock_create_failed")
		}
		return h
	}
	t.Run("delayed_release", func(t *testing.T) {
		root := browserLiveFixtureRoot(t)
		h := lock(t, root)
		released := make(chan struct{})
		go func() {
			time.Sleep(150 * time.Millisecond)
			_ = windows.CloseHandle(h)
			close(released)
		}()
		defer func() { <-released }()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := browserLiveCleanupRoot(ctx, root); err != nil {
			t.Fatal("delayed_profile_release_not_retried")
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("fixture_profile_remains")
		}
	})
	t.Run("bounded_timeout", func(t *testing.T) {
		root := browserLiveFixtureRoot(t)
		h := lock(t, root)
		defer windows.CloseHandle(h)
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		start := time.Now()
		if err := browserLiveCleanupRoot(ctx, root); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("locked_profile_did_not_timeout")
		}
		if time.Since(start) > 2*time.Second {
			t.Fatal("profile_cleanup_exceeded_bound")
		}
	})
	t.Run("reject_unowned_root", func(t *testing.T) {
		root := t.TempDir()
		if err := browserLiveCleanupRoot(context.Background(), root); err == nil {
			t.Fatal("unowned_directory_accepted")
		}
		if _, err := os.Stat(root); err != nil {
			t.Fatal("unowned_directory_modified")
		}
	})
}
