package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/bot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type auxiliaryFixtureProvider struct {
	mu       sync.Mutex
	requests []provider.Request
	reply    string
	hook     func(context.Context, provider.Request) (<-chan provider.Chunk, error)
}

func (*auxiliaryFixtureProvider) Name() string { return "synthetic-auxiliary" }
func (p *auxiliaryFixtureProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()
	if p.hook != nil {
		return p.hook(ctx, req)
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: p.reply}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func auxiliaryFixture(t *testing.T) (*App, *config.Config, *auxiliaryFixtureProvider) {
	t.Helper()
	processAppData := os.Getenv("APPDATA")
	home := isolateDesktopUserDirs(t)
	root := lifecycleProfile(t)
	rel, err := filepath.Rel(home, sideChatStorePath())
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatal("synthetic sidebar storage escaped its temporary desktop home")
	}
	t.Logf("AUX_FIXTURE process_appdata=%q case_appdata=%q sidebar=%q config=%q",
		processAppData, os.Getenv("APPDATA"), sideChatStorePath(), config.UserConfigPath())
	p := &auxiliaryFixtureProvider{reply: "{\"memories\":[]}"}
	kind := "synthetic-auxiliary-" + event.NewRequestID()
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	t.Setenv("ORCA_SYNTHETIC_AUX_KEY", "synthetic-only")
	cfg := config.Default()
	cfg.Providers = []config.ProviderEntry{{Name: p.Name(), Kind: kind, Model: "model", APIKeyEnv: "ORCA_SYNTHETIC_AUX_KEY"}}
	cfg.DefaultModel, cfg.Bot.Model = p.Name()+"/model", ""
	cfg.Desktop.ProviderAccess = nil
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{"side": {ID: "side", WorkspaceRoot: root, model: cfg.DefaultModel}},
		sideChatCancels: map[string]sideChatCancel{}}
	return app, cfg, p
}

func TestAuxiliaryMissingSourceNeverFallsBack(t *testing.T) {
	for _, mode := range []string{"memory", "continuity", "welcome", "sidebar"} {
		t.Run(mode, func(t *testing.T) {
			app, cfg, p := auxiliaryFixture(t)
			var err error
			switch mode {
			case "memory":
				_, err = app.generateAssistantMemoryUpdates(assistantMemoryPendingItem{Model: "removed-source"}, nil)
			case "continuity":
				p.reply = "NEW"
				_, err = app.decideBotContinuity(context.Background(), "removed-source", bot.SessionChoice{}, "synthetic")
			case "welcome":
				cfg.Bot.Model = "removed-web/model"
				if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
					t.Fatal(err)
				}
				_, err = app.generateWelcomeSuggestions()
			case "sidebar":
				app.tabs["side"].model = "removed-source"
				err = app.SendSideChat("side", "synthetic")
			}
			if err == nil || len(p.requests) != 0 {
				t.Fatalf("unavailable source called the default provider: error=%v calls=%d", err, len(p.requests))
			}
		})
	}
}

func TestAuxiliaryDirectRequestsHaveHostAttribution(t *testing.T) {
	for _, mode := range []string{"memory", "continuity", "welcome", "probe", "sidebar"} {
		t.Run(mode, func(t *testing.T) {
			app, cfg, p := auxiliaryFixture(t)
			var err error
			switch mode {
			case "memory":
				_, err = app.generateAssistantMemoryUpdates(assistantMemoryPendingItem{Model: cfg.DefaultModel}, nil)
			case "continuity":
				p.reply = "NEW"
				_, err = app.decideBotContinuity(context.Background(), cfg.DefaultModel, bot.SessionChoice{}, "synthetic")
			case "welcome":
				p.reply = "[\"Review synthetic work\",\"Check synthetic plans\",\"Summarize synthetic notes\",\"Explain synthetic steps\"]"
				_, err = app.generateWelcomeSuggestions()
			case "probe":
				err = probeProviderCredential(context.Background(), cfg.Providers[0], "synthetic-only")
			case "sidebar":
				err = app.SendSideChat("side", "synthetic")
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(p.requests) != 1 {
				t.Fatalf("calls=%d", len(p.requests))
			}
			req := p.requests[0]
			if req.RequestID == "" || req.ParentTaskID == "" || req.Purpose == "" {
				t.Fatal("auxiliary request has no host request/task/purpose attribution")
			}
			want := map[string]provider.RequestPurpose{"memory": auxiliaryMemory, "continuity": provider.RequestPurposeClassifier,
				"welcome": auxiliaryWelcome, "probe": auxiliaryProbe, "sidebar": provider.RequestPurposeTurn}
			if req.Purpose != want[mode] {
				t.Fatal("auxiliary purpose was mislabeled")
			}
		})
	}
}

func TestSideChatCancellationDoesNotCommitPartialReply(t *testing.T) {
	app, _, p := auxiliaryFixture(t)
	entered := make(chan struct{})
	p.hook = func(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
		ch := make(chan provider.Chunk, 1)
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "synthetic partial"}
		close(entered)
		go func() { <-ctx.Done(); close(ch) }()
		return ch, nil
	}
	done := make(chan error, 1)
	go func() { done <- app.SendSideChat("side", "synthetic") }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("sidebar failed before synthetic transport: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("sidebar did not reach synthetic transport")
	}
	if err := app.CancelSideChat("side"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel treated as completion: %v", err)
	}
	if len(app.ListSideChat("side")) != 0 {
		t.Fatal("canceled partial output committed as a completed sidebar exchange")
	}
}

func TestAutomaticAuxiliaryWebSourceNeverUsesDefault(t *testing.T) {
	for _, mode := range []string{"memory", "continuity", "welcome", "title", "probe"} {
		t.Run(mode, func(t *testing.T) {
			app, cfg, p := auxiliaryFixture(t)
			web := config.ProviderEntry{Name: "deepseek-web-beta", Kind: cfg.Providers[0].Kind, Model: "web"}
			cfg.Providers = append(cfg.Providers, web)
			ref := web.Name + "/" + web.Model
			cfg.Bot.Model = ref
			if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
				t.Fatal(err)
			}
			var err error
			switch mode {
			case "memory":
				_, err = app.generateAssistantMemoryUpdates(assistantMemoryPendingItem{Model: ref}, nil)
			case "continuity":
				_, err = app.decideBotContinuity(context.Background(), ref, bot.SessionChoice{}, "synthetic")
			case "welcome":
				_, err = app.generateWelcomeSuggestions()
			case "title":
				_, err = app.generateTopicTitleWithProvider("", "synthetic user", "synthetic answer", ref)
			case "probe":
				err = probeProviderCredential(context.Background(), web, "synthetic")
			}
			if err == nil || len(p.requests) != 0 {
				t.Fatalf("automatic web auxiliary authorized or substituted a source: error=%v calls=%d", err, len(p.requests))
			}
		})
	}
}

func TestAuxiliaryTitleRequiresOriginatingModel(t *testing.T) {
	app, cfg, p := auxiliaryFixture(t)
	p.reply = "Synthetic title"
	app.tabs["other"] = &WorkspaceTab{ID: "other", model: cfg.DefaultModel}
	if _, err := app.generateTopicTitleWithProvider("", "synthetic user", "synthetic answer", ""); err == nil || len(p.requests) != 0 {
		t.Fatal("unknown title provenance borrowed another tab's model")
	}
	if _, err := app.generateTopicTitleWithProvider("", "synthetic user", "synthetic answer", "removed/model"); err == nil || len(p.requests) != 0 {
		t.Fatal("removed title model fell back to another source")
	}
	if _, err := app.generateTopicTitleWithProvider("", "synthetic user", "synthetic answer", cfg.DefaultModel); err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != 1 || p.requests[0].Purpose != auxiliaryTitle || p.requests[0].ParentTaskID == "" {
		t.Fatal("explicit title request lost its source or host identity")
	}
}

func TestSideChatBindingAndFailureRequireExplicitReset(t *testing.T) {
	for _, mode := range []string{"source", "legacy", "incomplete", "concurrent"} {
		t.Run(mode, func(t *testing.T) {
			app, cfg, p := auxiliaryFixture(t)
			p.reply = "synthetic answer"
			if mode == "legacy" {
				if err := saveSideChatFile(sideChatFile{Sessions: map[string][]SideChatMessage{
					"side": {{ID: "old", Role: "user", Content: "synthetic old source"}},
				}}); err != nil {
					t.Fatal(err)
				}
				if err := app.SendSideChat("side", "new source"); err == nil || len(p.requests) != 0 {
					t.Fatal("legacy unbound history silently sent to a new source")
				}
				return
			}
			if mode == "concurrent" {
				entered := make(chan struct{})
				p.hook = func(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
					close(entered)
					ch := make(chan provider.Chunk)
					go func() { <-ctx.Done(); close(ch) }()
					return ch, nil
				}
				done := make(chan error, 1)
				go func() { done <- app.SendSideChat("side", "first") }()
				lifecycleWait(t, entered)
				if err := app.SendSideChat("side", "second"); err == nil {
					t.Fatal("concurrent sidebar admitted")
				}
				if err := app.ClearSideChat("side"); err == nil {
					t.Fatal("running sidebar cleared")
				}
				_ = app.CancelSideChat("side")
				if err := <-done; !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				return
			}
			if mode == "incomplete" {
				p.hook = func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
					ch := make(chan provider.Chunk, 1)
					ch <- provider.Chunk{Type: provider.ChunkText, Text: "synthetic incomplete"}
					close(ch)
					return ch, nil
				}
				if err := app.SendSideChat("side", "first"); err == nil {
					t.Fatal("incomplete stream accepted")
				}
				p.hook = nil
			} else {
				if err := app.SendSideChat("side", "first"); err != nil {
					t.Fatal(err)
				}
				other := cfg.Providers[0]
				other.Name = "synthetic-other-source"
				cfg.Providers = append(cfg.Providers, other)
				if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
					t.Fatal(err)
				}
				app.tabs["side"].model = other.Name + "/" + other.Model
			}
			before := len(p.requests)
			if err := app.SendSideChat("side", "second"); err == nil || len(p.requests) != before {
				t.Fatal("uncertain request or changed source silently resumed")
			}
			if err := app.ClearSideChat("side"); err != nil {
				t.Fatal(err)
			}
			if err := app.SendSideChat("side", "explicit new sidebar"); err != nil {
				t.Fatal(err)
			}
			last := p.requests[len(p.requests)-1]
			if last.RequestID == p.requests[0].RequestID || last.ParentTaskID == p.requests[0].ParentTaskID ||
				last.ConversationID == p.requests[0].ConversationID {
				t.Fatal("explicit reset reused old request/task/conversation identity")
			}
		})
	}
}

func TestSideChatCorruptStoreNeverCreatesNewConversation(t *testing.T) {
	app, _, p := auxiliaryFixture(t)
	path := sideChatStorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{invalid synthetic sidebar"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := app.SendSideChat("side", "synthetic"); err == nil || len(p.requests) != 0 {
		t.Fatal("corrupt sidebar state silently became a new provider conversation")
	}
}

func TestAuxiliaryReservedWebSourceCannotBecomeAPI(t *testing.T) {
	app, cfg, p := auxiliaryFixture(t)
	disguised := cfg.Providers[0]
	disguised.Name = "deepseek-web-beta"
	cfg.Providers = append(cfg.Providers, disguised)
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	app.tabs["side"].model = disguised.Name + "/" + disguised.Model
	if err := app.SendSideChat("side", "synthetic"); err == nil || len(p.requests) != 0 {
		t.Fatal("web identity resolved to an ordinary API provider")
	}
}

func TestAuxiliaryAccountReceiptPersistsAccuratePurpose(t *testing.T) {
	for _, purpose := range []provider.RequestPurpose{auxiliaryMemory, auxiliaryTitle, auxiliaryWelcome, auxiliaryProbe} {
		t.Run(string(purpose), func(t *testing.T) {
			f := newAccountStreamFixture(t, "openai")
			f.settled.Store(true)
			req := bindAuxiliaryRequest(context.Background(), purpose, integrationRequest())
			stream, err := f.registeredProvider().Stream(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := collectAuxiliaryText(context.Background(), stream); err != nil {
				t.Fatal(err)
			}
			f.assertSettled()
			doc, err := f.document()
			if err != nil || len(doc.Records) != 1 || doc.Records[0].Purpose != string(purpose) ||
				doc.Records[0].TurnID != req.ParentTaskID || doc.Records[0].CostMicros == nil || *doc.Records[0].CostMicros != integrationCost {
				t.Fatal("auxiliary receipt lost purpose, host attribution or exact server cost", err)
			}
		})
	}
}

func TestAuxiliaryCanceledAccountReceiptDoesNotInventZero(t *testing.T) {
	f := newAccountStreamFixture(t, "anthropic")
	f.blockedStream.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := bindAuxiliaryRequest(ctx, auxiliaryMemory, integrationRequest())
	stream, err := f.registeredProvider().Stream(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := collectAuxiliaryText(ctx, stream); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled auxiliary request was reported complete", err)
	}
	lifecycleWait(t, f.bodyClosed)
	rows := f.app.OrcaAccountReceipts()
	if len(rows) != 1 || rows[0].Purpose != string(auxiliaryMemory) || rows[0].TurnID != req.ParentTaskID ||
		rows[0].Status != "pending" || rows[0].Cost != "" || f.inferences.Load() != 1 {
		t.Fatal("canceled auxiliary replayed, lost attribution, or invented a zero charge")
	}
}

func TestSideChatUnavailableStorageBlocksDispatch(t *testing.T) {
	app, _, p := auxiliaryFixture(t)
	if err := os.MkdirAll(sideChatStorePath(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := app.SendSideChat("side", "synthetic"); err == nil || len(p.requests) != 0 {
		t.Fatal("sidebar dispatched before its binding could be stored")
	}
}

func TestAuxiliaryBetaCatalogFailureNeverBecomesPaidProbe(t *testing.T) {
	for _, mode := range []string{"error", "empty", "available"} {
		t.Run(mode, func(t *testing.T) {
			_, cfg, p := auxiliaryFixture(t)
			var fetchErr error
			var models []string
			if mode == "error" {
				fetchErr = errors.New("synthetic catalog unavailable")
			}
			if mode == "available" {
				models = []string{"model"}
			}
			got, err := validateProviderPresetModels(context.Background(), cfg.Providers[0], "synthetic-only", models, fetchErr)
			if mode != "available" && err == nil {
				t.Error("failed free catalog lookup was silently accepted through an inference probe")
			}
			if mode == "available" && (err != nil || len(got) != 1 || got[0] != "model") {
				t.Error("valid explicit catalog was rejected", err)
			}
			if len(p.requests) != 0 {
				t.Fatal("catalog failure triggered a potentially paid inference")
			}
		})
	}
}

func TestAuxiliaryStableCatalogProbePreservesSelectedSource(t *testing.T) {
	_, cfg, p := auxiliaryFixture(t)
	channel = "stable"
	models, err := validateProviderPresetModels(context.Background(), cfg.Providers[0], "synthetic-only", nil, errors.New("synthetic catalog unavailable"))
	if err != nil || len(models) != 1 || models[0] != "model" || len(p.requests) != 1 ||
		p.requests[0].Purpose != auxiliaryProbe || p.requests[0].RequestID == "" || p.requests[0].ParentTaskID == "" {
		t.Fatal("ordinary non-beta validation behavior changed source or lost attribution", err)
	}
}
