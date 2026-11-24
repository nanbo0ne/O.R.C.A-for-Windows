package webbeta

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type loadingBrowser struct {
	fakeBrowser
	ready atomic.Bool
}

func (b *loadingBrowser) State() BrowserState {
	return BrowserState{Ready: b.ready.Load(), URL: Origin}
}

func TestBackgroundReadinessDoesNotSendOrEnable(t *testing.T) {
	b := &loadingBrowser{}
	e := testEngine(t, b)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- WaitUntilAvailable(ctx, e) }()
	select {
	case err := <-finished:
		t.Fatalf("did not wait for loading: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	b.ready.Store(true)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	n := len(b.commands)
	b.mu.Unlock()
	if n != 0 {
		t.Fatal("readiness sent a request")
	}
	s := e.Status().Settings
	s.Enabled = false
	if err := e.Configure(s); err != nil {
		t.Fatal(err)
	}
	if err := WaitUntilAvailable(ctx, e); err == nil || !strings.Contains(err.Error(), "尚未启用") {
		t.Fatal("disabled consent bypassed", err)
	}
	if e.Status().Settings.Enabled {
		t.Fatal("readiness enabled requests")
	}
}

func TestBackgroundReadinessTimeoutAndCancellation(t *testing.T) {
	e := testEngine(t, &loadingBrowser{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := WaitUntilAvailable(ctx, e); err == nil || !strings.Contains(err.Error(), "后台加载") {
		t.Fatal("missing bounded loading failure", err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if err := WaitUntilAvailable(ctx2, e); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestRestartRestoresConsentWithoutSending(t *testing.T) {
	b := &fakeBrowser{}
	e := testEngine(t, b)
	path := e.path
	e.Close()
	restored, err := NewEngineWithRequestLimit(path, b, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := WaitUntilAvailable(context.Background(), restored); err != nil {
		t.Fatal(err)
	}
	if !restored.Status().Settings.Enabled {
		t.Fatal("lost explicit consent")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.commands) != 0 {
		t.Fatal("restart sent an opening")
	}
}

func TestAvailabilityDistinguishesReadyFromEnabled(t *testing.T) {
	if err := AvailabilityError(nil); err == nil || !strings.Contains(err.Error(), "尚未打开") {
		t.Fatal("missing unopened state")
	}
	e := testEngine(t, &fakeBrowser{})
	s := e.Status().Settings
	s.Enabled = false
	if err := e.Configure(s); err != nil {
		t.Fatal(err)
	}
	if err := AvailabilityError(e); err == nil || !strings.Contains(err.Error(), "尚未启用") {
		t.Fatal("ready page presented as enabled")
	}
	s.Enabled = true
	if err := e.Configure(s); err != nil {
		t.Fatal(err)
	}
	if err := AvailabilityError(e); err != nil {
		t.Fatal(err)
	}
	e.fail(&Failure{Code: "restricted"})
	if err := AvailabilityError(e); err == nil || !strings.Contains(err.Error(), "暂停") {
		t.Fatal("restriction not respected")
	}
}

func TestToolEnvelopeRejectsQuotedUnknownAndDuplicate(t *testing.T) {
	tools := []provider.ToolSchema{{Name: "read_file"}}
	valid := `ORCA_TOOL_V1 {"id":"1","name":"read_file","arguments":{"path":"synthetic.txt"}}`
	if _, err := parseTool(valid, tools, nil); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"Example:\n" + valid, "```\n" + valid + "\n```", `ORCA_TOOL_V1 {"id":"2","name":"shell","arguments":{}}`, valid + "garbage", `ORCA_TOOL_V1 {"id":"3","name":"read_file","arguments":`} {
		if _, err := parseTool(raw, tools, nil); err == nil {
			t.Fatalf("accepted unsafe envelope %q", raw)
		}
	}
	if _, err := parseTool(valid, tools, []provider.Message{{ToolCalls: []provider.ToolCall{{ID: "1"}}}}); err == nil {
		t.Fatal("duplicate execution accepted")
	}
}

func TestToolRejectsAmbiguousJSONKeys(t *testing.T) {
	for _, raw := range []string{
		`ORCA_TOOL_V1 {"id":"1","name":"read_file","name":"shell","arguments":{}}`,
		`ORCA_TOOL_V1 {"id":"1","name":"read_file","arguments":{"path":"safe","path":"other"}}`,
		`ORCA_TOOL_V1 {"id":"1","name":"read_file","arguments":{"nested":[{"value":1,"value":2}]}}`,
	} {
		if _, err := parseTool(raw, []provider.ToolSchema{{Name: "read_file"}}, nil); err == nil {
			t.Fatal("ambiguous tool accepted")
		}
	}
}
func TestAuxiliaryWebIdentityIsIsolated(t *testing.T) {
	for _, purpose := range []provider.RequestPurpose{provider.RequestPurposeClassifier, provider.RequestPurposeCompaction, provider.RequestPurposeCompactionFallback} {
		r := provider.Request{ParentTaskID: "synthetic-parent", RequestID: "one", Purpose: purpose, ConversationID: "main-task-must-not-be-reused"}
		one, err := requestOwner(r)
		if err != nil {
			t.Fatal(err)
		}
		if one == r.ConversationID {
			t.Fatal("auxiliary request reused main task binding")
		}
		r.RequestID = "two"
		two, err := requestOwner(r)
		if err != nil || one == two {
			t.Fatal("auxiliary identities shared")
		}
		r.Tools = []provider.ToolSchema{{Name: "unwanted"}}
		if _, err = requestOwner(r); err == nil {
			t.Fatal("auxiliary tools accepted")
		}
	}
	if _, err := requestOwner(provider.Request{ParentTaskID: "synthetic-parent", RequestID: "one"}); err == nil {
		t.Fatal("unidentified request accepted")
	}
	if _, err := requestOwner(provider.Request{ParentTaskID: "synthetic-parent", ConversationID: "task"}); err == nil {
		t.Fatal("missing operation accepted")
	}
}

func TestAuxiliaryWebStreamUsesSeparateContext(t *testing.T) {
	b := &fakeBrowser{}
	e := testEngine(t, b)
	SetEngine(e)
	defer SetEngine(nil)
	for _, purpose := range []provider.RequestPurpose{provider.RequestPurposeClassifier, provider.RequestPurposeCompaction} {
		stream, err := (&Adapter{}).Stream(context.Background(), provider.Request{ParentTaskID: "synthetic-parent", RequestID: string(purpose), Purpose: purpose, DisableThinking: true, Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic summary"}}})
		if err != nil {
			t.Fatal(err)
		}
		var text string
		var done bool
		for chunk := range stream {
			if chunk.Type == provider.ChunkError {
				t.Fatal(chunk.Err)
			}
			if chunk.Type == provider.ChunkText {
				text += chunk.Text
			}
			done = done || chunk.Type == provider.ChunkDone
		}
		if text != "synthetic" || !done {
			t.Fatal("auxiliary stream not completed")
		}
	}
	if len(b.commands) != 4 || b.commands[1].URL == b.commands[3].URL || b.commands[1].Thinking || b.commands[3].Thinking {
		t.Fatal("auxiliary context or thinking policy not isolated")
	}
}
func TestFileBytesNotPersisted(t *testing.T) {
	raw, err := json.Marshal(provider.Message{Role: provider.RoleUser, Files: []provider.FileContent{{Name: "synthetic.txt", Digest: "hash", Data: []byte("secret-not-persisted")}}})
	if err != nil {
		t.Fatal(err)
	}
	var restored provider.Message
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.Files) != 1 || len(restored.Files[0].Data) != 0 {
		t.Fatal("attachment bytes persisted")
	}
}
func TestLifetimeCounterDoesNotLimitAccount(t *testing.T) {
	b := &fakeBrowser{}
	e := testEngine(t, b)
	e.mu.Lock()
	e.state.Sent = 20
	if err := saveState(e.path, e.state); err != nil {
		t.Fatal(err)
	}
	e.mu.Unlock()
	s, err := loadState(e.path)
	if err != nil || s.Sent != 20 {
		t.Fatal("telemetry not durable")
	}
	err = e.run(context.Background(), Command{ID: "over-budget"}, func(Event) error { return nil })
	if err != nil || len(b.commands) != 1 {
		t.Fatal("lifetime telemetry incorrectly limited account", err)
	}
}
func TestPoolFillsExactlyTwoAndRespectsOpeningRounds(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	s := e.Status().Settings
	s.OpeningRounds = 2
	s.ReservePaused = false
	if err := e.Configure(s); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		status := e.Status()
		if status.Ready == 2 && !status.Preparing {
			if status.Sent != 4 {
				t.Fatalf("sent %d", status.Sent)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("reserve preparation did not complete")
}
