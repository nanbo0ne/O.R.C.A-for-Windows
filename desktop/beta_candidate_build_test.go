//go:build windows

package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/proc"
)

func TestBetaCandidateModeCannotPublishOrAssertAcceptance(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("PowerShell 7 is required")
	}
	script, err := filepath.Abs("../scripts/build-orca-beta.ps1")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"package", []string{"-PrepareCandidate", "-Package"}, "Choose exactly one build mode"},
		{"frontend", []string{"-PrepareCandidate", "-PrepareFrontend"}, "Choose exactly one build mode"},
		{"acceptance", []string{"-PrepareCandidate", "-IntegrationGatesPassed"}, "cannot assert completed acceptance"},
		{"unspecified-output", []string{"-PrepareCandidate"}, "Candidate output must"},
		{"outside-temp", []string{"-PrepareCandidate", "-OutputDirectory", t.TempDir()}, "Candidate output must"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			args := append([]string{"-NoProfile", "-NonInteractive", "-File", script}, tc.args...)
			cmd := exec.CommandContext(ctx, pwsh, args...)
			proc.HideWindow(cmd)
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), tc.want) {
				t.Fatalf("candidate mode did not reject safely: %v\n%s", err, output)
			}
		})
	}
}
