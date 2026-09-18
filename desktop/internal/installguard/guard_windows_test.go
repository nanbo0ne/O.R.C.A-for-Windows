package installguard

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/installipc"
	"golang.org/x/sys/windows"
)

func TestGuardFixture(t *testing.T) {
	mode := os.Getenv("ORCA_GUARD_FIXTURE")
	if mode == "" {
		return
	}
	if mode == "self-exit" {
		time.Sleep(250 * time.Millisecond)
		os.Exit(0)
	}
	if mode == "graceful" {
		exe, _ := os.Executable()
		stop, err := installipc.Listen(context.Background(), filepath.Dir(exe), func() { os.Exit(0) })
		if err != nil {
			os.Exit(91)
		}
		defer stop()
	}
	if path := os.Getenv("ORCA_GUARD_READY"); path != "" {
		_ = os.WriteFile(path, []byte("ready"), 0600)
	}
	for {
		time.Sleep(time.Second)
	}
}

func fixture(t *testing.T, dir, mode string) *exec.Cmd {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Orca.exe")
	if err := os.WriteFile(path, body, 0700); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "fixture.ready")
	cmd := exec.Command(path, "-test.run=^TestGuardFixture$")
	cmd.Env = append(os.Environ(), "ORCA_GUARD_FIXTURE="+mode, "ORCA_GUARD_READY="+ready)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if mode != "self-exit" {
		until := time.Now().Add(3 * time.Second)
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			if time.Now().After(until) {
				t.Fatal("fixture startup timeout")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return cmd
}

func options(dir string) Options {
	return Options{Directory: dir, Files: []string{"Orca.exe", "node.exe", "codegraph/index.js", "uninstall.exe"}}
}

func TestAbsentProcessDoesNotWaitOrCreateTarget(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "中文 安装路径")
	start := time.Now()
	r := Run(context.Background(), options(dir))
	if r.Code != Ready {
		t.Fatalf("%+v", r)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("no-process fast path took %s", time.Since(start))
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("preflight created installation directory")
	}
	t.Logf("no process: %dms", r.ElapsedMS)
}

func TestOnlyTargetBackgroundProcessIsTerminated(t *testing.T) {
	target, other := t.TempDir(), t.TempDir()
	owned, unrelated := fixture(t, target, "hung"), fixture(t, other, "hung")
	start := time.Now()
	r := Run(context.Background(), options(target))
	if r.Code != Ready {
		t.Fatalf("%+v", r)
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second || elapsed < 5*time.Second {
		t.Fatalf("grace period outside contract: %s", elapsed)
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(unrelated.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	if state, _ := windows.WaitForSingleObject(h, 0); state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("different installation was terminated")
	}
	if err := owned.Wait(); err != nil {
		t.Fatalf("owned exit: %v", err)
	}
	t.Logf("hung target closed: %dms; other instance alive", r.ElapsedMS)
}

func TestGracefulAndSelfExitContinueImmediately(t *testing.T) {
	for _, mode := range []string{"graceful", "self-exit"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			fixture(t, dir, mode)
			r := Run(context.Background(), options(dir))
			if r.Code != Ready || r.ElapsedMS > 2000 {
				t.Fatalf("%+v", r)
			}
			t.Logf("%s: %dms", mode, r.ElapsedMS)
		})
	}
}

func TestExternalLockAndRetryPreserveFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Orca.exe")
	if err := os.WriteFile(path, []byte("existing app"), 0600); err != nil {
		t.Fatal(err)
	}
	p, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := Run(context.Background(), options(dir))
	windows.CloseHandle(h)
	expectedPath, pathErr := canonical(path)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if r.Code != Locked || r.File != expectedPath || r.SystemError != uint32(windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("%+v", r)
	}
	body, _ := os.ReadFile(path)
	if string(body) != "existing app" {
		t.Fatal("blocked preflight changed file")
	}
	r = Run(context.Background(), options(dir))
	if r.Code != Ready {
		t.Fatalf("retry: %+v", r)
	}
}

func TestReadOnlyFileIsPermissionFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Orca.exe")
	if err := os.WriteFile(path, []byte("read only"), 0600); err != nil {
		t.Fatal(err)
	}
	p, _ := windows.UTF16PtrFromString(path)
	if err := windows.SetFileAttributes(p, windows.FILE_ATTRIBUTE_READONLY); err != nil {
		t.Fatal(err)
	}
	defer windows.SetFileAttributes(p, windows.FILE_ATTRIBUTE_NORMAL)
	r := Run(context.Background(), options(dir))
	if r.Code != NotWritable || r.SystemError != uint32(windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("%+v", r)
	}
}

func TestCancelledGuardDoesNotKill(t *testing.T) {
	dir := t.TempDir()
	cmd := fixture(t, dir, "hung")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(150*time.Millisecond, cancel)
	r := Run(ctx, options(dir))
	if r.Code != Cancelled {
		t.Fatalf("%+v", r)
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	if state, _ := windows.WaitForSingleObject(h, 0); state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("cancel killed fixture")
	}
}

func TestFileManifestCannotEscape(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"..\\outside.exe", "C:\\outside.exe", "Orca.exe:stream", ""} {
		if _, err := validateFiles(dir, []string{name}); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}

func TestTerminationIdentityChecksCreationTimeAndPath(t *testing.T) {
	p := process{path: `C:\app\Orca.exe`, created: windows.Filetime{LowDateTime: 100}}
	if !sameIdentity(p, strings.ToUpper(p.path), p.created) {
		t.Fatal("case alias rejected")
	}
	if sameIdentity(p, p.path, windows.Filetime{LowDateTime: 101}) {
		t.Fatal("PID reuse accepted")
	}
	if sameIdentity(p, `C:\other\Orca.exe`, p.created) {
		t.Fatal("different image accepted")
	}
}
