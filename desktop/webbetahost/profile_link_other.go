//go:build !windows

package webbetahost

import "os"

func profileLink(info os.FileInfo) bool { return info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 }
