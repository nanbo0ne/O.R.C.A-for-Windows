package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/proc"
)

func TestBuildInfoJSON(t *testing.T) {
	oldChannel, oldVersion := channel, version
	t.Cleanup(func() { channel, version = oldChannel, oldVersion })
	for _, pair := range [][2]string{{"stable", "dev"}, {"beta", "3.0.14-beta.2"}, {"stable", "3.0.14"}, {"beta", "quoted\"\nversion"}} {
		channel, version = pair[0], pair[1]
		var out bytes.Buffer
		handled, err := writeBuildInfoJSON([]string{buildInfoJSONArg}, &out)
		if !handled || err != nil {
			t.Fatalf("probe: handled=%v, err=%v", handled, err)
		}
		var got map[string]any
		decoder := json.NewDecoder(&out)
		if err := decoder.Decode(&got); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"schemaVersion": float64(1), "channel": channel, "version": version}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("identity=%v, want %v", got, want)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			t.Fatalf("unexpected trailing output: %v", err)
		}
	}
	for _, args := range [][]string{nil, {"--other"}, {"--orca-build-info-json=1"}} {
		var out bytes.Buffer
		if handled, err := writeBuildInfoJSON(args, &out); handled || err != nil || out.Len() != 0 {
			t.Fatalf("unrelated args handled: %q", args)
		}
	}
	for _, args := range [][]string{{buildInfoJSONArg, "--orca-restart-wait-ms=10000"}, {"--other", buildInfoJSONArg}, {buildInfoJSONArg, buildInfoJSONArg}} {
		var out bytes.Buffer
		if handled, err := writeBuildInfoJSON(args, &out); !handled || err == nil || out.Len() != 0 {
			t.Fatalf("ambiguous probe args were not stopped: %q", args)
		}
	}
	if handled, err := writeBuildInfoJSON([]string{buildInfoJSONArg}, buildInfoFailWriter{}); !handled || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("output error lost: handled=%v, err=%v", handled, err)
	}
}

type buildInfoFailWriter struct{}

func (buildInfoFailWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestBuildInfoIsFirstMainStatement(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "main" {
			continue
		}
		branch, ok := fn.Body.List[0].(*ast.IfStmt)
		if !ok {
			t.Fatal("identity probe must precede all main processing")
		}
		assign, ok := branch.Init.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			t.Fatal("missing identity dispatch")
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			t.Fatal("missing identity call")
		}
		name, ok := call.Fun.(*ast.Ident)
		if !ok || name.Name != "writeBuildInfoJSON" {
			t.Fatal("normal initialization runs before identity probe")
		}
		if _, ok := branch.Body.List[len(branch.Body.List)-1].(*ast.ReturnStmt); !ok {
			t.Fatal("identity branch must exit main")
		}
		return
	}
	t.Fatal("missing main")
}

func TestBuildInfoMainIsolatedProfile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the isolated-profile probe uses the Windows AppData layout; this gate runs on the Windows runner")
	}
	if os.Getenv("ORCA_BUILD_INFO_TEST_CHILD") == "1" {
		os.Args = []string{"synthetic-orca", buildInfoJSONArg}
		main()
		// The existing TestMain creates an empty AppData fixture and removes it
		// after m.Run. Check it before that cleanup can hide any startup writes.
		profile, err := os.UserConfigDir()
		if err != nil {
			t.Fatal(err)
		}
		if entries, err := os.ReadDir(profile); err != nil || len(entries) != 0 {
			t.Fatalf("probe wrote to TestMain profile: entries=%v, err=%v", entries, err)
		}
		return
	}
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBuildInfoMainIsolatedProfile$")
	cmd.Dir = root
	cmd.Env = []string{"ORCA_BUILD_INFO_TEST_CHILD=1", "HOME=" + root, "USERPROFILE=" + root, "APPDATA=" + root, "LOCALAPPDATA=" + root, "TEMP=" + root, "TMP=" + root}
	for _, key := range []string{"SystemRoot", "WINDIR", "SystemDrive"} {
		if value := os.Getenv(key); value != "" {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	proc.HideWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("headless main: %v: %s", err, output)
	}
	var got map[string]any
	// The test runner adds PASS after the headless main returns and TestMain
	// cleans up. Everything before that trailer must be exactly one JSON value.
	if !bytes.HasSuffix(output, []byte("PASS\n")) {
		t.Fatalf("missing successful child test completion: %s", output)
	}
	if err := json.Unmarshal(bytes.TrimSuffix(output, []byte("PASS\n")), &got); err != nil {
		t.Fatalf("headless main output: %v: %s", err, output)
	}
	if !reflect.DeepEqual(got, map[string]any{"schemaVersion": float64(1), "channel": channel, "version": version}) {
		t.Fatalf("headless main identity: %v", got)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("headless main initialized the synthetic profile: entries=%v, err=%v", entries, err)
	}
}

func TestBuildInfoLinkedExecutableFixtures(t *testing.T) {
	if os.PathSeparator != '\\' {
		t.Skip("Windows executable probe fixture")
	}
	requireProgramFilesGo(t)
	if _, err := os.Stat(filepath.Join("..", "scripts", "test-orca-build-info.ps1")); err != nil {
		t.Fatal(err)
	}
	runBetaSnapshotFixture(t, "test-orca-build-info.ps1")
}
