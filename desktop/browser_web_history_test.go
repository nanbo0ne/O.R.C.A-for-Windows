package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

// This is an in-memory implementation, never WebView2, HTTP, or a model.
type browserHistoryWebFixture struct {
	mu       sync.Mutex
	commands []webbeta.Command
	response string
}

func (b *browserHistoryWebFixture) State() webbeta.BrowserState {
	return webbeta.BrowserState{Ready: true, URL: webbeta.Origin}
}
func (b *browserHistoryWebFixture) Logout(context.Context) error { return nil }
func (b *browserHistoryWebFixture) Run(ctx context.Context, command webbeta.Command, emit func(webbeta.Event) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	b.commands = append(b.commands, command)
	response := b.response
	b.mu.Unlock()
	url := command.URL
	if url == "" {
		url = webbeta.Origin + "/a/chat/s/synthetic-browser-owned"
		response = "synthetic opening"
	}
	if err := emit(webbeta.Event{ID: command.ID, Kind: "text", Text: response}); err != nil {
		return err
	}
	return emit(webbeta.Event{ID: command.ID, Kind: "done", URL: url})
}

type browserHistoryWebProvider struct {
	*webbeta.Adapter
	fixture  *browserHistoryWebFixture
	prepare  func(provider.Request) string
	requests []provider.Request
}

func (p *browserHistoryWebProvider) Stream(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, r)
	response := p.prepare(r)
	p.fixture.mu.Lock()
	p.fixture.response = response
	p.fixture.mu.Unlock()
	return p.Adapter.Stream(ctx, r)
}

func browserHistoryToolFrame(id, name, arguments string) string {
	body, _ := json.Marshal(map[string]any{"type": "tool", "id": id, "name": name, "arguments": json.RawMessage(arguments)})
	return "ORCA_TURN_V3 " + string(body)
}

func TestBrowserRunWebAdapterKeepsChatAndOnlyUploadsCurrentImage(t *testing.T) {
	s, native, _ := browserFixture(t)
	autoBrowserConsent(s)
	native.observe = func(o *browserhost.Observation) {
		im := image.NewRGBA(image.Rect(0, 0, 2, 2))
		im.SetRGBA(0, 0, color.RGBA{R: uint8(o.Generation), A: 255})
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, im); err != nil {
			panic(err)
		}
		o.Screenshot, o.MIME = encoded.Bytes(), "image/png"
		o.Elements = nil
		o.ViewportWidth, o.ViewportHeight = 160, 80
	}
	web := &browserHistoryWebFixture{}
	engine, err := webbeta.NewEngine(filepath.Join(t.TempDir(), "synthetic-web-state.json"), web)
	if err != nil {
		t.Fatal(err)
	}
	old := webbeta.CurrentEngine()
	webbeta.SetEngine(engine)
	defer func() { webbeta.SetEngine(old); engine.Close() }()
	settings := webbeta.DefaultSettings()
	settings.Enabled, settings.ReservePaused, settings.RPM = true, true, 10
	if err := engine.Configure(settings); err != nil {
		t.Fatal(err)
	}
	p := &browserHistoryWebProvider{Adapter: &webbeta.Adapter{}, fixture: web}
	p.prepare = func(r provider.Request) string {
		if ch, ok := scopedBrowserProbe(r); ok {
			for c := range ch {
				if c.ToolCall != nil {
					return browserHistoryToolFrame(c.ToolCall.ID, c.ToolCall.Name, c.ToolCall.Arguments)
				}
			}
		}
		if len(r.Tools) == 1 && r.Tools[0].Name == "report_image" {
			probe := r
			probe.Messages = r.Messages[len(r.Messages)-1:]
			return browserHistoryToolFrame("image-probe", "report_image", qualificationAnswer(t, probe))
		}
		if len(r.Tools) == 0 {
			return `ORCA_TURN_V3 {"type":"final","text":"{\"satisfied\":true,\"reason\":\"fresh synthetic evidence\"}"}`
		}
		if native.actions == 0 {
			return "inspected\n" + browserHistoryToolFrame("one-action", "browser_action", `{"kind":"wait"}`)
		}
		return browserHistoryToolFrame("complete", "browser_complete", `{"summary":"synthetic done"}`)
	}
	ctx, cancel := context.WithTimeout(computerParentTestContext(t), 55*time.Second)
	defer cancel()
	parent, _ := agent.ParentTurn(ctx)
	wrapped := &browserUsageProvider{Provider: p, parent: parent}
	result, err := s.run(ctx, "owner", "outer", browserInput(), wrapped, "synthetic/web")
	if err != nil || result != "synthetic done" || native.actions != 1 {
		t.Fatalf("result=%q err=%v actions=%d", result, err, native.actions)
	}
	if len(p.requests) != 5 {
		t.Fatalf("unexpected model generations: %d", len(p.requests))
	}
	for i, request := range p.requests {
		if request.ParentTaskID != parent || request.ConversationID != p.requests[0].ConversationID {
			t.Fatal("scope identity changed")
		}
		if i == 0 {
			continue
		}
		before, _ := json.Marshal(p.requests[i-1].Messages)
		after, _ := json.Marshal(request.Messages[:len(p.requests[i-1].Messages)])
		if sha256.Sum256(before) != sha256.Sum256(after) {
			t.Fatal("acknowledged prefix hash changed")
		}
		for _, message := range request.Messages[:len(p.requests[i-1].Messages)] {
			for _, im := range message.Images {
				if im.Data != "" || im.Path == "" || im.Name == "" || im.MediaType == "" || im.Size == 0 {
					t.Fatal("old image bytes retained or identity lost")
				}
			}
		}
	}
	web.mu.Lock()
	commands := append([]webbeta.Command(nil), web.commands...)
	web.mu.Unlock()
	if len(commands) != 6 {
		t.Fatalf("new chat, replay or missing request: %d", len(commands))
	}
	for i, command := range commands[1:] {
		if command.URL != commands[1].URL || command.URL == "" {
			t.Fatal("web conversation changed")
		}
		if i == 0 {
			if len(command.Attachments) != 0 {
				t.Fatal("unexpected pre-consent attachment")
			}
		} else if len(command.Attachments) != 1 {
			t.Fatalf("step %d resent historical attachments: %d", i, len(command.Attachments))
		}
		if i > 0 && strings.Contains(command.Text, "Synthetic browser capability test") {
			t.Fatal("acknowledged prompt replayed")
		}
	}
	for _, want := range []string{`"tool_call_id":"one-action"`, `"name":"browser_action"`, "native_completed"} {
		if !strings.Contains(commands[4].Text, want) {
			t.Fatalf("actual correlated tool result missing: %s", want)
		}
	}
	if bytes.Equal(commands[3].Attachments[0].Data, commands[4].Attachments[0].Data) {
		t.Fatal("fixture did not supply a different next screenshot")
	}
	if len(engine.Status().Conversations) != 1 {
		t.Fatal("browser run created more than one web chat")
	}
}

func TestBrowserHistoryClearsOnlyAttachmentBytesWithoutChangingHashes(t *testing.T) {
	var requests []provider.Request
	p := &controlTestProvider{stream: func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		requests = append(requests, r)
		return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: "confirmed"}), nil
	}}
	h := &browserRunHistory{conversation: "synthetic", parent: "parent", calls: map[string]bool{}}
	first := provider.Message{Role: provider.RoleUser, Content: "first", Images: []provider.ImageContent{{Path: "snapshot:one", Name: "one.png", MediaType: "image/png", Size: 3, Data: "old"}}, Files: []provider.FileContent{{Name: "one.txt", MediaType: "text/plain", Digest: "unchanged-digest", Data: []byte("old")}}}
	if _, _, err := h.collect(context.Background(), p, []provider.Message{first}, nil); err != nil {
		t.Fatal(err)
	}
	second := provider.Message{Role: provider.RoleUser, Content: "second", Images: []provider.ImageContent{{Path: "snapshot:two", Name: "two.png", MediaType: "image/png", Size: 3, Data: "new"}}}
	if _, _, err := h.collect(context.Background(), p, []provider.Message{second}, nil); err != nil {
		t.Fatal(err)
	}
	old := requests[1].Messages[0]
	if old.Images[0].Data != "" || len(old.Files[0].Data) != 0 || old.Files[0].Digest != first.Files[0].Digest {
		t.Fatal("old bytes/digest incorrect")
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(old)
	if !bytes.Equal(a, b) {
		t.Fatal("attachment identity hash changed")
	}
	if requests[0].Messages[0].Images[0].Data != "old" || string(requests[0].Messages[0].Files[0].Data) != "old" || requests[1].Messages[2].Images[0].Data != "new" {
		t.Fatal("request snapshots alias history mutation")
	}
}

func TestBrowserRunFailedNativeActionNeverFabricatesSuccessOrReplays(t *testing.T) {
	s, native, _ := browserFixture(t)
	autoBrowserConsent(s)
	failure := errors.New("synthetic uncertain native result")
	native.execute = func(context.Context) error { return failure }
	decisions := 0
	p := browserCompleter(func(provider.Request) <-chan provider.Chunk {
		decisions++
		return browserCall("uncertain", "browser_action", `{"kind":"wait"}`)
	})
	if _, err := s.run(computerParentTestContext(t), "owner", "outer", browserInput(), p, "synthetic/api"); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if decisions != 1 || native.actions != 1 {
		t.Fatal("uncertain operation replayed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if receipts := s.owners["owner"].task.budget.executions; len(receipts) != 1 || receipts[0].Outcome != "unconfirmed" {
		t.Fatalf("invented native success: %+v", receipts)
	}
}

func TestBrowserImageProbeRejectsMismatchedEvidence(t *testing.T) {
	h := &browserRunHistory{conversation: "synthetic", parent: "parent", calls: map[string]bool{}}
	p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
		return browserCall("bad-probe", "report_image", `{"colors":"wrong,wrong","shape":"circle"}`), nil
	}}
	if err := probeBrowserImage(context.Background(), p, h); err == nil {
		t.Fatal("invalid image evidence passed")
	}
	last := h.messages[len(h.messages)-1]
	if last.Role != provider.RoleTool || last.ToolCallID != "bad-probe" || strings.Contains(last.Content, "support confirmed") || !strings.Contains(last.Content, "failed") {
		t.Fatal("probe invented success")
	}
}

func TestBrowserObservationAttachmentHasEnabledExtension(t *testing.T) {
	data, _, _, err := computerQualificationImage()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(data)
	m, err := browserObservationMessage(browserhost.Observation{TabID: "tab", DocumentID: "doc", Generation: 2, MIME: "image/png", Screenshot: raw}, true)
	if err != nil {
		t.Fatal(err)
	}
	vault := webbeta.NewVault()
	if _, err := vault.Grant("owner", "request", m.Images[0].Name, raw); err != nil {
		t.Fatal("actual browser screenshot rejected by web adapter", err)
	}
}

func TestBrowserHistoryDoesNotCommitFailedStream(t *testing.T) {
	h := &browserRunHistory{conversation: "owned", parent: "parent", messages: []provider.Message{{Role: provider.RoleSystem, Content: "fixed goal"}}, calls: map[string]bool{}}
	p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
		out := make(chan provider.Chunk, 1)
		out <- provider.Chunk{Type: provider.ChunkText, Text: "unconfirmed fragment"}
		close(out)
		return out, nil
	}}
	if _, _, err := h.collect(context.Background(), p, []provider.Message{{Role: provider.RoleUser, Content: "unconfirmed request"}}, nil); err == nil {
		t.Fatal("missing terminal confirmation accepted")
	}
	if len(h.messages) != 1 || h.messages[0].Content != "fixed goal" || len(h.calls) != 0 {
		t.Fatal("failed generation was committed as acknowledged history")
	}
}
