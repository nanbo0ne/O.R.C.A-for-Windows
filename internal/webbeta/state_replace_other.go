//go:build !windows

package webbeta

import "os"

func replaceState(from, to string) error { return os.Rename(from, to) }
