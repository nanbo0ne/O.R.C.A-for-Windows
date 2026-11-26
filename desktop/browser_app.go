package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var (
	errBrowserOwner   = errors.New("browser tab belongs to another conversation or no longer exists")
	errBrowserBusy    = errors.New("browser profile is controlled by an active task; take over first")
	errBrowserStopped = errors.New("browser parent turn stopped; request user guidance before a new turn")
)

type BrowserViewport = browserhost.Viewport
type BrowserCommand struct {
	Op         string `json:"op"`
	TabID      string `json:"tabId"`
	Value      string `json:"value"`
	ApprovalID string `json:"approvalId"`
	Allow      bool   `json:"allow"`
}
type BrowserApproval struct {
	ID     string `json:"id"`
	Origin string `json:"origin"`
	Reason string `json:"reason"`
}
type BrowserConsent struct {
	Origin string `json:"origin"`
	Model  string `json:"model"`
	Reason string `json:"reason"`
}
type BrowserState struct {
	Supported   bool                   `json:"supported"`
	Tabs        []browserhost.Tab      `json:"tabs"`
	ActiveTabID string                 `json:"activeTabId"`
	Busy        bool                   `json:"busy"`
	Error       string                 `json:"error"`
	Approval    *BrowserApproval       `json:"approval"`
	TaskState   string                 `json:"taskState"`
	TaskID      string                 `json:"taskId,omitempty"`
	Consents    []BrowserConsent       `json:"consents"`
	Downloads   []browserhost.Download `json:"downloads"`
	Uploads     []browserhost.Upload   `json:"uploads"`
}

func emptyBrowserState(err error) BrowserState {
	v := BrowserState{Tabs: []browserhost.Tab{}, Consents: []BrowserConsent{}, Downloads: []browserhost.Download{}, Uploads: []browserhost.Upload{}, TaskState: "idle"}
	if err != nil {
		v.Error = err.Error()
	}
	return v
}

type browserOwner struct {
	profile        string
	storageKey     string
	tabs           []string
	active         string
	err            string
	taskState      string
	task           *browserTask
	viewport       BrowserViewport
	lastVisibleTab string
	clearApproval  *BrowserApproval
	uploadApproval *browserUploadConsent
}
type browserTab struct {
	browserhost.Tab
	owner   string
	epoch   uint64
	created bool
}
type browserPending struct {
	BrowserApproval
	task   *browserTask
	answer chan bool
}
type browserBudget struct {
	owner, session string
	deadline       time.Time
	actions        int
	executions     []browserExecutionReceipt
	staleReplans   int
	stopped        bool
	seen           map[string]bool
}
type browserTask struct {
	id, owner, tab, profile, turn, session, model string
	ctx                                           context.Context
	cancel                                        context.CancelFunc
	expire                                        context.CancelCauseFunc
	timer                                         *time.Timer
	waitAt                                        time.Time
	budget                                        *browserBudget
	origins                                       map[string]bool
	consents                                      []BrowserConsent
	pending                                       *browserPending
	initialURL                                    string
	stopState                                     string
	popup                                         *browserPopup
	lastConsent                                   time.Time
}

// ioMu serializes native operations, never policy callbacks or cancellation.
// mu protects bookkeeping only and is never held while calling the native host.
type browserService struct {
	mu         sync.Mutex
	ioMu       sync.Mutex
	persistMu  sync.Mutex
	pauseMu    sync.Mutex
	host       browserhost.Backend
	root       string
	owners     map[string]*browserOwner
	revoked    map[string]bool
	paused     map[string]bool
	tabs       map[string]*browserTab
	leases     map[string]*browserTask
	budgets    map[string]*browserBudget
	downloads  map[string]browserhost.Download
	uploads    map[string]browserhost.Upload
	snapshots  map[string][]*browserUploadSnapshot
	clearing   map[string]bool
	shownOwner string
	closed     bool
	emit       func(string, BrowserState)
	// fullAccess reports whether the owner's conversation runs with every tool
	// approval already granted (the composer's 完全访问 switch). The browser must
	// follow that switch instead of asking again for each page it opens.
	fullAccess func(owner string) bool
}

func newBrowserService(host browserhost.Backend, root string, emit func(string, BrowserState)) *browserService {
	s := &browserService{host: host, root: root, emit: emit, owners: map[string]*browserOwner{}, revoked: map[string]bool{}, tabs: map[string]*browserTab{}, leases: map[string]*browserTask{}, budgets: map[string]*browserBudget{}, downloads: map[string]browserhost.Download{}, clearing: map[string]bool{}}
	s.uploads = map[string]browserhost.Upload{}
	s.paused = map[string]bool{}
	s.snapshots = map[string][]*browserUploadSnapshot{}
	host.SetNavigationGuard(s.navigationAllowed)
	return s
}

func (a *App) setTabBrowserAwarePause(tabID string, paused bool) {
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil || tab.Ctrl == nil {
		a.mu.RUnlock()
		return
	}
	owner, ctrl, s := tab.ID, tab.Ctrl, a.browser
	a.mu.RUnlock()
	if s != nil {
		// Serialize pause/resume while native cancellation drains, without holding
		// App.mu or the bookkeeping mutex across native calls.
		s.pauseMu.Lock()
		defer s.pauseMu.Unlock()
		if paused {
			s.pauseOwner(owner)
		} else {
			s.mu.Lock()
			delete(s.paused, owner)
			s.mu.Unlock()
		}
	}
	ctrl.SetPaused(paused)
}

func (s *browserService) pauseOwner(owner string) {
	s.mu.Lock()
	s.paused[owner] = true
	for _, b := range s.budgets {
		if b.owner == owner {
			b.stopped = true
		}
	}
	o := s.owners[owner]
	var task *browserTask
	if o != nil && o.task != nil && s.leases[o.profile] == o.task {
		task = o.task
		task.stopState = "taken_over"
		task.cancel()
		o.taskState = "stopping"
	}
	s.mu.Unlock()
	if task == nil {
		return
	}
	s.notify(owner)
	// Inputs already dispatched cannot be undone. Do not acknowledge pause
	// until the in-flight native operation has returned; admission stays shut.
	s.ioMu.Lock()
	s.ioMu.Unlock()
	s.mu.Lock()
	if current := s.owners[owner]; current == o && current.task == task {
		current.taskState = "taken_over"
	}
	s.mu.Unlock()
	s.notify(owner)
}
func browserID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func browserProfileKey(owner, scope, root string) string {
	identity := "conversation:" + owner
	if scope == "project" && strings.TrimSpace(root) != "" {
		p, err := filepath.Abs(root)
		if err == nil {
			if resolved, err := filepath.EvalSymlinks(p); err == nil {
				p = resolved
			}
			p = filepath.Clean(p)
			if runtime.GOOS == "windows" {
				p = strings.ToLower(p)
			}
			identity = "project:" + p
		}
	}
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}

func browserStorageKey(owner, topicID, scope, root string) string {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		// Untitled/legacy transient owners must never share an empty identity.
		return owner
	}
	project := ""
	if scope == "project" {
		project = browserProfileKey("", scope, root)
	}
	identity, _ := json.Marshal([]string{"browser-topic-v1", scope, project, topicID})
	sum := sha256.Sum256(identity)
	return hex.EncodeToString(sum[:])
}
func browserURL(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "about:blank" {
		return raw, "", nil
	}
	if len(raw) > 8192 || strings.ContainsAny(raw, "\r\n\x00\\") {
		return "", "", errors.New("invalid browser URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return "", "", errors.New("only explicit http:// or https:// URLs without credentials are allowed")
	}
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	return u.String(), u.Scheme + "://" + u.Host, nil
}
func (s *browserService) register(owner, key string, persistentKey ...string) error {
	storageKey := owner
	if len(persistentKey) > 0 && persistentKey[0] != "" {
		storageKey = persistentKey[0]
	}
	// A previous owner's metadata write must finish before a reopened topic
	// restores the same storage identity. Match saveMetadata's lock order.
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.revoked[owner] {
		return errBrowserStopped
	}
	if v := s.owners[owner]; v != nil {
		if v.profile != key || v.storageKey != storageKey {
			return errBrowserOwner
		}
		return nil
	}
	for _, existing := range s.owners {
		if existing.storageKey == storageKey {
			return errBrowserOwner
		}
	}
	s.owners[owner] = &browserOwner{profile: key, storageKey: storageKey, taskState: "idle"}
	s.restoreMetadataLocked(owner)
	return nil
}
func (s *browserService) state(owner string) BrowserState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked(owner)
}
func (s *browserService) stateLocked(owner string) BrowserState {
	v := emptyBrowserState(nil)
	v.Supported = s.host.Supported()
	o := s.owners[owner]
	if o == nil {
		v.Error = errBrowserOwner.Error()
		return v
	}
	v.ActiveTabID, v.Error, v.TaskState = o.active, o.err, o.taskState
	v.Busy = s.leases[o.profile] != nil
	for _, id := range o.tabs {
		if t := s.tabs[id]; t != nil {
			v.Tabs = append(v.Tabs, t.Tab)
		}
	}
	for _, d := range s.downloads {
		if tab := s.tabs[d.TabID]; tab != nil && tab.owner == owner {
			v.Downloads = append(v.Downloads, d)
		}
	}
	if o.clearApproval != nil {
		p := *o.clearApproval
		v.Approval = &p
	}
	for _, u := range s.uploads {
		if tab := s.tabs[u.TabID]; tab != nil && tab.owner == owner {
			v.Uploads = append(v.Uploads, u)
		}
	}
	if o.uploadApproval != nil {
		p := o.uploadApproval.approval
		v.Approval = &p
	}
	if t := o.task; t != nil {
		v.TaskID = t.id
		v.Consents = append(v.Consents, t.consents...)
		if t.pending != nil {
			p := t.pending.BrowserApproval
			v.Approval = &p
		}
		if t.popup != nil && !t.popup.resolved {
			p := t.popup.approval
			v.Approval = &p
		}
	}
	return v
}
func (s *browserService) notify(owner string) {
	if s.emit != nil {
		s.emit(owner, s.state(owner))
	}
}
func (s *browserService) fail(owner string, err error) BrowserState {
	s.mu.Lock()
	if o := s.owners[owner]; o != nil {
		o.err = err.Error()
	}
	s.mu.Unlock()
	return s.state(owner)
}
func (s *browserService) onEvent(e browserhost.Event) {
	s.mu.Lock()
	t := s.tabs[e.TabID]
	if t == nil || s.closed {
		s.mu.Unlock()
		return
	}
	owner := t.owner
	if e.Kind == "popup" {
		address, origin, err := browserURL(e.URL)
		if err != nil {
			t.Error = err.Error()
			s.mu.Unlock()
			s.notify(owner)
			return
		}
		o := s.owners[owner]
		task := s.leases[o.profile]
		if task != nil {
			if task.owner == owner && task.tab == e.TabID && task.ctx.Err() == nil && task.popup == nil {
				task.popup = &browserPopup{approval: BrowserApproval{ID: browserID(), Origin: origin, Reason: "Open this blocked popup in a new owned browser tab and allow its page data to be sent to " + task.model + ": " + address + "?"}, url: address, answer: make(chan bool, 1)}
				t.epoch++
				o.taskState = "awaiting_approval"
			}
			s.mu.Unlock()
			s.notify(owner)
			return
		}
		s.mu.Unlock()
		go func() {
			s.command(context.Background(), owner, BrowserCommand{Op: "new", Value: address})
			s.notify(owner)
		}()
		return
	}
	if e.Kind == "upload" && e.Upload != nil {
		if e.Upload.TabID == e.TabID {
			s.uploads[e.Upload.ID] = *e.Upload
		}
		s.mu.Unlock()
		s.notify(owner)
		return
	}
	if e.Kind == "download" && e.Download != nil {
		if e.Download.TabID == e.TabID {
			s.downloads[e.Download.ID] = *e.Download
		}
		s.mu.Unlock()
		s.notify(owner)
		return
	}
	t.epoch++
	// Only these events carry a complete native tab-state snapshot. Focus,
	// navigation-start and error events omit history flags rather than clear them.
	fullState := e.Kind == "loaded" || e.Kind == "history" || e.Kind == "title" || e.Kind == "source" || e.Kind == "navigationCompleted" || e.Kind == "navigation-completed"
	if e.URL != "" && e.Kind != "blocked" && e.Kind != "popup" {
		t.URL = e.URL
	}
	if fullState || e.Title != "" {
		t.Title = boundedComputerText(e.Title, 1024)
	}
	t.Error = boundedComputerText(e.Error, 2048)
	if fullState {
		t.CanGoBack, t.CanGoForward = e.CanGoBack, e.CanGoForward
	}
	if e.Kind == "blocked" || e.Kind == "popup" {
		t.Error = "Blocked browser request: " + boundedComputerText(e.URL, 1024)
	}
	if e.Kind == "user-input" || e.Kind == "interaction" || e.Kind == "manual" || e.Kind == "error" || e.Kind == "blocked" || e.Kind == "popup" {
		if o := s.owners[owner]; o != nil && o.task != nil && s.leases[o.profile] == o.task {
			if e.Kind == "user-input" || e.Kind == "manual" || e.Kind == "interaction" {
				o.task.stopState = "taken_over"
			}
			o.task.budget.stopped = true
			o.task.cancel()
		}
	}
	switch e.Kind {
	case "navigation", "navigationStarting", "navigation-starting", "loading", "navigating":
		t.Loading = true
	case "navigationCompleted", "navigation-completed", "loaded", "error":
		t.Loading = false
	}
	s.mu.Unlock()
	s.saveMetadata(owner)
	s.notify(owner)
}
func (s *browserService) navigationAllowed(id, raw string) bool {
	_, origin, err := browserURL(raw)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tab := s.tabs[id]
	if tab == nil || s.closed {
		return false
	}
	o := s.owners[tab.owner]
	if o == nil {
		return false
	}
	if s.clearing[o.profile] {
		return raw == "about:blank"
	}
	t := s.leases[o.profile]
	if t == nil {
		tab.epoch++
		return true
	}
	if t.tab != id || t.ctx.Err() != nil {
		return false
	}
	if origin == "" || t.origins[origin] {
		tab.epoch++
		return true
	}
	// Native callbacks must return immediately. An unapproved redirect is not
	// replayed after consent; the controller stops and reports the exact origin.
	o.err = "Blocked navigation to " + origin + "; explicit origin and model-exposure consent is required"
	t.budget.stopped = true
	t.cancel()
	return false
}
func (s *browserService) command(ctx context.Context, owner string, cmd BrowserCommand) BrowserState {
	defer s.saveMetadata(owner)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if cmd.Op == "approve" {
		return s.approve(owner, cmd)
	}
	if cmd.Op == "takeover" || cmd.Op == "cancelTask" {
		s.mu.Lock()
		o := s.owners[owner]
		if o != nil && o.task != nil && s.leases[o.profile] == o.task {
			o.task.stopState = "cancelled"
			if cmd.Op == "takeover" {
				o.task.stopState = "taken_over"
			}
			o.task.budget.stopped = true
			o.task.cancel()
			o.taskState = "stopping"
		}
		s.mu.Unlock()
		return s.state(owner)
	}
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	s.mu.Lock()
	o := s.owners[owner]
	if o == nil || s.closed {
		s.mu.Unlock()
		return s.fail(owner, errBrowserOwner)
	}
	downloadOp := strings.HasPrefix(cmd.Op, "download-") || strings.HasPrefix(cmd.Op, "upload-")
	if s.leases[o.profile] != nil && !downloadOp {
		s.mu.Unlock()
		return s.fail(owner, errBrowserBusy)
	}
	if !s.host.Supported() {
		s.mu.Unlock()
		return s.fail(owner, browserhost.ErrUnsupported)
	}
	id := cmd.TabID
	if id == "" {
		id = o.active
	}
	tab := s.tabs[id]
	if cmd.Op == "clear" && tab == nil {
		id = browserID()
		tab = &browserTab{Tab: browserhost.Tab{ID: id, URL: "about:blank", Zoom: 1}, owner: owner}
		s.tabs[id] = tab
		o.tabs = append(o.tabs, id)
		o.active = id
	}
	if cmd.Op != "new" && (tab == nil || tab.owner != owner) {
		s.mu.Unlock()
		return s.fail(owner, errBrowserOwner)
	}
	profile := o.profile
	currentURL := ""
	if tab != nil {
		currentURL = tab.URL
	}
	o.err = ""
	if tab != nil {
		tab.epoch++
	}
	s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return s.fail(owner, err)
	}
	if tab != nil && !tab.created && cmd.Op != "close" && cmd.Op != "clear" && !downloadOp {
		if err := s.host.Create(ctx, filepath.Join(s.root, profile), id, currentURL); err != nil {
			return s.fail(owner, err)
		}
		s.mu.Lock()
		tab.created = true
		s.mu.Unlock()
	}
	var err error
	switch cmd.Op {
	case "new":
		s.mu.Lock()
		tooMany := len(o.tabs) >= 32
		s.mu.Unlock()
		if tooMany {
			err = errors.New("browser tab limit reached")
			break
		}
		raw := cmd.Value
		if raw == "" {
			raw = "about:blank"
		}
		var u string
		u, _, err = browserURL(raw)
		if err != nil {
			break
		}
		id = browserID()
		s.mu.Lock()
		s.tabs[id] = &browserTab{Tab: browserhost.Tab{ID: id, URL: u, Zoom: 1}, owner: owner}
		o.tabs = append(o.tabs, id)
		o.active = id
		s.mu.Unlock()
		err = s.host.Create(ctx, filepath.Join(s.root, profile), id, u)
		if err != nil {
			s.removeTab(id)
		} else {
			s.mu.Lock()
			if live := s.tabs[id]; live != nil {
				live.created = true
			} else {
				err = errBrowserOwner
			}
			s.mu.Unlock()
		}
	case "close":
		if tab.created {
			err = s.host.Close(ctx, id)
		}
		if err == nil {
			s.removeTab(id)
		}
	case "select":
		s.mu.Lock()
		o.active = id
		s.mu.Unlock()
	case "navigate":
		var u string
		u, _, err = browserURL(cmd.Value)
		if err == nil {
			err = s.host.Navigate(ctx, id, u)
		}
	case "back", "forward", "reload", "stop", "find", "zoom":
		if len(cmd.Value) > 2048 {
			err = errors.New("browser command value too long")
			break
		}
		if cmd.Op == "zoom" {
			var z float64
			z, err = strconv.ParseFloat(cmd.Value, 64)
			if err != nil || math.IsNaN(z) || math.IsInf(z, 0) || z < 0.5 || z > 2 {
				err = errors.New("zoom must be between 0.5 and 2")
				break
			}
		}
		_, err = s.host.Command(ctx, id, cmd.Op, cmd.Value)
		if err == nil && cmd.Op == "zoom" {
			z, _ := strconv.ParseFloat(cmd.Value, 64)
			s.mu.Lock()
			tab.Zoom = z
			s.mu.Unlock()
		}
	case "clear":
		s.mu.Lock()
		if !cmd.Allow || o.clearApproval == nil || cmd.ApprovalID != o.clearApproval.ID {
			o.clearApproval = &BrowserApproval{ID: browserID(), Origin: "shared profile", Reason: "Permanently clear cookies, cache and site storage for this project browser profile, shared by its conversations? All tabs using this profile will close. This cannot be undone."}
			s.mu.Unlock()
			break
		}
		o.clearApproval = nil
		s.mu.Unlock()
		err = s.clearProfile(ctx, id, profile)
	case "download-list", "download-approve", "download-deny", "download-cancel":
		err = s.downloadCommand(ctx, owner, id, cmd)
	case "upload-list", "upload-deny":
		err = s.uploadCommand(ctx, owner, id, cmd)
	default:
		err = errors.New("unsupported browser command")
	}
	if err != nil {
		return s.fail(owner, err)
	}
	if cmd.Op == "new" || cmd.Op == "select" || cmd.Op == "close" {
		s.mu.Lock()
		v := o.viewport
		v.TabID = o.active
		if s.shownOwner != owner {
			v.Visible = false
		}
		s.mu.Unlock()
		if v.Visible && v.TabID != "" {
			if err := s.host.Present(ctx, v); err != nil {
				return s.fail(owner, err)
			}
		}
	}
	if cmd.Op != "close" {
		s.refreshTab(ctx, id)
	}
	return s.state(owner)
}
func (s *browserService) refreshTab(ctx context.Context, id string) {
	raw, err := s.host.Command(ctx, id, "state", "")
	if err != nil {
		return
	}
	var state browserhost.Tab
	if json.Unmarshal([]byte(raw), &state) != nil || state.ID != id {
		return
	}
	s.mu.Lock()
	if t := s.tabs[id]; t != nil {
		t.Tab = state
	}
	s.mu.Unlock()
}

// Caller holds ioMu. During clearing, callbacks cannot navigate back to a live
// page or persist the transient about:blank URL over the recovery metadata.
func (s *browserService) clearProfile(ctx context.Context, id, profile string) (retErr error) {
	s.mu.Lock()
	before := map[string]browserTab{}
	owners := map[string]bool{}
	for key, tab := range s.tabs {
		if o := s.owners[tab.owner]; o != nil && o.profile == profile {
			before[key] = *tab
			owners[tab.owner] = true
		}
	}
	s.clearing[profile] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clearing, profile)
		if retErr != nil {
			for key, tab := range before {
				if live := s.tabs[key]; live != nil {
					live.Tab = tab.Tab
					live.epoch++
				}
			}
		}
		s.mu.Unlock()
		for owner := range owners {
			s.saveMetadata(owner)
			s.notify(owner)
		}
	}()
	for key, tab := range before {
		if key == id || !tab.created {
			continue
		}
		if err := s.host.Close(ctx, key); err != nil {
			return err
		}
		s.mu.Lock()
		if live := s.tabs[key]; live != nil {
			live.created = false
		}
		s.mu.Unlock()
	}
	if !before[id].created {
		if err := s.host.Create(ctx, filepath.Join(s.root, profile), id, "about:blank"); err != nil {
			return err
		}
		s.mu.Lock()
		s.tabs[id].created = true
		s.mu.Unlock()
	} else if err := s.host.Navigate(ctx, id, "about:blank"); err != nil {
		return err
	}
	tick := time.NewTicker(30 * time.Millisecond)
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
		if state.URL == "about:blank" && !state.Loading {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
	if _, err := s.host.Command(ctx, id, "clear", ""); err != nil {
		return err
	}
	if err := s.host.Close(ctx, id); err != nil {
		return err
	}
	for key := range before {
		s.removeTab(key)
	}
	return nil
}

func (s *browserService) downloadCommand(ctx context.Context, owner, id string, cmd BrowserCommand) error {
	if cmd.Op == "download-list" {
		raw, err := s.host.Command(ctx, id, cmd.Op, "")
		if err != nil {
			return err
		}
		var entries []browserhost.Download
		if err = json.Unmarshal([]byte(raw), &entries); err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, d := range entries {
			if d.TabID == id {
				s.downloads[d.ID] = d
			}
		}
		return nil
	}
	downloadID := cmd.Value
	if cmd.Op == "download-approve" {
		var request struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		}
		if err := browserDecode(cmd.Value, &request); err != nil {
			return err
		}
		if !filepath.IsAbs(request.Path) || filepath.Clean(request.Path) != request.Path || strings.HasPrefix(request.Path, `\\`) {
			return errors.New("download approval requires the user's exact absolute local destination path")
		}
		if err := browserDownloadPath(request.Path); err != nil {
			return err
		}
		downloadID = request.ID
	}
	s.mu.Lock()
	d, ok := s.downloads[downloadID]
	tab := s.tabs[id]
	valid := ok && d.TabID == id && tab != nil && tab.owner == owner
	s.mu.Unlock()
	if !valid {
		return errBrowserOwner
	}
	_, err := s.host.Command(ctx, id, cmd.Op, cmd.Value)
	return err
}

func browserDownloadPath(path string) error {
	if err := browserhost.ValidateTransferPath(path); err != nil {
		return err
	}
	if strings.Contains(path[len(filepath.VolumeName(path)):], ":") {
		return errors.New("download destination cannot use alternate data streams")
	}
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Clean(parent), filepath.Clean(resolved)) {
		return errors.New("download destination cannot traverse a symlink or junction")
	}
	if file, err := os.Lstat(path); err == nil {
		if !file.Mode().IsRegular() || file.Mode()&os.ModeSymlink != 0 {
			return errors.New("download target must be a regular non-symlink file")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}
func (s *browserService) removeTab(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tabs[id]
	if t == nil {
		return
	}
	delete(s.tabs, id)
	s.cleanupSnapshotsLocked(id)
	for key, u := range s.uploads {
		if u.TabID == id {
			delete(s.uploads, key)
		}
	}
	for key, d := range s.downloads {
		if d.TabID == id {
			delete(s.downloads, key)
		}
	}
	o := s.owners[t.owner]
	if o == nil {
		return
	}
	for i, v := range o.tabs {
		if v == id {
			o.tabs = append(o.tabs[:i], o.tabs[i+1:]...)
			break
		}
	}
	if o.active == id {
		o.active = ""
		if len(o.tabs) > 0 {
			o.active = o.tabs[len(o.tabs)-1]
		}
	}
}
func (s *browserService) present(ctx context.Context, owner string, v BrowserViewport) error {
	for _, n := range []float64{v.X, v.Y, v.Width, v.Height, v.Scale} {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return errors.New("invalid viewport")
		}
	}
	if v.Visible && (v.X < 0 || v.Y < 0 || v.Width <= 0 || v.Height <= 0 || v.Width > 16384 || v.Height > 16384 || v.Scale < 0.25 || v.Scale > 8) {
		return errors.New("invalid viewport bounds")
	}
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	s.mu.Lock()
	o := s.owners[owner]
	if v.TabID == "" && o != nil {
		v.TabID = o.active
	}
	t := s.tabs[v.TabID]
	if t == nil || t.owner != owner || o == nil {
		s.mu.Unlock()
		return errBrowserOwner
	}
	if !v.Visible && s.shownOwner != owner {
		s.mu.Unlock()
		return nil
	}
	valid := !s.closed && t != nil && t.owner == owner && o != nil && o.active == v.TabID
	if valid {
		o.viewport = v
	}
	s.mu.Unlock()
	if !valid {
		return errBrowserOwner
	}
	if err := s.host.Present(ctx, v); err != nil {
		return err
	}
	s.mu.Lock()
	if v.Visible {
		s.shownOwner = owner
		if current := s.owners[owner]; current != nil {
			current.lastVisibleTab = v.TabID
		}
	} else if s.shownOwner == owner {
		s.shownOwner = ""
	}
	s.mu.Unlock()
	return nil
}
func (s *browserService) approve(owner string, cmd BrowserCommand) BrowserState {
	s.mu.Lock()
	o := s.owners[owner]
	if o != nil && o.task != nil && o.task.popup != nil && o.task.popup.approval.ID == cmd.ApprovalID && !o.task.popup.resolved && o.task.ctx.Err() == nil {
		o.task.popup.resolved = true
		o.task.popup.answer <- cmd.Allow
		s.mu.Unlock()
		return s.state(owner)
	}
	if o != nil && o.uploadApproval != nil && o.uploadApproval.approval.ID == cmd.ApprovalID {
		p := o.uploadApproval
		o.uploadApproval = nil
		s.mu.Unlock()
		return s.approveUpload(owner, p, cmd.Allow)
	}
	if o != nil && o.clearApproval != nil && o.clearApproval.ID == cmd.ApprovalID {
		if !cmd.Allow {
			o.clearApproval = nil
			s.mu.Unlock()
			return s.state(owner)
		}
		s.mu.Unlock()
		return s.command(context.Background(), owner, BrowserCommand{Op: "clear", ApprovalID: cmd.ApprovalID, Allow: true})
	}
	if o == nil || o.task == nil || o.task.pending == nil || o.task.pending.ID != cmd.ApprovalID || o.task.ctx.Err() != nil {
		s.mu.Unlock()
		return s.fail(owner, errors.New("approval expired or belongs to another conversation"))
	}
	p := o.task.pending
	o.task.pending = nil
	p.answer <- cmd.Allow
	s.mu.Unlock()
	return s.state(owner)
}
func (s *browserService) consent(t *browserTask, origin, reason string) error {
	// 完全访问 covers the browser too: the account already accepted every tool
	// approval for this conversation, so a page load is not a second decision.
	if s.fullAccess != nil && s.fullAccess(t.owner) {
		s.mu.Lock()
		if err := s.checkTaskLocked(t); err != nil {
			s.mu.Unlock()
			return err
		}
		t.consents = append(t.consents, BrowserConsent{Origin: origin, Model: t.model, Reason: reason})
		t.lastConsent = time.Now()
		s.mu.Unlock()
		return nil
	}
	s.mu.Lock()
	if err := s.checkTaskLocked(t); err != nil {
		s.mu.Unlock()
		return err
	}
	if t.pending != nil {
		s.mu.Unlock()
		return errBrowserBusy
	}
	if !time.Now().Before(t.budget.deadline) {
		t.expire(context.DeadlineExceeded)
		s.mu.Unlock()
		return context.DeadlineExceeded
	}
	p := &browserPending{BrowserApproval: BrowserApproval{ID: browserID(), Origin: origin, Reason: reason}, task: t, answer: make(chan bool, 1)}
	t.pending = p
	t.waitAt = time.Now()
	t.timer.Stop()
	s.owners[t.owner].taskState = "awaiting_approval"
	s.mu.Unlock()
	resumeParent := provider.BeginTaskHumanWait(t.ctx)
	defer func() {
		s.mu.Lock()
		if t.pending == p {
			t.pending = nil
		}
		if !t.waitAt.IsZero() {
			t.budget.deadline = t.budget.deadline.Add(time.Since(t.waitAt))
			t.waitAt = time.Time{}
			if t.ctx.Err() == nil && s.leases[t.profile] == t {
				t.timer.Reset(time.Until(t.budget.deadline))
			}
		}
		s.mu.Unlock()
		resumeParent()
	}()
	s.notify(t.owner)
	select {
	case <-t.ctx.Done():
		return t.ctx.Err()
	case allowed := <-p.answer:
		if !allowed {
			return errors.New("browser consent denied")
		}
		if err := t.ctx.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		if err := s.checkTaskLocked(t); err != nil {
			s.mu.Unlock()
			return err
		}
		t.consents = append(t.consents, BrowserConsent{Origin: origin, Model: t.model, Reason: reason})
		t.lastConsent = time.Now()
		s.owners[t.owner].taskState = "running"
		s.mu.Unlock()
		return nil
	}
}
func (s *browserService) closeOwner(owner string) {
	s.mu.Lock()
	s.revoked[owner] = true
	o := s.owners[owner]
	if o == nil {
		s.mu.Unlock()
		return
	}
	if o.task != nil {
		o.task.budget.stopped = true
		o.task.cancel()
	}
	ids := append([]string(nil), o.tabs...)
	delete(s.owners, owner)
	for _, id := range ids {
		delete(s.tabs, id)
	}
	s.mu.Unlock()
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, id := range ids {
		if err := s.host.Close(ctx, id); err != nil {
			// Keep snapshots while native closure is uncertain. Shutdown will
			// release them after the host can no longer read selected files.
			continue
		}
		s.mu.Lock()
		s.cleanupSnapshotsLocked(id)
		s.mu.Unlock()
	}
}
func (s *browserService) shutdown() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	for _, t := range s.leases {
		t.budget.stopped = true
		t.cancel()
	}
	s.mu.Unlock()
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	if confirmer, ok := s.host.(browserhost.ShutdownConfirmer); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := confirmer.ShutdownConfirmed(ctx)
		cancel()
		if err != nil {
			return
		}
	} else {
		s.host.Shutdown()
		return
	}
	s.mu.Lock()
	for id := range s.snapshots {
		s.cleanupSnapshotsLocked(id)
	}
	s.mu.Unlock()
}

func (a *App) initBrowser() {
	if !browserControlEnabled() {
		return
	}
	var s *browserService
	h := browserhost.NewWithWindowClass(browserWindowClass(), func(e browserhost.Event) {
		if s != nil {
			s.onEvent(e)
		}
	})
	s = newBrowserService(h, filepath.Join(filepath.Dir(config.UserConfigPath()), "browser-profiles"), func(owner string, state BrowserState) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "browser:state", map[string]any{"ownerTabId": owner, "state": state})
		}
	})
	s.fullAccess = func(owner string) bool {
		a.mu.RLock()
		t := a.tabs[owner]
		a.mu.RUnlock()
		return t != nil && t.Ctrl != nil && t.Ctrl.AutoApproveTools()
	}
	a.browser = s
}
func (a *App) browserOwner(owner string) (*browserService, error) {
	if !browserControlEnabled() {
		return nil, browserhost.ErrUnsupported
	}
	a.mu.RLock()
	t := a.tabs[owner]
	if t == nil || t.ReadOnly {
		a.mu.RUnlock()
		return nil, errBrowserOwner
	}
	scope, root, topicID := t.Scope, t.WorkspaceRoot, t.TopicID
	a.mu.RUnlock()
	if a.browser == nil {
		return nil, browserhost.ErrUnsupported
	}
	storageKey := browserStorageKey(owner, topicID, scope, root)
	if err := a.browser.register(owner, browserProfileKey(storageKey, scope, root), storageKey); err != nil {
		return nil, err
	}
	return a.browser, nil
}
func (a *App) BrowserAvailable() bool {
	return browserControlEnabled() && runtime.GOOS == "windows" && a.browser != nil
}
func (a *App) GetBrowserState(ownerTabID string) BrowserState {
	s, err := a.browserOwner(ownerTabID)
	if err != nil {
		return emptyBrowserState(err)
	}
	return s.state(ownerTabID)
}
func (a *App) BrowserCommand(ownerTabID string, cmd BrowserCommand) BrowserState {
	done, err := a.beginAppWork()
	if err != nil {
		return emptyBrowserState(err)
	}
	defer done()
	s, err := a.browserOwner(ownerTabID)
	if err != nil {
		return emptyBrowserState(err)
	}
	if cmd.Op == "upload-approve" {
		state := a.chooseBrowserUpload(s, ownerTabID, cmd)
		s.notify(ownerTabID)
		return state
	}
	state := s.command(a.bootContext(), ownerTabID, cmd)
	s.notify(ownerTabID)
	return state
}
func (a *App) SetBrowserViewport(ownerTabID string, viewport BrowserViewport) error {
	s, err := a.browserOwner(ownerTabID)
	if err != nil {
		return err
	}
	if viewport.Visible {
		a.mu.RLock()
		defer a.mu.RUnlock()
		if a.activeTabID != ownerTabID {
			return errBrowserOwner
		}
	}
	return s.present(a.bootContext(), ownerTabID, viewport)
}
