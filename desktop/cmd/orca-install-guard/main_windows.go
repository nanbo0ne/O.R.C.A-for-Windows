//go:build windows

package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/installguard"
	"golang.org/x/sys/windows"
)

func main() { os.Exit(run()) }

func run() int {
	dir := flag.String("dir", "", "installation directory")
	manifest := flag.String("manifest", "", "relative payload file list")
	status := flag.String("status", "", "private installer status INI")
	logPath := flag.String("log", "", "diagnostic JSONL")
	cancelPath := flag.String("cancel", "", "cancellation signal file")
	parent := flag.Uint("parent", 0, "installer PID")
	flag.Parse()
	log, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return installguard.DetectionFailed
	}
	defer log.Close()
	publish := func(r installguard.Result) {
		_ = json.NewEncoder(log).Encode(r)
		_ = log.Sync()
		_ = writeStatus(*status, r)
	}
	ctx, stop := context.WithTimeout(context.Background(), 12*time.Second)
	defer stop()
	var handle windows.Handle
	if *parent != 0 {
		handle, err = windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(*parent))
		if err != nil {
			publish(installguard.Result{Code: installguard.DetectionFailed, Phase: "parent", Message: "安装器进程不可用"})
			return installguard.DetectionFailed
		}
		defer windows.CloseHandle(handle)
		var parentCreated, selfCreated, exited, kernel, user windows.Filetime
		parentErr := windows.GetProcessTimes(handle, &parentCreated, &exited, &kernel, &user)
		selfErr := windows.GetProcessTimes(windows.CurrentProcess(), &selfCreated, &exited, &kernel, &user)
		if parentErr != nil || selfErr != nil || parentCreated.Nanoseconds() > selfCreated.Nanoseconds() {
			publish(installguard.Result{Code: installguard.DetectionFailed, Phase: "parent", Message: "安装器进程身份已变化，请重新打开安装器"})
			return installguard.DetectionFailed
		}
	}
	go func() {
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if *cancelPath != "" {
					if _, err := os.Stat(*cancelPath); err == nil {
						stop()
						return
					}
				}
				if handle != 0 {
					if state, _ := windows.WaitForSingleObject(handle, 0); state == windows.WAIT_OBJECT_0 {
						stop()
						return
					}
				}
			}
		}
	}()
	data, err := os.ReadFile(*manifest)
	if err != nil {
		publish(installguard.Result{Code: installguard.DetectionFailed, Phase: "manifest", Message: "安装文件清单缺失，请重新下载安装器"})
		return installguard.DetectionFailed
	}
	var files []string
	for _, line := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		if rel := strings.TrimSpace(line); rel != "" {
			files = append(files, rel)
		}
	}
	result := installguard.Run(ctx, installguard.Options{Directory: *dir, Files: files, Progress: publish})
	publish(result)
	return result.Code
}

func writeStatus(path string, r installguard.Result) error {
	clean := func(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }
	s := fmt.Sprintf("[guard]\r\ncode=%d\r\nphase=%s\r\nmessage=%s\r\nfile=%s\r\nowners=%s\r\nsystem_error=%d\r\nelapsed_ms=%d\r\n", r.Code, clean(r.Phase), clean(r.Message), clean(r.File), clean(r.Owners), r.SystemError, r.ElapsedMS)
	units := utf16.Encode([]rune("\ufeff" + s))
	data := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(data[i*2:], u)
	}
	if err := os.WriteFile(path+".tmp", data, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
