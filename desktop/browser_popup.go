package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
)

type browserPopup struct {
	approval BrowserApproval
	url      string
	answer   chan bool
	resolved bool
}

func (s *browserService) handlePopup(t *browserTask) (retErr error) {
	s.mu.Lock()
	p := t.popup
	s.mu.Unlock()
	if p == nil {
		return nil
	}
	select {
	case <-t.ctx.Done():
		return t.ctx.Err()
	case allow := <-p.answer:
		if !allow {
			return errors.New("browser popup denied")
		}
	}
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	s.mu.Lock()
	if err := s.checkTaskLocked(t); err != nil {
		s.mu.Unlock()
		return err
	}
	o := s.owners[t.owner]
	if t.budget.actions >= browserMaxActions || len(o.tabs) >= 32 {
		s.mu.Unlock()
		return errBrowserStopped
	}
	id := browserID()
	t.budget.actions++
	t.popup = nil
	t.tab = id
	receipt := s.startExecutionLocked(t, "", "", browserhost.Action{Kind: "popup", URL: p.url})
	_, origin, _ := browserURL(p.url)
	t.origins[origin] = true
	t.consents = append(t.consents, BrowserConsent{Origin: origin, Model: t.model, Reason: p.approval.Reason})
	s.tabs[id] = &browserTab{Tab: browserhost.Tab{ID: id, URL: p.url, Zoom: 1}, owner: t.owner}
	o.tabs = append(o.tabs, id)
	o.active = id
	o.taskState = "running"
	v := o.viewport
	v.TabID = id
	show := s.shownOwner == t.owner && v.Visible
	s.mu.Unlock()
	defer func() { s.finishExecution(t, receipt, retErr) }()
	if err := s.host.Create(t.ctx, filepath.Join(s.root, t.profile), id, p.url); err != nil {
		s.removeTab(id)
		return err
	}
	s.mu.Lock()
	if tab := s.tabs[id]; tab != nil {
		tab.created = true
	}
	s.mu.Unlock()
	if show {
		if err := s.host.Present(t.ctx, v); err != nil {
			return err
		}
	}
	return s.waitTabReady(t.ctx, id)
}
func (s *browserService) waitTabReady(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		raw, err := s.host.Command(ctx, id, "state", "")
		if err != nil {
			return err
		}
		var state browserhost.Tab
		if err = json.Unmarshal([]byte(raw), &state); err != nil {
			return err
		}
		if state.Error != "" {
			return errors.New("browser navigation failed")
		}
		if state.ID == id && state.URL != "" && state.URL != "about:blank" && !state.Loading {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
func (s *browserService) waitUploads(t *browserTask) error {
	for {
		s.mu.Lock()
		if err := s.checkTaskLocked(t); err != nil {
			s.mu.Unlock()
			return err
		}
		pending := false
		for _, u := range s.uploads {
			if u.TabID == t.tab && (u.Status == "preparing" || u.Status == "pending" || u.Status == "validating") {
				pending = true
			}
		}
		if pending {
			s.owners[t.owner].taskState = "awaiting_upload"
		} else if s.owners[t.owner].taskState == "awaiting_upload" {
			s.owners[t.owner].taskState = "running"
		}
		s.mu.Unlock()
		if !pending {
			return nil
		}
		select {
		case <-t.ctx.Done():
			return t.ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
