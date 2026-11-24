//go:build windows

package browserhost

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestNativeEventOverflowFailsClosed(t *testing.T) {
	var safety nativeEventSafety
	tab := &nativeTab{id: "tab", generation: 7}
	events := make(chan Event, 1)
	if !safety.enqueue(events, tab, Event{TabID: tab.id, Kind: "title"}) {
		t.Fatal("first event rejected")
	}
	if safety.enqueue(events, tab, Event{TabID: tab.id, Kind: "user-input"}) {
		t.Fatal("overflow reported as delivered")
	}
	if !errors.Is(safety.check(tab), errNativeEventOverflow) || tab.generation != 8 {
		t.Fatalf("overflow did not invalidate action fence: generation=%d error=%v", tab.generation, safety.check(tab))
	}
	// Nil COM pointers are intentional: overflow must not synchronously close
	// the tab or any active transfer while inside a native event callback.
	if tab.closed {
		t.Fatal("overflow reentered native close")
	}
	var seen []Event
	safety.dispatch(func(e Event) { seen = append(seen, e) }, <-events)
	if len(seen) != 1 || seen[0].Kind != "error" || seen[0].Error != errNativeEventOverflow.Error() {
		t.Fatalf("old queued state replaced the safety error: %+v", seen)
	}
	if safety.enqueue(events, tab, Event{TabID: tab.id, Kind: "loaded"}) || safety.check(tab) == nil {
		t.Fatal("draining the queue restored unsafe action capability")
	}
	if tab.generation != 8 {
		t.Fatal("repeated overflow changed the faulted generation")
	}
	safety.forget(tab)
	restarted := &nativeTab{id: tab.id, generation: 1}
	if err := safety.check(restarted); err != nil {
		t.Fatal(err)
	}
	if !safety.enqueue(events, restarted, Event{TabID: restarted.id, Kind: "loaded"}) {
		t.Fatal("closed/restarted tab remained latched")
	}
}

func TestNativeEventBlockedConsumerDoesNotBlockProducer(t *testing.T) {
	var safety nativeEventSafety
	tab := &nativeTab{id: "tab", generation: 1}
	events := make(chan Event, 1)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	seen := make(chan Event, 3)
	emit := func(e Event) {
		if e.Kind == "title" {
			close(entered)
			<-release
		}
		seen <- e
	}
	safety.enqueue(events, tab, Event{TabID: tab.id, Kind: "title"})
	go func() {
		defer close(finished)
		safety.dispatch(emit, <-events)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("consumer never entered")
	}
	// This channel models a service holding ioMu while waiting for native work.
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		safety.enqueue(events, tab, Event{TabID: tab.id, Kind: "history"})
		safety.enqueue(events, tab, Event{TabID: tab.id, Kind: "popup"})
	}()
	select {
	case <-producerDone:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("STA producer waited for blocked service emitter")
	}
	if !errors.Is(safety.check(tab), errNativeEventOverflow) {
		close(release)
		t.Fatal("action was not rejected while service emitter was blocked")
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("consumer did not recover")
	}
	for _, kind := range []string{"title", "error"} {
		select {
		case event := <-seen:
			if event.Kind != kind {
				t.Fatalf("event order: got %s, want %s", event.Kind, kind)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing %s after emitter recovery", kind)
		}
	}
}

func TestNativeEventCriticalOverflowCannotBeIgnored(t *testing.T) {
	for _, kind := range []string{"user-input", "popup", "blocked", "navigation", "error", "download", "upload"} {
		t.Run(kind, func(t *testing.T) {
			var safety nativeEventSafety
			tab := &nativeTab{id: "failed"}
			other := &nativeTab{id: "other"}
			events := make(chan Event, 1)
			safety.enqueue(events, other, Event{TabID: other.id, Kind: "title"})
			if safety.enqueue(events, tab, Event{TabID: tab.id, Kind: kind}) || safety.check(tab) == nil {
				t.Fatal("critical event loss did not reject native actions")
			}
			if err := safety.check(other); err != nil {
				t.Fatalf("unaffected tab was rejected: %v", err)
			}
			var seen []Event
			safety.dispatch(func(e Event) { seen = append(seen, e) }, <-events)
			if len(seen) != 2 || seen[0].TabID != tab.id || seen[0].Kind != "error" || seen[1].TabID != other.id {
				t.Fatalf("fault did not precede ordinary delivery: %+v", seen)
			}
		})
	}
}

func TestNativeEventFaultStoreIsBounded(t *testing.T) {
	var safety nativeEventSafety
	events := make(chan Event)
	for i := 0; i < 10000; i++ {
		tab := &nativeTab{id: fmt.Sprint(i)}
		if safety.enqueue(events, tab, Event{TabID: tab.id, Kind: "popup"}) {
			t.Fatal("unconsumed event reported as delivered")
		}
	}
	if len(safety.faults) != maxNativeEventFaults || safety.host == nil {
		t.Fatalf("unbounded fault store: %d", len(safety.faults))
	}
	if !errors.Is(safety.check(&nativeTab{id: "new"}), errNativeHostEventOverflow) {
		t.Fatal("capacity overflow did not reject new native actions")
	}
	var count int
	safety.dispatch(func(e Event) {
		if e.Kind != "error" {
			t.Fatal("host overflow delivered stale normal event")
		}
		count++
	}, Event{Kind: "loaded"})
	if count != maxNativeEventFaults+1 {
		t.Fatalf("fault notification count=%d", count)
	}
	safety.dispatch(func(Event) { t.Fatal("faults were emitted without a bound") }, Event{Kind: "loaded"})
}

func TestNativeEventUnknownTabFailsHostClosed(t *testing.T) {
	var safety nativeEventSafety
	if safety.enqueue(nil, nil, Event{TabID: "untracked", Kind: "error"}) {
		t.Fatal("nil queue accepted event")
	}
	if !errors.Is(safety.check(nil), errNativeHostEventOverflow) {
		t.Fatal("unattributed event loss silently allowed native actions")
	}
}

func TestNativeEventLateCloseCannotClearNewFault(t *testing.T) {
	var safety nativeEventSafety
	old := &nativeTab{id: "reused"}
	current := &nativeTab{id: old.id}
	safety.enqueue(nil, current, Event{TabID: current.id, Kind: "error"})
	safety.forget(old)
	if !errors.Is(safety.check(current), errNativeEventOverflow) {
		t.Fatal("old tab close cleared new tab fault")
	}
}
