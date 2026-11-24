//go:build !windows

package fileutil

import "os"

func renameReplace(tmp, dest string, validate func() error) error {
	if validate != nil {
		if err := validate(); err != nil {
			return err
		}
	}
	return os.Rename(tmp, dest)
}
