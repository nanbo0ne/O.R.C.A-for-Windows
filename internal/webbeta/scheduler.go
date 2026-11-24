// Package webbeta contains the isolated, account-scoped browser provider runtime.
package webbeta

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"
)

var ErrPaused = errors.New("web beta queue paused; inspect the webpage before resuming")

type queuedJob struct {
	ctx        context.Context
	background bool
	run        func(context.Context) error
	done       chan error
	started    bool // protected by Scheduler.mu
}

// Scheduler owns one account. A running operation retains its slot until it
// acknowledges cancellation; a cancelled caller alone never releases the slot.
type Scheduler struct {
	mu        sync.Mutex
	jobs      []*queuedJob
	starts    []time.Time
	rpm       int
	paused    bool
	wake      chan struct{}
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	spacing   time.Duration
	lastStart time.Time
	readyAt   time.Time
	now       func() time.Time
	cooldown  func(time.Duration) time.Duration
}

func NewScheduler(rpm int) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Scheduler{rpm: clampRPM(rpm), wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel, done: make(chan struct{}), spacing: time.Minute, now: time.Now, cooldown: jitteredCooldown}
	go s.loop()
	return s
}

// Jitter only extends the configured minimum. It is scheduling backoff, not a
// claim of human activity or a way to bypass upstream restrictions.
func jitteredCooldown(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	return base + time.Duration(rand.Int64N(int64(base/5)+1))
}

func (s *Scheduler) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	base := s.spacing / time.Duration(s.rpm)
	delay := s.cooldown(base)
	if delay < base {
		delay = base
	}
	if delay > base+base/5 {
		delay = base + base/5
	}
	start := s.lastStart
	if start.IsZero() {
		start = s.now()
	}
	// Generation time already consumes the start-to-start rate interval.
	// Do not charge another full interval after a slow response finishes.
	s.readyAt = start.Add(delay)
}
func clampRPM(n int) int {
	if n < 1 {
		return 1
	}
	if n > 10 {
		return 10
	}
	return n
}
func (s *Scheduler) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Scheduler) SetRPM(n int) { s.mu.Lock(); s.rpm = clampRPM(n); s.mu.Unlock(); s.signal() }
func (s *Scheduler) Pause()       { s.mu.Lock(); s.paused = true; s.mu.Unlock(); s.signal() }
func (s *Scheduler) Resume()      { s.mu.Lock(); s.paused = false; s.mu.Unlock(); s.signal() }
func (s *Scheduler) Pending() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.jobs) }
func (s *Scheduler) RejectPending(err error) {
	s.mu.Lock()
	jobs := s.jobs
	s.jobs = nil
	s.mu.Unlock()
	for _, j := range jobs {
		j.done <- err
	}
	s.signal()
}
func (s *Scheduler) Close() {
	s.cancel()
	s.RejectPending(context.Canceled)
	s.signal()
	<-s.done
}

func (s *Scheduler) Do(ctx context.Context, background bool, run func(context.Context) error) error {
	j := &queuedJob{ctx: ctx, background: background, run: run, done: make(chan error, 1)}
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return context.Canceled
	}
	s.jobs = append(s.jobs, j)
	s.mu.Unlock()
	s.signal()
	select {
	case err := <-j.done:
		return err
	case <-ctx.Done():
		s.signal()
		return s.cancelledReceipt(j, ctx.Err())
	case <-s.ctx.Done():
		return s.cancelledReceipt(j, context.Canceled)
	}
}

func (s *Scheduler) cancelledReceipt(j *queuedJob, cause error) error {
	s.mu.Lock()
	started := j.started
	if !started {
		for i, queued := range s.jobs {
			if queued == j {
				s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
				break
			}
		}
	}
	s.mu.Unlock()
	if started {
		// Browser.Run owns cancellation and its bounded stop/disconnect path.
		// The caller must not release session state or attachment snapshots early.
		if err := <-j.done; err != nil {
			return err
		}
	}
	return cause
}

func (s *Scheduler) next() (*queuedJob, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.jobs[:0]
	for _, j := range s.jobs {
		if j.ctx.Err() == nil {
			kept = append(kept, j)
		}
	}
	s.jobs = kept
	if s.paused || len(s.jobs) == 0 {
		return nil, 0
	}
	now := s.now()
	for len(s.starts) > 0 && now.Sub(s.starts[0]) >= s.spacing {
		s.starts = s.starts[1:]
	}
	var wait time.Duration
	if len(s.starts) > 0 {
		wait = s.spacing/time.Duration(s.rpm) - now.Sub(s.starts[len(s.starts)-1])
	}
	if len(s.starts) >= s.rpm {
		if d := s.spacing - now.Sub(s.starts[0]); d > wait {
			wait = d
		}
	}
	// A sampled start interval survives wakeups and configuration changes.
	if d := s.readyAt.Sub(now); d > wait {
		wait = d
	}
	if !s.lastStart.IsZero() {
		// Lowering RPM applies immediately; raising it cannot shorten a committed delay.
		if d := s.lastStart.Add(s.spacing / time.Duration(s.rpm)).Sub(now); d > wait {
			wait = d
		}
	}
	if wait > 0 {
		return nil, wait
	}
	i := 0
	for n, j := range s.jobs {
		if !j.background {
			i = n
			break
		}
	}
	j := s.jobs[i]
	j.started = true
	s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
	s.starts = append(s.starts, now)
	s.lastStart = now
	return j, 0
}
func (s *Scheduler) loop() {
	defer close(s.done)
	for {
		if s.ctx.Err() != nil {
			return
		}
		j, wait := s.next()
		if j != nil {
			ctx, cancel := context.WithCancel(j.ctx)
			stop := context.AfterFunc(s.ctx, cancel)
			err := ctx.Err()
			if err == nil {
				err = j.run(ctx)
			}
			s.finish()
			j.done <- err
			stop()
			cancel()
			continue
		}
		if wait <= 0 {
			wait = time.Hour
		}
		timer := time.NewTimer(wait)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-s.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}
