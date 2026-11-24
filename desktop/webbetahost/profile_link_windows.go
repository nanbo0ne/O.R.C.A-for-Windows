//go:build windows

package webbetahost

import (
	"os"
	"syscall"
)

func profileLink(info os.FileInfo) bool {
	attr, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !ok || attr.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
