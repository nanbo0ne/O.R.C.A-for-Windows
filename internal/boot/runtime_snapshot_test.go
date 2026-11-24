package boot

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

func TestRuntimePreparationUsesOneConfigSnapshot(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "deepseek-orca.toml", "default_model = 'snapshot/model'\n[codegraph]\nenabled = false\n")
	calls := 0
	ctrl, err := Build(context.Background(), Options{WorkspaceRoot: dir, Stderr: io.Discard,
		PrepareRuntimeProviders: func(_ context.Context, cfg *config.Config, selected string) ([]config.ProviderEntry, error) {
			calls++
			if selected != "snapshot/model" || cfg.DefaultModel != selected {
				t.Fatal("runtime preparation received another selection")
			}
			// A second config load would now fail, or resolve a different model.
			writeFile(t, dir, "deepseek-orca.toml", "[invalid syntax")
			return []config.ProviderEntry{{Name: "snapshot", Kind: "openai", BaseURL: "https://synthetic.invalid", Models: []string{"model"}}}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	if calls != 1 {
		t.Fatal("runtime preparation repeated", calls)
	}
}

func TestRuntimePreparationFailureAndCancellationAbortBuild(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "deepseek-orca.toml", "default_model = 'snapshot/model'\n[codegraph]\nenabled = false\n")
	failure := errors.New("synthetic runtime unavailable")
	calls := 0
	opts := Options{WorkspaceRoot: dir, Stderr: io.Discard, PrepareRuntimeProviders: func(context.Context, *config.Config, string) ([]config.ProviderEntry, error) {
		calls++
		return nil, failure
	}}
	if _, err := Build(context.Background(), opts); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, opts); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("cancelled build started a runtime", calls, err)
	}
}
