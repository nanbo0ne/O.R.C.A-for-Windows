//go:build windows

package installipc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func waitCallback(t *testing.T, called <-chan struct{}) {
	t.Helper()
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown callback did not finish")
	}
}

func assertAbsent(t *testing.T, dir string) {
	t.Helper()
	if sent, err := RequestShutdown(dir); err != nil || sent {
		t.Fatalf("RequestShutdown without listener = %v, %v", sent, err)
	}
}

func TestEventDACLIsCurrentUserOnly(t *testing.T) {
	dir := t.TempDir()
	called := make(chan struct{})
	stop, err := Listen(context.Background(), dir, func() { close(called) })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	name, sid, err := eventIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, `Local\`) || !strings.Contains(name, sid) {
		t.Fatalf("event not scoped to session and SID: %q", name)
	}
	namePtr, _ := windows.UTF16PtrFromString(name)
	h, err := windows.OpenEvent(windows.READ_CONTROL|windows.EVENT_MODIFY_STATE, false, namePtr)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	sd, err := windows.GetSecurityInfo(h, windows.SE_KERNEL_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || owner.String() != sid {
		t.Fatalf("unexpected event owner: %v, %v", owner, err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 || control&windows.SE_DACL_PRESENT == 0 {
		t.Fatalf("DACL must be explicit and protected: %#x, %v", control, err)
	}
	dacl, defaulted, err := sd.DACL()
	if err != nil || dacl == nil || defaulted || dacl.AceCount != 1 {
		t.Fatalf("expected exactly one explicit allow ACE: %v, defaulted=%v, %v", dacl, defaulted, err)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	allowedSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != 0 || allowedSID.String() != sid || uint32(ace.Mask) != windows.EVENT_ALL_ACCESS {
		t.Fatalf("unexpected ACE: header=%+v mask=%#x SID=%s", ace.Header, ace.Mask, allowedSID.String())
	}
	// This is a real kernel event: an independently opened modify-only handle
	// must signal it, and the listener must deliver the request.
	modify, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, namePtr)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetEvent(modify); err != nil {
		windows.CloseHandle(modify)
		t.Fatal(err)
	}
	windows.CloseHandle(modify)
	waitCallback(t, called)
}

func TestCanonicalDirectoryAndIsolation(t *testing.T) {
	dir := t.TempDir()
	name, _, err := eventIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{strings.ToUpper(dir), dir + `\.`, `\\?\` + dir, strings.ReplaceAll(dir, `\`, "/") + "/"} {
		got, _, err := eventIdentity(alias)
		if err != nil || got != name {
			t.Fatalf("alias %q: name=%q err=%v, want %q", alias, got, err, name)
		}
	}
	link := filepath.Join(t.TempDir(), "install-link")
	if err := os.Symlink(dir, link); err == nil {
		got, _, err := eventIdentity(link)
		if err != nil || got != name {
			t.Fatalf("symlink identity = %q, %v, want %q", got, err, name)
		}
	} else {
		t.Logf("symlink alias unavailable: %v", err)
	}
	called := make(chan struct{})
	stop, err := Listen(context.Background(), dir, func() { close(called) })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	assertAbsent(t, t.TempDir())
	if sent, err := RequestShutdown(strings.ToUpper(dir)); err != nil || !sent {
		t.Fatalf("canonical alias request = %v, %v", sent, err)
	}
	waitCallback(t, called)
}

func TestListenerOneShotAndCallbackCanStop(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32
	called := make(chan struct{})
	stopCh := make(chan func(), 1)
	stop, err := Listen(context.Background(), dir, func() {
		calls.Add(1)
		(<-stopCh)()
		close(called)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	stopCh <- stop
	name, _, _ := eventIdentity(dir)
	ptr, _ := windows.UTF16PtrFromString(name)
	// Keep the original event alive across repeated signals even after the
	// listener releases its own handle.
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, ptr)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 32; i++ {
		if err := windows.SetEvent(h); err != nil {
			windows.CloseHandle(h)
			t.Fatal(err)
		}
	}
	waitCallback(t, called)
	windows.CloseHandle(h)
	stop()
	if calls.Load() != 1 {
		t.Fatalf("callback count = %d", calls.Load())
	}
	assertAbsent(t, dir)
}

func TestStopAndCancellationReleaseListener(t *testing.T) {
	for _, cancelContext := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "context"}[cancelContext], func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls atomic.Int32
			stop, err := Listen(ctx, dir, func() { calls.Add(1) })
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			if cancelContext {
				cancel()
				deadline := time.Now().Add(5 * time.Second)
				name, _, _ := eventIdentity(dir)
				ptr, _ := windows.UTF16PtrFromString(name)
				for {
					h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, ptr)
					if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					windows.CloseHandle(h)
					if time.Now().After(deadline) {
						t.Fatal("context cancellation did not release event")
					}
					time.Sleep(time.Millisecond)
				}
			}
			var wg sync.WaitGroup
			for i := 0; i < 16; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); stop() }()
			}
			wg.Wait()
			assertAbsent(t, dir)
			if calls.Load() != 0 {
				t.Fatal("stopping listener invoked callback")
			}
			restarted, err := Listen(context.Background(), dir, func() {})
			if err != nil {
				t.Fatalf("listener leaked named event: %v", err)
			}
			restarted()
		})
	}
}

func TestRejectPreexistingEventAndPreserveAccessErrors(t *testing.T) {
	for _, sddl := range []string{"D:P(A;;GA;;;WD)", "D:P"} {
		t.Run(sddl, func(t *testing.T) {
			dir := t.TempDir()
			name, _, _ := eventIdentity(dir)
			ptr, _ := windows.UTF16PtrFromString(name)
			sd, err := windows.SecurityDescriptorFromString(sddl)
			if err != nil {
				t.Fatal(err)
			}
			attrs := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
			h, err := windows.CreateEvent(&attrs, 1, 0, ptr)
			if err != nil {
				t.Fatal(err)
			}
			stop, err := Listen(context.Background(), dir, func() { t.Error("adopted foreign event") })
			if stop != nil {
				stop()
			}
			if err == nil {
				windows.CloseHandle(h)
				t.Fatal("accepted preexisting event")
			}
			if sddl == "D:P" {
				if sent, err := RequestShutdown(dir); sent || !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
					windows.CloseHandle(h)
					t.Fatalf("access denied was swallowed: %v, %v", sent, err)
				}
			}
			windows.CloseHandle(h)
			assertAbsent(t, dir)
		})
	}
}

func TestInvalidInputs(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", " ", file, filepath.Join(dir, "missing"), "bad\x00path"} {
		if sent, err := RequestShutdown(invalid); sent || err == nil {
			t.Fatalf("invalid request path %q accepted: %v, %v", invalid, sent, err)
		}
		if stop, err := Listen(context.Background(), invalid, func() {}); err == nil {
			stop()
			t.Fatalf("invalid listen path %q accepted", invalid)
		}
	}
	if _, err := Listen(nil, dir, func() {}); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := Listen(context.Background(), dir, nil); err == nil {
		t.Fatal("nil callback accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Listen(ctx, dir, func() {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context = %v", err)
	}
}

func TestRequestFromSeparateProcess(t *testing.T) {
	dir := t.TempDir()
	called := make(chan struct{})
	stop, err := Listen(context.Background(), dir, func() { close(called) })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestShutdownRequestChild$")
	cmd.Env = append(os.Environ(), "ORCA_INSTALLIPC_TEST_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child request: %v\n%s", err, out)
	}
	waitCallback(t, called)
	assertAbsent(t, dir)
}

func TestShutdownRequestChild(t *testing.T) {
	dir := os.Getenv("ORCA_INSTALLIPC_TEST_DIR")
	if dir == "" {
		t.Skip("only executed by TestRequestFromSeparateProcess")
	}
	if sent, err := RequestShutdown(dir); err != nil || !sent {
		t.Fatalf("child request = %v, %v", sent, err)
	}
}
