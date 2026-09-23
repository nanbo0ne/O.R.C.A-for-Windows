package control

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/permission"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type guidanceProvider struct {
	request         provider.Request
	images          []provider.ImageContent
	waitForGuidance bool
	taskImage       bool
	taskSent        bool
	calls           int
}

func (*guidanceProvider) Name() string { return "synthetic" }
func (p *guidanceProvider) Stream(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	p.request = r
	p.images = agent.TurnImages(ctx)
	p.calls++
	ch := make(chan provider.Chunk, 2)
	found := false
	for _, m := range r.Messages {
		if _, ok := agent.SteerDisplayText(m.Content); ok {
			found = true
		}
	}
	switch {
	case p.waitForGuidance && !found:
		// Keep the synthetic turn active until the concurrent bridge call queues
		// its guidance. No timing sleeps or real provider requests are needed.
		runtime.Gosched()
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: fmt.Sprint("wait-", p.calls), Name: "guidance_wait", Arguments: fmt.Sprintf(`{"step":%d}`, p.calls)}}
	case p.taskImage && !p.taskSent:
		if len(p.images) != 1 {
			return nil, fmt.Errorf("guidance images = %d, want 1", len(p.images))
		}
		p.taskSent = true
		args, _ := json.Marshal(map[string]any{"prompt": "inspect guidance", "images": []string{p.images[0].Path}})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "inspect-guidance", Name: "task", Arguments: string(args)}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "Synthetic answer"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func startGuidanceTestTurn(t *testing.T, root string, p provider.Provider, reg *tool.Registry, vision bool, onEvent func(event.Event)) (*Controller, string, <-chan error) {
	t.Helper()
	started := make(chan string, 1)
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnStarted {
			started <- e.TurnID
		}
		if onEvent != nil {
			onEvent(e)
		}
	})
	reg.Add(fakeControlTool{name: "guidance_wait"})
	executor := agent.New(p, reg, agent.NewSession(""), agent.Options{ImageLoader: func(ctx context.Context, image provider.ImageContent) (provider.ImageContent, error) {
		return LoadImageContent(ctx, root, image)
	}}, sink)
	c := New(Options{Runner: executor, Executor: executor, WorkspaceRoot: root, VisionEnabled: vision, VisionMode: "auto", Policy: permission.New("allow", []string{"read_file", "image_send"}, nil, nil)})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- c.RunTurn(ctx, "Original task") }()
	select {
	case turn := <-started:
		return c, turn, done
	case err := <-done:
		t.Fatalf("turn ended before startup: %v", err)
	case <-ctx.Done():
		t.Fatal("turn never started")
	}
	return nil, "", nil
}

func TestGuidanceAttachmentsReachProviderAndSurviveReload(t *testing.T) {
	for _, vision := range []bool{true, false} {
		t.Run(map[bool]string{true: "direct", false: "task-only"}[vision], func(t *testing.T) {
			root := t.TempDir()
			raw := taskImagePNG(t, 28)
			putTaskImage(t, root, ".orca/attachments/guide.png", raw)
			putTaskImage(t, root, ".orca/attachments/notes.txt", []byte("Synthetic file context"))
			p := &guidanceProvider{waitForGuidance: true}
			c, turn, done := startGuidanceTestTurn(t, root, p, tool.NewRegistry(), vision, func(e event.Event) {
				if e.Kind == event.Steer {
					// Mutate the source before production image hydration runs.
					putTaskImage(t, root, ".orca/attachments/guide.png", taskImagePNG(t, 200))
				}
			})
			display := "Review @[图.png](.orca/attachments/guide.png) @[notes.txt](.orca/attachments/notes.txt)"
			input := "Review @.orca/attachments/guide.png @.orca/attachments/notes.txt"
			if err := c.SteerDisplay(input, display, turn, "guide1"); err != nil {
				t.Fatal(err)
			}
			session := c.executor.Session()
			consumed := false
			for _, e := range session.DisplaySnapshot() {
				consumed = consumed || strings.Contains(e.DisplayText, "@[图.png](")
			}
			if !consumed {
				t.Fatal("bridge acknowledged before guidance entered the session")
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			var guide provider.Message
			for _, m := range p.request.Messages {
				if _, ok := agent.SteerDisplayText(m.Content); ok {
					guide = m
				}
			}
			if !strings.Contains(guide.Content, "Synthetic file context") {
				t.Fatal("guidance skipped file resolution")
			}
			if vision {
				if len(guide.Images) != 1 || guide.Images[0].Data != base64.StdEncoding.EncodeToString(raw) {
					t.Fatal("guidance did not send image snapshot")
				}
			} else if len(guide.Images) != 0 {
				t.Fatal("text-only model received image bytes")
			}
			if len(p.images) != 1 || p.images[0].Data != base64.StdEncoding.EncodeToString(raw) {
				t.Fatal("task image snapshot not available")
			}
			snapshotPath := p.images[0].Path
			if snapshotPath == ".orca/attachments/guide.png" {
				t.Fatal("guidance did not receive an immutable backing snapshot")
			}
			if !strings.Contains(guide.Content, snapshotPath) {
				t.Fatal("model guidance did not remap the snapshot path")
			}
			path := filepath.Join(root, "session.jsonl")
			if err := session.Save(path); err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(saved), base64.StdEncoding.EncodeToString(raw)) {
				t.Fatal("base64 leaked into persisted history")
			}
			loaded, err := agent.LoadSession(path)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, e := range loaded.DisplaySnapshot() {
				if strings.Contains(e.DisplayText, "@[图.png]("+snapshotPath+")") && strings.Contains(e.DisplayText, "@[notes.txt](") {
					found = true
				}
			}
			if !found {
				t.Fatal("visible attachment refs were lost on reload")
			}
			persistedImage, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(snapshotPath)))
			if err != nil || !bytes.Equal(persistedImage, raw) {
				t.Fatalf("persisted guidance snapshot changed: %v", err)
			}
		})
	}
}

func TestGuidanceImagesReachProductionTaskResolver(t *testing.T) {
	root := t.TempDir()
	raw := taskImagePNG(t, 28)
	putTaskImage(t, root, ".orca/attachments/guide.png", raw)
	vision := &taskImageCaptureProvider{}
	reg := tool.NewRegistry()
	reg.Add(controllerImageTask(t, root, vision))
	p := &guidanceProvider{waitForGuidance: true, taskImage: true}
	c, turn, done := startGuidanceTestTurn(t, root, p, reg, false, nil)
	if err := c.SteerDisplay("Inspect @.orca/attachments/guide.png", "Inspect @[guide.png](.orca/attachments/guide.png)", turn, "task-guide"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(vision.requests) != 1 {
		t.Fatalf("guidance did not reach the task resolver: %d child requests", len(vision.requests))
	}
	request := vision.requests[0]
	last := request.Messages[len(request.Messages)-1]
	if len(last.Images) != 1 || last.Images[0].Data != base64.StdEncoding.EncodeToString(raw) {
		t.Fatal("task did not receive the validated guidance bytes")
	}
}

func TestGuidanceDynamicTaskImagesRetainCumulativeBudget(t *testing.T) {
	root := t.TempDir()
	c := New(Options{WorkspaceRoot: root, VisionMode: "auto", Policy: permission.New("allow", nil, nil, nil)})
	ctx, end := agent.WithParentTurn(context.Background())
	defer end()
	ctx = c.withTaskImages(ctx, false)
	p := &taskImageCaptureProvider{}
	task := controllerImageTask(t, root, p)
	for i := 0; i < 9; i++ {
		path := fmt.Sprintf(".orca/attachments/guidance-%d.png", i)
		raw := taskImagePNG(t, uint8(i))
		putTaskImage(t, root, path, raw)
		attached := provider.ImageContent{Path: path, Name: filepath.Base(path), MediaType: "image/png", Size: int64(len(raw)), Data: base64.StdEncoding.EncodeToString(raw)}
		ctx = agent.WithTurnImages(ctx, append(agent.TurnImages(ctx), attached))
		ctx = c.withTaskImages(ctx, false)
		args, _ := json.Marshal(map[string]any{"prompt": "inspect", "images": []string{path}})
		_, err := task.Execute(ctx, args)
		if (err != nil) != (i == 8) {
			t.Fatalf("call=%d error=%v", i, err)
		}
	}
	if len(p.requests) != 8 {
		t.Fatalf("image budget reset across guidance: %d requests", len(p.requests))
	}
}

func TestGuidanceValidatedBytesBypassProductionLoader(t *testing.T) {
	root := t.TempDir()
	raw := taskImagePNG(t, 28)
	putTaskImage(t, root, ".orca/attachments/guide.png", raw)
	c := New(Options{WorkspaceRoot: root})
	image, err := c.prepareVisionImage(".orca/attachments/guide.png")
	if err != nil {
		t.Fatal(err)
	}
	putTaskImage(t, root, image.Path, taskImagePNG(t, 200))
	p := &guidanceProvider{}
	var executor *agent.Agent
	var ack <-chan error
	loads := 0
	executor = agent.New(p, tool.NewRegistry(), agent.NewSession(""), agent.Options{ImageLoader: func(ctx context.Context, image provider.ImageContent) (provider.ImageContent, error) {
		loads++
		return LoadImageContent(ctx, root, image)
	}}, event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnStarted {
			var accepted bool
			ack, accepted = executor.TrySteerRich(agent.RichInput{Text: "inspect", Images: []provider.ImageContent{image}}, "inspect", []provider.ImageContent{image}, "frozen")
			if !accepted {
				t.Error("active agent refused guidance")
			}
		}
	}))
	if err := executor.Run(context.Background(), "Original task"); err != nil {
		t.Fatal(err)
	}
	if err := <-ack; err != nil {
		t.Fatal(err)
	}
	for _, m := range p.request.Messages {
		if _, ok := agent.SteerDisplayText(m.Content); ok {
			if loads != 0 || len(m.Images) != 1 || m.Images[0].Data != base64.StdEncoding.EncodeToString(raw) {
				t.Fatal("production hydration replaced the validated snapshot")
			}
			return
		}
	}
	t.Fatal("guidance missing from request")
}

func TestGuidanceControllerRejectsAtBlockedAnswerCommit(t *testing.T) {
	committed, release := make(chan struct{}), make(chan struct{})
	p := &guidanceProvider{}
	c, turn, done := startGuidanceTestTurn(t, t.TempDir(), p, tool.NewRegistry(), false, func(e event.Event) {
		if e.Kind == event.AnswerCommitted {
			close(committed)
			<-release
		}
	})
	<-committed
	running := c.Running()
	err := c.SteerDisplay("late", "late", turn, "late-id")
	close(release)
	if runErr := <-done; runErr != nil {
		t.Fatal(runErr)
	}
	if !running || err == nil {
		t.Fatalf("terminal guidance admission: running=%v error=%v", running, err)
	}
}

type guidanceBlockedProvider struct{ entered chan struct{} }

func (*guidanceBlockedProvider) Name() string { return "synthetic-blocked" }
func (p *guidanceBlockedProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	close(p.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestGuidanceControllerWaitsForConsumptionAndCancelsWithoutDeadlock(t *testing.T) {
	p := &guidanceBlockedProvider{entered: make(chan struct{})}
	var executor *agent.Agent
	executor = agent.New(p, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnStarted {
			// A consumed seed makes SteerConsumed flip only when the tested
			// bridge call enqueues, while the provider is held until cancellation.
			executor.Steer("seed")
		}
	}))
	c := New(Options{Runner: executor, Executor: executor, WorkspaceRoot: t.TempDir()})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.RunTurn(ctx, "Original task") }()
	select {
	case <-p.entered:
	case <-ctx.Done():
		t.Fatal("provider did not start")
	}
	turn := c.TurnStatus().TurnID
	result := make(chan error, 1)
	go func() { result <- c.SteerDisplay("pending guidance", "pending guidance", turn, "pending-id") }()
	for executor.SteerConsumed() {
		select {
		case err := <-result:
			t.Fatalf("bridge returned without enqueueing: %v", err)
		case <-ctx.Done():
			t.Fatal("guidance was never enqueued")
		default:
			runtime.Gosched()
		}
	}
	select {
	case err := <-result:
		t.Fatalf("bridge acknowledged unconsumed guidance: %v", err)
	default:
	}
	if ack := c.CancelTurn(turn); !ack.Accepted {
		t.Fatal("waiting guidance blocked cancellation")
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("bridge cancellation = %v", err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("turn cancellation = %v", err)
	}
	for _, m := range executor.Session().Snapshot() {
		if text, ok := agent.SteerDisplayText(m.Content); ok && text == "pending guidance" {
			t.Fatal("cancelled guidance was consumed")
		}
	}
}

func TestGuidanceRejectsStaleCancelledAndInvalidAttachments(t *testing.T) {
	root := t.TempDir()
	p := &guidanceProvider{}
	s := agent.NewSession("")
	exec := agent.New(p, tool.NewRegistry(), s, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec})
	c.cpRoot = root
	c.running = true
	c.activeTurnID = "new"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.activeContext = ctx
	if c.SteerDisplay("text", "text", "old", "stale") == nil {
		t.Fatal("accepted stale turn")
	}
	for _, path := range []string{".orca/attachments/missing.png", ".orca/attachments/missing.txt"} {
		if err := c.SteerDisplay("Review @"+path, "missing", "new", "missing"); err == nil || !strings.Contains(err.Error(), "guidance attachment @"+path+" is unavailable") {
			t.Fatalf("missing explicit attachment silently ignored: %v", err)
		}
	}
	putTaskImage(t, root, ".orca/attachments/bad.png", []byte("not an image"))
	if c.SteerDisplay("@.orca/attachments/bad.png", "picture", "new", "bad") == nil {
		t.Fatal("accepted invalid image")
	}
	cancel()
	if c.SteerDisplay("text", "text", "new", "cancelled") == nil {
		t.Fatal("accepted cancelled guidance")
	}
	if err := exec.Run(context.Background(), "Original"); err != nil {
		t.Fatal(err)
	}
	for _, m := range p.request.Messages {
		if _, ok := agent.SteerDisplayText(m.Content); ok {
			t.Fatal("rejected guidance reached provider")
		}
	}
}
