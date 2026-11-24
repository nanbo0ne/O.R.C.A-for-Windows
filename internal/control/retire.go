package control

import (
	"context"
	"errors"
	"time"
)

var ErrControllerRetired = errors.New("controller identity retired; start a new conversation")

// Retire permanently closes foreground admission before requesting cancellation.
// It does not claim that an executing tool has stopped; callers must WaitIdle.
func (c *Controller) Retire() {
	c.mu.Lock()
	c.retired = true
	c.mu.Unlock()
	if c.jobs != nil {
		c.jobs.Stop()
	}
	c.Cancel()
}

func (c *Controller) Retired() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.retired
}

// WaitIdle observes actual Runner/Agent completion, including non-cooperative
// synchronous tools. A deadline leaves the controller retired and still running.
func (c *Controller) WaitIdle(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for c.Running() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	if c.Retired() && c.jobs != nil {
		return c.jobs.WaitStopped(ctx)
	}
	return nil
}
