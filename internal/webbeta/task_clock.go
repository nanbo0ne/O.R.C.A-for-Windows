package webbeta

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type taskClockKey struct{}

// The active timer has no fixed Deadline: approval can move it. The original
// caller's deadline and cancellation still propagate through Context.
type activeTaskContext struct{ context.Context }

func (c activeTaskContext) Err() error {
	if c.Context.Err() != nil && errors.Is(context.Cause(c.Context), context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return c.Context.Err()
}

type taskTimeoutError struct{}

func (*taskTimeoutError) Error() string {
	return (&taskBudgetError{reason: "active_time", used: int64(taskLifetime / time.Second), limit: int64(taskLifetime / time.Second)}).Error()
}
func (*taskTimeoutError) Unwrap() []error { return []error{context.DeadlineExceeded, ErrTaskBudget} }
func (e *taskTimeoutError) CancellationError(cause error) error {
	return provider.NoRequestReplay(fmt.Errorf("%s: %w", e.Error(), errors.Join(cause, e)))
}

type taskClock struct {
	engine *Engine
	id     string
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelCauseFunc
	timer  *time.Timer
	stop   func() bool
	left   time.Duration
	since  time.Time
	waitAt time.Time
	waits  int
	save   func(time.Duration) error
}

func newTaskClock(parent context.Context, remaining time.Duration, save func(time.Duration) error) (context.Context, *taskClock) {
	ctx, cancel := context.WithCancelCause(parent)
	c := &taskClock{ctx: ctx, cancel: cancel, left: remaining, since: time.Now(), save: save}
	c.timer = time.AfterFunc(remaining, c.expire)
	c.stop = context.AfterFunc(ctx, func() {
		c.mu.Lock()
		c.timer.Stop()
		c.mu.Unlock()
	})
	return activeTaskContext{ctx}, c
}

func (c *taskClock) expire() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx.Err() != nil || c.waits > 0 {
		return
	}
	remaining := c.left - time.Since(c.since)
	if remaining > 0 {
		c.timer.Reset(remaining)
		return
	}
	c.cancel(&taskTimeoutError{})
}

func (c *taskClock) pause() func() {
	c.mu.Lock()
	if c.ctx.Err() != nil {
		c.mu.Unlock()
		return func() {}
	}
	if c.waits == 0 {
		c.left -= time.Since(c.since)
		if c.left <= 0 {
			c.cancel(&taskTimeoutError{})
			c.mu.Unlock()
			return func() {}
		}
		c.waitAt = time.Now()
		c.timer.Stop()
	}
	c.waits++
	c.mu.Unlock()
	var once sync.Once
	return func() { once.Do(c.resume) }
}

func (c *taskClock) resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits--
	if c.waits > 0 || c.ctx.Err() != nil {
		return
	}
	// Persist before another model/tool action can use the extended budget.
	if err := c.save(time.Since(c.waitAt)); err != nil {
		c.cancel(err)
		return
	}
	c.since = time.Now()
	c.timer.Reset(c.left)
}

func (c *taskClock) close() {
	c.cancel(context.Canceled)
	c.stop()
	c.mu.Lock()
	c.timer.Stop()
	c.mu.Unlock()
}
