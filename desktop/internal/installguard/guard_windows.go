//go:build windows

// Package installguard implements the installer's local, path-scoped preflight.
package installguard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/installipc"
	"golang.org/x/sys/windows"
)

const (
	Ready           = 0
	Running         = 20
	Locked          = 21
	NotWritable     = 22
	DetectionFailed = 23
	Cancelled       = 24
)

type Result struct {
	Code        int    `json:"code"`
	Phase       string `json:"phase"`
	Message     string `json:"message"`
	File        string `json:"file,omitempty"`
	Owners      string `json:"owners,omitempty"`
	SystemError uint32 `json:"system_error,omitempty"`
	ElapsedMS   int64  `json:"elapsed_ms"`
}

type Options struct {
	Directory string
	Files     []string
	Progress  func(Result)
}

type process struct {
	pid, parent uint32
	path        string
	created     windows.Filetime
	handle      windows.Handle
}

func failure(code int, phase, message, path string, err error) Result {
	r := Result{Code: code, Phase: phase, Message: message, File: path}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		r.SystemError = uint32(errno)
	}
	return r
}

// Resolve the existing prefix too, so junction aliases cannot broaden ownership.
func canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	prefix, suffix := filepath.Clean(abs), ""
	for {
		ptr, e := windows.UTF16PtrFromString(prefix)
		if e != nil {
			return "", e
		}
		handle, err := windows.CreateFile(ptr, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if err == nil {
			buf := make([]uint16, 32768)
			n, finalErr := windows.GetFinalPathNameByHandle(handle, &buf[0], uint32(len(buf)), 0)
			windows.CloseHandle(handle)
			if finalErr != nil {
				return "", finalErr
			}
			if n >= uint32(len(buf)) {
				return "", windows.ERROR_FILENAME_EXCED_RANGE
			}
			resolved := windows.UTF16ToString(buf[:n])
			if strings.HasPrefix(resolved, `\\?\UNC\`) {
				resolved = `\\` + strings.TrimPrefix(resolved, `\\?\UNC\`)
			} else {
				resolved = strings.TrimPrefix(resolved, `\\?\`)
			}
			return filepath.Join(resolved, suffix), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		if info, statErr := os.Lstat(prefix); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("unresolved reparse point: %s", prefix)
			}
			if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok && data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
				return "", fmt.Errorf("unresolved reparse point: %s", prefix)
			}
		}
		parent := filepath.Dir(prefix)
		if parent == prefix {
			return "", err
		}
		suffix = filepath.Join(filepath.Base(prefix), suffix)
		prefix = parent
	}
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func imageIdentity(handle windows.Handle) (string, windows.Filetime, error) {
	return imageIdentityWithResolver(handle, canonical)
}

func imageIdentityWithResolver(handle windows.Handle, resolve func(string) (string, error)) (string, windows.Filetime, error) {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return "", created, err
	}
	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size); err != nil {
		return "", created, err
	}
	path, err := resolve(windows.UTF16ToString(buf[:size]))
	return path, created, err
}

func alive(p process) bool {
	result, err := windows.WaitForSingleObject(p.handle, 0)
	return err != nil || result != windows.WAIT_OBJECT_0
}

func sameIdentity(p process, path string, created windows.Filetime) bool {
	return strings.EqualFold(p.path, path) && p.created == created
}

func ownedProcesses(dir string) ([]process, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	resolvedPaths := map[string]string{}
	resolve := func(path string) (string, error) {
		if value, ok := resolvedPaths[path]; ok {
			return value, nil
		}
		value, err := canonical(path)
		if err == nil {
			resolvedPaths[path] = value
		}
		return value, err
	}
	var candidates []process
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		name := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
		switch name {
		case "orca.exe", "deepseek-orca-desktop.exe", "node.exe", "cmd.exe", "conhost.exe":
		default:
			continue
		}
		if entry.ProcessID == uint32(os.Getpid()) {
			continue
		}
		handle, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, entry.ProcessID)
		if e != nil {
			continue
		} // Unknown ownership never grants permission to terminate.
		path, created, e := imageIdentityWithResolver(handle, resolve)
		if e != nil {
			windows.CloseHandle(handle)
			continue
		}
		if !strings.EqualFold(filepath.Base(path), name) {
			windows.CloseHandle(handle)
			continue
		}
		// Parent identity must come from this handle, not the older Toolhelp entry.
		var basic windows.PROCESS_BASIC_INFORMATION
		var parent uint32
		if e = windows.NtQueryInformationProcess(handle, windows.ProcessBasicInformation, unsafe.Pointer(&basic), uint32(unsafe.Sizeof(basic)), nil); e == nil {
			parent = uint32(basic.InheritedFromUniqueProcessId)
		}
		candidates = append(candidates, process{entry.ProcessID, parent, path, created, handle})
	}
	if err != windows.ERROR_NO_MORE_FILES {
		for _, p := range candidates {
			windows.CloseHandle(p.handle)
		}
		return nil, err
	}
	owned := map[uint32]process{}
	for _, p := range candidates {
		for _, rel := range []string{"Orca.exe", "deepseek-orca-desktop.exe", "node.exe", filepath.Join("codegraph", "node.exe")} {
			if strings.EqualFold(p.path, filepath.Join(dir, rel)) {
				owned[p.pid] = p
			}
		}
	}
	// Only a live, older, positively identified parent proves a runtime descendant.
	for changed := true; changed; {
		changed = false
		for _, p := range candidates {
			if _, ok := owned[p.pid]; ok {
				continue
			}
			switch strings.ToLower(filepath.Base(p.path)) {
			case "node.exe", "cmd.exe", "conhost.exe":
			default:
				continue
			}
			parent, ok := owned[p.parent]
			if ok && parent.created.Nanoseconds() <= p.created.Nanoseconds() && alive(parent) {
				owned[p.pid] = p
				changed = true
			}
		}
	}
	result := make([]process, 0, len(owned))
	for _, p := range candidates {
		if _, ok := owned[p.pid]; ok {
			result = append(result, p)
		} else {
			windows.CloseHandle(p.handle)
		}
	}
	return result, nil
}

var user32 = windows.NewLazySystemDLL("user32.dll")
var enumWindows = user32.NewProc("EnumWindows")
var getWindowPID = user32.NewProc("GetWindowThreadProcessId")
var postMessage = user32.NewProc("PostMessageW")

func closeWindows(processes []process) {
	pids := map[uint32]bool{}
	for _, p := range processes {
		if alive(p) {
			pids[p.pid] = true
		}
	}
	callback := syscall.NewCallback(func(hwnd, param uintptr) uintptr {
		var pid uint32
		getWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pids[pid] {
			postMessage.Call(hwnd, 0x0010, 0, 0)
		}
		return 1
	})
	enumWindows.Call(callback, 0)
}

func remaining(processes []process) []process {
	var result []process
	for _, p := range processes {
		if alive(p) {
			result = append(result, p)
		}
	}
	return result
}

func wait(ctx context.Context, processes []process, duration time.Duration) []process {
	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		left := remaining(processes)
		if len(left) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return left
		case <-deadline.C:
			return left
		case <-tick.C:
		}
	}
}

func terminate(p process) error {
	if !alive(p) {
		return nil
	}
	// Open a terminate handle only after ownership is known; then recheck identity.
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, p.pid)
	if err != nil {
		if !alive(p) {
			return nil
		}
		return err
	}
	defer windows.CloseHandle(handle)
	path, created, err := imageIdentity(handle)
	if err != nil {
		return err
	}
	if !sameIdentity(p, path, created) {
		return errors.New("process identity changed")
	}
	return windows.TerminateProcess(handle, 0)
}

// Run never waits if no owned processes exist. File checks still protect against
// an unreadable process, a third-party lock, and an inaccessible destination.
func Run(ctx context.Context, options Options) (result Result) {
	started := time.Now()
	defer func() { result.ElapsedMS = time.Since(started).Milliseconds() }()
	emit := func(phase, message string) {
		if options.Progress != nil {
			options.Progress(Result{Code: -1, Phase: phase, Message: message, ElapsedMS: time.Since(started).Milliseconds()})
		}
	}
	emit("detect", "正在检查安装位置")
	if !filepath.IsAbs(options.Directory) || strings.ContainsAny(options.Directory, "\"\r\n\x00") {
		return failure(DetectionFailed, "detect", "未指定安装目录", "", nil)
	}
	dir, err := canonical(options.Directory)
	if err != nil {
		return failure(NotWritable, "directory", "无法访问安装目录，请调整安装位置", options.Directory, err)
	}
	if filepath.Dir(dir) == dir {
		return failure(NotWritable, "directory", "不能安装到磁盘根目录", dir, nil)
	}
	files, err := validateFiles(dir, options.Files)
	if err != nil {
		return failure(DetectionFailed, "manifest", "安装文件清单无效，请重新下载安装器", dir, err)
	}
	processes, err := ownedProcesses(dir)
	if err != nil {
		processes, err = ownedProcesses(dir)
	}
	if err != nil {
		return failure(DetectionFailed, "detect", "进程检查失败，请重新检查", "", err)
	}
	defer func() {
		for _, p := range processes {
			windows.CloseHandle(p.handle)
		}
	}()
	if len(remaining(processes)) > 0 {
		if ctx.Err() != nil {
			return failure(Cancelled, "cancelled", "已取消检查，未覆盖安装文件", "", ctx.Err())
		}
		emit("closing", "正在保存并退出此目录的 O.R.C.A.")
		if ctx.Err() != nil {
			return failure(Cancelled, "cancelled", "已取消检查，未覆盖安装文件", "", ctx.Err())
		}
		requested, _ := installipc.RequestShutdown(dir)
		if !requested {
			closeWindows(processes)
		}
		left := wait(ctx, processes, 5*time.Second)
		if ctx.Err() != nil {
			return failure(Cancelled, "cancelled", "已取消检查，未覆盖安装文件", "", ctx.Err())
		}
		if len(left) > 0 {
			emit("stopping", "正在结束此目录的残留进程")
			for i := len(left) - 1; i >= 0; i-- {
				if ctx.Err() != nil {
					return failure(Cancelled, "cancelled", "已取消检查，未覆盖安装文件", "", ctx.Err())
				}
				if err := terminate(left[i]); err != nil && alive(left[i]) {
					return failure(Running, "stopping", fmt.Sprintf("进程仍在运行（PID %d），请退出后重新检查", left[i].pid), left[i].path, err)
				}
			}
			left = wait(ctx, left, 2*time.Second)
			if ctx.Err() != nil {
				return failure(Cancelled, "cancelled", "已取消检查，未覆盖安装文件", "", ctx.Err())
			}
			if len(left) > 0 {
				return failure(Running, "stopping", "进程尚未退出，请重新检查", left[0].path, nil)
			}
		}
	}
	if ctx.Err() != nil {
		return failure(Cancelled, "cancelled", "已取消检查，未覆盖安装文件", "", ctx.Err())
	}
	emit("files", "正在检查文件与目录权限")
	if blocked := probeFiles(ctx, dir, files); blocked.Code != Ready {
		return blocked
	}
	return Result{Code: Ready, Phase: "ready", Message: "检查完成，可以安装"}
}

func validateFiles(dir string, relative []string) ([]string, error) {
	if len(relative) == 0 {
		return nil, errors.New("empty file manifest")
	}
	result := make([]string, 0, len(relative))
	for _, rel := range relative {
		if rel == "" || filepath.IsAbs(rel) || strings.ContainsAny(rel, ":\r\n\x00") {
			return nil, fmt.Errorf("invalid relative file %q", rel)
		}
		file, err := canonical(filepath.Join(dir, rel))
		if err != nil {
			return nil, err
		}
		if !inside(dir, file) || strings.EqualFold(dir, file) {
			return nil, fmt.Errorf("file escapes install directory: %q", rel)
		}
		result = append(result, file)
	}
	return result, nil
}

func probeFiles(ctx context.Context, dir string, files []string) Result {
	directories := map[string]bool{dir: true}
	for _, file := range files {
		if ctx.Err() != nil {
			return failure(Cancelled, "cancelled", "已取消检查", "", ctx.Err())
		}
		directories[filepath.Dir(file)] = true
		info, err := os.Stat(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return failure(NotWritable, "files", "无法访问安装文件，请检查目录权限", file, err)
		}
		if info.IsDir() {
			return failure(NotWritable, "files", "目标文件位置被同名目录占用，请调整安装位置", file, nil)
		}
		ptr, _ := windows.UTF16PtrFromString(file)
		handle, err := windows.CreateFile(ptr, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if err == nil {
			windows.CloseHandle(handle)
			continue
		}
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			r := failure(Locked, "files", "文件被其他程序占用，请关闭占用程序后重新检查", file, err)
			r.Owners = lockOwners(file)
			return r
		}
		return failure(NotWritable, "files", "文件不可替换，请检查权限或调整安装位置", file, err)
	}
	checked := map[string]bool{}
	for target := range directories {
		ancestor := target
		for {
			_, err := os.Stat(ancestor)
			if err == nil {
				break
			}
			if !os.IsNotExist(err) {
				return failure(NotWritable, "directory", "目录不可写，请调整安装位置", ancestor, err)
			}
			next := filepath.Dir(ancestor)
			if next == ancestor {
				return failure(NotWritable, "directory", "安装目录不可用", target, err)
			}
			ancestor = next
		}
		if checked[ancestor] {
			continue
		}
		checked[ancestor] = true
		f, err := os.CreateTemp(ancestor, ".orca-install-check-*")
		if err != nil {
			return failure(NotWritable, "directory", "目录不可写，请调整安装位置", ancestor, err)
		}
		name := f.Name()
		closeErr := f.Close()
		removeErr := os.Remove(name)
		if closeErr != nil {
			return failure(NotWritable, "directory", "目录写入检查失败", ancestor, closeErr)
		}
		if removeErr != nil {
			return failure(NotWritable, "directory", "目录删除权限不足，请调整安装位置", ancestor, removeErr)
		}
	}
	return Result{Code: Ready}
}
