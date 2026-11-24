package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type browserUploadSnapshot struct {
	path, root, name, hash string
	size                   int64
}
type browserUploadConsent struct {
	approval BrowserApproval
	request  browserhost.Upload
	snapshot *browserUploadSnapshot
}

func newBrowserUploadSnapshot(source, root string) (*browserUploadSnapshot, error) {
	if err := browserhost.ValidateManagedPath(root); err != nil {
		return nil, err
	}
	raw, err := readBrowserUploadSource(source, nil)
	if err != nil {
		return nil, err
	}
	private := filepath.Join(root, "upload-snapshots", browserID())
	if err = browserhost.ValidateManagedPath(private); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(private, 0700); err != nil {
		return nil, err
	}
	if err = browserhost.ValidateManagedPath(private); err != nil {
		return nil, err
	}
	// Store verified handle bytes through the existing attachment writer. Do not
	// reopen the user path after validating it; that would reintroduce TOCTOU.
	rel, err := control.SaveAttachmentDataURLAt(private, filepath.Base(source), "data:application/octet-stream;base64,"+base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		_ = os.Remove(private)
		return nil, err
	}
	path := filepath.Join(private, filepath.FromSlash(rel))
	snap := &browserUploadSnapshot{path: path, root: private, name: filepath.Base(source)}
	hash := sha256.Sum256(raw)
	snap.hash = hex.EncodeToString(hash[:])
	snap.size = int64(len(raw))
	destination := filepath.Join(private, snap.name)
	if err = browserhost.ValidateManagedPath(destination); err != nil {
		snap.remove()
		return nil, err
	}
	if err = os.Rename(path, destination); err != nil {
		snap.remove()
		return nil, err
	}
	snap.path = destination
	if err = os.Chmod(snap.path, 0400); err != nil {
		snap.remove()
		return nil, err
	}
	return snap, nil
}

func readBrowserUploadSource(source string, afterOpen func()) ([]byte, error) {
	if !filepath.IsAbs(source) || strings.HasPrefix(source, `\\`) {
		return nil, errors.New("upload requires a regular absolute local file selected by the user")
	}
	if err := browserhost.ValidateTransferPath(source); err != nil {
		return nil, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("upload source must be a regular non-symlink file")
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(filepath.Clean(resolved), filepath.Clean(source)) {
		return nil, errors.New("upload source cannot traverse a symlink or junction")
	}
	if strings.Contains(source[len(filepath.VolumeName(source)):], ":") || info.Size() <= 0 || info.Size() > 25*1024*1024 {
		return nil, errors.New("upload must be a 1-byte to 25-MB regular file without alternate data streams")
	}
	f, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("upload source changed while opening")
	}
	if afterOpen != nil {
		afterOpen()
	}
	raw, err := io.ReadAll(io.LimitReader(f, 25*1024*1024+1))
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	current, err := os.Lstat(source)
	if err != nil {
		return nil, err
	}
	resolvedAfter, err := filepath.EvalSymlinks(source)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(opened, after) || !os.SameFile(opened, current) || !current.Mode().IsRegular() || current.Mode()&os.ModeSymlink != 0 || !strings.EqualFold(resolved, resolvedAfter) || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) || int64(len(raw)) != opened.Size() {
		return nil, errors.New("upload source changed while copying; select it again")
	}
	return raw, nil
}
func (s *browserUploadSnapshot) valid() bool {
	raw, err := readBrowserUploadSource(s.path, nil)
	if err != nil || int64(len(raw)) != s.size {
		return false
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]) == s.hash
}
func (s *browserUploadSnapshot) remove() {
	if s == nil {
		return
	}
	if browserhost.ValidateManagedPath(s.path) != nil || browserhost.ValidateManagedPath(s.root) != nil {
		return
	}
	rel, err := filepath.Rel(s.root, s.path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return
	}
	_ = os.Chmod(s.path, 0600)
	_ = os.Remove(s.path)
	_ = os.Remove(filepath.Join(s.root, ".orca", "attachments"))
	_ = os.Remove(filepath.Join(s.root, ".orca"))
	_ = os.Remove(s.root)
}
func (s *browserService) cleanupSnapshotsLocked(tabID string) {
	for _, snap := range s.snapshots[tabID] {
		snap.remove()
	}
	delete(s.snapshots, tabID)
}

func (a *App) chooseBrowserUpload(s *browserService, owner string, cmd BrowserCommand) BrowserState {
	s.mu.Lock()
	u, ok := s.uploads[cmd.Value]
	tab := s.tabs[cmd.TabID]
	valid := ok && u.TabID == cmd.TabID && tab != nil && tab.owner == owner && u.Status == "pending"
	s.mu.Unlock()
	if !valid {
		return s.fail(owner, errBrowserOwner)
	}
	if a.ctx == nil {
		return s.fail(owner, errors.New("native file picker unavailable"))
	}
	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Choose one file to prepare for browser upload", Filters: []wruntime.FileFilter{{DisplayName: "All files", Pattern: "*.*"}}})
	if err != nil {
		return s.fail(owner, err)
	}
	if path == "" {
		return s.state(owner)
	}
	if err = s.prepareUpload(owner, u.ID, path); err != nil {
		return s.fail(owner, err)
	}
	return s.state(owner)
}
func (s *browserService) prepareUpload(owner, requestID, path string) error {
	s.mu.Lock()
	u, ok := s.uploads[requestID]
	tab := s.tabs[u.TabID]
	o := s.owners[owner]
	valid := ok && tab != nil && tab.owner == owner && o != nil && u.Status == "pending" && o.uploadApproval == nil && o.clearApproval == nil && (o.task == nil || o.task.pending == nil)
	s.mu.Unlock()
	if !valid {
		return errors.New("upload request expired, unowned, or another approval is pending")
	}
	snapshot, err := newBrowserUploadSnapshot(path, s.root)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.uploads[requestID]
	tab = s.tabs[u.TabID]
	o = s.owners[owner]
	if !ok || current.Status != "pending" || current.DocumentID != u.DocumentID || current.Generation != u.Generation || tab == nil || tab.owner != owner || o == nil || o.uploadApproval != nil {
		snapshot.remove()
		return errors.New("upload request changed during file selection")
	}
	o.uploadApproval = &browserUploadConsent{request: u, snapshot: snapshot, approval: BrowserApproval{ID: browserID(), Origin: u.Origin, Reason: fmt.Sprintf("Provide file %q (%d bytes, SHA-256 %s) to the file input on %s? This sends the selected private snapshot to the page; it does not itself confirm a completed network upload.", snapshot.name, snapshot.size, snapshot.hash, u.URL)}}
	s.snapshots[u.TabID] = append(s.snapshots[u.TabID], snapshot)
	return nil
}
func (s *browserService) approveUpload(owner string, p *browserUploadConsent, allow bool) BrowserState {
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	s.mu.Lock()
	u, ok := s.uploads[p.request.ID]
	tab := s.tabs[u.TabID]
	valid := !s.closed && ok && tab != nil && tab.owner == owner && u.Status == "pending" && u.DocumentID == p.request.DocumentID && u.Generation == p.request.Generation
	s.mu.Unlock()
	if !valid {
		p.snapshot.remove()
		return s.fail(owner, errors.New("upload approval expired or page changed"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !allow {
		_, err := s.host.Command(ctx, u.TabID, "upload-deny", u.ID)
		p.snapshot.remove()
		if err != nil {
			return s.fail(owner, err)
		}
		return s.state(owner)
	}
	if !p.snapshot.valid() {
		p.snapshot.remove()
		return s.fail(owner, errors.New("upload snapshot changed after consent; select it again"))
	}
	args, _ := json.Marshal(map[string]any{"id": u.ID, "paths": []string{p.snapshot.path}})
	// Retain on both success and uncertain failure until the tab closes: a
	// renderer may read a selected file lazily after the command returned.
	_, err := s.host.Command(ctx, u.TabID, "upload-approve", string(args))
	if err != nil {
		return s.fail(owner, err)
	}
	return s.state(owner)
}
func (s *browserService) uploadCommand(ctx context.Context, owner, id string, cmd BrowserCommand) error {
	if cmd.Op == "upload-list" {
		raw, err := s.host.Command(ctx, id, cmd.Op, "")
		if err != nil {
			return err
		}
		var entries []browserhost.Upload
		if err = json.Unmarshal([]byte(raw), &entries); err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, u := range entries {
			if u.TabID == id {
				s.uploads[u.ID] = u
			}
		}
		return nil
	}
	s.mu.Lock()
	u, ok := s.uploads[cmd.Value]
	tab := s.tabs[id]
	valid := ok && u.TabID == id && tab != nil && tab.owner == owner
	s.mu.Unlock()
	if !valid {
		return errBrowserOwner
	}
	_, err := s.host.Command(ctx, id, "upload-deny", u.ID)
	return err
}
