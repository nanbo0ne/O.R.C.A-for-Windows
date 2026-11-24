package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/fileutil"
)

const browserMetadataErrorPrefix = "Could not save browser tab metadata: "

type browserMetadata struct {
	Version    int               `json:"version"`
	Owner      string            `json:"owner"`
	StorageKey string            `json:"storageKey,omitempty"`
	Profile    string            `json:"profile"`
	Active     string            `json:"activeTabId"`
	Tabs       []browserhost.Tab `json:"tabs"`
}

func (s *browserService) metadataPath(storageKey string) string {
	hash := sha256.Sum256([]byte(storageKey))
	return filepath.Join(s.root, "tab-metadata", hex.EncodeToString(hash[:])+".json")
}

// Restored metadata never starts a browser or makes a request. Only an explicit
// user selection or an admitted, consented agent task instantiates the tab.
func (s *browserService) restoreMetadataLocked(owner string) {
	o := s.owners[owner]
	path := s.metadataPath(o.storageKey)
	if err := browserhost.ValidateManagedPath(path); err != nil {
		o.err = browserMetadataErrorPrefix + err.Error()
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	var saved browserMetadata
	if json.NewDecoder(io.LimitReader(f, 128*1024)).Decode(&saved) != nil || saved.Profile != o.profile || len(saved.Tabs) > 32 {
		return
	}
	if saved.Version == 1 {
		if saved.Owner != owner || o.storageKey != owner {
			return
		}
	} else if saved.Version != 2 || saved.StorageKey != o.storageKey {
		return
	}
	for _, tab := range saved.Tabs {
		if id, err := hex.DecodeString(tab.ID); err != nil || len(id) != 16 {
			continue
		}
		if _, _, err := browserURL(tab.URL); err != nil {
			continue
		}
		active := tab.ID == saved.Active
		if saved.Owner != owner {
			// Native events are addressed by tab ID, so never reuse a closed
			// owner's native identity when its topic opens in a new runtime tab.
			tab.ID = browserID()
		}
		if s.tabs[tab.ID] != nil {
			continue
		}
		tab.Title = boundedComputerText(tab.Title, 1024)
		tab.Loading = false
		tab.Error = ""
		tab.CanGoBack = false
		tab.CanGoForward = false
		tab.Zoom = 1
		s.tabs[tab.ID] = &browserTab{Tab: tab, owner: owner}
		o.tabs = append(o.tabs, tab.ID)
		if active {
			o.active = tab.ID
		}
	}
	if o.active == "" && len(o.tabs) > 0 {
		o.active = o.tabs[0]
	}
}
func (s *browserService) saveMetadata(owner string) {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	s.mu.Lock()
	o := s.owners[owner]
	if o == nil {
		s.mu.Unlock()
		return
	}
	if s.clearing[o.profile] {
		s.mu.Unlock()
		return
	}
	v := browserMetadata{Version: 2, Owner: owner, StorageKey: o.storageKey, Profile: o.profile, Active: o.active, Tabs: []browserhost.Tab{}}
	path := s.metadataPath(o.storageKey)
	for _, id := range o.tabs {
		if t := s.tabs[id]; t != nil {
			v.Tabs = append(v.Tabs, browserhost.Tab{ID: id, URL: t.URL, Title: boundedComputerText(t.Title, 1024), Zoom: 1})
		}
	}
	s.mu.Unlock()
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	if err = browserhost.ValidateManagedPath(path); err != nil {
		s.metadataFailure(owner, err)
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return
	}
	if err = browserhost.ValidateManagedPath(path); err != nil {
		s.metadataFailure(owner, err)
		return
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".browser-*.tmp")
	if err != nil {
		return
	}
	tmp := f.Name()
	defer func() {
		if browserhost.ValidateManagedPath(tmp) == nil {
			_ = os.Remove(tmp)
		}
	}()
	if f.Chmod(0600) != nil {
		f.Close()
		return
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return
	}
	if f.Sync() != nil {
		f.Close()
		return
	}
	if f.Close() != nil {
		return
	}
	if err = browserhost.ValidateManagedPath(path); err != nil {
		s.metadataFailure(owner, err)
		return
	}
	if err = fileutil.RenameReplace(tmp, path); err != nil {
		s.metadataFailure(owner, err)
		return
	}
	s.mu.Lock()
	if o := s.owners[owner]; o != nil && strings.HasPrefix(o.err, browserMetadataErrorPrefix) {
		o.err = ""
	}
	s.mu.Unlock()
}

func (s *browserService) metadataFailure(owner string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o := s.owners[owner]; o != nil {
		o.err = browserMetadataErrorPrefix + boundedComputerText(strings.TrimSpace(err.Error()), 256)
	}
}
