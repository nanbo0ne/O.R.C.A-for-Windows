//go:build windows

package browserhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	wv "github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost/internal/webviewabi"
	"golang.org/x/sys/windows"
)

// Host owns uploads uploadStore. All state is STA-confined. Configure BEFORE
// exposing/navigating a tab, and wait for ready(nil), not just the return value.
// closeTab calls closeUploads before releasing the native view. The service
// owns immutable approved snapshots and keeps them until the tab closes.
type uploadStore struct {
	tabs map[*nativeTab]*tabUploads
	next uint64
}

type tabUploads struct {
	receiver    *wv.ICoreWebView2DevToolsProtocolEventReceiver
	token       int64
	ready       bool
	requests    []*nativeUpload
	childEvents []uploadEventRegistration
	children    map[string]*uploadChild
	childCalls  map[uint64]func(error)
	nextCall    uint64
}

type uploadEventRegistration struct {
	receiver *wv.ICoreWebView2DevToolsProtocolEventReceiver
	token    int64
}

type uploadChild struct {
	path          []string
	targetID, url string
	ready         bool
	timer         *time.Timer
}

type nativeUpload struct {
	data     Upload
	ctx      context.Context
	cancel   context.CancelFunc
	deadline time.Time
	timer    *time.Timer
	objectID string
	revision uint64
	finished bool
	inFlight bool
}

// ready runs exactly once on the STA after both CDP acknowledgements, unless
// this function returns an immediate error. The caller must not navigate first.
func (h *Host) configureUploads(t *nativeTab, ready func(error)) error {
	if ready == nil {
		return errors.New("upload interception needs a readiness callback")
	}
	if h.uploads.tabs == nil {
		h.uploads.tabs = make(map[*nativeTab]*tabUploads)
	}
	if h.uploads.tabs[t] != nil {
		return errors.New("upload interception already configured")
	}
	var receiver *wv.ICoreWebView2DevToolsProtocolEventReceiver
	err := raw(unsafe.Pointer(t.view), t.view.Vtbl.GetDevToolsProtocolEventReceiver, uintptr(unsafe.Pointer(ptr("Page.fileChooserOpened"))), uintptr(unsafe.Pointer(&receiver)))
	if err != nil || receiver == nil {
		return fmt.Errorf("file chooser interception unavailable: %v", err)
	}
	s := &tabUploads{receiver: receiver, children: make(map[string]*uploadChild), childCalls: make(map[uint64]func(error))}
	handler := newCOMCallback(false, func(sender, arg uintptr) uintptr {
		if arg == 0 || t.closed {
			return 0
		}
		a := (*wv.ICoreWebView2DevToolsProtocolEventReceivedEventArgs)(unsafe.Pointer(arg))
		h.fileChooserOpened(t, s, readString(unsafe.Pointer(a), a.Vtbl.GetParameterObjectAsJson))
		return 0
	})
	defer handler.release()
	err = raw(unsafe.Pointer(receiver), receiver.Vtbl.AddDevToolsProtocolEventReceived, handler.pointer(), uintptr(unsafe.Pointer(&s.token)))
	if err != nil {
		_ = raw(unsafe.Pointer(receiver), receiver.Vtbl.Release)
		return err
	}
	h.uploads.tabs[t] = s
	for _, name := range []string{"Target.attachedToTarget", "Target.receivedMessageFromTarget", "Target.detachedFromTarget"} {
		if err := h.registerUploadChildEvent(t, s, name); err != nil {
			return err
		}
	}
	return h.uploadCDPAsync(t, "Page.enable", map[string]any{}, func(_ json.RawMessage, err error) {
		if err != nil || t.closed {
			if err == nil {
				err = errors.New("browser tab closed")
			}
			ready(err)
			return
		}
		err = h.uploadCDPAsync(t, "Page.setInterceptFileChooserDialog", map[string]any{"enabled": true}, func(_ json.RawMessage, err error) {
			if t.closed && err == nil {
				err = errors.New("browser tab closed")
			}
			if err != nil {
				ready(err)
				return
			}
			err = h.uploadCDPAsync(t, "Target.setAutoAttach", uploadAutoAttachParams(), func(_ json.RawMessage, err error) {
				if t.closed && err == nil {
					err = errors.New("browser tab closed")
				}
				s.ready = err == nil
				ready(err)
			})
			if err != nil {
				ready(err)
			}
		})
		if err != nil {
			ready(err)
		}
	})
}

func (h *Host) uploadCDPAsync(t *nativeTab, method string, params any, complete func(json.RawMessage, error)) error {
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if t.closed {
		return errors.New("browser tab closed")
	}
	handler := newCOMCallback(true, func(hr, result uintptr) uintptr {
		if int32(hr) < 0 {
			complete(nil, fmt.Errorf("upload setup %s failed: 0x%x", method, hr))
			return 0
		}
		var data json.RawMessage
		if result != 0 {
			data = json.RawMessage(windows.UTF16PtrToString((*uint16)(unsafe.Pointer(result))))
		}
		complete(data, nil)
		return 0
	})
	defer handler.release()
	return raw(unsafe.Pointer(t.view), t.view.Vtbl.CallDevToolsProtocolMethod, uintptr(unsafe.Pointer(ptr(method))), uintptr(unsafe.Pointer(ptr(string(encoded)))), handler.pointer())
}

func (h *Host) fileChooserOpened(t *nativeTab, s *tabUploads, body string) {
	var event struct {
		FrameID       string `json:"frameId"`
		BackendNodeID int64  `json:"backendNodeId"`
		Mode          string `json:"mode"`
	}
	if json.Unmarshal([]byte(body), &event) != nil || event.FrameID == "" || event.BackendNodeID <= 0 || !s.ready {
		return
	}
	// Each new chooser supersedes the previous unresolved request. Interception
	// itself cancels the native dialog; denial never selects or reads a file.
	for _, p := range s.requests {
		if !p.finished {
			h.finishUpload(t, p, "cancelled", nil)
		}
	}
	if len(s.requests) >= 64 {
		s.requests = s.requests[len(s.requests)-63:]
	}
	h.uploads.next++
	ctx, cancel := context.WithCancel(context.Background())
	p := &nativeUpload{ctx: ctx, cancel: cancel, deadline: time.Now().Add(2 * time.Minute), data: Upload{
		ID: "upload-" + strconv.FormatUint(h.uploads.next, 10), TabID: t.id, DocumentID: t.document, Generation: t.generation,
		FrameID: event.FrameID, BackendNodeID: event.BackendNodeID, Status: "preparing",
	}}
	s.requests = append(s.requests, p)
	if h.emit == nil {
		h.finishUpload(t, p, "denied", errors.New("upload approval UI unavailable"))
		return
	}
	data := p.data
	p.timer = time.AfterFunc(time.Until(p.deadline), func() {
		_ = h.post(context.Background(), func() {
			if !p.finished {
				h.finishUpload(t, p, "timedOut", nil)
			}
		})
	})
	go h.prepareUpload(p.ctx, t, p, data)
}

func (h *Host) prepareUpload(parent context.Context, t *nativeTab, p *nativeUpload, data Upload) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, actionFenceKey{}, actionFence{document: data.DocumentID, generation: data.Generation})
	objectID, revision, details, err := h.captureUpload(ctx, data)
	// The request can have been revoked while CDP was in flight. Only the STA
	// can publish it or keep its remote object reference.
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer finishCancel()
	_ = h.call(finishCtx, func() error {
		if t.closed || p.finished || t.document != data.DocumentID || t.generation != data.Generation {
			if objectID != "" && !t.closed {
				_ = h.uploadCDPAsync(t, "Runtime.releaseObject", map[string]any{"objectId": objectID}, func(json.RawMessage, error) {})
			}
			if !p.finished {
				h.finishUpload(t, p, "stale", ErrStaleObservation)
			}
			return nil
		}
		p.objectID = objectID
		if err != nil {
			h.finishUpload(t, p, "failed", err)
			return nil
		}
		p.objectID, p.revision = objectID, revision
		p.data.URL, p.data.Origin, p.data.Multiple, p.data.Accept = details.URL, details.Origin, details.Multiple, details.Accept
		p.data.Status, p.data.RequiresApproval = "pending", true
		if !h.publishUpload(p) {
			h.finishUpload(t, p, "denied", errors.New("upload approval UI unavailable"))
		}
		return nil
	})
}

type uploadTarget struct {
	URL      string `json:"url"`
	Origin   string `json:"origin"`
	Multiple bool   `json:"multiple"`
	Accept   string `json:"accept"`
	Revision uint64 `json:"revision"`
}

func (h *Host) captureUpload(ctx context.Context, d Upload) (string, uint64, uploadTarget, error) {
	var target uploadTarget
	body, err := h.cdp(ctx, d.TabID, "Page.createIsolatedWorld", map[string]any{"frameId": d.FrameID, "worldName": "orca-browser-upload", "grantUniveralAccess": false})
	if err != nil {
		return "", 0, target, err
	}
	var world struct {
		ID int64 `json:"executionContextId"`
	}
	if err = json.Unmarshal(body, &world); err != nil || world.ID == 0 {
		return "", 0, target, errors.New("upload world unavailable")
	}
	body, err = h.cdp(ctx, d.TabID, "DOM.resolveNode", map[string]any{"backendNodeId": d.BackendNodeID, "executionContextId": world.ID})
	if err != nil {
		return "", 0, target, err
	}
	var node struct {
		Object struct {
			ID string `json:"objectId"`
		} `json:"object"`
	}
	if err = json.Unmarshal(body, &node); err != nil || node.Object.ID == "" {
		return "", 0, target, errors.New("upload target unavailable")
	}
	body, err = h.cdp(ctx, d.TabID, "Runtime.callFunctionOn", map[string]any{"objectId": node.Object.ID, "functionDeclaration": captureUploadTarget, "returnByValue": true})
	if err != nil {
		return node.Object.ID, 0, target, err
	}
	value, err := uploadRuntimeValue(body)
	if err == nil {
		err = json.Unmarshal(value, &target)
	}
	if err == nil && (target.URL == "" || target.Origin == "" || target.Origin == "null") {
		err = errors.New("upload requires a non-opaque document origin")
	}
	return node.Object.ID, target.Revision, target, err
}

func uploadRuntimeValue(body json.RawMessage) (json.RawMessage, error) {
	var result struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if len(result.Exception) != 0 || len(result.Result.Value) == 0 {
		return nil, errors.New("upload document or input is no longer valid")
	}
	return result.Result.Value, nil
}

func (h *Host) publishUpload(p *nativeUpload) bool {
	if h.emit == nil {
		return false
	}
	d := p.data
	select {
	case h.events <- Event{TabID: d.TabID, Kind: "upload", URL: d.URL, Error: d.Error, Upload: &d}:
		return true
	default:
		return false
	}
}

func (h *Host) finishUpload(t *nativeTab, p *nativeUpload, status string, cause error) {
	if p.finished {
		return
	}
	p.finished = true
	if p.inFlight && status != "selected" && status != "failed" && status != "uncertain" {
		status = "uncertain"
		cause = errors.Join(cause, errors.New("file selection may already have reached the page"))
	}
	p.cancel()
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.data.Status, p.data.RequiresApproval = status, false
	if cause != nil {
		p.data.Error = cause.Error()
	}
	if p.objectID != "" && !t.closed {
		_ = h.uploadCDPAsync(t, "Runtime.releaseObject", map[string]any{"objectId": p.objectID}, func(json.RawMessage, error) {})
		p.objectID = ""
	}
	h.publishUpload(p)
}

func (h *Host) closeUploads(t *nativeTab) {
	s := h.uploads.tabs[t]
	if s == nil {
		return
	}
	delete(h.uploads.tabs, t)
	for _, p := range s.requests {
		h.finishUpload(t, p, "cancelled", nil)
	}
	for _, child := range s.children {
		if child.timer != nil {
			child.timer.Stop()
		}
	}
	for _, registration := range s.childEvents {
		_ = raw(unsafe.Pointer(registration.receiver), registration.receiver.Vtbl.RemoveDevToolsProtocolEventReceived, uintptr(registration.token))
		_ = raw(unsafe.Pointer(registration.receiver), registration.receiver.Vtbl.Release)
	}
	s.childCalls = nil
	_ = raw(unsafe.Pointer(s.receiver), s.receiver.Vtbl.RemoveDevToolsProtocolEventReceived, uintptr(s.token))
	_ = raw(unsafe.Pointer(s.receiver), s.receiver.Vtbl.Release)
	// Never disable interception while the view is still alive.
}

func uploadAutoAttachParams() map[string]any {
	return map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": false, "filter": []any{map[string]any{"type": "iframe", "exclude": false}, map[string]any{"exclude": true}}}
}

func (h *Host) registerUploadChildEvent(t *nativeTab, s *tabUploads, name string) error {
	var receiver *wv.ICoreWebView2DevToolsProtocolEventReceiver
	err := raw(unsafe.Pointer(t.view), t.view.Vtbl.GetDevToolsProtocolEventReceiver, uintptr(unsafe.Pointer(ptr(name))), uintptr(unsafe.Pointer(&receiver)))
	if err != nil || receiver == nil {
		return fmt.Errorf("child file chooser guard unavailable: %v", err)
	}
	registration := uploadEventRegistration{receiver: receiver}
	handler := newCOMCallback(false, func(sender, arg uintptr) uintptr {
		if arg != 0 && !t.closed {
			a := (*wv.ICoreWebView2DevToolsProtocolEventReceivedEventArgs)(unsafe.Pointer(arg))
			h.uploadChildEvent(t, s, nil, name, json.RawMessage(readString(unsafe.Pointer(a), a.Vtbl.GetParameterObjectAsJson)))
		}
		return 0
	})
	defer handler.release()
	err = raw(unsafe.Pointer(receiver), receiver.Vtbl.AddDevToolsProtocolEventReceived, handler.pointer(), uintptr(unsafe.Pointer(&registration.token)))
	if err != nil {
		_ = raw(unsafe.Pointer(receiver), receiver.Vtbl.Release)
		return err
	}
	s.childEvents = append(s.childEvents, registration)
	return nil
}

// A renderer's Page interceptor does not cover an out-of-process iframe.
// Auto-attach pauses each child before script execution, installs a deny-only
// interceptor, then resumes it. Nested iframe targets receive the same guard.
func (h *Host) uploadChildEvent(t *nativeTab, s *tabUploads, parent []string, method string, params json.RawMessage) {
	if t.closed {
		return
	}
	switch method {
	case "Target.attachedToTarget":
		var event struct {
			SessionID string `json:"sessionId"`
			Waiting   bool   `json:"waitingForDebugger"`
			Info      struct {
				ID   string `json:"targetId"`
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(params, &event) != nil || event.SessionID == "" || event.Info.Type != "iframe" {
			h.failUploadChild(t, errors.New("unexpected child target"))
			return
		}
		if !event.Waiting {
			h.failUploadChild(t, errors.New("child frame was not paused for file chooser interception"))
			return
		}
		path := append(append([]string(nil), parent...), event.SessionID)
		if len(path) > 16 || len(s.children) >= 64 {
			h.failUploadChild(t, errors.New("too many nested upload targets"))
			return
		}
		child := &uploadChild{path: path, targetID: event.Info.ID, url: event.Info.URL}
		s.children[strings.Join(path, "/")] = child
		child.timer = time.AfterFunc(10*time.Second, func() {
			_ = h.post(context.Background(), func() {
				if !t.closed && !child.ready {
					h.failUploadChild(t, errors.New("child file chooser interception timed out"))
				}
			})
		})
		steps := []struct {
			method string
			params any
		}{
			{"Page.enable", map[string]any{}},
			{"Page.setInterceptFileChooserDialog", map[string]any{"enabled": true, "cancel": true}},
			{"Target.setAutoAttach", uploadAutoAttachParams()},
			{"Runtime.runIfWaitingForDebugger", map[string]any{}},
		}
		var run func(int)
		run = func(index int) {
			if t.closed {
				return
			}
			if index == len(steps) {
				child.ready = true
				child.timer.Stop()
				return
			}
			step := steps[index]
			h.sendUploadChild(t, s, path, step.method, step.params, func(err error) {
				if err != nil {
					h.failUploadChild(t, err)
					return
				}
				run(index + 1)
			})
		}
		run(0)
	case "Target.receivedMessageFromTarget":
		var event struct {
			SessionID string `json:"sessionId"`
			Message   string `json:"message"`
		}
		if json.Unmarshal(params, &event) != nil || event.SessionID == "" {
			return
		}
		var message struct {
			ID     uint64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(event.Message), &message) != nil {
			return
		}
		if message.ID != 0 {
			if done := s.childCalls[message.ID]; done != nil {
				delete(s.childCalls, message.ID)
				var err error
				if message.Error != nil {
					err = errors.New(message.Error.Message)
				}
				done(err)
			}
		} else {
			path := append(append([]string(nil), parent...), event.SessionID)
			h.uploadChildEvent(t, s, path, message.Method, message.Params)
		}
	case "Target.detachedFromTarget":
		var event struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(params, &event) == nil {
			path := append(append([]string(nil), parent...), event.SessionID)
			key := strings.Join(path, "/")
			if child := s.children[key]; child != nil {
				child.timer.Stop()
				delete(s.children, key)
			}
		}
	case "Page.fileChooserOpened":
		var event struct {
			FrameID       string `json:"frameId"`
			BackendNodeID int64  `json:"backendNodeId"`
		}
		if json.Unmarshal(params, &event) != nil {
			return
		}
		child := s.children[strings.Join(parent, "/")]
		if child == nil {
			return
		}
		h.uploads.next++
		p := &nativeUpload{data: Upload{ID: "upload-" + strconv.FormatUint(h.uploads.next, 10), TabID: t.id, URL: child.url, DocumentID: t.document, Generation: t.generation, FrameID: event.FrameID, BackendNodeID: event.BackendNodeID, Status: "denied", Error: "Uploads from cross-process frames are not supported; open the frame as an approved tab."}, finished: true}
		h.publishUpload(p)
	}
}

func (h *Host) sendUploadChild(t *nativeTab, s *tabUploads, path []string, method string, params any, done func(error)) {
	s.nextCall++
	id := s.nextCall
	s.childCalls[id] = done
	message, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	for i := len(path) - 1; i > 0; i-- {
		s.nextCall++
		message, _ = json.Marshal(map[string]any{"id": s.nextCall, "method": "Target.sendMessageToTarget", "params": map[string]any{"sessionId": path[i], "message": string(message)}})
	}
	err := h.uploadCDPAsync(t, "Target.sendMessageToTarget", map[string]any{"sessionId": path[0], "message": string(message)}, func(_ json.RawMessage, err error) {
		if err != nil {
			if callback := s.childCalls[id]; callback != nil {
				delete(s.childCalls, id)
				callback(err)
			}
		}
	})
	if err != nil {
		delete(s.childCalls, id)
		done(err)
	}
}

func (h *Host) failUploadChild(t *nativeTab, cause error) {
	if t.closed {
		return
	}
	select {
	case h.events <- Event{TabID: t.id, Kind: "error", Error: "Browser closed because child file chooser isolation failed: " + cause.Error()}:
	default:
	}
	h.closeTab(t)
}

func (h *Host) uploadCommand(ctx context.Context, id, command, value string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var result string
	var tab *nativeTab
	var request *nativeUpload
	var approved Upload
	var objectID string
	var revision uint64
	var requestCtx context.Context
	var params struct {
		ID    string   `json:"id"`
		Paths []string `json:"paths"`
	}
	if command == "upload-approve" {
		if err := json.Unmarshal([]byte(value), &params); err != nil {
			return "", errors.New("upload approval requires id and immutable snapshot paths")
		}
	} else {
		params.ID = value
	}
	err := h.call(ctx, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		t, err := h.tab(id)
		if err != nil {
			return err
		}
		tab = t
		s := h.uploads.tabs[t]
		if s == nil || !s.ready {
			return errors.New("file chooser interception is not ready")
		}
		if command == "upload-list" || command == "upload-cancel-pending" {
			items := make([]Upload, 0, len(s.requests))
			for _, p := range s.requests {
				if !p.finished && (p.data.DocumentID != t.document || p.data.Generation != t.generation) {
					h.finishUpload(t, p, "stale", ErrStaleObservation)
				}
				if command == "upload-cancel-pending" && !p.finished {
					h.finishUpload(t, p, "cancelled", nil)
				}
				items = append(items, p.data)
			}
			body, err := json.Marshal(items)
			result = string(body)
			return err
		}
		for _, p := range s.requests {
			if p.data.ID == params.ID {
				request = p
				break
			}
		}
		if request == nil {
			return errors.New("upload request does not belong to this tab")
		}
		p := request
		if p.finished {
			return errors.New("upload request is no longer active")
		}
		if command == "upload-deny" {
			h.finishUpload(t, p, "denied", nil)
			body, err := json.Marshal(p.data)
			result = string(body)
			return err
		}
		if command != "upload-approve" {
			return errors.New("unsupported upload command")
		}
		if p.data.Status != "pending" {
			return errors.New("upload request is not awaiting approval")
		}
		if p.data.DocumentID != t.document || p.data.Generation != t.generation {
			h.finishUpload(t, p, "stale", ErrStaleObservation)
			return ErrStaleObservation
		}
		if !time.Now().Before(p.deadline) {
			h.finishUpload(t, p, "timedOut", nil)
			return errors.New("upload approval expired")
		}
		approved, objectID, revision, requestCtx = p.data, p.objectID, p.revision, p.ctx
		p.data.Status, p.data.RequiresApproval = "validating", false
		h.publishUpload(p)
		return nil
	})
	if err != nil {
		return "", err
	}
	if command != "upload-approve" {
		return result, nil
	}
	operationCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopCancel := context.AfterFunc(requestCtx, cancel)
	defer stopCancel()
	operationCtx = context.WithValue(operationCtx, actionFenceKey{}, actionFence{document: approved.DocumentID, generation: approved.Generation})
	paths, err := validateUploadSnapshots(operationCtx, params.Paths, approved.Multiple)
	if err == nil {
		var body json.RawMessage
		body, err = h.cdp(operationCtx, id, "Runtime.callFunctionOn", map[string]any{
			"objectId": objectID, "functionDeclaration": checkUploadTarget, "returnByValue": true,
			"arguments": []any{map[string]any{"value": revision}, map[string]any{"value": approved.URL}, map[string]any{"value": approved.Multiple}, map[string]any{"value": approved.Accept}},
		})
		if err == nil {
			var valid json.RawMessage
			valid, err = uploadRuntimeValue(body)
			if err == nil && string(valid) != "true" {
				err = ErrStaleObservation
			}
		}
	}
	dispatched := false
	if err == nil {
		err = h.call(operationCtx, func() error {
			if request.finished || tab.closed || tab.document != approved.DocumentID || tab.generation != approved.Generation {
				return ErrStaleObservation
			}
			if err := operationCtx.Err(); err != nil {
				return err
			}
			request.inFlight = true
			return nil
		})
		if err == nil {
			dispatched = true
			_, err = h.cdp(operationCtx, id, "DOM.setFileInputFiles", map[string]any{"files": paths, "backendNodeId": approved.BackendNodeID})
		}
	}
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer finishCancel()
	status := "selected"
	if err != nil {
		status = "failed"
		if errors.Is(err, ErrStaleObservation) {
			status = "stale"
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = "cancelled"
			if dispatched {
				status = "uncertain"
			}
		}
		if dispatched {
			status = "uncertain"
		}
	}
	finishErr := h.call(finishCtx, func() error {
		if !request.finished {
			if dispatched && err == nil {
				tab.generation++
			}
			h.finishUpload(tab, request, status, err)
		}
		body, marshalErr := json.Marshal(request.data)
		result = string(body)
		return marshalErr
	})
	if err != nil {
		return "", err
	}
	if finishErr != nil {
		return "", finishErr
	}
	return result, nil
}

// This validates service-owned snapshots only after upload consent. It never
// walks a directory or reads file contents. Native code cannot prove the UI's
// consent or snapshot provenance; the owning service is the trusted caller.
func validateUploadSnapshots(ctx context.Context, paths []string, multiple bool) ([]string, error) {
	if len(paths) == 0 || len(paths) > 16 || (!multiple && len(paths) != 1) {
		return nil, errors.New("wrong number of approved upload files")
	}
	seen := make(map[string]bool)
	out := make([]string, 0, len(paths))
	var total int64
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path = strings.ReplaceAll(path, "/", `\`)
		if len(path) < 4 || !filepath.IsAbs(path) || len(filepath.VolumeName(path)) != 2 || strings.ContainsAny(path[2:], ":\x00") {
			return nil, errors.New("upload snapshot must be an absolute local file")
		}
		path = filepath.Clean(path)
		if err := ValidateTransferPath(path); err != nil {
			return nil, err
		}
		key := strings.ToLower(path)
		if seen[key] {
			return nil, errors.New("duplicate upload snapshot")
		}
		seen[key] = true
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("upload snapshot must be a regular file")
		}
		for check := path; ; check = filepath.Dir(check) {
			u16, err := windows.UTF16PtrFromString(check)
			if err != nil {
				return nil, err
			}
			attrs, err := windows.GetFileAttributes(u16)
			if err != nil {
				return nil, err
			}
			if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
				return nil, errors.New("upload snapshot cannot contain links")
			}
			if filepath.Dir(check) == check {
				break
			}
		}
		total += info.Size()
		if total > 64<<20 {
			return nil, errors.New("approved upload exceeds 64 MiB")
		}
		out = append(out, path)
	}
	return out, nil
}

const captureUploadTarget = `function(){
 if(!(this instanceof HTMLInputElement)||this.type!=='file'||!this.isConnected||this.ownerDocument!==document||this.disabled||this.webkitdirectory)throw Error('unsupported file input');
 if(!window.__orcaUploadWatch){
  window.__orcaUploadRevision=0;
  window.__orcaUploadWatch=new MutationObserver(()=>window.__orcaUploadRevision++);
  window.__orcaUploadWatch.observe(document,{subtree:true,childList:true,attributes:true,characterData:true});
  for(const name of ['pointerdown','keydown','input','change'])document.addEventListener(name,()=>window.__orcaUploadRevision++,true);
 }
 return {url:document.URL,origin:location.origin,multiple:this.multiple,accept:this.accept,revision:window.__orcaUploadRevision};
}`

const checkUploadTarget = `function(revision,url,multiple,accept){
 return this instanceof HTMLInputElement&&this.type==='file'&&this.isConnected&&this.ownerDocument===document&&!this.disabled&&!this.webkitdirectory&&document.URL===url&&this.multiple===multiple&&this.accept===accept&&window.__orcaUploadRevision===revision;
}`
