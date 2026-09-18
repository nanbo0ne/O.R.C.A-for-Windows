package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/installguard"
	"golang.org/x/sys/windows"
)

func TestStatusPublishingWaitsForShortLivedReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.ini")
	if err := writeStatus(path, installguard.Result{Code: -1, Message: "checking"}); err != nil {
		t.Fatal(err)
	}
	p, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { time.Sleep(40 * time.Millisecond); windows.CloseHandle(h); close(done) }()
	err = writeStatus(path, installguard.Result{Code: 0, Phase: "ready", Message: "检查完成", File: "中文目录"})
	<-done
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xfe {
		t.Fatal("NSIS status must be UTF-16LE")
	}
	units := make([]uint16, (len(data)-2)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[2+i*2:])
	}
	text := string(utf16.Decode(units))
	if !strings.Contains(text, "code=0\r\n") || !strings.Contains(text, "检查完成") || !strings.Contains(text, "中文目录") {
		t.Fatalf("incomplete final status: %q", text)
	}
}
