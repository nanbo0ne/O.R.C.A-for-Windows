package main

import (
	"encoding/json"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
)

func TestBrowserStateJSONCollectionsAlwaysArrays(t *testing.T) {
	s, _, id := browserFixture(t)
	oldChannel := channel
	channel = browserPreviewChannel
	defer func() { channel = oldChannel }()
	a := &App{browser: s, tabs: map[string]*WorkspaceTab{}}
	states := map[string]BrowserState{
		"normal":          s.state("owner"),
		"unknown-service": s.state("unknown"),
		"unknown-app":     a.GetBrowserState("unknown"),
		"failed-command":  a.BrowserCommand("unknown", BrowserCommand{Op: "new"}),
	}
	a.updateApplying.Store(true)
	states["admission-denied"] = a.BrowserCommand("unknown", BrowserCommand{Op: "new"})
	task, end, err := s.beginTask(computerParentTestContext(t), "owner", "state-contract", id, "fake/model")
	if err != nil {
		t.Fatal(err)
	}
	defer end(nil)
	states["task-without-consents"] = s.state("owner")
	if len(task.consents) != 0 {
		t.Fatal("fixture unexpectedly has consent receipts")
	}
	s.onEvent(browserhost.Event{TabID: id, Kind: "download", Download: &browserhost.Download{ID: "d", TabID: id, Status: "pending"}})
	s.onEvent(browserhost.Event{TabID: id, Kind: "upload", Upload: &browserhost.Upload{ID: "u", TabID: id, Status: "pending"}})
	states["populated-transfers"] = s.state("owner")
	channel = "stable"
	states["official-unregistered"] = a.GetBrowserState("owner")
	for name, state := range states {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"tabs", "consents", "downloads", "uploads"} {
				if _, ok := decoded[key].([]any); !ok {
					t.Fatalf("%s is missing, null or not an array: %s", key, data)
				}
			}
		})
	}
}
