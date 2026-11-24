package skill

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCatalogSnapshotRetainsDisabledAndDoesNotLeakBetweenRoots(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	dir := filepath.Join(root, ".orca", "skills")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"enabled", "disabled"} {
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte("---\nname: "+name+"\ndescription: synthetic\n---\nfixture\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := New(Options{HomeDir: home, ProjectRoot: root, DisabledNames: []string{"disabled"}, DisableBuiltins: true, Stderr: io.Discard})
	all := s.ListAll()
	enabled := s.Enabled(all)
	if len(all) != 2 || len(enabled) != 1 || enabled[0].Name != "enabled" {
		t.Fatalf("all=%v enabled=%v", all, enabled)
	}
	if !reflect.DeepEqual(s.List(), enabled) {
		t.Fatal("executable view changed")
	}
	if err := os.Remove(filepath.Join(dir, "enabled.md")); err != nil {
		t.Fatal(err)
	}
	if len(s.Enabled(all)) != 1 || len(s.List()) != 0 {
		t.Fatal("snapshot or refresh semantics invalid")
	}
	other := New(Options{HomeDir: home, ProjectRoot: t.TempDir(), DisableBuiltins: true})
	if len(other.ListAll()) != 0 {
		t.Fatal("cross-root catalog leak")
	}
}

func BenchmarkSkillCatalogViews(b *testing.B) {
	root := b.TempDir()
	home := b.TempDir()
	dir := filepath.Join(root, ".orca", "skills")
	if err := os.MkdirAll(dir, 0700); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		name := fmt.Sprintf("fixture-%03d", i)
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte("---\nname: "+name+"\ndescription: synthetic\n---\nfixture\n"), 0600); err != nil {
			b.Fatal(err)
		}
	}
	s := New(Options{HomeDir: home, ProjectRoot: root, DisabledNames: []string{"fixture-000"}, Stderr: io.Discard})
	b.Run("previous-two-scans", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = s.List()
			_ = s.ListAll()
		}
	})
	b.Run("single-snapshot", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			all := s.ListAll()
			_ = s.Enabled(all)
		}
	})
}
