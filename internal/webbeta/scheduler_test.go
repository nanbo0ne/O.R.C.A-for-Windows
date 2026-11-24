package webbeta

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestQueuedCancelAndForegroundPriority(t *testing.T) {
	s := NewScheduler(10)
	defer s.Close()
	s.Pause()
	var mu sync.Mutex
	var order []string
	run := func(name string) func(context.Context) error {
		return func(context.Context) error { mu.Lock(); order = append(order, name); mu.Unlock(); return nil }
	}
	cancelled, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 3)
	go func() { finished <- s.Do(context.Background(), true, run("background")) }()
	go func() { finished <- s.Do(cancelled, false, run("cancelled")) }()
	go func() { finished <- s.Do(context.Background(), false, run("foreground")) }()
	deadline := time.After(time.Second)
	for s.Pending() != 3 {
		select {
		case <-deadline:
			t.Fatal("queue not populated")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("queued cancellation blocked")
	}
	s.mu.Lock()
	s.spacing = 20 * time.Millisecond
	s.mu.Unlock()
	s.Resume()
	for i := 0; i < 2; i++ {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("queue stuck")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "foreground" || order[1] != "background" {
		t.Fatalf("unexpected order %v", order)
	}
}

func TestCancelledCallerDoesNotReleaseRunningSlot(t *testing.T) {
	s := NewScheduler(10)
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	go s.Do(ctx, false, func(context.Context) error { close(started); <-release; return nil })
	<-started
	cancel()
	second := make(chan struct{})
	go s.Do(context.Background(), false, func(context.Context) error { close(second); return nil })
	select {
	case <-second:
		t.Fatal("released slot before stop acknowledgement")
	case <-time.After(20 * time.Millisecond):
	}
	s.mu.Lock()
	s.spacing = time.Millisecond
	s.mu.Unlock()
	close(release)
	s.signal()
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("slot never released")
	}
}

func TestRunningCancelWaitsForCleanupReceipt(t *testing.T) {
	s := NewScheduler(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer func() { once.Do(func() { close(release) }); s.Close() }()
	done := make(chan error, 1)
	go func() {
		done <- s.Do(ctx, false, func(ctx context.Context) error { close(started); <-ctx.Done(); <-release; return ctx.Err() })
	}()
	<-started
	cancel()
	select {
	case <-done:
		t.Fatal("caller observed cancellation before cleanup")
	case <-time.After(30 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("unexpected receipt: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup receipt lost")
	}
}

func TestRejectedQueueDoesNotReviveOnResume(t *testing.T) {
	s := NewScheduler(10)
	defer s.Close()
	s.Pause()
	done := make(chan error, 1)
	go func() {
		done <- s.Do(context.Background(), false, func(context.Context) error { t.Error("rejected job ran"); return nil })
	}()
	deadline := time.After(time.Second)
	for s.Pending() != 1 {
		select {
		case <-deadline:
			t.Fatal("job did not queue")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	s.RejectPending(ErrPaused)
	s.Resume()
	select {
	case err := <-done:
		if err != ErrPaused {
			t.Fatalf("wrong receipt: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("rejection did not reach caller")
	}
}

func TestRateIntervalIncludesGenerationTimeAndIsSampledOnce(t *testing.T) {
	now := time.Unix(1000, 0)
	samples := 0
	s := &Scheduler{rpm: 2, spacing: time.Minute, now: func() time.Time { return now }, cooldown: func(base time.Duration) time.Duration { samples++; return base + base/5 }}
	s.lastStart = now.Add(-20 * time.Second)
	s.starts = []time.Time{s.lastStart}
	s.jobs = []*queuedJob{{ctx: context.Background()}}
	s.finish()
	for i := 0; i < 10; i++ {
		job, wait := s.next()
		if job != nil || wait != 16*time.Second {
			t.Fatalf("unexpected admission or wait: %v %v", job, wait)
		}
	}
	if samples != 1 {
		t.Fatalf("resampled cooldown %d times", samples)
	}
	now = now.Add(15 * time.Second)
	if job, wait := s.next(); job != nil || wait != time.Second {
		t.Fatalf("early start: %v %v", job, wait)
	}
	now = now.Add(time.Second)
	if job, wait := s.next(); job == nil || wait != 0 {
		t.Fatalf("job not admitted: %v %v", job, wait)
	}
}

func TestCooldownRPMChangesCannotBypassLimits(t *testing.T) {
	now := time.Unix(1000, 0)
	s := &Scheduler{rpm: 2, spacing: time.Minute, now: func() time.Time { return now }, cooldown: func(base time.Duration) time.Duration { return base + base/5 }}
	s.lastStart = now
	s.jobs = []*queuedJob{{ctx: context.Background()}}
	s.finish()
	s.SetRPM(10)
	if job, wait := s.next(); job != nil || wait != 36*time.Second {
		t.Fatalf("raising RPM shortened frozen delay: %v %v", job, wait)
	}
	s.SetRPM(1)
	if job, wait := s.next(); job != nil || wait != time.Minute {
		t.Fatalf("lower RPM not respected: %v %v", job, wait)
	}
	s.SetRPM(2)
	s.starts = []time.Time{now.Add(-time.Second), now}
	if job, wait := s.next(); job != nil || wait != 59*time.Second {
		t.Fatalf("rolling limit bypassed: %v %v", job, wait)
	}
}

func TestLongGenerationDoesNotPaySecondRateInterval(t *testing.T) {
	now := time.Unix(1000, 0)
	s := &Scheduler{rpm: 2, spacing: time.Minute, now: func() time.Time { return now }, cooldown: func(base time.Duration) time.Duration { return base + base/5 }}
	s.lastStart = now.Add(-3 * time.Minute)
	s.starts = []time.Time{s.lastStart}
	s.jobs = []*queuedJob{{ctx: context.Background()}}
	s.finish()
	if job, wait := s.next(); job == nil || wait != 0 {
		t.Fatalf("slow generation was charged twice: %v %v", job, wait)
	}
}

func TestCancelledCooldownJobNeverRuns(t *testing.T) {
	now := time.Unix(1000, 0)
	s := &Scheduler{rpm: 2, spacing: time.Minute, now: func() time.Time { return now }, cooldown: jitteredCooldown}
	ctx, cancel := context.WithCancel(context.Background())
	s.jobs = []*queuedJob{{ctx: ctx}}
	s.finish()
	cancel()
	now = now.Add(2 * time.Minute)
	if job, _ := s.next(); job != nil || len(s.jobs) != 0 {
		t.Fatal("cancelled cooldown job survived")
	}
}

func TestJitterOnlyExtendsIntervalWithinTwentyPercent(t *testing.T) {
	for rpm := 1; rpm <= 10; rpm++ {
		base := time.Minute / time.Duration(rpm)
		for i := 0; i < 1000; i++ {
			got := jitteredCooldown(base)
			if got < base || got > base+base/5 {
				t.Fatalf("rpm %d: %v outside [%v,%v]", rpm, got, base, base+base/5)
			}
		}
	}
}
