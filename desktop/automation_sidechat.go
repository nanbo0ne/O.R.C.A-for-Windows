package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/boot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/fileutil"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool/hosttools"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

type AutomationView = hosttools.AutomationView

func (a *App) ListAutomations() []AutomationView {
	return hosttools.ListAutomations()
}

func (a *App) PauseAutomation(id string) error {
	if hosttools.PauseAutomation(id) {
		return nil
	}
	return fmt.Errorf("automation %q is not scheduled or does not exist", id)
}

func (a *App) ResumeAutomation(id string) error {
	done, err := a.beginAppWork()
	if err != nil {
		return err
	}
	defer done()
	if hosttools.ResumeAutomation(id) {
		return nil
	}
	return fmt.Errorf("automation %q is not paused or does not exist", id)
}

func (a *App) CancelAutomation(id string) error {
	if hosttools.CancelAutomation(id) {
		return nil
	}
	return fmt.Errorf("automation %q does not exist", id)
}

func (a *App) ClearFinishedAutomations() error {
	hosttools.ClearFinishedAutomations()
	return nil
}

type SideChatMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"createdAt"`
}

type sideChatCancel struct {
	token  string
	cancel context.CancelFunc
}

type sideChatFile struct {
	Sessions map[string][]SideChatMessage `json:"sessions"`
	Bindings map[string]sideChatBinding   `json:"bindings,omitempty"`
}

type sideChatBinding struct {
	ConversationID string `json:"conversation_id"`
	Source         string `json:"source"`
	Context        string `json:"context"`
	Generation     string `json:"generation,omitempty"`
	Pending        bool   `json:"pending,omitempty"`
}

func sideChatStorePath() string {
	return filepath.Join(desktopConfigDir(), "side-chats.json")
}

func loadSideChatFile() sideChatFile {
	out, _ := readSideChatFile()
	return out
}

func readSideChatFile() (sideChatFile, error) {
	out := sideChatFile{Sessions: map[string][]SideChatMessage{}, Bindings: map[string]sideChatBinding{}}
	b, err := os.ReadFile(sideChatStorePath())
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, errors.New("side chat storage is unavailable; no request was sent")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF {
		return sideChatFile{}, errors.New("side chat storage is invalid; no new conversation was created")
	}
	if out.Sessions == nil {
		out.Sessions = map[string][]SideChatMessage{}
	}
	if out.Bindings == nil {
		out.Bindings = map[string]sideChatBinding{}
	}
	return out, nil
}

func saveSideChatFile(f sideChatFile) error {
	path := sideChatStorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".side-chat-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err = tmp.Chmod(0600); err != nil {
		return err
	}
	if _, err = tmp.Write(b); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return fileutil.RenameReplace(tmp.Name(), path)
}

func (a *App) sideChatKey(tabID string) string {
	tab := a.tabByID(tabID)
	if tab == nil {
		return strings.TrimSpace(tabID)
	}
	if tab.Ctrl != nil {
		if path := strings.TrimSpace(tab.Ctrl.SessionPath()); path != "" {
			return canonicalTabSessionPath(path)
		}
	}
	if path := strings.TrimSpace(tab.currentSessionPath()); path != "" {
		return canonicalTabSessionPath(path)
	}
	return tab.ID
}

func (a *App) ListSideChat(tabID string) []SideChatMessage {
	key := a.sideChatKey(tabID)
	if key == "" {
		return []SideChatMessage{}
	}
	a.sideChatMu.Lock()
	defer a.sideChatMu.Unlock()
	f := loadSideChatFile()
	history := f.Sessions[key]
	if len(history) == 0 {
		return []SideChatMessage{}
	}
	return append([]SideChatMessage{}, history...)
}

func (a *App) ClearSideChat(tabID string) error {
	key := a.sideChatKey(tabID)
	if key == "" {
		return nil
	}
	a.sideChatMu.Lock()
	defer a.sideChatMu.Unlock()
	if a.sideChatCancels[key].cancel != nil {
		return errors.New("side chat is still running; cancel it and wait before clearing")
	}
	f, err := readSideChatFile()
	if err != nil {
		return err
	}
	delete(f.Sessions, key)
	delete(f.Bindings, key)
	return saveSideChatFile(f)
}

func (a *App) CancelSideChat(tabID string) error {
	key := a.sideChatKey(tabID)
	a.sideChatMu.Lock()
	cancel := a.sideChatCancels[key].cancel
	a.sideChatMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (a *App) SendSideChat(tabID, input string) error {
	done, err := a.beginAppWork()
	if err != nil {
		return err
	}
	defer done()
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	source, err := a.sideChatSource(tabID)
	if err != nil {
		return err
	}
	key, entry := source.key, source.entry
	web := entry.Kind == "deepseekweb-beta"
	generation := ""
	releaseAdmission := func() {}
	if web {
		webBetaAdmission.Lock()
		var once sync.Once
		releaseAdmission = func() { once.Do(webBetaAdmission.Unlock) }
		defer releaseAdmission()
		engine := webbeta.CurrentEngine()
		if webBetaAdmission.blocked || engine == nil {
			return errors.New("web side chat requires an already open, explicitly enabled web session")
		}
		status := engine.Status()
		if !status.Settings.Enabled || !status.Browser.Ready || status.Blocked != "" {
			return errors.New("web side chat is unavailable; sign in and enable web sending explicitly")
		}
		generation = status.Generation
	}
	p, err := boot.NewProvider(entry)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(a.bootContext())
	token := newSideChatID("run")
	a.sideChatMu.Lock()
	if a.sideChatCancels[key].cancel != nil {
		a.sideChatMu.Unlock()
		cancel()
		return errors.New("side chat is already running")
	}
	if a.sideChatCancels == nil {
		a.sideChatCancels = map[string]sideChatCancel{}
	}
	a.sideChatCancels[key] = sideChatCancel{token: token, cancel: cancel}
	a.sideChatMu.Unlock()
	defer func() {
		cancel()
		a.sideChatMu.Lock()
		if a.sideChatCancels[key].token == token {
			delete(a.sideChatCancels, key)
		}
		a.sideChatMu.Unlock()
	}()

	if !source.current() {
		return errors.New("main conversation or model changed; side chat was not sent")
	}
	sourceID := fmt.Sprintf("%x", sha256.Sum256([]byte(entry.Name+"\x00"+entry.Kind+"\x00"+entry.BaseURL+"\x00"+entry.Model)))
	a.sideChatMu.Lock()
	f, err := readSideChatFile()
	if err != nil {
		a.sideChatMu.Unlock()
		return err
	}
	history := append([]SideChatMessage(nil), f.Sessions[key]...)
	binding, bound := f.Bindings[key]
	if (!bound && len(history) != 0) || (bound && (binding.Source != sourceID || binding.Generation != generation ||
		binding.Pending || web && binding.Context != source.context)) || web && len(history) >= 80 {
		a.sideChatMu.Unlock()
		return errors.New("side chat context, source or completion state changed; clear side chat explicitly before sending again")
	}
	if !bound {
		binding = sideChatBinding{ConversationID: "sidechat:" + event.NewRequestID(), Source: sourceID, Context: source.context, Generation: generation}
	}
	if !web {
		binding.Context = source.context
	}
	binding.Pending = true
	f.Bindings[key] = binding
	err = saveSideChatFile(f)
	a.sideChatMu.Unlock()
	if err != nil {
		return err
	}
	userMsg := SideChatMessage{ID: newSideChatID("user"), Role: "user", Content: input, CreatedAt: time.Now().UnixMilli()}
	history = append(history, userMsg)
	req := bindAuxiliaryRequest(ctx, provider.RequestPurposeTurn, provider.Request{
		ConversationID: binding.ConversationID,
		ContextEpoch:   1,
		Messages:       sideChatMessages(binding.Context, history, web),
		Temperature:    0.2,
		MaxTokens:      1200,
	})
	if scoped, ok := p.(provider.TaskProvider); ok {
		taskCtx, finish, err := scoped.BeginTask(ctx, binding.ConversationID, req.ParentTaskID)
		if err != nil {
			return err
		}
		// Identity teardown must drain the host-side save as well as the stream.
		defer finish()
		ctx = taskCtx
	}
	if !source.current() {
		return errors.New("main conversation or model changed; side chat was not sent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ch, err := p.Stream(ctx, req)
	releaseAdmission()
	if err != nil {
		return err
	}
	answer, err := collectAuxiliaryText(ctx, ch)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(answer)
	if text == "" {
		return errors.New("side chat returned no answer; request was not replayed")
	}
	if !source.current() {
		return errors.New("main conversation or model changed before side chat completed")
	}
	history = append(history, SideChatMessage{ID: newSideChatID("assistant"), Role: "assistant", Content: text, CreatedAt: time.Now().UnixMilli()})
	a.sideChatMu.Lock()
	defer a.sideChatMu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if a.sideChatCancels[key].token != token {
		return errors.New("side chat identity changed before completion")
	}
	f, err = readSideChatFile()
	if err != nil {
		return err
	}
	if f.Bindings[key].ConversationID != binding.ConversationID {
		return errors.New("side chat was cleared before completion")
	}
	if !source.current() {
		return errors.New("main conversation or model changed before side chat completed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	binding.Pending = false
	f.Bindings[key] = binding
	f.Sessions[key] = trimSideChatHistory(history)
	return saveSideChatFile(f)
}

type sideChatSourceSnapshot struct {
	key     string
	entry   *config.ProviderEntry
	context string
	current func() bool
}

func (a *App) sideChatSource(tabID string) (sideChatSourceSnapshot, error) {
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		a.mu.RUnlock()
		return sideChatSourceSnapshot{}, errors.New("side chat session is not ready")
	}
	ref, root, ctrl := tab.model, tab.WorkspaceRoot, tab.Ctrl
	effort := cloneStringPtr(tab.effort)
	path := tab.currentSessionPath()
	key := tab.ID
	if path != "" {
		key = canonicalTabSessionPath(path)
	}
	a.mu.RUnlock()
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return sideChatSourceSnapshot{}, err
	}
	selected := ref
	if strings.TrimSpace(selected) == "" {
		selected = cfg.DefaultModel
	}
	entry, err := exactAuxiliaryProvider(cfg, selected)
	if err != nil {
		return sideChatSourceSnapshot{}, err
	}
	if normalized := compatibleTabEffort(entry, effort); normalized != nil {
		entry.Effort = *normalized
	}
	return sideChatSourceSnapshot{key: key, entry: entry, context: sideChatControllerContext(ctrl), current: func() bool {
		a.mu.RLock()
		defer a.mu.RUnlock()
		return a.tabByIDLocked(tabID) == tab && tab.Ctrl == ctrl && tab.model == ref &&
			tab.WorkspaceRoot == root && tab.currentSessionPath() == path
	}}, nil
}

func (a *App) sideChatRequestMessages(tabID string, side []SideChatMessage) []provider.Message {
	return sideChatMessages(a.sideChatMainContext(tabID), side, false)
}

func sideChatMessages(contextText string, side []SideChatMessage, preservePrefix bool) []provider.Message {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "你是 Orca 的侧边聊天助手。你只能根据主对话和用户问题进行只读解答，不要修改文件、运行命令、创建自动化或调用任何工具。回答要简洁、具体、中文优先。主对话最近 1-2 轮最重要，但必要时可以参考更早上下文。"},
		{Role: provider.RoleUser, Content: "主对话上下文如下：\n\n" + contextText},
	}
	start := 0
	if !preservePrefix && len(side) > 12 {
		start = len(side) - 12
	}
	for _, m := range side[start:] {
		role := provider.RoleUser
		if m.Role == "assistant" {
			role = provider.RoleAssistant
		}
		msgs = append(msgs, provider.Message{Role: role, Content: m.Content})
	}
	return msgs
}

func (a *App) sideChatMainContext(tabID string) string {
	return sideChatControllerContext(a.ctrlByTabID(tabID))
}

func sideChatControllerContext(ctrl *control.Controller) string {
	if ctrl == nil {
		return "（主对话尚未就绪）"
	}
	history := historyMessages(ctrl.History(), sessionDisplayResolver(controllerSessionDir(ctrl), ctrl.SessionPath()))
	if len(history) == 0 {
		return "（主对话暂无可见消息）"
	}
	visible := make([]HistoryMessage, 0, len(history))
	for _, m := range history {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		visible = append(visible, m)
	}
	if len(visible) == 0 {
		return "（主对话暂无可见用户或 AI 消息）"
	}
	start := 0
	if len(visible) > 8 {
		start = len(visible) - 8
	}
	var b strings.Builder
	if start > 0 {
		b.WriteString("更早上下文已省略；以下是最近主对话，最后 1-2 轮优先参考。\n\n")
	}
	for _, m := range visible[start:] {
		role := "用户"
		if m.Role == "assistant" {
			role = "AI"
		}
		content := strings.TrimSpace(m.Content)
		if len([]rune(content)) > 3000 {
			runes := []rune(content)
			content = string(runes[:3000]) + "\n（已截断）"
		}
		b.WriteString(role)
		b.WriteString("：\n")
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func trimSideChatHistory(history []SideChatMessage) []SideChatMessage {
	const keep = 80
	if len(history) <= keep {
		return history
	}
	return append([]SideChatMessage(nil), history[len(history)-keep:]...)
}

func newSideChatID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
