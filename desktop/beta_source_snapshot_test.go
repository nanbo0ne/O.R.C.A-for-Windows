package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/proc"
)

func TestBetaSourceSnapshotSyntheticFixtures(t *testing.T) {
	runBetaSnapshotFixture(t, "test-orca-source-snapshot.ps1")
}

func TestBetaFrontendSnapshotSyntheticFixtures(t *testing.T) {
	runBetaSnapshotFixture(t, "test-orca-frontend-snapshot.ps1")
}

func TestBetaNativeBuildIsolationSyntheticFixtures(t *testing.T) {
	runBetaSnapshotFixture(t, "test-orca-native-build.ps1")
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
