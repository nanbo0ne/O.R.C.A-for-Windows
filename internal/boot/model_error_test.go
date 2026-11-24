package boot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"

	_ "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/openai"
	_ "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool/builtin"
)

func TestBuildExplicitWorkspaceOverridesEnclosingGitConfig(t *testing.T) {
	if config.IsolatedProfileEnabled() {
		t.Fatal("standard boot fixture unexpectedly inherited beta isolation")
	}
	outer := robustTempDir(t)
	if err := os.Mkdir(filepath.Join(outer, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(outer, "nested-fixture")
	writeFile(t, outer, "orca.toml", "default_model = 'enclosing-repo-model'\n")
	writeFile(t, inner, "orca.toml", "default_model = 'fixture-missing-model'\n[codegraph]\nenabled = false\n")
	t.Chdir(inner)
	if got := resolveWorkspaceRoot(""); got != outer {
		t.Fatalf("auto root = %q, want enclosing fixture %q", got, outer)
	}
	_, err := Build(context.Background(), Options{WorkspaceRoot: inner, Sink: event.Discard})
	if !errors.Is(err, ErrUnknownModel) || !strings.Contains(err.Error(), "fixture-missing-model") {
		t.Fatalf("explicit fixture config was not used: %v", err)
	}
}

// TestBuildUnknownModelErrorIsActionable: a default_model that doesn't resolve
// (e.g. a preset name after [[providers]] replaced the built-in presets) must
// fail with a message that names the model, lists what IS configured, and hints
// at the [[providers]] trap — not a silent empty model.
func TestBuildUnknownModelErrorIsActionable(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "mimo"

[codegraph]
enabled = false

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://example.invalid"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`)

	_, err := Build(context.Background(), Options{Sink: event.Discard, WorkspaceRoot: dir})
	if err == nil {
		t.Fatal("expected an error for an unresolvable default_model")
	}
	msg := err.Error()
	for _, want := range []string{`"mimo"`, "deepseek-flash", "[[providers]]"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q should mention %q", msg, want)
		}
	}
}

// TestBuildNoticesMissingAPIKey: a resolvable model whose API key env is unset
// builds fine (RequireKey is false so the UI stays reachable) but must emit a
// notice naming the env var, instead of silently showing a dead/empty model.
func TestBuildNoticesMissingAPIKey(t *testing.T) {
	const keyEnv = "DEEPSEEK_ORCA_MISSING_KEY_FOR_TEST"
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "x"

[codegraph]
enabled = false

[[providers]]
name = "x"
kind = "openai"
base_url = "https://example.invalid"
model = "m"
api_key_env = "`+keyEnv+`"
`)

	var notices []string
	ctrl, err := Build(context.Background(), Options{
		WorkspaceRoot: dir,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
	})
	if err != nil {
		t.Fatalf("Build should succeed with RequireKey=false even without a key: %v", err)
	}
	defer ctrl.Close()

	found := false
	for _, n := range notices {
		if strings.Contains(n, keyEnv) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a notice naming the unset key env %q; got %v", keyEnv, notices)
	}
}
