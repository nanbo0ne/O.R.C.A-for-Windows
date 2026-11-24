package skill

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func catalogTool(t *testing.T, tools []tool.Tool, name string) tool.Tool {
	t.Helper()
	for _, entry := range tools {
		if entry.Name() == name {
			return entry
		}
	}
	t.Fatalf("missing tool %s", name)
	return nil
}

func TestSubagentCatalogPreservesOverridesAndDisabledNames(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	writeSkill(t, home, ".orca/skills/explore.md", renderSkillFile("explore", "global", "global", RunSubagent, "global-model", "", nil))
	writeSkill(t, root, ".orca/skills/explore.md", renderSkillFile("explore", "project", "project", RunSubagent, "project-model", "", []string{"read_file"}))
	writeSkill(t, root, ".orca/skills/review.md", renderSkillFile("review", "inline override", "inline", RunInline, "", "", nil))
	store := New(Options{HomeDir: home, ProjectRoot: root, DisabledNames: []string{"research", "security-review"}, Stderr: io.Discard})
	all := store.ListAll()
	var ran Skill
	runner := func(_ context.Context, sk Skill, _ string, _ SubagentRunOptions) (string, error) {
		ran = sk
		return sk.Body, nil
	}
	for _, catalog := range [][]Skill{all, store.Enabled(all)} {
		tools := BuiltinSubagentToolsFromCatalog(store, catalog, runner)
		var names []string
		for _, entry := range tools {
			names = append(names, entry.Name())
		}
		if !reflect.DeepEqual(names, []string{"explore", "review"}) {
			t.Fatalf("disabled precedence changed: %v", names)
		}
		if _, err := catalogTool(t, tools, "explore").Execute(context.Background(), json.RawMessage(`{"task":"synthetic"}`)); err != nil {
			t.Fatal(err)
		}
		if ran.Scope != ScopeProject || ran.Model != "project-model" || strings.TrimSpace(ran.Body) != "project" || !reflect.DeepEqual(ran.AllowedTools, []string{"read_file"}) {
			t.Fatalf("override or content changed: %+v", ran)
		}
		if _, err := catalogTool(t, tools, "review").Execute(context.Background(), json.RawMessage(`{"task":"synthetic"}`)); err == nil || !strings.Contains(err.Error(), "overridden as inline") {
			t.Fatalf("inline override must not spawn a subagent: %v", err)
		}
	}
}

func TestSubagentCatalogRegistrationDoesNotRescanAndExecutionDoes(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	path := writeSkill(t, root, ".orca/skills/explore.md", renderSkillFile("explore", "fixture", "old body", RunSubagent, "old-model", "", nil))
	store := New(Options{HomeDir: home, ProjectRoot: root, DisableBuiltins: true, Stderr: io.Discard})
	catalog := store.Enabled(store.ListAll())
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	calls := 0
	runner := func(_ context.Context, sk Skill, _ string, _ SubagentRunOptions) (string, error) {
		calls++
		return sk.Body, nil
	}
	tools := BuiltinSubagentToolsFromCatalog(store, catalog, runner)
	entry := catalogTool(t, tools, "explore")
	args := json.RawMessage(`{"task":"synthetic"}`)
	if _, err := entry.Execute(context.Background(), args); err == nil || calls != 0 {
		t.Fatal("deleted skill executed from stale snapshot")
	}
	resolver := entry.(interface {
		ResolveProfile(json.RawMessage) *event.Profile
	})
	if resolver.ResolveProfile(args) != nil {
		t.Fatal("deleted profile resolved from stale snapshot")
	}
	writeSkill(t, root, ".orca/skills/explore.md", renderSkillFile("explore", "fixture", "new body", RunSubagent, "new-model", "", nil))
	if result, err := entry.Execute(context.Background(), args); err != nil || strings.TrimSpace(result) != "new body" || calls != 1 {
		t.Fatalf("must re-read current body: %q %v calls=%d", result, err, calls)
	}
	if profile := resolver.ResolveProfile(args); profile == nil || profile.Model != "new-model" {
		t.Fatalf("must re-read current profile: %+v", profile)
	}
	store.disabled["explore"] = true
	if _, err := entry.Execute(context.Background(), args); err == nil || resolver.ResolveProfile(args) != nil || calls != 1 {
		t.Fatal("disabled current skill was executed/resolved from snapshot")
	}
	delete(store.disabled, "explore")
	// Invalid current content must not fall back to the old parsed snapshot.
	writeSkill(t, root, ".orca/skills/explore.md", "---\nname: ../escape\ndescription: invalid\n---\nunsafe")
	if _, err := entry.Execute(context.Background(), args); err == nil || calls != 1 {
		t.Fatal("invalid replacement executed from stale snapshot")
	}
	other := New(Options{HomeDir: home, ProjectRoot: t.TempDir(), DisableBuiltins: true, Stderr: io.Discard})
	if tools := BuiltinSubagentToolsFromCatalog(other, other.List(), runner); len(tools) != 0 {
		t.Fatal("per-build registrations leaked across roots")
	}
}

func TestSubagentCatalogDoesNotBypassCurrentDiscoveryGuard(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	writeSkill(t, root, ".orca/skills/explore.md", renderSkillFile("explore", "synthetic", "fixture body", RunSubagent, "fixture-model", "", nil))
	store := New(Options{HomeDir: home, ProjectRoot: root, DisableBuiltins: true, Stderr: io.Discard})
	entry := catalogTool(t, BuiltinSubagentToolsFromCatalog(store, store.List(), func(context.Context, Skill, string, SubagentRunOptions) (string, error) {
		t.Fatal("runner crossed current profile discovery guard")
		return "", nil
	}), "explore")
	// Enabling an isolated profile makes this synthetic .orca directory outside
	// that profile ineligible. A parsed snapshot is not execution authority.
	if err := config.SetIsolatedProfileHome(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.SetIsolatedProfileHome("") })
	if _, err := entry.Execute(context.Background(), json.RawMessage(`{"task":"synthetic"}`)); err == nil {
		t.Fatal("snapshot bypassed current profile discovery boundary")
	}
}
