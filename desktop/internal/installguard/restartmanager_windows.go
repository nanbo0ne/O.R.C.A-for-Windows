package installguard

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type rmProcess struct {
	PID     uint32
	Started windows.Filetime
}
type rmInfo struct {
	Process     rmProcess
	Name        [256]uint16
	Service     [64]uint16
	Type        uint32
	Status      uint32
	Session     uint32
	Restartable int32
}

// Restart Manager is diagnostic only. We never call RmShutdown.
func lockOwners(file string) string {
	dll := windows.NewLazySystemDLL("rstrtmgr.dll")
	start, register, list, end := dll.NewProc("RmStartSession"), dll.NewProc("RmRegisterResources"), dll.NewProc("RmGetList"), dll.NewProc("RmEndSession")
	var session uint32
	var key [33]uint16
	r, _, _ := start.Call(uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(&key[0])))
	if r != 0 {
		return ""
	}
	defer end.Call(uintptr(session))
	path, _ := windows.UTF16PtrFromString(file)
	r, _, _ = register.Call(uintptr(session), 1, uintptr(unsafe.Pointer(&path)), 0, 0, 0, 0)
	if r != 0 {
		return ""
	}
	var needed, count, reasons uint32
	r, _, _ = list.Call(uintptr(session), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)), 0, uintptr(unsafe.Pointer(&reasons)))
	if r != uintptr(windows.ERROR_MORE_DATA) || needed == 0 || needed > 256 {
		return ""
	}
	items := make([]rmInfo, needed)
	count = needed
	r, _, _ = list.Call(uintptr(session), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&items[0])), uintptr(unsafe.Pointer(&reasons)))
	if r != 0 {
		return ""
	}
	var names []string
	for _, item := range items[:count] {
		names = append(names, fmt.Sprintf("%s (PID %d)", windows.UTF16ToString(item.Name[:]), item.Process.PID))
	}
	return strings.Join(names, ", ")
}
