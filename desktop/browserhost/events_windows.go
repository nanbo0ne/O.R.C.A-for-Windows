//go:build windows

package browserhost

import (
	"errors"
	"sync"
)

var (
	errNativeEventOverflow     = errors.New("browser event delivery overflowed; close this tab and open a new tab before continuing")
	errNativeHostEventOverflow = errors.New("browser event safety capacity exceeded; restart the browser host before continuing")
)

const maxNativeEventFaults = 64

type nativeEventFault struct {
	tab     *nativeTab
	event   Event
	pending bool
}

// The STA owns enqueue/forget and tab generations. Only this small fault store
// is shared with the existing event worker. It never calls COM, emits while
// holding its mutex, blocks on a consumer, or starts another goroutine.
type nativeEventSafety struct {
	mu     sync.Mutex
	faults map[string]*nativeEventFault
	host   *nativeEventFault
}

// enqueue must replace ALL native event sends, including transfer events.
// Once a notification is lost, no further operation may trust that tab's
// service-side state. Critical events are not silently treated as delivered.
func (s *nativeEventSafety) enqueue(events chan<- Event, tab *nativeTab, event Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.host != nil || s.faults[event.TabID] != nil {
		return false
	}
	select {
	case events <- event:
		return true
	default:
	}
	if tab == nil || tab.id != event.TabID || len(s.faults) >= maxNativeEventFaults {
		s.host = &nativeEventFault{event: Event{TabID: event.TabID, Kind: "error", Error: errNativeHostEventOverflow.Error()}, pending: true}
		return false
	}
	if s.faults == nil {
		s.faults = make(map[string]*nativeEventFault)
	}
	tab.generation++
	s.faults[tab.id] = &nativeEventFault{tab: tab, event: Event{TabID: tab.id, Kind: "error", Error: errNativeEventOverflow.Error()}, pending: true}
	return false
}

// check is required at the native tab/action boundary and before allowing a
// page navigation. Closing a failed tab remains permitted without this check.
func (s *nativeEventSafety) check(tab *nativeTab) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.host != nil {
		return errNativeHostEventOverflow
	}
	if tab != nil {
		if fault := s.faults[tab.id]; fault != nil {
			return errNativeEventOverflow
		}
	}
	return nil
}

// forget runs only after the old native tab has closed. An old asynchronous
// close must not clear a fault belonging to a newer tab using the same ID.
func (s *nativeEventSafety) forget(tab *nativeTab) {
	if tab == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if fault := s.faults[tab.id]; fault != nil && fault.tab == tab {
		delete(s.faults, tab.id)
	}
}

// dispatch runs on the existing event worker. Faults have priority over old
// queued state, which must not overwrite the error or resume an agent task.
// A blocked emitter may delay UI reporting, but cannot delay the STA's latch.
func (s *nativeEventSafety) dispatch(emit func(Event), event Event) {
	if emit == nil {
		return
	}
	s.flush(emit)
	s.mu.Lock()
	blocked := s.host != nil || s.faults[event.TabID] != nil
	s.mu.Unlock()
	if !blocked {
		emit(event)
	}
	s.flush(emit)
}

func (s *nativeEventSafety) flush(emit func(Event)) {
	var batch [maxNativeEventFaults + 1]Event
	count := 0
	s.mu.Lock()
	for _, fault := range s.faults {
		if fault.pending {
			batch[count] = fault.event
			count++
			fault.pending = false
		}
	}
	if s.host != nil && s.host.pending {
		batch[count] = s.host.event
		count++
		s.host.pending = false
	}
	s.mu.Unlock()
	for i := 0; i < count; i++ {
		emit(batch[i])
	}
}
