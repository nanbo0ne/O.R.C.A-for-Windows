//go:build windows

package webbetahost

import (
	"os"
	"syscall"
	"testing"
)

type profileInfoFixture struct {
	os.FileInfo
	attr any
}

func (profileInfoFixture) Mode() os.FileMode { return os.ModeDir }
func (i profileInfoFixture) Sys() any        { return i.attr }

func TestProfileRejectsAllReparseAttributes(t *testing.T) {
	if profileLink(profileInfoFixture{attr: &syscall.Win32FileAttributeData{}}) {
		t.Fatal("ordinary directory rejected")
	}
	for _, attr := range []any{nil, &syscall.Win32FileAttributeData{FileAttributes: syscall.FILE_ATTRIBUTE_REPARSE_POINT}} {
		if !profileLink(profileInfoFixture{attr: attr}) {
			t.Fatal("unknown/reparse identity accepted")
		}
	}
}
