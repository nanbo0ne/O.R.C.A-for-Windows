// Package monitor provides optional, memory-only observation of live work.
// Producers never wait for the reader, and an inactive sink never opens bodies.
package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	MaxBytes    = 24 << 20 // reserve the remainder of 32 MiB for bounded copies/snapshots
	MaxRequests = 100
	MaxBody     = 256 << 10
	MaxSnapshot = 512 << 10
	maxEntries  = 8192
)

type Entry struct {
	Generation string          `json:"generation"`
	Seq        uint64          `json:"seq"`
	TabID      string          `json:"tabId"`
	TurnID     string          `json:"turnId"`
	RequestID  string          `json:"requestId,omitempty"`
	AttemptID  string          `json:"attemptId,omitempty"`
	Time       int64           `json:"time"`
	Kind       string          `json:"kind"`
	Phase      string          `json:"phase,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	Incomplete bool            `json:"incomplete,omitempty"`
}

type Snapshot struct {
	Generation string  `json:"generation"`
	Entries    []Entry `json:"entries"`
	Cursor     uint64  `json:"cursor"`
	Evicted    uint64  `json:"evicted"`
	Dropped    uint64  `json:"dropped"`
	Bytes      int     `json:"bytes"`
	Expired    bool    `json:"expired"`
}

type Store struct {
	mu              sync.Mutex
	active          atomic.Bool
	dropped         atomic.Uint64
	generation, tab string
	entries         []Entry
	bytes           int
	seq, evicted    uint64
	requests        []string
	phases          map[string]string
	phaseTurns      []string
	expires         time.Time
	timer           *time.Timer
}

var generations atomic.Uint64

func New(tab string) *Store {
	s := &Store{tab: tab, generation: strconv.FormatUint(generations.Add(1), 10), expires: time.Now().Add(5 * time.Second), phases: make(map[string]string)}
	s.active.Store(true)
	s.timer = time.AfterFunc(5*time.Second, s.expire)
	return s
}

func (s *Store) expire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active.Load() {
		return
	}
	if remaining := time.Until(s.expires); remaining > 0 {
		s.timer.Reset(remaining)
		return
	}
	s.active.Store(false)
	s.entries, s.requests, s.bytes = nil, nil, 0
	s.phases, s.phaseTurns = nil, nil
}

func (s *Store) Dropped() {
	if s.Active() {
		s.dropped.Add(1)
	}
}

func (s *Store) Generation() string { return s.generation }
func (s *Store) Active() bool       { return s != nil && s.active.Load() }
func (s *Store) Close() {
	if s == nil {
		return
	}
	s.active.Store(false)
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.entries, s.requests = nil, nil
	s.phases, s.phaseTurns = nil, nil
	s.bytes = 0
	s.mu.Unlock()
}

// Record bounds producer work and drops contention instead of blocking inference.
// makeData runs only after the subscription/lease check, under a try-lock.
func (s *Store) Record(turn, request, kind, phase string, makeData func() (json.RawMessage, bool)) {
	s.record(turn, request, "", kind, phase, makeData)
}

func (s *Store) record(turn, request, attempt, kind, phase string, makeData func() (json.RawMessage, bool)) {
	if !s.Active() {
		return
	}
	if !s.mu.TryLock() {
		s.dropped.Add(1)
		return
	}
	defer s.mu.Unlock()
	if !s.active.Load() {
		return
	}
	if time.Now().After(s.expires) {
		s.active.Store(false)
		s.entries, s.requests, s.bytes = nil, nil, 0
		s.phases, s.phaseTurns = nil, nil
		return
	}
	previous := s.phases[turn]
	if kind == "phase" && phase == previous {
		return
	}
	if (previous == "stopped" || previous == "final" || previous == "error") && phase != "" {
		if kind == "phase" {
			return
		}
		phase = ""
	}
	if previous == "cancelling" && phase != "" && phase != "stopped" && phase != "final" && phase != "error" {
		if kind == "phase" {
			return
		}
		phase = ""
	}
	var data json.RawMessage
	incomplete := false
	if makeData != nil {
		data, incomplete = makeData()
	}
	if len(data) > MaxBody {
		data = json.RawMessage(`{"omitted":"payload limit"}`)
		incomplete = true
	}
	if kind == "request" {
		for len(s.requests) >= MaxRequests && len(s.entries) > 0 {
			s.evict()
		}
	}
	s.seq++
	e := Entry{Generation: s.generation, Seq: s.seq, TabID: s.tab, TurnID: turn, RequestID: request, AttemptID: attempt, Time: time.Now().UnixMilli(), Kind: kind, Phase: phase, Data: data, Incomplete: incomplete}
	cost := len(data) + 512
	for len(s.entries) > 0 && (s.bytes+cost > MaxBytes || len(s.entries) >= maxEntries) {
		s.evict()
	}
	s.entries = append(s.entries, e)
	if kind == "request" {
		s.requests = append(s.requests, request)
	}
	s.bytes += cost
	if phase != "" {
		if _, exists := s.phases[turn]; !exists {
			if len(s.phaseTurns) >= 128 {
				delete(s.phases, s.phaseTurns[0])
				s.phaseTurns = s.phaseTurns[1:]
				s.dropped.Add(1) // an older turn's phase guard is no longer retained
			}
			s.phaseTurns = append(s.phaseTurns, turn)
		}
		s.phases[turn] = phase
	}
}

func (s *Store) evict() {
	if s.entries[0].Kind == "request" && len(s.requests) > 0 {
		s.requests = s.requests[1:]
	}
	s.bytes -= len(s.entries[0].Data) + 512
	s.entries[0] = Entry{}
	s.entries = s.entries[1:]
	s.evicted++
}

func (s *Store) Snapshot(after uint64) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Now().After(s.expires) {
		s.active.Store(false)
		s.entries, s.requests, s.bytes = nil, nil, 0
		s.phases, s.phaseTurns = nil, nil
	}
	v := Snapshot{Generation: s.generation, Entries: []Entry{}, Cursor: after, Evicted: s.evicted, Dropped: s.dropped.Load(), Bytes: s.bytes, Expired: !s.active.Load()}
	if v.Expired {
		return v
	}
	s.expires = time.Now().Add(5 * time.Second)
	n := 0
	for _, e := range s.entries {
		if e.Seq <= after {
			continue
		}
		cost := len(e.Data) + 512
		if n+cost > MaxSnapshot {
			break
		}
		v.Entries = append(v.Entries, e) // immutable sanitized data; never the source body
		v.Cursor = e.Seq
		n += cost
	}
	return v
}

type Binding struct {
	Sink func() *Store
	Turn string
}
type contextKey struct{}
type requestKey struct{}
type requestInfo struct{ ID, Purpose string }

func WithBinding(ctx context.Context, b Binding) context.Context {
	return context.WithValue(ctx, contextKey{}, b)
}
func WithRequest(ctx context.Context, id, purpose string) context.Context {
	return context.WithValue(ctx, requestKey{}, requestInfo{id, purpose})
}

// ObserveHTTP is called immediately before Do, once for every retry attempt.
// Only the JSON body is observed: never headers, URLs, cookies or credentials.
func ObserveHTTP(ctx context.Context, req *http.Request) func(int, error) {
	b, ok := ctx.Value(contextKey{}).(Binding)
	if !ok || b.Sink == nil {
		return func(int, error) {}
	}
	s := b.Sink()
	if !s.Active() {
		return func(int, error) {}
	}
	ri, _ := ctx.Value(requestKey{}).(requestInfo)
	id := ri.ID + "/" + strconv.FormatUint(generations.Add(1), 10)
	s.record(b.Turn, ri.ID, id, "request", "wait-first", func() (json.RawMessage, bool) {
		if req.GetBody == nil {
			return json.RawMessage(`{"omitted":"body not replayable"}`), true
		}
		r, err := req.GetBody()
		if err != nil {
			return nil, true
		}
		defer r.Close()
		data, partial := ScanJSON(r)
		wrapped, _ := json.Marshal(struct {
			ProviderRequestID string          `json:"providerRequestId"`
			Purpose           string          `json:"purpose"`
			Body              json.RawMessage `json:"body"`
		}{ri.ID, ri.Purpose, data})
		return wrapped, partial
	})
	return func(status int, err error) {
		phase := ""
		if err != nil || status >= 400 {
			phase = "wait" // transport failures may retry; only TurnDone is terminal
		}
		if current := b.Sink(); current != s {
			return
		}
		s.record(b.Turn, ri.ID, id, "transport", phase, func() (json.RawMessage, bool) {
			// Errors can contain echoed credentials/body; retain only the status.
			d, _ := json.Marshal(map[string]any{"status": status, "failed": err != nil})
			return d, false
		})
	}
}
