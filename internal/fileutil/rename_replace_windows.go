package fileutil

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func renameReplace(tmp, dest string, validate func() error) error {
	return renameReplaceWithRetry(tmp, dest, os.Rename, time.Sleep, validate)
}

// Ordinary Windows readers may omit delete sharing. Allow them to close, but
// preserve the last rename error and both files if the obstruction persists.
func renameReplaceWithRetry(tmp, dest string, rename func(string, string) error, sleep func(time.Duration), validate func() error) error {
	var err error
	for attempt := 0; attempt < 7; attempt++ {
		if validate != nil {
			if err := validate(); err != nil {
				return err
			}
		}
		err = rename(tmp, dest)
		if err == nil || (!errors.Is(err, windows.ERROR_SHARING_VIOLATION) &&
			!errors.Is(err, windows.ERROR_LOCK_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED)) {
			return err
		}
		if attempt < 6 {
			sleep(10 * time.Millisecond * time.Duration(1<<attempt))
		}
	}
	return err
}
