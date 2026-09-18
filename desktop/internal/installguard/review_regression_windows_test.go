//go:build windows

package installguard

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/proc"
	"golang.org/x/sys/windows"
)

func TestGuardReviewTreeFixture(t *testing.T) {
	childPath := os.Getenv("ORCA_GUARD_REVIEW_CHILD")
	if childPath == "" {
		return
	}
	child := exec.Command(childPath, "-test.run=^TestGuardFixture$")
	child.Env = append(os.Environ(), "ORCA_GUARD_FIXTURE=hung", "ORCA_GUARD_READY="+childPath+".ready")
	child.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	if err := os.WriteFile(os.Getenv("ORCA_GUARD_REVIEW_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Second)
	}
}

type reviewTree struct {
	dir       string
	parent    *exec.Cmd
	childPID  uint32
	childPath string
	child     windows.Handle
}

func reviewStartTree(t *testing.T, childName string) reviewTree {
	t.Helper()
	base := t.TempDir()
	dir, other := filepath.Join(base, "installation"), filepath.Join(base, "other installation")
	for _, path := range []string{dir, other} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	parentPath, childPath := filepath.Join(dir, "Orca.exe"), filepath.Join(other, childName)
	for _, path := range []string{parentPath, childPath} {
		if err := os.WriteFile(path, body, 0700); err != nil {
			t.Fatal(err)
		}
	}
	pidPath := filepath.Join(dir, "child.pid")
	cmd := exec.Command(parentPath, "-test.run=^TestGuardReviewTreeFixture$")
	cmd.Env = append(os.Environ(), "ORCA_GUARD_REVIEW_CHILD="+childPath, "ORCA_GUARD_REVIEW_PID="+pidPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	// The test retains the job, so killing the fixture parent does not kill its
	// child. Cleanup still reaps both, including failures before PID publication.
	job, err := proc.StartTracked(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { proc.KillTracked(cmd, job); _ = cmd.Wait() })
	if job == 0 {
		t.Fatal("fixture needs a Windows job for bounded child cleanup")
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		data, pidErr := os.ReadFile(pidPath)
		_, readyErr := os.Stat(childPath + ".ready")
		if pidErr == nil && readyErr == nil {
			pid, err := strconv.ParseUint(string(data), 10, 32)
			if err != nil || pid == 0 {
				t.Fatalf("invalid fixture child PID %q: %v", data, err)
			}
			handle := reviewOpenProcess(t, uint32(pid))
			path, _, err := imageIdentity(handle)
			want, canonicalErr := canonical(childPath)
			if err != nil || canonicalErr != nil || !strings.EqualFold(path, want) {
				t.Fatalf("fixture child identity = %q (%v), want %q (%v)", path, err, want, canonicalErr)
			}
			return reviewTree{dir: dir, parent: cmd, childPID: uint32(pid), childPath: want, child: handle}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("fixture child startup timeout")
	return reviewTree{}
}

func reviewOpenProcess(t *testing.T, pid uint32) windows.Handle {
	t.Helper()
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { windows.CloseHandle(handle) })
	return handle
}

func reviewRequireAlive(t *testing.T, handle windows.Handle) {
	t.Helper()
	state, err := windows.WaitForSingleObject(handle, 100)
	if err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("fixture process exited: wait state=%d, error=%v", state, err)
	}
}

func reviewOwnedProcesses(t *testing.T, dir string) map[uint32]process {
	t.Helper()
	processes, err := ownedProcesses(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, p := range processes {
			windows.CloseHandle(p.handle)
		}
	})
	result := make(map[uint32]process, len(processes))
	for _, p := range processes {
		result[p.pid] = p
	}
	return result
}

func TestGuardRegressionCancellationAtStopping(t *testing.T) {
	dir := t.TempDir()
	cmd := fixture(t, dir, "hung")
	handle := reviewOpenProcess(t, uint32(cmd.Process.Pid))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reachedStopping := false
	o := options(dir)
	o.Progress = func(r Result) {
		if r.Phase == "stopping" {
			reachedStopping = true
			cancel()
		}
	}
	r := Run(ctx, o)
	if !reachedStopping {
		t.Fatalf("test did not reach stopping: %+v", r)
	}
	if r.Code != Cancelled || r.Phase != "cancelled" {
		t.Fatalf("cancel at stopping returned %+v", r)
	}
	reviewRequireAlive(t, handle)
}

func TestGuardRegressionCancellationAtClosing(t *testing.T) {
	dir := t.TempDir()
	cmd := fixture(t, dir, "graceful")
	handle := reviewOpenProcess(t, uint32(cmd.Process.Pid))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reachedClosing := false
	o := options(dir)
	o.Progress = func(r Result) {
		if r.Phase == "closing" {
			reachedClosing = true
			cancel()
		}
	}
	r := Run(ctx, o)
	if !reachedClosing || r.Code != Cancelled {
		t.Fatalf("cancel at closing returned %+v; reached closing=%v", r, reachedClosing)
	}
	reviewRequireAlive(t, handle)
}

func TestGuardRegressionAnotherInstallChildSurvives(t *testing.T) {
	tree := reviewStartTree(t, "Orca.exe")
	owned := reviewOwnedProcesses(t, tree.dir)
	parent, ok := owned[uint32(tree.parent.Process.Pid)]
	if !ok {
		t.Fatal("target installation's parent was not identified")
	}
	if child, ok := owned[tree.childPID]; ok {
		t.Fatalf("another installation's child was classified as owned: %s", child.path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if r := Run(ctx, options(tree.dir)); r.Code != Ready {
		t.Fatalf("target shutdown failed: %+v", r)
	}
	if state, err := windows.WaitForSingleObject(parent.handle, 0); err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("target parent did not exit: state=%d, error=%v", state, err)
	}
	reviewRequireAlive(t, tree.child)
}

func TestGuardRegressionLiveHandleAncestry(t *testing.T) {
	tree := reviewStartTree(t, "node.exe")
	owned := reviewOwnedProcesses(t, tree.dir)
	for _, want := range []struct {
		pid, parent uint32
		path        string
	}{
		{uint32(tree.parent.Process.Pid), uint32(os.Getpid()), filepath.Join(tree.dir, "Orca.exe")},
		{tree.childPID, uint32(tree.parent.Process.Pid), tree.childPath},
	} {
		p, ok := owned[want.pid]
		if !ok {
			t.Fatalf("owned fixture PID %d missing", want.pid)
		}
		var basic windows.PROCESS_BASIC_INFORMATION
		if err := windows.NtQueryInformationProcess(p.handle, windows.ProcessBasicInformation, unsafe.Pointer(&basic), uint32(unsafe.Sizeof(basic)), nil); err != nil {
			t.Fatal(err)
		}
		if uint32(basic.UniqueProcessId) != want.pid || uint32(basic.InheritedFromUniqueProcessId) != want.parent || p.parent != want.parent {
			t.Fatalf("PID %d: captured parent=%d, live identity=%+v, want parent=%d", p.pid, p.parent, basic, want.parent)
		}
		path, created, err := imageIdentity(p.handle)
		if err != nil || !sameIdentity(p, path, created) || !strings.EqualFold(path, want.path) {
			t.Fatalf("captured identity does not match retained handle: path=%q, created=%+v, error=%v", path, created, err)
		}
		reviewRequireAlive(t, p.handle)
	}
}

func TestGuardRegressionDanglingJunctionRejected(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "installation")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	link, outside := filepath.Join(dir, "redirect"), filepath.Join(base, "missing outside")
	cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", link, outside)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create temporary junction: %v\n%s", err, output)
	}
	defer os.Remove(link)
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("junction target must be absent: %v", err)
	}
	files := []string{filepath.Join("redirect", "payload.dat")}
	if paths, err := validateFiles(dir, files); err == nil {
		resolved, resolveErr := filepath.EvalSymlinks(link)
		t.Logf("junction mode=%s, attributes=%#v; EvalSymlinks=%q, error=%v", info.Mode(), info.Sys(), resolved, resolveErr)
		t.Fatalf("accepted dangling junction outside installation: %v", paths)
	}
	if r := Run(context.Background(), Options{Directory: dir, Files: files}); r.Code != DetectionFailed || r.Phase != "manifest" {
		t.Fatalf("dangling junction did not fail manifest validation: %+v", r)
	}
	if _, err := validateFiles(dir, []string{filepath.Join("new", "subdir", "payload.dat")}); err != nil {
		t.Fatalf("ordinary missing directories must remain valid: %v", err)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("validation touched the junction target: %v", err)
	}
}

func TestGuardRegressionMappedFileIsPreflightBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.dat")
	const contents = "temporary mapped payload"
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := windows.CreateFileMapping(windows.Handle(f.Fd()), nil, windows.PAGE_READONLY, 0, 0, nil)
	if err != nil {
		f.Close()
		t.Fatal(err)
	}
	defer windows.CloseHandle(mapping)
	view, mapErr := windows.MapViewOfFile(mapping, windows.FILE_MAP_READ, 0, 0, 0)
	closeErr := f.Close()
	if mapErr != nil {
		t.Fatal(mapErr)
	}
	defer windows.UnmapViewOfFile(view)
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	// OPEN_EXISTING can succeed while the mapping prevents truncation. Ready is
	// advisory; NSIS must handle extraction errors with AllowSkipFiles off.
	if r := probeFiles(context.Background(), dir, []string{path}); r.Code != Ready {
		t.Fatalf("mapped-file preflight boundary changed: %+v", r)
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil, windows.CREATE_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err == nil {
		windows.CloseHandle(handle)
		t.Fatal("fixture mapping unexpectedly allowed truncation")
	}
	if !errors.Is(err, windows.ERROR_USER_MAPPED_FILE) {
		t.Fatalf("overwrite error=%v, want ERROR_USER_MAPPED_FILE", err)
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != contents {
		t.Fatalf("mapped fixture contents changed: %q, error=%v", body, err)
	}
}
