package main

import (
	"context"
	"crypto/rand"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/installipc"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
)

type installerShutdownListener struct {
	mu      sync.Mutex
	stop    func()
	cancel  context.CancelFunc
	closing bool
}

const (
	installerDraftFlushRequest = "orca:installer-shutdown:flush"
	installerDraftFlushAck     = "orca:installer-shutdown:flushed"
	installerDraftFlushTimeout = time.Second
)

var errInstallerDraftFlush = errors.New("frontend could not persist all composer drafts")

// Wails supplies this bus in its lifecycle context. Use only its event methods;
// no bound App method, HTTP endpoint or configuration is needed for the handshake.
type installerDraftEvents interface {
	On(string, func(...interface{})) func()
	Emit(string, ...interface{})
}

func requestInstallerDraftFlush(ctx context.Context, timeout time.Duration) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	events, ok := ctx.Value("events").(installerDraftEvents)
	if !ok || ctx.Value("frontend") == nil {
		return nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	requestID := rand.Text()
	ack := make(chan bool, 1)
	off := events.On(installerDraftFlushAck, func(data ...interface{}) {
		if len(data) != 2 {
			return
		}
		id, idOK := data[0].(string)
		saved, savedOK := data[1].(bool)
		if !idOK || id != requestID || !savedOK {
			return
		}
		select {
		case ack <- saved:
		default:
		}
	})
	if off != nil {
		defer off()
	}
	if err := waitCtx.Err(); err != nil {
		return err
	}
	events.Emit(installerDraftFlushRequest, requestID)
	select {
	case saved := <-ack:
		if err := waitCtx.Err(); err != nil {
			return err
		}
		if !saved {
			return errInstallerDraftFlush
		}
		return nil
	case <-waitCtx.Done():
		return waitCtx.Err()
	}
}

func (a *App) startInstallerShutdownListener() {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("installer shutdown listener: executable directory: %v", err)
		return
	}
	if err := a.listenForInstallerShutdown(a.bootContext(), filepath.Dir(exe), a.quitApp); err != nil {
		log.Printf("installer shutdown listener: %v", err)
	}
}

func (a *App) listenForInstallerShutdown(ctx context.Context, dir string, quit func()) error {
	if ctx == nil || quit == nil {
		return errors.New("installer shutdown requires a context and quit callback")
	}
	s := &a.installerShutdown
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.stop != nil {
		return nil
	}
	listenCtx, cancel := context.WithCancel(ctx)
	stop, err := installipc.Listen(listenCtx, dir, func() {
		// Do not hold s.mu while waiting for the webview: normal shutdown must
		// be able to cancel this wait and remove the acknowledgement listener.
		if err := requestInstallerDraftFlush(listenCtx, installerDraftFlushTimeout); err != nil && listenCtx.Err() == nil {
			log.Printf("installer shutdown draft flush: %v", err)
		}
		s.mu.Lock()
		if s.closing || listenCtx.Err() != nil {
			s.mu.Unlock()
			return
		}
		// Normal shutdown must not close controllers during this snapshot.
		a.snapshotForInstallerShutdown()
		s.mu.Unlock()
		// quitApp can synchronously enter shutdown, which acquires s.mu.
		quit()
	})
	if err != nil {
		cancel()
		return err
	}
	s.stop = stop
	s.cancel = cancel
	return nil
}

func (a *App) stopInstallerShutdownListener() {
	s := &a.installerShutdown
	s.mu.Lock()
	s.closing = true
	stop := s.stop
	cancel := s.cancel
	s.stop = nil
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if stop != nil {
		stop()
	}
}

func (a *App) snapshotForInstallerShutdown() {
	// Copy controller pointers under the app lock; a runtime switch can replace
	// tab.Ctrl while these disk snapshots run. Persist tab/goal drafts as well.
	a.mu.Lock()
	controllers := make([]*control.Controller, 0, len(a.tabs))
	for _, tab := range a.tabs {
		if tab.Ctrl != nil {
			controllers = append(controllers, tab.Ctrl)
		}
	}
	// An installer can arrive before asynchronous tab restoration has finished.
	// Restore publishes activeTabID only after it has populated the entire list.
	if a.activeTabID != "" && a.tabs[a.activeTabID] != nil {
		a.saveTabsLocked()
	}
	a.mu.Unlock()
	for _, ctrl := range controllers {
		if err := ctrl.Snapshot(); err != nil {
			log.Printf("installer shutdown snapshot: %v", err)
		}
	}
}
