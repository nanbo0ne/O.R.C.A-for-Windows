package control

import (
	"context"
	"errors"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
)

// Optional desktop observation; other transports remain unchanged.
func (c *Controller) workMonitorContext(ctx context.Context) context.Context {
	if sink, ok := c.sink.(interface {
		WorkMonitorContext(context.Context, string) context.Context
	}); ok {
		id, _ := agent.ParentTurn(ctx)
		return sink.WorkMonitorContext(ctx, id)
	}
	return ctx
}

func (c *Controller) workMonitorState(turn, phase string) {
	if sink, ok := c.sink.(interface{ WorkMonitorState(string, string) }); ok {
		sink.WorkMonitorState(turn, phase)
	}
}

func (c *Controller) workMonitorCompleted(turn string, err error) {
	phase := "stopped" // an auxiliary operation is not a committed final answer
	if err != nil && !errors.Is(err, context.Canceled) {
		phase = "error"
	}
	c.workMonitorState(turn, phase)
}
