//go:build windows

// Package installipc requests a graceful shutdown of a desktop installation in
// the current Windows session, restricted to the current process user's SID.
package installipc

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

func eventIdentity(installDir string) (string, string, error) {
	if strings.TrimSpace(installDir) == "" {
		return "", "", errors.New("installipc: empty installation directory")
	}
	abs, err := filepath.Abs(installDir)
	if err != nil {
		return "", "", fmt.Errorf("installipc: absolute directory: %w", err)
	}
	dir, err := os.Open(abs)
	if err != nil {
		return "", "", fmt.Errorf("installipc: open directory: %w", err)
	}
	defer dir.Close()
	info, err := dir.Stat()
	if err != nil {
		return "", "", fmt.Errorf("installipc: stat directory: %w", err)
	}
	if !info.IsDir() {
		return "", "", errors.New("installipc: installation path is not a directory")
	}
	// Resolve junctions, symlinks, short names and extended-path spellings using
	// the directory handle, so installer and desktop derive the same identity.
	buf := make([]uint16, 260)
	for {
		n, err := windows.GetFinalPathNameByHandle(windows.Handle(dir.Fd()), &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return "", "", fmt.Errorf("installipc: canonical directory: %w", err)
		}
		if n < uint32(len(buf)) {
			break
		}
		buf = make([]uint16, n+1)
	}
	canonical := strings.ToLower(windows.UTF16ToString(buf))
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", "", fmt.Errorf("installipc: current user SID: %w", err)
	}
	sid := user.User.Sid.String()
	name := fmt.Sprintf(`Local\ORCA.InstallShutdown.v1.%s.%x`, sid, sha256.Sum256([]byte(canonical)))
	return name, sid, nil
}

// RequestShutdown signals an existing listener. False with no error means no
// listener exists; true means the request was sent, not that shutdown completed.
// Invalid directories, access denial and other OS failures are returned as errors.
func RequestShutdown(installDir string) (bool, error) {
	name, _, err := eventIdentity(installDir)
	if err != nil {
		return false, err
	}
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false, err
	}
	event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, namePtr)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("installipc: open shutdown event: %w", err)
	}
	defer windows.CloseHandle(event)
	if err := windows.SetEvent(event); err != nil {
		return false, fmt.Errorf("installipc: signal shutdown event: %w", err)
	}
	return true, nil
}

// Listen registers a one-shot shutdown callback. Cancellation or the idempotent
// stop function releases the listener's handles and waiter. Handles are released
// before callback runs, so callback may itself stop the listener during shutdown.
// Stop does not wait for a callback already selected for delivery to finish.
func Listen(ctx context.Context, installDir string, callback func()) (func(), error) {
	if ctx == nil || callback == nil {
		return nil, errors.New("installipc: context and callback are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, sid, err := eventIdentity(installDir)
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;;0x001f0003;;;" + sid + ")")
	if err != nil {
		return nil, fmt.Errorf("installipc: user-only DACL: %w", err)
	}
	attrs := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	event, err := windows.CreateEvent(&attrs, 1, 0, namePtr)
	if err != nil {
		// CreateEvent returns a valid handle AND ERROR_ALREADY_EXISTS when an
		// object is already present. Never adopt its untrusted security descriptor.
		if event != 0 {
			windows.CloseHandle(event)
		}
		return nil, fmt.Errorf("installipc: create shutdown event: %w", err)
	}
	cancelEvent, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(event)
		return nil, fmt.Errorf("installipc: create cancellation event: %w", err)
	}
	var mu sync.Mutex
	stopped := false
	done := make(chan struct{})
	cancel := func() {
		mu.Lock()
		defer mu.Unlock()
		if !stopped {
			stopped = true
			_ = windows.SetEvent(cancelEvent)
		}
	}
	detach := context.AfterFunc(ctx, cancel)
	go func() {
		// Cancellation has priority when both events are signalled.
		result, waitErr := windows.WaitForMultipleObjects([]windows.Handle{cancelEvent, event}, false, windows.INFINITE)
		detach()
		mu.Lock()
		fire := waitErr == nil && result == windows.WAIT_OBJECT_0+1 && !stopped && ctx.Err() == nil
		stopped = true
		windows.CloseHandle(event)
		windows.CloseHandle(cancelEvent)
		mu.Unlock()
		close(done)
		if fire {
			callback()
		}
	}()
	return func() {
		cancel()
		<-done
	}, nil
}
