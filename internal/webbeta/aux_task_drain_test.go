package webbeta

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func TestActualCompactNowJoinsAuxiliaryProducer(t *testing.T) {
	b := &heldTaskStop{entered: make(chan struct{}), release: make(chan struct{}), started: make(chan struct{}), confirmed: true}
	e := testEngine(t, b)
	var once sync.Once
	release := func() { once.Do(func() { close(b.release) }) }
	t.Cleanup(release)
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	e.state.Conversations = []Conversation{{ID: "fixture", URL: Origin + "/a/chat/s/fixture", Phase: "ready"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := agent.NewSession("synthetic")
	for i := 0; i < 8; i++ {
		session.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("synthetic history ", 2000)})
		session.Add(provider.Message{Role: provider.RoleAssistant, Content: "synthetic answer"})
	}
	before := len(session.Messages)
	a := agent.New(&Adapter{}, tool.NewRegistry(), session, agent.Options{}, event.FuncSink(func(ev event.Event) {
		if ev.Kind == event.Reasoning {
			cancel()
		}
	}))
	done := make(chan error, 1)
	go func() { done <- a.CompactNow(ctx, "") }()
	select {
	case <-b.started:
		cancel()
	case err := <-done:
		t.Fatalf("compact ended without model request: %v", err)
	case <-time.After(time.Second):
		t.Fatal("compact did not start")
	}
	select {
	case <-b.entered:
	case <-time.After(time.Second):
		t.Fatal("auxiliary producer did not enter stop")
	}
	select {
	case err := <-done:
		t.Fatalf("compact returned while auxiliary producer held: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	release()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("compact drain held after producer exit")
	}
	if len(session.Messages) != before {
		t.Fatal("cancelled compact changed history")
	}
	if c := e.Status().Conversations[0]; !strings.HasPrefix(c.Owner, "internal:compaction:") {
		t.Fatal("compact was merged into the main webpage binding")
	}
}

func TestTaskScopeTracksMultipleAuxiliaryLeasesAndRejectsLateAttach(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	finish, err := e.beginOperation("main")
	if err != nil {
		t.Fatal(err)
	}
	scope, _, err := e.enterTaskScope(nil, "main", "parent")
	if err != nil {
		t.Fatal(err)
	}
	otherFinish, err := e.beginOperation("other")
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := e.enterTaskScope(nil, "other", "other-parent")
	if err != nil {
		t.Fatal(err)
	}
	var releases []func()
	for _, owner := range []string{"internal:classifier:r1", "internal:compaction:r2"} {
		release, err := e.acquireStream(owner, scope)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
		t.Cleanup(release)
	}
	otherRelease, err := e.acquireStream("internal:classifier:r3", other)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(otherRelease)
	e.releaseTaskScope(scope, time.Millisecond, func() {}, finish)
	if _, err := e.acquireStream("internal:classifier:late", scope); !errors.Is(err, ErrTaskDraining) {
		t.Fatal("late attachment accepted", err)
	}
	releases[0]()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := e.WaitTaskDrain(ctx, "parent"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("second auxiliary producer not joined", err)
	}
	releases[1]()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := e.WaitTaskDrain(ctx2, "parent"); err != nil {
		t.Fatal("unrelated scope held main drain", err)
	}
	if _, err := e.acquireStream("internal:classifier:stale", scope); !errors.Is(err, ErrTaskDraining) {
		t.Fatal("ended scope resurrected", err)
	}
	otherRelease()
	e.releaseTaskScope(other, time.Second, func() {}, otherFinish)
}

func TestAuxiliaryPurposeScopeReleaseJoinsProducer(t *testing.T) {
	for _, purpose := range []provider.RequestPurpose{provider.RequestPurposeClassifier, provider.RequestPurposeCompactionFallback} {
		t.Run(string(purpose), func(t *testing.T) {
			b := &heldTaskStop{entered: make(chan struct{}), release: make(chan struct{}), confirmed: true}
			e := testEngine(t, b)
			var once sync.Once
			release := func() { once.Do(func() { close(b.release) }) }
			t.Cleanup(release)
			old := CurrentEngine()
			SetEngine(e)
			defer SetEngine(old)
			e.state.Conversations = []Conversation{{ID: "fixture", URL: Origin + "/a/chat/s/fixture", Phase: "ready"}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx, end, err := (&Adapter{}).BeginTask(ctx, "main-owner", "parent")
			if err != nil {
				t.Fatal(err)
			}
			ch, err := (&Adapter{}).Stream(ctx, provider.Request{ParentTaskID: "parent", RequestID: "synthetic", Purpose: purpose, Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}})
			if err != nil {
				end()
				t.Fatal(err)
			}
			for c := range ch {
				if c.Type == provider.ChunkReasoning {
					cancel()
					break
				}
			}
			<-b.entered
			done := make(chan struct{})
			go func() { end(); close(done) }()
			select {
			case <-done:
				t.Fatal("auxiliary purpose escaped scope release")
			case <-time.After(30 * time.Millisecond):
			}
			release()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("auxiliary scope did not drain")
			}
		})
	}
}
