package webbetahost

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

// A login form after an established identity is a hard boundary, even when no
// Agent request is running. A later successful login cannot revive old URLs.
type identityLatch struct {
	known, lost bool
	marker      string
	persisted   bool
}

const identityMarker = ".orca-identity-changed"

func loadIdentityLatch(profile string) (identityLatch, error) {
	l := identityLatch{marker: filepath.Join(profile, identityMarker)}
	if err := rejectLinks(l.marker); err != nil {
		return l, err
	}
	_, err := os.Stat(l.marker)
	if err == nil {
		l.known, l.lost, l.persisted = true, true, true
		return l, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	return l, err
}

func (l *identityLatch) persistLoss() error {
	if l.marker == "" || l.persisted {
		return nil
	}
	if err := rejectLinks(l.marker); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.marker), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(l.marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		l.persisted = true
		return nil
	}
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	l.persisted = err == nil
	return err
}

func (l *identityLatch) observe(s webbeta.BrowserState) webbeta.BrowserState {
	if l.known && s.Blocked == "signed_out" {
		l.lost = true
	}
	if s.Ready {
		l.known = true
	}
	if l.lost {
		s.Ready, s.Busy, s.Blocked = false, false, "identity_changed"
		if err := l.persistLoss(); err != nil {
			s.Blocked = "identity_storage_failed"
		}
	}
	return s
}
