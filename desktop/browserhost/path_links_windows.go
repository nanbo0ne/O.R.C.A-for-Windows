//go:build windows

package browserhost

import (
	"os"

	"golang.org/x/sys/windows"
)

func browserPathLink(path string, _ os.FileInfo) (bool, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attrs, err := windows.GetFileAttributes(name)
	return attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0, err
}
