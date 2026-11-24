//go:build windows

package browserhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"

	wv "github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost/internal/webviewabi"
	"golang.org/x/sys/windows"
)

// Integration: Host owns transfers transferStore. configure calls
// configureTransfers before navigation; closeTab calls closeTransfers before
// releasing the controller. Command delegates download-* to transferCommand.
// Every field below is confined to the Host STA, including timer callbacks.
type transferStore struct {
	tabs            map[*nativeTab]*tabTransfers
	next            uint64
	approvalTimeout time.Duration
	trace           func(id, step string, state int32, elapsed time.Duration)
}

// Optional synthetic-test instrumentation; production does not install a sink.
func (h *Host) traceTransfer(p *nativeTransfer, step string, state int32, started time.Time) {
	if h.transfers.trace != nil {
		h.transfers.trace(p.data.ID, step, state, time.Since(started))
	}
}

type tabTransfers struct {
	view       *wv.ICoreWebView2_4
	token      int64
	items      []*nativeTransfer
	operations map[uintptr]*nativeTransfer
	changed    *comCallback
}

type nativeTransfer struct {
	data                             Download
	args                             *wv.ICoreWebView2DownloadStartingEventArgs
	deferral                         *wv.ICoreWebView2Deferral
	operation                        *wv.ICoreWebView2DownloadOperation
	bytesToken, stateToken           int64
	bytesRegistered, stateRegistered bool
	timer                            *time.Timer
	pollTimer                        *time.Timer
	lastPublished                    Download
	deadline                         time.Time
	finished                         bool
	stageDir, stagePath              string
	parentLocks                      []windows.Handle
	stageLock                        windows.Handle
}

func (h *Host) configureTransfers(t *nativeTab) error {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return errors.New("safe browser downloads require a 64-bit host")
	}
	if h.transfers.tabs == nil {
		h.transfers.tabs = make(map[*nativeTab]*tabTransfers)
	}
	if h.transfers.tabs[t] != nil {
		return nil
	}
	var view *wv.ICoreWebView2_4
	err := raw(unsafe.Pointer(t.view), t.view.Vtbl.QueryInterface,
		uintptr(unsafe.Pointer(&wv.IIDICoreWebView2_4)), uintptr(unsafe.Pointer(&view)))
	if err != nil || view == nil {
		return fmt.Errorf("safe browser downloads unavailable (ICoreWebView2_4): %v", err)
	}
	s := &tabTransfers{view: view, operations: make(map[uintptr]*nativeTransfer)}
	s.changed = newCOMCallback(false, func(sender, unused uintptr) uintptr {
		if p := s.operations[sender]; p != nil && !p.finished {
			h.refreshTransfer(s, p)
		}
		return 0
	})
	handler := newCOMCallback(false, func(sender, arg uintptr) uintptr {
		if arg != 0 {
			h.startTransfer(t, s, (*wv.ICoreWebView2DownloadStartingEventArgs)(unsafe.Pointer(arg)))
		}
		return 0
	})
	defer handler.release()
	err = raw(unsafe.Pointer(view), view.Vtbl.AddDownloadStarting, handler.pointer(), uintptr(unsafe.Pointer(&s.token)))
	if err != nil {
		s.changed.release()
		_ = raw(unsafe.Pointer(view), view.Vtbl.Release)
		return err
	}
	h.transfers.tabs[t] = s
	return nil
}

func (h *Host) startTransfer(t *nativeTab, s *tabTransfers, a *wv.ICoreWebView2DownloadStartingEventArgs) {
	// Set denial before any fallible work. A deferral holds this decision, not a
	// navigation permission. Never permit the native download/save dialog.
	cancelErr := raw(unsafe.Pointer(a), a.Vtbl.PutCancel, 1)
	handledErr := raw(unsafe.Pointer(a), a.Vtbl.PutHandled, 1)
	var operation *wv.ICoreWebView2DownloadOperation
	err := raw(unsafe.Pointer(a), a.Vtbl.GetDownloadOperation, uintptr(unsafe.Pointer(&operation)))
	if err != nil || operation == nil {
		// A failed cancellation is fatal to this isolated tab.
		if cancelErr != nil {
			h.closeTab(t)
		}
		return
	}
	if cancelErr != nil || handledErr != nil || t.closed || h.closing.Load() {
		_ = raw(unsafe.Pointer(operation), operation.Vtbl.Cancel)
		_ = raw(unsafe.Pointer(operation), operation.Vtbl.Release)
		if cancelErr != nil {
			h.closeTab(t)
		}
		return
	}
	// Bound pending operations and history even when a page floods downloads.
	active := 0
	for _, p := range s.items {
		if !p.finished {
			active++
		}
	}
	if active >= 16 {
		_ = raw(unsafe.Pointer(operation), operation.Vtbl.Cancel)
		_ = raw(unsafe.Pointer(operation), operation.Vtbl.Release)
		return
	}
	if len(s.items) >= 128 {
		for i, p := range s.items {
			if p.finished {
				s.items = append(s.items[:i], s.items[i+1:]...)
				break
			}
		}
	}
	h.transfers.next++
	p := &nativeTransfer{operation: operation, data: Download{
		ID: "download-" + strconv.FormatUint(h.transfers.next, 10), TabID: t.id,
		URL:      readString(unsafe.Pointer(operation), operation.Vtbl.GetUri),
		Filename: filepath.Base(readString(unsafe.Pointer(a), a.Vtbl.GetResultFilePath)),
		Status:   "pending", RequiresApproval: true, TotalBytes: -1,
	}}
	s.items = append(s.items, p)
	s.operations[uintptr(unsafe.Pointer(operation))] = p
	if err = raw(unsafe.Pointer(a), a.Vtbl.AddRef); err != nil {
		h.finishTransfer(s, p, "denied", err)
		return
	}
	p.args = a
	if err = raw(unsafe.Pointer(a), a.Vtbl.GetDeferral, uintptr(unsafe.Pointer(&p.deferral))); err != nil || p.deferral == nil {
		h.finishTransfer(s, p, "denied", errors.New("download approval deferral unavailable"))
		return
	}
	if err = raw(unsafe.Pointer(operation), operation.Vtbl.AddBytesReceivedChanged, s.changed.pointer(), uintptr(unsafe.Pointer(&p.bytesToken))); err != nil {
		h.finishTransfer(s, p, "denied", err)
		return
	}
	p.bytesRegistered = true
	if err = raw(unsafe.Pointer(operation), operation.Vtbl.AddStateChanged, s.changed.pointer(), uintptr(unsafe.Pointer(&p.stateToken))); err != nil {
		h.finishTransfer(s, p, "denied", err)
		return
	}
	p.stateRegistered = true
	_ = raw(unsafe.Pointer(operation), operation.Vtbl.GetTotalBytesToReceive, uintptr(unsafe.Pointer(&p.data.TotalBytes)))
	_ = raw(unsafe.Pointer(operation), operation.Vtbl.GetBytesReceived, uintptr(unsafe.Pointer(&p.data.BytesReceived)))
	timeout := h.transfers.approvalTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	p.deadline = time.Now().Add(timeout)
	p.timer = time.AfterFunc(timeout, func() {
		// Do not use an expiring post context: skipping a queued timeout would
		// leak its deferral. post still exits when this host stops.
		_ = h.post(context.Background(), func() {
			if !p.finished && p.data.RequiresApproval {
				h.finishTransfer(s, p, "timedOut", nil)
			}
		})
	})
	if !h.publishTransfer(p) {
		h.finishTransfer(s, p, "denied", errors.New("download approval UI unavailable"))
	}
}

// Never block the COM thread on a slow UI. State remains available via list;
// losing the initial approval notification denies that transfer immediately.
func (h *Host) publishTransfer(p *nativeTransfer) bool {
	if h.emit == nil {
		return false
	}
	d := p.data
	if d == p.lastPublished {
		return true
	}
	select {
	case h.events <- Event{TabID: d.TabID, Kind: "download", URL: d.URL, Error: d.Error, Download: &d}:
		p.lastPublished = d
		return true
	default:
		return false
	}
}

func (h *Host) refreshTransfer(s *tabTransfers, p *nativeTransfer) {
	op := p.operation
	if op == nil || p.finished {
		return
	}
	var state int32
	started := time.Now()
	err := raw(unsafe.Pointer(op), op.Vtbl.GetState, uintptr(unsafe.Pointer(&state)))
	h.traceTransfer(p, "native-state", state, started)
	if err == nil {
		err = raw(unsafe.Pointer(op), op.Vtbl.GetBytesReceived, uintptr(unsafe.Pointer(&p.data.BytesReceived)))
	}
	_ = raw(unsafe.Pointer(op), op.Vtbl.GetTotalBytesToReceive, uintptr(unsafe.Pointer(&p.data.TotalBytes)))
	if err != nil {
		h.finishTransfer(s, p, "interrupted", err)
		return
	}
	if state == wv.DownloadStateInterrupted {
		var reason int32
		_ = raw(unsafe.Pointer(op), op.Vtbl.GetInterruptReason, uintptr(unsafe.Pointer(&reason)))
		h.finishTransfer(s, p, "interrupted", fmt.Errorf("download interrupted (reason %d)", reason))
	} else if state == wv.DownloadStateCompleted && !p.data.RequiresApproval {
		started := time.Now()
		_, err := validateDownloadDestination(p.data.Path)
		h.traceTransfer(p, "validate-destination", state, started)
		var src *uint16
		if err == nil {
			src, err = windows.UTF16PtrFromString(p.stagePath)
		}
		if err == nil {
			var dst *uint16
			dst, err = windows.UTF16PtrFromString(p.data.Path)
			// MoveFile fails if the destination appeared while downloading.
			if err == nil {
				started := time.Now()
				err = windows.MoveFile(src, dst)
				h.traceTransfer(p, "publish-move", state, started)
			}
		}
		if err != nil {
			h.finishTransfer(s, p, "interrupted", fmt.Errorf("publish download: %w", err))
			return
		}
		h.finishTransfer(s, p, "completed", nil)
	} else {
		if !p.data.RequiresApproval {
			p.data.Phase = "transferring"
			if p.data.TotalBytes >= 0 && p.data.BytesReceived >= p.data.TotalBytes {
				p.data.Phase = "finalizing"
			}
		}
		h.publishTransfer(p)
	}
}

// Bytes can arrive before the event handlers are attached, with no later byte
// event until completion. Refresh on approval and poll the live native values.
func (h *Host) pollTransfer(s *tabTransfers, p *nativeTransfer) {
	if p.finished {
		return
	}
	p.pollTimer = time.AfterFunc(500*time.Millisecond, func() {
		_ = h.post(context.Background(), func() {
			if p.finished {
				return
			}
			h.refreshTransfer(s, p)
			h.pollTransfer(s, p)
		})
	})
}

func (h *Host) completeTransferDeferral(p *nativeTransfer) error {
	defer h.traceTransfer(p, "complete-deferral", -1, time.Now())
	var err error
	if p.deferral != nil {
		d := p.deferral
		p.deferral = nil
		err = raw(unsafe.Pointer(d), d.Vtbl.Complete)
		_ = raw(unsafe.Pointer(d), d.Vtbl.Release)
	}
	if p.args != nil {
		_ = raw(unsafe.Pointer(p.args), p.args.Vtbl.Release)
		p.args = nil
	}
	return err
}

func (h *Host) finishTransfer(s *tabTransfers, p *nativeTransfer, status string, cause error) {
	if p.finished {
		return
	}
	defer h.traceTransfer(p, "finish-"+status, -1, time.Now())
	p.finished = true // Cancel/Complete may synchronously reenter the callbacks.
	p.data.Status, p.data.RequiresApproval = status, false
	p.data.Phase = ""
	if p.pollTimer != nil {
		p.pollTimer.Stop()
		p.pollTimer = nil
	}
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	op := p.operation
	if status != "completed" {
		if p.args != nil {
			cause = errors.Join(cause, raw(unsafe.Pointer(p.args), p.args.Vtbl.PutCancel, 1))
		}
		if op != nil {
			cause = errors.Join(cause, raw(unsafe.Pointer(op), op.Vtbl.Cancel))
		}
	}
	cause = errors.Join(cause, h.completeTransferDeferral(p))
	if op != nil {
		delete(s.operations, uintptr(unsafe.Pointer(op)))
		// EventRegistrationToken is an INT64 passed by VALUE on 64-bit Windows.
		if p.bytesRegistered {
			_ = raw(unsafe.Pointer(op), op.Vtbl.RemoveBytesReceivedChanged, uintptr(p.bytesToken))
		}
		if p.stateRegistered {
			_ = raw(unsafe.Pointer(op), op.Vtbl.RemoveStateChanged, uintptr(p.stateToken))
		}
		_ = raw(unsafe.Pointer(op), op.Vtbl.Release)
		p.operation = nil
	}
	if p.stageDir != "" {
		// Only remove the files this transfer owns, never recursively delete a
		// destination directory. WebView2 may retain a locked partial file.
		if p.stageLock != 0 {
			for _, path := range []string{p.stagePath, p.stagePath + ".crdownload"} {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					cause = errors.Join(cause, fmt.Errorf("remove download staging: %w", err))
				}
			}
			_ = windows.CloseHandle(p.stageLock)
			p.stageLock = 0
		}
		if err := os.Remove(p.stageDir); err != nil && !os.IsNotExist(err) {
			cause = errors.Join(cause, fmt.Errorf("remove download staging directory: %w", err))
		}
	}
	releaseDownloadDirectories(p.parentLocks)
	p.parentLocks = nil
	if cause != nil {
		p.data.Error = cause.Error()
	}
	h.publishTransfer(p)
}

func (h *Host) closeTransfers(t *nativeTab) {
	s := h.transfers.tabs[t]
	if s == nil {
		return
	}
	delete(h.transfers.tabs, t)
	for _, p := range s.items {
		h.finishTransfer(s, p, "cancelled", nil)
	}
	_ = raw(unsafe.Pointer(s.view), s.view.Vtbl.RemoveDownloadStarting, uintptr(s.token))
	_ = raw(unsafe.Pointer(s.view), s.view.Vtbl.Release)
	s.changed.release()
}

func (h *Host) transferCommand(ctx context.Context, id, command, value string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var result string
	err := h.call(ctx, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		t, err := h.tab(id)
		if err != nil {
			return err
		}
		s := h.transfers.tabs[t]
		if s == nil {
			return errors.New("safe downloads not configured")
		}
		if command == "download-list" || command == "download-cancel-pending" {
			items := make([]Download, 0, len(s.items))
			for _, p := range s.items {
				if command == "download-cancel-pending" && p.data.RequiresApproval {
					h.finishTransfer(s, p, "cancelled", nil)
				}
				items = append(items, p.data)
			}
			data, err := json.Marshal(items)
			result = string(data)
			return err
		}
		request := struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		}{ID: value}
		if command == "download-approve" {
			if err := json.Unmarshal([]byte(value), &request); err != nil {
				return errors.New("download approval requires id and a confirmed absolute path")
			}
		}
		var p *nativeTransfer
		for _, item := range s.items {
			if item.data.ID == request.ID {
				p = item
				break
			}
		}
		if p == nil {
			return errors.New("download does not belong to this tab")
		}
		if p.finished {
			return errors.New("download is no longer active")
		}
		switch command {
		case "download-deny":
			if !p.data.RequiresApproval {
				return errors.New("download was already approved; use cancel")
			}
			h.finishTransfer(s, p, "denied", nil)
		case "download-cancel":
			h.finishTransfer(s, p, "cancelled", nil)
		case "download-approve":
			if !p.data.RequiresApproval {
				return errors.New("download was already approved")
			}
			if !time.Now().Before(p.deadline) {
				h.finishTransfer(s, p, "timedOut", nil)
				return errors.New("download approval expired")
			}
			err = h.approveTransfer(ctx, s, p, request.Path)
			if err != nil {
				h.finishTransfer(s, p, "denied", err)
				return err
			}
		default:
			return errors.New("unsupported download command")
		}
		data, err := json.Marshal(p.data)
		result = string(data)
		return err
	})
	if err != nil {
		return "", err
	}
	return result, nil
}

func (h *Host) approveTransfer(ctx context.Context, s *tabTransfers, p *nativeTransfer, destination string) error {
	path, err := validateDownloadDestination(destination)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	p.parentLocks, err = pinDownloadParents(filepath.Dir(path))
	if err != nil {
		return err
	}
	if _, err = validateDownloadDestination(path); err != nil {
		return err
	}
	// Staging starts only after explicit approval. It prevents both partial
	// target files and WebView2's documented overwrite of an existing path.
	p.stageDir, err = os.MkdirTemp(filepath.Dir(path), ".orca-download-")
	if err != nil {
		return err
	}
	p.stageLock, err = pinDownloadDirectory(p.stageDir)
	if err != nil {
		return err
	}
	p.stagePath = filepath.Join(p.stageDir, filepath.Base(path))
	u16, err := windows.UTF16PtrFromString(p.stagePath)
	if err != nil {
		return err
	}
	if err = raw(unsafe.Pointer(p.args), p.args.Vtbl.PutResultFilePath, uintptr(unsafe.Pointer(u16))); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if !time.Now().Before(p.deadline) {
		return errors.New("download approval expired")
	}
	if err = raw(unsafe.Pointer(p.args), p.args.Vtbl.PutCancel, 0); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	p.data.Path, p.data.Status, p.data.RequiresApproval = path, "inProgress", false
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	if err = h.completeTransferDeferral(p); err != nil {
		return err
	}
	if !p.finished {
		h.refreshTransfer(s, p)
		h.pollTransfer(s, p)
	}
	return nil
}

func validateDownloadDestination(path string) (string, error) {
	// Reject UNC/device paths and alternate data streams before filesystem IO.
	path = strings.ReplaceAll(path, "/", `\`)
	if len(path) < 4 || !filepath.IsAbs(path) || len(filepath.VolumeName(path)) != 2 || path[1] != ':' {
		return "", errors.New("choose an absolute local file path")
	}
	if path[0] < 'A' || path[0] > 'Z' && path[0] < 'a' || path[0] > 'z' {
		return "", errors.New("invalid drive")
	}
	for _, component := range strings.Split(path[3:], `\`) {
		if component == "" || component == "." || component == ".." || !filepath.IsLocal(component) || strings.TrimRight(component, " .") != component {
			return "", errors.New("invalid download path component")
		}
		for _, r := range component {
			if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
				return "", errors.New("invalid download filename")
			}
		}
		base := strings.ToUpper(strings.SplitN(component, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && strings.ContainsRune("123456789", rune(base[3]))) {
			return "", errors.New("reserved device filename")
		}
	}
	path = filepath.Clean(path)
	if err := ValidateTransferPath(path); err != nil {
		return "", err
	}
	if _, err := os.Lstat(path); err == nil {
		return "", errors.New("download destination already exists")
	} else if !os.IsNotExist(err) {
		return "", err
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		u16, err := windows.UTF16PtrFromString(dir)
		if err != nil {
			return "", err
		}
		attrs, err := windows.GetFileAttributes(u16)
		if err != nil {
			return "", err
		}
		if attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return "", errors.New("download parent must be an existing local directory without links")
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return path, nil
}
