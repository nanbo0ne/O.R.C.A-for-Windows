package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/proc"
)

func TestBetaSourceSnapshotSyntheticFixtures(t *testing.T) {
	runBetaSnapshotFixture(t, "test-orca-source-snapshot.ps1")
}

func TestBetaFrontendSnapshotSyntheticFixtures(t *testing.T) {
	requireNode24Runtime(t)
	runBetaSnapshotFixture(t, "test-orca-frontend-snapshot.ps1")
}

func TestBetaNativeBuildIsolationSyntheticFixtures(t *testing.T) {
	requireProgramFilesGo(t)
	runBetaSnapshotFixture(t, "test-orca-native-build.ps1")
}

// requireProgramFilesGo skips fixtures that hash and execute a real Go
// toolchain installed under Program Files. A hosted runner exposes Go through
// the tool cache, whose directories are reparse points the fixture refuses by
// design, so the fixture cannot hold there.
func requireProgramFilesGo(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		return
	}
	path := filepath.Join(os.Getenv("ProgramFiles"), "Go", "bin", "go.exe")
	if _, err := os.Stat(path); err != nil {
		t.Skip("the fixture requires the Program Files Go toolchain; a tool-cache install is refused as a reparse point")
	}
}

// requireNode24Runtime skips the configuration regression when no Node 24
// runtime is available. The fixture pins the npm configuration behaviour of
// Node 24 and must not silently accept another major version.
func requireNode24Runtime(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		return
	}
	candidates := []string{
		filepath.Join(os.Getenv("USERPROFILE"), ".cache", "codex-runtimes", "codex-primary-runtime", "dependencies", "node", "bin", "node.exe"),
	}
	if path, err := exec.LookPath("node.exe"); err == nil {
		candidates = append(candidates, path)
	}
	for _, candidate := range candidates {
		output, err := exec.Command(candidate, "--version").Output()
		if err == nil && strings.HasPrefix(strings.TrimSpace(string(output)), "v24.") {
			return
		}
	}
	t.Skip("the configuration regression requires a Node 24 runtime")
}

func runBetaSnapshotFixture(t *testing.T, name string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("source snapshot fixtures use Windows PowerShell paths; this gate runs on the Windows runner")
	}
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("PowerShell 7 is required for source snapshot fixtures")
	}
	script, err := filepath.Abs(filepath.Join("../scripts", name))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, pwsh, "-NoProfile", "-NonInteractive", "-File", script, "-FixtureRoot", filepath.Join(t.TempDir(), "snapshot-fixtures"))
	cmd.Env = os.Environ()
	proc.HideWindow(cmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("source snapshot synthetic fixture: %v\n%s", err, output)
	} else {
		t.Logf("%s", output)
	}
}
