//go:build windows

package browserhost

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

	wv "github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost/internal/webviewabi"
	"golang.org/x/sys/windows"
)

const rawPointerMarker int64 = 0x1a2b3c4d5e6f7081
const rawPointerText = "download-\u6d4b\u8bd5-\U0001f600-file.bin"

var (
	rawPointerOperation wv.ICoreWebView2DownloadOperation
	rawPointerView13    wv.View13
	rawPointerProfile2  wv.Profile2
	rawPointerI64       = wv.ComProc(syscall.NewCallback(rawWriteI64AfterGC))
	rawPointerI32       = wv.ComProc(syscall.NewCallback(rawWriteI32AfterGC))
	rawPointerObject    = wv.ComProc(syscall.NewCallback(rawWriteObjectAfterGC))
	rawPointerQI        = wv.ComProc(syscall.NewCallback(rawQueryInterfaceAfterGC))
	rawPointerString    = wv.ComProc(syscall.NewCallback(rawReadStringAfterGC))
)

//go:noinline
func rawGrowCallbackStack(depth int) {
	var padding [2048]byte
	padding[depth] = byte(depth)
	if depth == 0 {
		runtime.GC()
		runtime.Gosched()
	} else {
		rawGrowCallbackStack(depth - 1)
	}
	runtime.KeepAlive(&padding)
}

func rawWriteI64AfterGC(self uintptr, out *int64) uintptr {
	address := uintptr(unsafe.Pointer(out))
	rawGrowCallbackStack(48)
	if uintptr(unsafe.Pointer(out)) != address {
		return 0x80004005
	}
	*out = rawPointerMarker
	return 0
}

func rawWriteI32AfterGC(self uintptr, out *int32) uintptr {
	address := uintptr(unsafe.Pointer(out))
	rawGrowCallbackStack(48)
	if uintptr(unsafe.Pointer(out)) != address {
		return 0x80004005
	}
	*out = 1
	return 0
}

func rawWriteObjectAfterGC(self uintptr, out **wv.ICoreWebView2DownloadOperation) uintptr {
	address := uintptr(unsafe.Pointer(out))
	rawGrowCallbackStack(48)
	if uintptr(unsafe.Pointer(out)) != address {
		return 0x80004005
	}
	*out = &rawPointerOperation
	return 0
}

func rawQueryInterfaceAfterGC(self uintptr, iid *windows.GUID, out *unsafe.Pointer) uintptr {
	iidAddress, outAddress := uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(out))
	rawGrowCallbackStack(48)
	if uintptr(unsafe.Pointer(iid)) != iidAddress || uintptr(unsafe.Pointer(out)) != outAddress {
		return 0x80004005
	}
	switch *iid {
	case wv.IIDView13:
		*out = unsafe.Pointer(&rawPointerView13)
	case wv.IIDProfile2:
		*out = unsafe.Pointer(&rawPointerProfile2)
	default:
		return 0x80004002
	}
	return 0
}

func rawReadStringAfterGC(self uintptr, text *uint16) uintptr {
	address := uintptr(unsafe.Pointer(text))
	rawGrowCallbackStack(48)
	if uintptr(unsafe.Pointer(text)) != address || windows.UTF16PtrToString(text) != rawPointerText {
		return 0x80004005
	}
	return 0
}

//go:noinline
func rawScalarPointerCall() error {
	var bytes int64
	if err := raw(nil, rawPointerI64, uintptr(unsafe.Pointer(&bytes))); err != nil || bytes != rawPointerMarker {
		return fmt.Errorf("INT64 out pointer: value=%x error=%v", bytes, err)
	}
	var state int32
	if err := raw(nil, rawPointerI32, uintptr(unsafe.Pointer(&state))); err != nil || state != 1 {
		return fmt.Errorf("state out pointer: value=%d error=%v", state, err)
	}
	if !readBool(nil, rawPointerI32) {
		return fmt.Errorf("BOOL out pointer lost after callback reentry")
	}
	return nil
}

//go:noinline
func rawInterfacePointerCall() error {
	var operation *wv.ICoreWebView2DownloadOperation
	if err := raw(nil, rawPointerObject, uintptr(unsafe.Pointer(&operation))); err != nil || operation != &rawPointerOperation {
		return fmt.Errorf("download operation out=%p error=%v", operation, err)
	}
	// Use local IID values too, so both incoming and outgoing stack-derived
	// pointers must survive a reentrant COM callback.
	iid := wv.IIDView13
	var view *wv.View13
	if err := raw(nil, rawPointerQI, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&view))); err != nil || view != &rawPointerView13 {
		return fmt.Errorf("View13 QI out=%p error=%v", view, err)
	}
	iid = wv.IIDProfile2
	var profile *wv.Profile2
	if err := raw(nil, rawPointerQI, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&profile))); err != nil || profile != &rawPointerProfile2 {
		return fmt.Errorf("Profile2 QI out=%p error=%v", profile, err)
	}
	return nil
}

func TestRawPointerLiveness(t *testing.T) {
	for _, test := range []struct {
		name string
		call func() error
	}{
		{"transfer-scalars-and-BOOL", rawScalarPointerCall},
		{"transfer-and-profile-interfaces", rawInterfacePointerCall},
		{"temporary-UTF16", func() error {
			return raw(nil, rawPointerString, uintptr(unsafe.Pointer(ptr(rawPointerText))))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for i := 0; i < 12; i++ {
				done := make(chan error, 1)
				go func() { done <- test.call() }()
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("raw COM callback did not return")
				}
			}
		})
	}
}

func TestRawPointerEscapeBoundary(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(here), "native_windows.go"), nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "raw" || function.Doc == nil {
			continue
		}
		for _, comment := range function.Doc.List {
			found = found || comment.Text == "//go:uintptrescapes"
		}
	}
	if !found {
		t.Fatal("raw lacks //go:uintptrescapes")
	}
	// Compile-time assertion: ptr must retain a typed pointer, not erase the
	// reference before the annotated call expression can see its conversion.
	var typedUTF16 func(string) *uint16 = ptr
	if typedUTF16("") == nil {
		t.Fatal("empty UTF16 string did not retain its terminator")
	}
}
