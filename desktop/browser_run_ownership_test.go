package main

import (
	"context"
	"errors"
	"testing"
)

func TestBrowserRunAddressRejectsOwnerClosedAfterAdmission(t *testing.T) {
	s, b, id := browserFixture(t)
	task, end, err := s.beginTask(computerParentTestContext(t), "owner", "call", id, "fake/model")
	if err != nil {
		t.Fatal(err)
	}
	defer end(context.Canceled)
	s.closeOwner("owner")
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("owner close between admission and first address lookup panicked: %v", p)
		}
	}()
	if address, err := s.taskAddress(task); address != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("closed owner exposed an address: %q %v", address, err)
	}
	if b.observations != 0 || b.actions != 0 {
		t.Fatal("closed owner reached observation/input")
	}
}

func TestBrowserRunAddressRejectsRevokedLeaseAndPause(t *testing.T) {
	for _, state := range []string{"lease-replaced", "paused", "shutdown", "tab-removed"} {
		t.Run(state, func(t *testing.T) {
			s, _, id := browserFixture(t)
			task, end, err := s.beginTask(computerParentTestContext(t), "owner", "call", id, "fake/model")
			if err != nil {
				t.Fatal(err)
			}
			defer end(context.Canceled)
			if address, err := s.taskAddress(task); err != nil || address != "https://example.test/page" {
				t.Fatalf("live owner unavailable: %q %v", address, err)
			}
			s.mu.Lock()
			switch state {
			case "lease-replaced":
				s.leases[task.profile] = &browserTask{}
			case "paused":
				s.paused[task.owner] = true
			case "shutdown":
				s.closed = true
			case "tab-removed":
				delete(s.tabs, task.tab)
			}
			s.mu.Unlock()
			// Restore fabricated boundary state so fixture teardown has real tasks.
			defer func() { s.mu.Lock(); s.closed = false; s.leases[task.profile] = task; s.mu.Unlock() }()
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("revoked task lookup panicked: %v", p)
				}
			}()
			if address, err := s.taskAddress(task); address != "" || !errors.Is(err, errBrowserStopped) {
				t.Fatalf("revoked task address leaked: %q %v", address, err)
			}
		})
	}
}
