//go:build windows

package webbetahost

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

func ownBrowserProcess(c *edge.Chromium) (windows.Handle, error) {
	view, err := c.GetController().GetCoreWebView2()
	if err != nil {
		return 0, err
	}
	defer view.Release()
	var pid uint32
	table := *(**[38]uintptr)(unsafe.Pointer(view))
	// ICoreWebView2::get_BrowserProcessId (fixed base interface, slot 37).
	hr, _, _ := syscall.SyscallN(table[37], uintptr(unsafe.Pointer(view)), uintptr(unsafe.Pointer(&pid)))
	if int32(hr) < 0 || pid == 0 {
		return 0, errors.New("cannot identify owned WebView2 process")
	}
	return windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
}

func releaseChromium(c *edge.Chromium) error {
	c.ShuttingDown()
	var closeErr error
	if controller := c.GetController(); controller != nil {
		view, err := controller.GetCoreWebView2()
		table := *(**[26]uintptr)(unsafe.Pointer(controller))
		hr, _, _ := syscall.SyscallN(table[24], uintptr(unsafe.Pointer(controller)))
		if int32(hr) < 0 {
			closeErr = fmt.Errorf("WebView2 controller close: 0x%x", hr)
		}
		if err == nil {
			view.Release() // GetCoreWebView2 reference acquired above.
			view.Release() // go-webview2 v1.0.23 Chromium's retained webview reference.
		}
		controller.Release()
	}
	if env := c.Environment(); env != nil {
		table := *(**[3]uintptr)(unsafe.Pointer(env))
		syscall.SyscallN(table[2], uintptr(unsafe.Pointer(env)))
	}
	return closeErr
}

// No process name matching or termination: only the handle obtained from this
// controller is waited upon. Timeout forbids deletion and identity recreation.
func (b *Browser) CloseAndWait(ctx context.Context) error {
	b.Close()
	select {
	case <-b.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closeError != nil {
		return b.closeError
	}
	if b.process == 0 {
		return nil
	}
	for {
		status, err := windows.WaitForSingleObject(b.process, 0)
		if err != nil {
			return err
		}
		if status == windows.WAIT_OBJECT_0 {
			windows.CloseHandle(b.process)
			b.process = 0
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}
