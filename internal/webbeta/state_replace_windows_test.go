//go:build windows

package webbeta

import (
	"errors"
	"golang.org/x/sys/windows"
	"testing"
	"time"
)

func TestStateReplacementRetryIsBoundedAndKeepsSamePaths(t *testing.T) {
	for _, failures := range []int{3, 100} {
		calls := 0
		err := replaceStateWithRetry("own-temp", "own-state", func(from, to string) error {
			if from != "own-temp" || to != "own-state" {
				t.Fatal("changed checkpoint paths")
			}
			calls++
			if calls <= failures {
				return windows.ERROR_ACCESS_DENIED
			}
			return nil
		}, func(time.Duration) {})
		if failures == 3 && (err != nil || calls != 4) {
			t.Fatal(err, calls)
		}
		if failures == 100 && (!errors.Is(err, windows.ERROR_ACCESS_DENIED) || calls != 7) {
			t.Fatal("permanent failure hidden or unbounded", err, calls)
		}
	}
	calls := 0
	err := replaceStateWithRetry("from", "to", func(string, string) error { calls++; return windows.ERROR_DISK_FULL }, func(time.Duration) { t.Fatal("retried unrelated error") })
	if !errors.Is(err, windows.ERROR_DISK_FULL) || calls != 1 {
		t.Fatal(err, calls)
	}
}
