//go:build windows

package browserhost

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Pin from the drive root down so an already-checked ancestor cannot be
// renamed or replaced while the next directory is opened. Keep these handles
// until publication/cleanup, not merely until approval returns.
func pinDownloadParents(parent string) ([]windows.Handle, error) {
	var directories []string
	for dir := parent; ; dir = filepath.Dir(dir) {
		directories = append(directories, dir)
		if filepath.Dir(dir) == dir {
			break
		}
	}
	var handles []windows.Handle
	for i := len(directories) - 1; i >= 0; i-- {
		handle, err := pinDownloadDirectory(directories[i])
		if err != nil {
			releaseDownloadDirectories(handles)
			return nil, err
		}
		handles = append(handles, handle)
	}
	return handles, nil
}

func pinDownloadDirectory(path string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	// Do not follow the final component. Denying delete sharing prevents
	// directory rename/replacement. Write sharing is necessary for the browser
	// and MoveFile to rename child files; attributes are rechecked at publish.
	handle, err := windows.CreateFile(name, windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return 0, err
	}
	var info windows.ByHandleFileInformation
	err = windows.GetFileInformationByHandle(handle, &info)
	if err == nil && (info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0) {
		err = errors.New("download directory must not be a reparse point")
	}
	if err != nil {
		_ = windows.CloseHandle(handle)
		return 0, err
	}
	return handle, nil
}

func releaseDownloadDirectories(handles []windows.Handle) {
	for i := len(handles) - 1; i >= 0; i-- {
		_ = windows.CloseHandle(handles[i])
	}
}
