package control

import (
	"context"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

// A drain wait exists to stop a turn from returning while a web child may still
// write. It must not become an unbounded hang: an uncooperative native stream
// would otherwise keep the parent turn running forever, which silently drops
// every later Submit because the controller still looks busy.
const webTaskDrainTimeout = 30 * time.Second

// Keep the original engine alive across identity changes. A task can also own a
// Web child even when its primary provider is an API model.
func captureWebTaskDrain(taskID string) func() {
	initial := webbeta.CurrentEngine()
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), webTaskDrainTimeout)
		defer cancel()
		current := webbeta.CurrentEngine()
		if initial != nil {
			_ = initial.WaitTaskDrain(ctx, taskID)
		}
		if current != nil && current != initial {
			_ = current.WaitTaskDrain(ctx, taskID)
		}
	}
}
