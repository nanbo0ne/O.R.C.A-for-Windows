package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/boot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/skill"
)

const controllerPerfKind = "synthetic-controller-perf"

var controllerPerfRegister sync.Once
var controllerPerfCalls atomic.Int64

type controllerPerfProvider struct{ name string }

func (p *controllerPerfProvider) Name() string { return p.name }
func (*controllerPerfProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.Purpose != "" && req.Purpose != provider.RequestPurposeTurn {
		return nil, fmt.Errorf("unexpected auxiliary request in synthetic experiment: %q", req.Purpose)
	}
	controllerPerfCalls.Add(1)
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "synthetic fixture response"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

type controllerPerfFixture struct {
	app  *App
	root string
	opts boot.Options
}

func newControllerPerfFixture(t *testing.T, skills int) controllerPerfFixture {
	t.Helper()
	base, _ := withBetaProfile(t)
	if err := initializeBeta(); err != nil {
		t.Fatal(err)
	}
	// Build probes bash during discovery even with no shell tool invocation.
	// Remove every candidate so this experiment never launches a subprocess.
	t.Setenv("PATH", "")
	for _, key := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		t.Setenv(key, filepath.Join(base, "no-executables"))
	}
	root := filepath.Join(base, "synthetic-workspace")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	for i := 0; i < skills; i++ {
		name := fmt.Sprintf("synthetic-%03d", i)
		dir := filepath.Join(root, ".agents", "skills", name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf("---\nname: %s\ndescription: Synthetic performance fixture only.\n---\nNo tools or external services.\n", name)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.DefaultModel = "perf/a"
	cfg.Providers = []config.ProviderEntry{{Name: "perf", Kind: controllerPerfKind, BaseURL: "https://synthetic.invalid", Models: []string{"a", "b"}, Default: "a", ContextWindow: 128000}}
	cfg.Agent.AutoPlan, cfg.Agent.AutoPlanClassifier, cfg.Agent.PlannerModel = "off", "", ""
	cfg.Agent.SubagentModel, cfg.Agent.SubagentModels = "", nil
	cfg.Codegraph.Enabled, cfg.Codegraph.AutoInstall, cfg.LSP.Enabled = false, false, false
	cfg.Plugins = nil
	cfg.ToolLibrary = config.ToolLibraryConfig{}
	cfg.Tools.Enabled, cfg.Tools.Search.Engine = []string{"read_file"}, "native"
	cfg.Sandbox.Bash = "off"
	cfg.Desktop.VisionMode = config.VisionModeOff
	cfg.Desktop.ProviderAccess = nil
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	return controllerPerfFixture{app: &App{}, root: root, opts: boot.Options{
		Model: "perf/a", WorkspaceRoot: root, SessionDir: filepath.Join(initializedBetaRoot, "synthetic-sessions"),
		RuntimeProfile: boot.RuntimeProfileCoding, Stderr: io.Discard, Sink: event.Discard,
	}}
}

type controllerPerfMetrics map[string][]time.Duration

func (m controllerPerfMetrics) add(name string, since time.Time) {
	m[name] = append(m[name], time.Since(since))
}
func (m controllerPerfMetrics) log(t *testing.T, skills int) {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		values := append([]time.Duration(nil), m[name]...)
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
		t.Logf("PERF skills=%d stage=%s n=%d p50_ms=%.3f p95_ms=%.3f min_ms=%.3f max_ms=%.3f",
			skills, name, len(values), ms(values[(len(values)-1)/2]), ms(values[int(math.Ceil(float64(len(values))*.95))-1]), ms(values[0]), ms(values[len(values)-1]))
	}
}

// Bounded, opt-in, offline experiment. "Cold" means fresh fixture/profile in
// this process, NOT cold OS caches, Wails startup or native WebView2 readiness.
func TestWebBetaSyntheticControllerPerf(t *testing.T) {
	if os.Getenv("ORCA_BETA_SYNTHETIC_PERF") != "1" {
		t.Skip("opt-in bounded synthetic controller experiment")
	}
	controllerPerfRegister.Do(func() {
		provider.Register(controllerPerfKind, func(c provider.Config) (provider.Provider, error) { return &controllerPerfProvider{name: c.Name}, nil })
	})
	for _, skills := range []int{0, 50} {
		t.Run(fmt.Sprintf("skills-%d", skills), func(t *testing.T) {
			metrics := controllerPerfMetrics{}
			defer metrics.log(t, skills)
			for _, stage := range []string{"config", "boot", "desktop"} {
				for i := 0; i < 3; i++ {
					t.Run(fmt.Sprintf("fresh-%s-%d", stage, i), func(t *testing.T) {
						f := newControllerPerfFixture(t, skills)
						start := time.Now()
						var ctrl *control.Controller
						var err error
						switch stage {
						case "config":
							_, err = config.LoadForRoot(f.root)
						case "boot":
							ctrl, err = boot.Build(context.Background(), f.opts)
						case "desktop":
							ctrl, err = f.app.buildController(context.Background(), f.opts)
						}
						metrics.add("fresh_profile_"+stage, start)
						if err != nil {
							t.Fatal(err)
						}
						if ctrl != nil {
							ctrl.Close()
						}
					})
				}
			}
			t.Run("warm-pairs", func(t *testing.T) {
				f := newControllerPerfFixture(t, skills)
				warm, err := f.app.buildController(context.Background(), f.opts)
				if err != nil {
					t.Fatal(err)
				}
				warm.Close()
				for i := 0; i < 8; i++ {
					var elapsed [2]time.Duration
					for j := 0; j < 2; j++ {
						which := (i + j) % 2
						start := time.Now()
						var ctrl *control.Controller
						if which == 0 {
							ctrl, err = f.app.buildController(context.Background(), f.opts)
						} else {
							ctrl, err = boot.Build(context.Background(), f.opts)
						}
						elapsed[which] = time.Since(start)
						if err != nil {
							t.Fatal(err)
						}
						start = time.Now()
						ctrl.Close()
						metrics.add("warm_controller_close", start)
					}
					metrics["warm_desktop_build"] = append(metrics["warm_desktop_build"], elapsed[0])
					metrics["warm_boot_build"] = append(metrics["warm_boot_build"], elapsed[1])
					metrics["warm_paired_desktop_minus_boot"] = append(metrics["warm_paired_desktop_minus_boot"], elapsed[0]-elapsed[1])
					for j := 0; j < 2; j++ {
						start := time.Now()
						cfg, err := config.LoadForRoot(f.root)
						metrics.add(fmt.Sprintf("warm_config_load_%d", j+1), start)
						if err != nil {
							t.Fatal(err)
						}
						start = time.Now()
						_, err = f.app.prepareLocalRuntimeProviders(context.Background(), cfg, cfg.DefaultModel)
						metrics.add("warm_local_provider_preparation", start)
						if err != nil {
							t.Fatal(err)
						}
					}
					start := time.Now()
					catalog := skill.New(skill.Options{ProjectRoot: f.root, Stderr: io.Discard}).ListAll()
					metrics.add("warm_beta_skill_list_all", start)
					if len(catalog) < skills {
						t.Fatal("synthetic skill fixtures were not discovered")
					}
				}
			})
			t.Run("backend-workflows", func(t *testing.T) {
				f := newControllerPerfFixture(t, skills)
				callsBefore := controllerPerfCalls.Load()
				for i := 0; i < 4; i++ {
					var first atomic.Pointer[time.Time]
					var tools atomic.Int64
					f.opts.Sink = event.FuncSink(func(e event.Event) {
						if e.Kind == event.Text {
							now := time.Now()
							first.CompareAndSwap(nil, &now)
						}
						if e.Kind == event.ToolDispatch {
							tools.Add(1)
						}
					})
					start := time.Now()
					ctrl, err := f.app.buildController(context.Background(), f.opts)
					if err != nil {
						t.Fatal(err)
					}
					metrics.add("workflow_fresh_controller_on_warm_profile", start)
					tab := &WorkspaceTab{ID: "perf", Scope: "project", WorkspaceRoot: f.root, Ctrl: ctrl, Ready: true, model: "perf/a", promptMode: promptModeCoding}
					f.app.tabs = map[string]*WorkspaceTab{"perf": tab}
					f.app.activeTabID = "perf"
					turnStart := time.Now()
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					err = ctrl.RunTurn(ctx, "hello synthetic fixture")
					cancel()
					if err != nil {
						ctrl.Close()
						t.Fatal(err)
					}
					if first.Load() == nil || tools.Load() != 0 {
						ctrl.Close()
						t.Fatal("missing synthetic text or unexpected tool execution")
					}
					metrics["turn_to_first_synthetic_text"] = append(metrics["turn_to_first_synthetic_text"], first.Load().Sub(turnStart))
					metrics["build_to_first_synthetic_text"] = append(metrics["build_to_first_synthetic_text"], first.Load().Sub(start))
					start = time.Now()
					if err := f.app.NewSession(); err != nil {
						ctrl.Close()
						t.Fatal(err)
					}
					metrics.add("project_new_conversation", start)
					f.app.ctx = context.Background()
					start = time.Now()
					if err := f.app.SetModelForTab("perf", "perf/b"); err != nil {
						ctrl.Close()
						t.Fatal(err)
					}
					metrics.add("project_model_change", start)
					if tab.Ctrl == ctrl || tab.model != "perf/b" {
						ctrl.Close()
						t.Fatal("model change did not rebuild controller")
					}
					start = time.Now()
					tab.Ctrl.Close()
					metrics.add("workflow_controller_shutdown", start)
					f.app.ctx = nil
				}
				if got := controllerPerfCalls.Load() - callsBefore; got != 4 {
					t.Fatalf("synthetic calls=%d, want exactly 4", got)
				}
			})
		})
	}
}
