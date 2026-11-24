package boot

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
)

func TestBuildPinsResolvedProviderKind(t *testing.T) {
	for _, kind := range []string{"deepseekweb-beta", "openai"} {
		t.Run(kind, func(t *testing.T) {
			root := robustTempDir(t)
			t.Chdir(root)
			writeFile(t, root, "orca.toml", fmt.Sprintf(`default_model = "alias/synthetic"
[codegraph]
enabled = false
[lsp]
enabled = false
[agent]
auto_plan = "off"
[[providers]]
name = "alias"
kind = %q
base_url = "https://synthetic.invalid"
model = "synthetic"
`, kind))
			ctrl, err := Build(context.Background(), Options{WorkspaceRoot: root, Sink: event.Discard, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			defer ctrl.Close()
			if err := os.Remove(filepath.Join(root, "orca.toml")); err != nil {
				t.Fatal(err)
			}
			metadata := ctrl.ContextMetadata()
			if metadata.ProviderKind != kind || metadata.ModelRef != "alias/synthetic" {
				t.Fatalf("resolved binding was not retained: %+v", metadata)
			}
		})
	}
}
