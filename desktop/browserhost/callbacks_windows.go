//go:build windows

package browserhost

import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Native pointers are not GC roots. Keep each object rooted until both its
// creator and COM have released their references, even after host shutdown.
var callbackRoots sync.Map

type comCallback struct {
	vtbl *[4]uintptr
	refs atomic.Int32
	mu   sync.Mutex
	fn   func(uintptr, uintptr) uintptr
	once bool
}

// Release the creator's reference after registration/dispatch returns. COM must
// AddRef any handler it retains; shared handlers may keep their creator reference
// until their final registration is removed. Cancellation is not a Release.
func newCOMCallback(once bool, fn func(uintptr, uintptr) uintptr) *comCallback {
	c := &comCallback{vtbl: &callbackVtbl, fn: fn, once: once}
	c.refs.Store(1)
	callbackRoots.Store(c.pointer(), c)
	return c
}

func (c *comCallback) pointer() uintptr { return uintptr(unsafe.Pointer(c)) }

func (c *comCallback) release() uint32 {
	n := c.refs.Add(-1)
	if n == 0 {
		c.mu.Lock()
		c.fn = nil
		c.mu.Unlock()
		callbackRoots.Delete(c.pointer())
	}
	return uint32(n)
}

func (c *comCallback) invoke(a, b uintptr) uintptr {
	c.mu.Lock()
	fn := c.fn
	if c.once {
		c.fn = nil
	}
	c.mu.Unlock()
	var result uintptr
	if fn != nil {
		result = fn(a, b)
	}
	runtime.KeepAlive(c)
	return result
}

var callbackVtbl = [4]uintptr{
	syscall.NewCallback(func(self *comCallback, iid *windows.GUID, out *unsafe.Pointer) uintptr {
		if out == nil || iid == nil {
			return 0x80004003
		}
		*out = nil
		unknown := windows.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
		if *iid != unknown {
			return 0x80004002
		}
		*out = unsafe.Pointer(self)
		self.refs.Add(1)
		return 0
	}),
	syscall.NewCallback(func(self *comCallback) uintptr { return uintptr(self.refs.Add(1)) }),
	syscall.NewCallback(func(self *comCallback) uintptr { return uintptr(self.release()) }),
	syscall.NewCallback(func(self *comCallback, a, b uintptr) uintptr { return self.invoke(a, b) }),
}
