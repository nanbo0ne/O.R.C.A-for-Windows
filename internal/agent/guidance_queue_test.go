package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func guidanceReceipt(t *testing.T, ack <-chan error) error {
	t.Helper()
	select {
	case err, ok := <-ack:
		if !ok {
			t.Fatal("receipt closed without an outcome")
		}
		if _, ok := <-ack; ok {
			t.Fatal("receipt delivered more than one outcome")
		}
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("guidance receipt was not resolved")
		return nil
	}
}

func TestGuidanceAdmissionSealedBeforeAnswerCommitted(t *testing.T) {
	committed, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first := true
	a := New(&fakeProvider{reply: "done"}, tool.NewRegistry(), NewSession(""), Options{}, event.FuncSink(func(e event.Event) {
		if e.Kind == event.AnswerCommitted && first {
			first = false
			close(committed)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}))
	if _, ok := a.TrySteerRich(RichInput{Text: "idle"}, "idle", nil, "idle"); ok {
		t.Fatal("idle agent accepted guidance")
	}
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, "first task") }()
	select {
	case <-committed:
	case <-ctx.Done():
		t.Fatal("answer never reached commit boundary")
	}
	ack, accepted := a.TrySteerRich(RichInput{Text: "late"}, "late", nil, "late-id")
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if accepted || ack != nil {
		t.Fatal("accepted guidance after final queue seal")
	}
	var nextAck <-chan error
	a.sink = event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnStarted {
			var ok bool
			nextAck, ok = a.TrySteerRich(RichInput{Text: "current"}, "current", nil, "current-id")
			if !ok {
				t.Error("new run did not reopen admission")
			}
		}
	})
	if err := a.Run(context.Background(), "next task"); err != nil {
		t.Fatal(err)
	}
	if err := guidanceReceipt(t, nextAck); err != nil {
		t.Fatal(err)
	}
	for _, m := range a.Session().Snapshot() {
		if text, ok := SteerDisplayText(m.Content); ok && text != "current" {
			t.Fatalf("stale guidance reached successor: %q", text)
		}
	}
}

func TestGuidanceQueuedBeforeSealIsConsumedAndCorrelated(t *testing.T) {
	var a *Agent
	var ack <-chan error
	queued := false
	consumed := 0
	a = New(&fakeProvider{reply: "done"}, tool.NewRegistry(), NewSession(""), Options{}, event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.Message:
			if queued {
				return
			}
			queued = true
			var ok bool
			ack, ok = a.TrySteerRich(RichInput{Text: "model guidance"}, "visible guidance", nil, "client-id")
			if !ok {
				t.Error("queue sealed before the final empty check")
			}
			select {
			case <-ack:
				t.Error("enqueue was acknowledged before consumption")
			default:
			}
		case event.Steer:
			consumed++
			if e.ItemID != "client-id" || e.MessageID == "" || e.Text != "visible guidance" {
				t.Errorf("uncorrelated consumed event: %+v", e)
			}
		}
	}))
	if err := a.Run(context.Background(), "task"); err != nil {
		t.Fatal(err)
	}
	if err := guidanceReceipt(t, ack); err != nil || consumed != 1 {
		t.Fatalf("receipt=%v consumed=%d", err, consumed)
	}
}

func TestGuidanceCancelledBeforeConsumptionRejectsEveryReceipt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var a *Agent
	var receipts []<-chan error
	ids := []string{"first-id", "second-id"}
	a = New(&fakeProvider{reply: "done"}, tool.NewRegistry(), NewSession(""), Options{}, event.FuncSink(func(e event.Event) {
		if e.Kind != event.TurnStarted {
			return
		}
		for _, id := range ids {
			ack, ok := a.TrySteerRich(RichInput{Text: id}, id, nil, id)
			if !ok {
				t.Error("active turn refused guidance")
			}
			receipts = append(receipts, ack)
		}
		cancel()
	}))
	if err := a.Run(ctx, "task"); !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v", err)
	}
	for i, ack := range receipts {
		if err := guidanceReceipt(t, ack); !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), ids[i]) {
			t.Fatalf("uncorrelated cancellation receipt: %v", err)
		}
	}
	if _, ok := a.TrySteerRich(RichInput{Text: "late"}, "late", nil, "late"); ok {
		t.Fatal("cancelled run left admission open")
	}
	a.sink = event.Discard
	if err := a.Run(context.Background(), "successor"); err != nil {
		t.Fatal(err)
	}
	for _, m := range a.Session().Snapshot() {
		if _, ok := SteerDisplayText(m.Content); ok {
			t.Fatal("cancelled guidance survived into successor")
		}
	}
}

type guidanceFailureProvider struct {
	fail func()
	err  error
}

func (*guidanceFailureProvider) Name() string { return "synthetic" }
func (p *guidanceFailureProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.fail()
	return nil, p.err
}

func TestGuidanceReceiptRejectedOnProviderErrorAndPanic(t *testing.T) {
	for _, panicRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "panic"}[panicRun], func(t *testing.T) {
			cause := errors.New("synthetic provider failure")
			var ack <-chan error
			p := &guidanceFailureProvider{err: cause}
			a := New(p, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
			p.fail = func() {
				var ok bool
				ack, ok = a.TrySteerRich(RichInput{Text: "pending"}, "pending", nil, "failure-id")
				if !ok {
					t.Error("active agent refused guidance")
				}
				if panicRun {
					panic(cause)
				}
			}
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				if err := a.Run(context.Background(), "task"); !errors.Is(err, cause) {
					t.Errorf("provider error lost: %v", err)
				}
			}()
			if panicRun && recovered != cause {
				t.Fatalf("unexpected panic: %v", recovered)
			}
			if err := guidanceReceipt(t, ack); err == nil || !strings.Contains(err.Error(), "failure-id") {
				t.Fatalf("pending receipt not rejected: %v", err)
			}
			if _, ok := a.TrySteerRich(RichInput{}, "", nil, "late"); ok {
				t.Fatal("failed run left admission open")
			}
		})
	}
}

func TestGuidanceReceiptRejectedAtStepLimit(t *testing.T) {
	var a *Agent
	var ack <-chan error
	a = New(&fakeProvider{reply: "done"}, tool.NewRegistry(), NewSession(""), Options{MaxSteps: 1}, event.FuncSink(func(e event.Event) {
		if e.Kind == event.Message {
			ack, _ = a.TrySteerRich(RichInput{Text: "pending"}, "pending", nil, "limit-id")
		}
	}))
	if err := a.Run(context.Background(), "task"); err == nil {
		t.Fatal("expected step limit")
	}
	if err := guidanceReceipt(t, ack); err == nil || !strings.Contains(err.Error(), "limit-id") {
		t.Fatalf("pending receipt not rejected: %v", err)
	}
}

func TestGuidanceHydrationPrefersValidatedBytesOverCacheAndLoader(t *testing.T) {
	a := New(nil, nil, NewSession(""), Options{ImageLoader: func(context.Context, provider.ImageContent) (provider.ImageContent, error) {
		t.Error("validated snapshot was reopened")
		return provider.ImageContent{}, errors.New("must not reopen")
	}}, event.Discard)
	a.imageCache["same.png"] = provider.ImageContent{Path: "same.png", Data: "old-cache"}
	for _, path := range []string{"same.png", "uncached.png"} {
		messages := []provider.Message{{Role: provider.RoleUser, Images: []provider.ImageContent{{Path: path, Data: "validated-snapshot"}}}}
		got := a.hydrateImageMessages(context.Background(), messages)
		if len(got[0].Images) != 1 || got[0].Images[0].Data != "validated-snapshot" {
			t.Fatalf("validated snapshot replaced: %+v", got)
		}
	}
}
