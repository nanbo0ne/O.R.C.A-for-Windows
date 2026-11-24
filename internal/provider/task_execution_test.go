package provider

import (
	"context"
	"testing"
)

func TestSerialTaskToolsScopeAndCancellation(t *testing.T) {
	type ownerKey struct{}
	base, cancel := context.WithCancel(context.WithValue(context.Background(), ownerKey{}, "owner"))
	serial := WithSerialTaskTools(base, true)
	child := WithSerialTaskTools(serial, false)
	if SerialTaskTools(base) || !SerialTaskTools(serial) || SerialTaskTools(child) || child.Value(ownerKey{}) != "owner" {
		t.Fatal("execution policy escaped its scope or lost ownership")
	}
	cancel()
	if serial.Err() != context.Canceled || child.Err() != context.Canceled {
		t.Fatal("lost cancellation")
	}
}

func TestHumanWaitHookIsOptInAndKeepsCancellation(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	BeginTaskHumanWait(base)()
	paused, resumed := 0, 0
	ctx := WithTaskHumanWait(base, func() func() {
		paused++
		return func() { resumed++ }
	})
	finish := BeginTaskHumanWait(ctx)
	cancel()
	finish()
	if paused != 1 || resumed != 1 || ctx.Err() != context.Canceled {
		t.Fatal("lost task hook or cancellation", paused, resumed, ctx.Err())
	}
}
