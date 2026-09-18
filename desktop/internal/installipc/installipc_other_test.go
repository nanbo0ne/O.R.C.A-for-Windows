//go:build !windows

package installipc

import (
	"context"
	"testing"
)

func TestUnsupportedPlatformIsNoOp(t *testing.T) {
	if sent, err := RequestShutdown("unused"); sent || err != nil {
		t.Fatalf("RequestShutdown = %v, %v", sent, err)
	}
	stop, err := Listen(context.Background(), "unused", func() { t.Fatal("unexpected callback") })
	if err != nil {
		t.Fatal(err)
	}
	stop()
	stop()
}
