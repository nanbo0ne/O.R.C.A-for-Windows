package webbeta

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func TestBrowserSnapshotsReuseChatWithoutReuploadingOldImage(t *testing.T) {
	b := &fakeBrowser{}
	e := testEngine(t, b)
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(pngBytes.Bytes())
	first := provider.ImageContent{Path: "synthetic-step-1", Name: "step-1.png", MediaType: "image/png", Size: int64(pngBytes.Len()), Data: encoded}
	r := provider.Request{ParentTaskID: "parent", ConversationID: "browser-run", RequestID: "step1", Purpose: provider.RequestPurposeBrowser, Messages: []provider.Message{{Role: provider.RoleUser, Content: "first synthetic observation", Images: []provider.ImageContent{first}}}}
	consume := func(r provider.Request) {
		t.Helper()
		ch, err := (&Adapter{}).Stream(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		for c := range ch {
			if c.Err != nil {
				t.Fatal(c.Err)
			}
		}
	}
	consume(r)
	r.Messages[0].Images[0].Data = ""
	second := first
	second.Path = "synthetic-step-2"
	second.Name = "step-2.png"
	r.RequestID = "step2"
	r.Messages = append(r.Messages, provider.Message{Role: provider.RoleAssistant, Content: "synthetic"}, provider.Message{Role: provider.RoleUser, Content: "second synthetic observation", Images: []provider.ImageContent{second}})
	consume(r)
	b.mu.Lock()
	if len(b.commands) != 3 {
		b.mu.Unlock()
		t.Fatal("unexpected preparation or replay")
	}
	a, z := b.commands[1], b.commands[2]
	b.mu.Unlock()
	if a.URL != z.URL || len(z.Attachments) != 1 || z.Attachments[0].Name != "step-2.png" || strings.Contains(z.Text, "first synthetic observation") || strings.Contains(z.Text, "synthetic-step-") {
		t.Fatal("snapshot continuation resent history or path")
	}
	r.ContextEpoch++
	r.RequestID = "migration"
	if _, err := (&Adapter{}).Stream(context.Background(), r); err == nil {
		t.Fatal("migration silently reuploaded old attachment")
	}
	if e.Status().Sent != 3 {
		t.Fatal("migration sent before attachment reauthorization")
	}
}

type streamingFrameBrowser struct {
	fakeBrowser
	release  <-chan struct{}
	quoted   bool
	requests int
}

func (b *streamingFrameBrowser) Run(ctx context.Context, c Command, emit func(Event) error) error {
	if c.URL == "" {
		return b.fakeBrowser.Run(ctx, c, emit)
	}
	b.requests++
	if b.quoted {
		if err := emit(Event{ID: c.ID, Kind: "text", Text: "Example:\nORCA_TOOL_V1 {\"id\":\"a\",\"name\":\"echo\",\"arguments\":{}}"}); err != nil {
			return err
		}
	} else {
		if err := emit(Event{ID: c.ID, Kind: "reasoning", Text: `ORCA_TOOL_V1 {"id":"not-a-call","name":"echo","arguments":{}}`}); err != nil {
			return err
		}
		for _, part := range []string{"ORCA_PRO", "GRESS_V2 {\"text\":\"先检查", "。\"}\n"} {
			if err := emit(Event{ID: c.ID, Kind: "text", Text: part}); err != nil {
				return err
			}
		}
		select {
		case <-b.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := emit(Event{ID: c.ID, Kind: "text", Text: `ORCA_TURN_V2 {"type":"final","text":"完成。"}`}); err != nil {
			return err
		}
	}
	return emit(Event{ID: c.ID, Kind: "done", URL: c.URL})
}

func TestProgressStreamsBeforeWebsiteCompletionAndIgnoresReasoningCalls(t *testing.T) {
	release := make(chan struct{})
	b := &streamingFrameBrowser{release: release}
	e := testEngine(t, b)
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ch, err := (&Adapter{}).Stream(ctx, provider.Request{ParentTaskID: "parent", ConversationID: "owner", RequestID: "r", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}, Tools: []provider.ToolSchema{{Name: "echo"}}})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	progress := false
	done := false
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if chunk.ToolCall != nil {
			t.Fatal("reasoning became executable")
		}
		if chunk.Type == provider.ChunkText {
			text += chunk.Text
			if !progress {
				if text != "先检查。" {
					t.Fatal("raw or incomplete frame leaked", text)
				}
				progress = true
				close(release)
			}
		}
		if chunk.Type == provider.ChunkDone {
			done = true
		}
	}
	if !progress || !done || text != "先检查。完成。" {
		t.Fatal("stream did not complete", text)
	}
}

func TestQuotedToolDoesNotTriggerCorrectionOrExecution(t *testing.T) {
	b := &streamingFrameBrowser{quoted: true}
	e := testEngine(t, b)
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	ch, err := (&Adapter{}).Stream(context.Background(), provider.Request{ParentTaskID: "parent", ConversationID: "owner", RequestID: "r", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}, Tools: []provider.ToolSchema{{Name: "echo"}}})
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	for chunk := range ch {
		if chunk.ToolCall != nil || chunk.Type == provider.ChunkDone {
			t.Fatal("example executed")
		}
		failed = failed || chunk.Err != nil
	}
	if !failed || b.requests != 1 {
		t.Fatal("example was repaired into executable output")
	}
}

type multiToolBrowser struct {
	fakeBrowser
	steps int
	url   string
}

func (b *multiToolBrowser) Run(ctx context.Context, c Command, emit func(Event) error) error {
	if c.URL == "" {
		return b.fakeBrowser.Run(ctx, c, emit)
	}
	b.steps++
	var output string
	if b.steps == 1 {
		b.url = c.URL
		output = "ORCA_PROGRESS_V2 {\"text\":\"检查中。\"}\n" + `ORCA_TURN_V2 {"type":"tools","calls":[{"id":"a","name":"synthetic_echo","arguments":{"value":7}},{"id":"b","name":"synthetic_echo","arguments":{"value":7}}]}`
	} else if b.steps == 2 {
		if c.URL != b.url {
			return errors.New("tool continuation changed website conversation")
		}
		for _, want := range []string{`"tool_call_id":"a"`, `"tool_call_id":"b"`, `"name":"synthetic_echo"`, `"status":"returned"`, "echo-result:7"} {
			if !strings.Contains(c.Text, want) {
				return errors.New("missing correlated tool result: " + want)
			}
		}
		if strings.Contains(c.Text, "synthetic user task") || strings.Contains(c.Text, "检查中。") {
			return errors.New("already acknowledged messages resent")
		}
		output = `ORCA_TURN_V2 {"type":"final","text":"两个结果已核对。"}`
	} else if b.steps == 3 {
		if c.URL != b.url || strings.Contains(c.Text, "两个结果已核对。") || !strings.Contains(c.Text, "synthetic follow-up") {
			return errors.New("next parent task lost the normalized final checkpoint")
		}
		output = `ORCA_TURN_V2 {"type":"final","text":"收到追问。"}`
	} else {
		return errors.New("unexpected generation")
	}
	if err := emit(Event{ID: c.ID, Kind: "text", Text: output}); err != nil {
		return err
	}
	return emit(Event{ID: c.ID, Kind: "done", URL: c.URL})
}

func TestActualAgentMultipleToolsProgressAndIncrementalContinuation(t *testing.T) {
	b := &multiToolBrowser{}
	e := testEngine(t, b)
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	registry := tool.NewRegistry()
	capability := &probeTool{}
	registry.Add(capability)
	gate := &probeGate{allow: true}
	var text strings.Builder
	sink := event.Lifecycle(event.FuncSink(func(ev event.Event) {
		if ev.Kind == event.Text {
			text.WriteString(ev.Text)
		}
	}))
	a := agent.New(&Adapter{name: "synthetic-web"}, registry, agent.NewSession("synthetic multi-tool"), agent.Options{Gate: gate}, sink)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Run(ctx, "synthetic user task"); err != nil {
		t.Fatal(err)
	}
	if capability.calls != 2 || gate.calls != 2 || b.steps != 2 || text.String() != "检查中。两个结果已核对。" {
		t.Fatalf("calls=%d gates=%d steps=%d text=%q", capability.calls, gate.calls, b.steps, text.String())
	}
	saved, err := loadState(e.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Calls) != 2 {
		t.Fatal("missing durable call records")
	}
	for _, c := range saved.Calls {
		if c.Status != "returned" {
			t.Fatal("result not acknowledged")
		}
	}
	if err := a.Run(ctx, "synthetic follow-up"); err != nil {
		t.Fatal(err)
	}
	if capability.calls != 2 || gate.calls != 2 || b.steps != 3 || text.String() != "检查中。两个结果已核对。收到追问。" {
		t.Fatal("follow-up repeated tools or lost the answer")
	}
	saved, err = loadState(e.path)
	if err != nil {
		t.Fatal(err)
	}
	generations, actions := 0, 0
	for _, budget := range saved.Tasks {
		generations += budget.Generations
		actions += budget.Actions
	}
	if len(saved.Tasks) != 2 || generations != 4 || actions != 2 {
		t.Fatalf("task attribution lost: tasks=%d generations=%d actions=%d", len(saved.Tasks), generations, actions)
	}
}
