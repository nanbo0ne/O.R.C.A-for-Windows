//go:build windows

package webbetahost

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
	"golang.org/x/sys/windows"
)

func TestLoginOnlyRefusesAllRunAndRecheckWithoutPosting(t *testing.T) {
	b := &Browser{loginOnly: true, state: webbeta.BrowserState{Ready: true}, commands: make(chan func(), 4)}
	for _, action := range []string{"", "send", "stop", "upload_probe", "upload_probe_image", "anything-new"} {
		if err := b.Run(context.Background(), webbeta.Command{Action: action}, func(webbeta.Event) error {
			t.Fatal("login-only emitted an automation event")
			return nil
		}); !errors.Is(err, ErrLoginOnly) {
			t.Fatalf("%q: %v", action, err)
		}
	}
	if err := b.Recheck(context.Background()); !errors.Is(err, ErrLoginOnly) {
		t.Fatal(err)
	}
	if len(b.commands) != 0 || b.active != "" {
		t.Fatal("login-only posted automation")
	}
	if _, err := NewLoginOnlyWithWindowClass("unused", ""); err == nil {
		t.Fatal("missing window class accepted")
	}
}

func TestLoginOnlyScriptsExcludeAdapterAndProbes(t *testing.T) {
	want := documentScripts(true, "synthetic-generation", false, false)
	for _, diagnostics := range []bool{false, true} {
		for _, probe := range []bool{false, true} {
			got := documentScripts(true, "synthetic-generation", diagnostics, probe)
			if len(got) != 1 || !reflect.DeepEqual(want, got) || !strings.Contains(got[0], "login_health") {
				t.Fatal("diagnostics changed the login-only detector")
			}
			for _, forbidden := range []string{".value", ".click(", "fetch(", "XMLHttpRequest", "__orcaWebBeta", "__orcaRawResponse", "document.cookie", "localStorage", "FileReader", "composer_probe"} {
				if strings.Contains(got[0], forbidden) {
					t.Fatalf("login detector contains %s", forbidden)
				}
			}
		}
	}
	if len(documentScripts(false, "generation", false, true)) != 3 || len(documentScripts(false, "generation", true, true)) != 4 {
		t.Fatal("normal adapter diagnostic gating changed")
	}
	scripts := documentScripts(false, "generation", false, false)
	if scripts[1] != rawResponseScript || !strings.Contains(scripts[1], "__orcaRawResponse") || !strings.Contains(scripts[2], "raw_adapter_unavailable") {
		t.Fatal("task raw observer must precede the page adapter")
	}
}

type loginNavigationFixture struct {
	table      *[10]uintptr
	url        string
	cancelled  bool
	readFailed bool
}

var loginNavigationFixtureTable = [10]uintptr{
	3: syscall.NewCallback(func(f *loginNavigationFixture, out *uintptr) uintptr {
		if f.readFailed {
			return 0x80004005
		}
		value, _ := windows.UTF16FromString(f.url)
		ptr, _, _ := windows.NewLazySystemDLL("ole32.dll").NewProc("CoTaskMemAlloc").Call(uintptr(len(value) * 2))
		if ptr == 0 {
			return 0x8007000e
		}
		copy(unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(value)), value)
		*out = ptr
		return 0
	}),
	6: syscall.NewCallback(func(self *loginNavigationFixture, value uintptr) uintptr {
		self.cancelled = value == 1
		return 0
	}),
	8: syscall.NewCallback(func(self *loginNavigationFixture, value uintptr) uintptr {
		self.cancelled = value == 1
		return 0
	}),
}

func TestLoginOnlyNativeNavigationGuard(t *testing.T) {
	for _, tc := range []struct {
		url     string
		blocked bool
	}{
		{"https://chat.deepseek.com/", false}, {"https://chat.deepseek.com/sign_in", false},
		{"http://chat.deepseek.com/", true}, {"https://chat.deepseek.com.evil.invalid/", true},
		{"https://chat.deepseek.com@evil.invalid/", true}, {"https://chat.deepseek.com:444/", true},
		{"https://accounts.deepseek.com/", true}, {"file:///C:/", true}, {"about:blank", true},
	} {
		f := &loginNavigationFixture{table: &loginNavigationFixtureTable, url: tc.url}
		if hr := loginNavigationStarting(unsafe.Pointer(f)); hr != 0 || f.cancelled != tc.blocked {
			t.Fatalf("%s cancelled=%v hr=%x", tc.url, f.cancelled, hr)
		}
	}
	f := &loginNavigationFixture{table: &loginNavigationFixtureTable, readFailed: true}
	loginNavigationStarting(unsafe.Pointer(f))
	if !f.cancelled {
		t.Fatal("URI read failure did not fail closed")
	}
	f.cancelled = false
	loginNewWindowRequested(unsafe.Pointer(f))
	if !f.cancelled {
		t.Fatal("popup was not handled/blocked")
	}
}

func TestLoginBoundaryCallbackNativeLifetime(t *testing.T) {
	called := false
	marker := 42
	callback := &loginBoundaryCallback{vtbl: &loginBoundaryVtable, fn: func(args unsafe.Pointer) uintptr {
		called = args == unsafe.Pointer(&marker)
		return 0
	}}
	callback.refs.Store(1)
	iid := windows.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	var out unsafe.Pointer
	hr, _, _ := syscall.SyscallN(loginBoundaryVtable[0], uintptr(unsafe.Pointer(callback)), uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&out)))
	if hr != 0 || out != unsafe.Pointer(callback) || callback.refs.Load() != 2 {
		t.Fatalf("QI hr=%x out=%p refs=%d", hr, out, callback.refs.Load())
	}
	syscall.SyscallN(loginBoundaryVtable[3], uintptr(unsafe.Pointer(callback)), 0, uintptr(unsafe.Pointer(&marker)))
	syscall.SyscallN(loginBoundaryVtable[2], uintptr(unsafe.Pointer(callback)))
	if !called || callback.refs.Load() != 1 {
		t.Fatal("callback invocation/refcount failed")
	}
	iid.Data1 = 1
	hr, _, _ = syscall.SyscallN(loginBoundaryVtable[0], uintptr(unsafe.Pointer(callback)), uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&out)))
	if hr != 0x80004002 || out != nil {
		t.Fatal("unsupported interface accepted")
	}
}
