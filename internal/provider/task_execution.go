package provider

import "context"

type serialTaskToolsKey struct{}
type taskHumanWaitKey struct{}

// WithTaskHumanWait installs a task-local active-time budget hook. Only the
// interactive host may enter a wait; tool arguments cannot suspend a budget.
func WithTaskHumanWait(ctx context.Context, pause func() func()) context.Context {
	return context.WithValue(ctx, taskHumanWaitKey{}, pause)
}

// BeginTaskHumanWait does not change cancellation, permissions or ownership.
func BeginTaskHumanWait(ctx context.Context) func() {
	pause, _ := ctx.Value(taskHumanWaitKey{}).(func() func())
	if pause != nil {
		return pause()
	}
	return func() {}
}

// WithSerialTaskTools sets a task-local execution policy. Providers set this in
// BeginTask; each Agent resets inherited policy before acquiring its own scope.
// Cancellation, deadlines and task ownership are otherwise unchanged.
func WithSerialTaskTools(ctx context.Context, serial bool) context.Context {
	return context.WithValue(ctx, serialTaskToolsKey{}, serial)
}

func SerialTaskTools(ctx context.Context) bool {
	serial, _ := ctx.Value(serialTaskToolsKey{}).(bool)
	return serial
}
