package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func syntheticUploadFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.txt")
	if err := os.WriteFile(path, []byte("synthetic-upload-original"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestBrowserUploadSnapshotIsImmutableAndPrivate(t *testing.T) {
	source := syntheticUploadFile(t)
	snap, err := newBrowserUploadSnapshot(source, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snap.remove()
	if snap.path == source || filepath.Base(snap.path) != "fixture.txt" || !snap.valid() {
		t.Fatal("invalid private snapshot")
	}
	if err = os.WriteFile(source, []byte("changed-original"), 0600); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(snap.path)
	if err != nil || string(content) != "synthetic-upload-original" {
		t.Fatal("snapshot changed with source")
	}
	if _, err = newBrowserUploadSnapshot(filepath.Dir(source), t.TempDir()); err == nil {
		t.Fatal("directory accepted")
	}
	if _, err = newBrowserUploadSnapshot("relative.txt", t.TempDir()); err == nil {
		t.Fatal("relative path accepted")
	}
}

func TestBrowserUploadSourceHandleRejectsReplacementAndMutation(t *testing.T) {
	for _, mode := range []string{"replace", "same-size-mutation"} {
		t.Run(mode, func(t *testing.T) {
			source := syntheticUploadFile(t)
			info, err := os.Stat(source)
			if err != nil {
				t.Fatal(err)
			}
			replacementBlocked := false
			raw, err := readBrowserUploadSource(source, func() {
				if mode == "replace" {
					if e := os.Rename(source, source+".original"); e != nil {
						replacementBlocked = true
						return
					}
				}
				if e := os.WriteFile(source, []byte(strings.Repeat("x", int(info.Size()))), 0600); e != nil {
					t.Fatal(e)
				}
				modified := info.ModTime().Add(time.Hour)
				if e := os.Chtimes(source, modified, modified); e != nil {
					t.Fatal(e)
				}
			})
			if replacementBlocked {
				if err != nil || string(raw) != "synthetic-upload-original" {
					t.Fatal("OS-rejected replacement did not preserve selected bytes", err)
				}
				t.Log("OS denied replacement of the open source handle")
				return
			}
			if err == nil || raw != nil {
				t.Fatal("changed source bytes accepted", string(raw), err)
			}
		})
	}
}

func TestBrowserUploadSnapshotRetainedOnUncertainClose(t *testing.T) {
	s, b, id := browserFixture(t)
	u := browserhost.Upload{ID: "u", TabID: id, URL: "https://example.test", Origin: "https://example.test", DocumentID: "d", Generation: 2, Status: "pending"}
	s.onEvent(browserhost.Event{Kind: "upload", TabID: id, Upload: &u})
	if err := s.prepareUpload("owner", u.ID, syntheticUploadFile(t)); err != nil {
		t.Fatal(err)
	}
	approval := s.state("owner").Approval.ID
	b.command = func(_, op, _ string) (string, error) {
		if op == "upload-approve" {
			return "", context.DeadlineExceeded
		}
		return "", nil
	}
	if state := s.approve("owner", BrowserCommand{ApprovalID: approval, Allow: true}); state.Error == "" {
		t.Fatal("uncertain native selection reported success")
	}
	s.mu.Lock()
	path := s.snapshots[id][0].path
	s.mu.Unlock()
	b.closeErr = context.DeadlineExceeded
	s.closeOwner("owner")
	if _, err := os.Stat(path); err != nil {
		t.Fatal("snapshot removed before confirmed native close", err)
	}
	s.shutdown()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("snapshot not released after native shutdown", err)
	}
}

func TestBrowserDownloadDestinationValidation(t *testing.T) {
	root := t.TempDir()
	if err := browserDownloadPath(filepath.Join(root, "new.txt")); err != nil {
		t.Fatal("regular local destination rejected", err)
	}
	for _, path := range []string{root, filepath.Join(root, "missing", "file.txt"), filepath.Join(root, "file.txt:stream")} {
		if err := browserDownloadPath(path); err == nil {
			t.Fatal("unsafe destination accepted", path)
		}
	}
}
func TestBrowserUploadExactUserApprovalAndOwnership(t *testing.T) {
	s, b, id := browserFixture(t)
	_ = s.register("other", "other-profile")
	u := browserhost.Upload{ID: "upload-1", TabID: id, URL: "https://example.test/form", Origin: "https://example.test", DocumentID: "document", Generation: 3, Status: "pending", RequiresApproval: true}
	s.onEvent(browserhost.Event{Kind: "upload", TabID: id, Upload: &u})
	if len(s.state("owner").Uploads) != 1 || len(s.state("other").Uploads) != 0 {
		t.Fatal("upload ownership leak")
	}
	source := syntheticUploadFile(t)
	if s.prepareUpload("other", u.ID, source) == nil {
		t.Fatal("other owner prepared upload")
	}
	if err := s.prepareUpload("owner", u.ID, source); err != nil {
		t.Fatal(err)
	}
	state := s.state("owner")
	if state.Approval == nil || !strings.Contains(state.Approval.Reason, "fixture.txt") || !strings.Contains(state.Approval.Reason, "SHA-256") {
		t.Fatal("missing exact file consent")
	}
	called := false
	b.command = func(tabID, op, value string) (string, error) {
		if op == "upload-approve" {
			called = true
			var body struct {
				ID    string
				Paths []string
			}
			if err := json.Unmarshal([]byte(value), &body); err != nil || body.ID != u.ID || len(body.Paths) != 1 || body.Paths[0] == source {
				t.Fatal("original path forwarded")
			}
			content, err := os.ReadFile(body.Paths[0])
			if err != nil || string(content) != "synthetic-upload-original" {
				t.Fatal("wrong snapshot")
			}
		}
		return "", nil
	}
	if st := s.approve("other", BrowserCommand{ApprovalID: state.Approval.ID, Allow: true}); st.Error == "" {
		t.Fatal("cross-owner upload approved")
	}
	if called {
		t.Fatal("native upload before approval")
	}
	s.approve("owner", BrowserCommand{ApprovalID: state.Approval.ID, Allow: true})
	if !called {
		t.Fatal("approved snapshot not dispatched")
	}
	s.mu.Lock()
	path := s.snapshots[id][0].path
	s.mu.Unlock()
	if _, err := os.Stat(path); err != nil {
		t.Fatal("selected snapshot removed too early")
	}
	s.command(context.Background(), "owner", BrowserCommand{Op: "close", TabID: id})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("snapshot retained after confirmed close")
	}
}
func TestBrowserUploadChangedDocumentAndSnapshotRejected(t *testing.T) {
	for _, mode := range []string{"document", "snapshot", "deny"} {
		t.Run(mode, func(t *testing.T) {
			s, b, id := browserFixture(t)
			u := browserhost.Upload{ID: "u", TabID: id, URL: "https://example.test", Origin: "https://example.test", DocumentID: "d", Generation: 2, Status: "pending"}
			s.onEvent(browserhost.Event{Kind: "upload", TabID: id, Upload: &u})
			if err := s.prepareUpload("owner", "u", syntheticUploadFile(t)); err != nil {
				t.Fatal(err)
			}
			state := s.state("owner")
			s.mu.Lock()
			path := s.snapshots[id][0].path
			s.mu.Unlock()
			if mode == "document" {
				u.DocumentID = "new"
				s.onEvent(browserhost.Event{Kind: "upload", TabID: id, Upload: &u})
			}
			if mode == "snapshot" {
				_ = os.Chmod(path, 0600)
				_ = os.WriteFile(path, []byte("mutated"), 0600)
			}
			b.command = func(_, op, _ string) (string, error) {
				if op == "upload-approve" {
					t.Fatal("invalid upload dispatched")
				}
				return "", nil
			}
			st := s.approve("owner", BrowserCommand{ApprovalID: state.Approval.ID, Allow: mode != "deny"})
			if mode != "deny" && st.Error == "" {
				t.Fatal("invalid upload reported success")
			}
		})
	}
}
func TestBrowserUploadWaitDoesNotInvalidateNativeRequest(t *testing.T) {
	s, b, id := browserFixture(t)
	autoBrowserConsent(s)
	ctx := computerParentTestContext(t)
	task, end, err := s.beginTask(ctx, "owner", "parent", id, "fake/model")
	if err != nil {
		t.Fatal(err)
	}
	defer end(nil)
	u := browserhost.Upload{ID: "u", TabID: id, Status: "pending"}
	s.onEvent(browserhost.Event{Kind: "upload", TabID: id, Upload: &u})
	done := make(chan error, 1)
	go func() { done <- s.waitUploads(task) }()
	select {
	case <-done:
		t.Fatal("did not await upload")
	case <-time.After(80 * time.Millisecond):
	}
	if b.observations != 0 {
		t.Fatal("upload waiting reobserved the page")
	}
	u.Status = "selected"
	s.onEvent(browserhost.Event{Kind: "upload", TabID: id, Upload: &u})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("selection did not release wait")
	}
}
func TestBrowserManualAndApprovedAgentPopup(t *testing.T) {
	for _, agentPopup := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual", true: "agent"}[agentPopup], func(t *testing.T) {
			s, _, id := browserFixture(t)
			if !agentPopup {
				s.onEvent(browserhost.Event{TabID: id, Kind: "popup", URL: "https://popup.test/path"})
				deadline := time.Now().Add(time.Second)
				for len(s.state("owner").Tabs) != 2 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if st := s.state("owner"); len(st.Tabs) != 2 || st.Tabs[1].URL != "https://popup.test/path" {
					t.Fatal(st)
				}
				return
			}
			autoBrowserConsent(s)
			task, end, err := s.beginTask(computerParentTestContext(t), "owner", "parent", id, "fake/model")
			if err != nil {
				t.Fatal(err)
			}
			defer end(nil)
			s.onEvent(browserhost.Event{TabID: id, Kind: "popup", URL: "https://popup.test/path"})
			if err = s.handlePopup(task); err != nil {
				t.Fatal(err)
			}
			if len(s.state("owner").Tabs) != 2 || task.tab == id || task.budget.actions != 1 {
				t.Fatal("approved popup did not transfer controlled tab")
			}
		})
	}
}
func TestBrowserToolProbeAllowsHarmlessProse(t *testing.T) {
	p := &controlTestProvider{stream: func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		ch, _ := browserProbeReply(r)
		var chunks []provider.Chunk
		chunks = append(chunks, provider.Chunk{Type: provider.ChunkText, Text: "Checking the requested capability."})
		for c := range ch {
			if c.Type != provider.ChunkDone {
				chunks = append(chunks, c)
			}
		}
		return controlChunks(chunks...), nil
	}}
	if err := probeBrowserTools(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}
func TestBrowserLateViewportCannotCrossConversation(t *testing.T) {
	s, b, id := browserFixture(t)
	_ = s.register("other", "other-profile")
	otherID := s.command(context.Background(), "other", BrowserCommand{Op: "new"}).ActiveTabID
	v := BrowserViewport{TabID: id, Visible: true, Width: 600, Height: 400, Scale: 1}
	if err := s.present(context.Background(), "owner", v); err != nil {
		t.Fatal(err)
	}
	v.TabID = otherID
	if err := s.present(context.Background(), "other", v); err != nil {
		t.Fatal(err)
	}
	if err := s.present(context.Background(), "owner", BrowserViewport{TabID: id, Visible: false}); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	shown := b.view
	b.mu.Unlock()
	if !shown.Visible || shown.TabID != otherID {
		t.Fatal("old cleanup hid new owner's browser")
	}
	old := channel
	channel = browserPreviewChannel
	defer func() { channel = old }()
	a := &App{browser: s, activeTabID: "other", tabs: map[string]*WorkspaceTab{"owner": {ID: "owner"}, "other": {ID: "other"}}}
	// Match registration identity without touching user configuration.
	s.mu.Lock()
	s.owners["owner"].profile = browserProfileKey("owner", "", "")
	s.mu.Unlock()
	if err := a.SetBrowserViewport("owner", BrowserViewport{TabID: id, Visible: true, Width: 600, Height: 400, Scale: 1}); !errors.Is(err, errBrowserOwner) {
		t.Fatal("late resize from old owner accepted", err)
	}
}
