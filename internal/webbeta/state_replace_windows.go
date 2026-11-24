//go:build windows

package webbeta

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func replaceState(from, to string) error {
	return replaceStateWithRetry(from, to, os.Rename, time.Sleep)
}

// Windows readers may briefly prevent replacement. Retry only the same atomic
// rename, never remove the old checkpoint and never replay the browser request.
func replaceStateWithRetry(from, to string, rename func(string, string) error, sleep func(time.Duration)) error {
	var err error
	for attempt := 0; attempt < 7; attempt++ {
		err = rename(from, to)
		if err == nil {
			return nil
		}
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_LOCK_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return err
		}
		if attempt < 6 {
			sleep(10 * time.Millisecond * time.Duration(1<<attempt))
		}
	}
	return err
}
