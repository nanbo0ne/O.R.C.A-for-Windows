//go:build windows

package webviewabi

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

const abiPointerMarker = uint64(0x1a2b3c4d5e6f7081)

var (
	abiPointerView        ICoreWebView2
	abiPointerSettings    ICoreWebView2Settings
	abiPointerWrite       = ComProc(syscall.NewCallback(abiWriteAfterGC))
	abiPointerGetView     = ComProc(syscall.NewCallback(abiGetViewAfterGC))
	abiPointerGetSettings = ComProc(syscall.NewCallback(abiGetSettingsAfterGC))
	abiPointerEmpty       = ComProc(syscall.NewCallback(func(uintptr, uintptr) uintptr { return 0 }))
)

// Retain the numeric address as well as the typed callback argument. If a
// wrapper lets the output live on a moving stack, detect relocation before
// dereferencing it; never reconstruct a Go pointer from a saved integer.
func abiWriteAfterGC(self uintptr, out *uint64) uintptr {
	address := uintptr(unsafe.Pointer(out))
	abiGrowCallbackStack(48)
	if uintptr(unsafe.Pointer(out)) != address {
		return 0x80004005
	}
	*out = abiPointerMarker
	return 0
}

func abiGetViewAfterGC(self uintptr, out **ICoreWebView2) uintptr {
	address := uintptr(unsafe.Pointer(out))
	abiGrowCallbackStack(48)
	if uintptr(unsafe.Pointer(out)) != address {
		return 0x80004005
	}
	*out = &abiPointerView
	return 0
}

func abiGetSettingsAfterGC(self uintptr, out **ICoreWebView2Settings) uintptr {
	address := uintptr(unsafe.Pointer(out))
	abiGrowCallbackStack(48)
	if uintptr(unsafe.Pointer(out)) != address {
		return 0x80004005
	}
	*out = &abiPointerSettings
	return 0
}

//go:noinline
func abiGrowCallbackStack(depth int) {
	var padding [2048]byte
	padding[depth] = byte(depth)
	if depth == 0 {
		runtime.GC()
		runtime.Gosched()
	} else {
		abiGrowCallbackStack(depth - 1)
	}
	runtime.KeepAlive(&padding)
}

//go:noinline
func abiDirectPointerCall() error {
	var value uint64
	hr, _, _ := abiPointerWrite.Call(0, uintptr(unsafe.Pointer(&value)))
	if hr != 0 || value != abiPointerMarker {
		return fmt.Errorf("direct COM out pointer: HRESULT=%x value=%x", hr, value)
	}
	return nil
}

func TestABIPointerLiveness(t *testing.T) {
	for _, test := range []struct {
		name string
		call func() error
	}{
		{"ComProc.Call", abiDirectPointerCall},
		{"GetCoreWebView2", func() error {
			controller := ICoreWebView2Controller{Vtbl: &ICoreWebView2ControllerVtbl{GetCoreWebView2: abiPointerGetView}}
			view, err := controller.GetCoreWebView2()
			if err != nil || view != &abiPointerView {
				return fmt.Errorf("GetCoreWebView2 out=%p error=%v", view, err)
			}
			return nil
		}},
		{"GetSettings", func() error {
			view := ICoreWebView2{Vtbl: &ICoreWebView2Vtbl{GetSettings: abiPointerGetSettings}}
			settings, err := view.GetSettings()
			if err != nil || settings != &abiPointerSettings {
				return fmt.Errorf("GetSettings out=%p error=%v", settings, err)
			}
			return nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for i := 0; i < 12; i++ {
				// A fresh goroutine starts with a small stack on every iteration.
				done := make(chan error, 1)
				go func() { done <- test.call() }()
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("simulated COM call did not return")
				}
			}
		})
	}
}

func TestABIRejectsSuccessfulNilInterfaces(t *testing.T) {
	controller := ICoreWebView2Controller{Vtbl: &ICoreWebView2ControllerVtbl{GetCoreWebView2: abiPointerEmpty}}
	if view, err := controller.GetCoreWebView2(); view != nil || err == nil {
		t.Fatalf("successful nil view not rejected: %p %v", view, err)
	}
	view := ICoreWebView2{Vtbl: &ICoreWebView2Vtbl{GetSettings: abiPointerEmpty}}
	if settings, err := view.GetSettings(); settings != nil || err == nil {
		t.Fatalf("successful nil settings not rejected: %p %v", settings, err)
	}
	var missing *ICoreWebView2
	if _, err := missing.GetSettings(); err == nil {
		t.Fatal("nil receiver not rejected")
	}
}

func TestABIPointerEscapeDirectives(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(here), "abi_windows.go"), nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"Call": false, "call": false}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if _, ok := want[function.Name.Name]; !ok || function.Doc == nil {
			continue
		}
		for _, comment := range function.Doc.List {
			if comment.Text == "//go:uintptrescapes" {
				want[function.Name.Name] = true
			}
		}
	}
	for name, present := range want {
		if !present {
			t.Errorf("%s lacks //go:uintptrescapes; an inlined inner SyscallN cannot protect an outer conversion", name)
		}
	}
}
