package main

// whalestack_app.go —— 用鲸栈账号登录桌面端，自动获得中转站的模型访问权限。
//
// 登录由统一登录服务完成（它负责账号校验与凭证签发），这里只做两件事：把
// 返回的凭证写进全局凭据文件，并把「鲸栈」注册成一个模型供应商。凭证按账号
// 派生、可重复登录复用，不需要用户手动复制 Key。

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

const (
	whaleStackOrigin   = "https://login.aichat.diy"
	whaleStackKeyEnv   = "WHALESTACK_API_KEY"
	whaleStackProvider = "鲸栈"
)

type whaleStackLogin struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	KeyName string `json:"keyName"`
}

type whaleStackError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// WhaleStackSignIn verifies the account, stores the relay credential and adds
// the provider, so the models are usable immediately.
func (a *App) WhaleStackSignIn(email, password string) (ProviderView, error) {
	email = strings.TrimSpace(email)
	if email == "" || password == "" {
		return ProviderView{}, errors.New("请输入用户名和密码")
	}
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		return ProviderView{}, err
	}
	client := &http.Client{Timeout: 25 * time.Second}
	request, err := http.NewRequest(http.MethodPost, whaleStackOrigin+"/api/app/login", bytes.NewReader(body))
	if err != nil {
		return ProviderView{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return ProviderView{}, errors.New("网络不可用，请稍后再试")
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		var failure whaleStackError
		_ = json.NewDecoder(response.Body).Decode(&failure)
		if failure.Error.Message != "" {
			return ProviderView{}, errors.New(failure.Error.Message)
		}
		return ProviderView{}, fmt.Errorf("登录失败（%d），请稍后再试", response.StatusCode)
	}

	var session whaleStackLogin
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		return ProviderView{}, errors.New("登录响应不完整，请稍后再试")
	}
	return a.completeWhaleStackSignIn(client, session)
}

// completeWhaleStackSignIn turns a verified account into the relay provider.
// Both sign-in routes end here so they cannot drift apart.
func (a *App) completeWhaleStackSignIn(client *http.Client, session whaleStackLogin) (ProviderView, error) {
	if session.BaseURL == "" || session.APIKey == "" {
		return ProviderView{}, errors.New("登录响应不完整，请稍后再试")
	}

	catalog, err := fetchWhaleCatalog(client, session.BaseURL, session.APIKey)
	if err != nil {
		return ProviderView{}, err
	}
	models := catalog.ids()
	if len(models) == 0 {
		return ProviderView{}, errors.New("账号下暂无可用模型，请联系管理员")
	}

	view := ProviderView{
		Name:      whaleStackProvider,
		Added:     true,
		Kind:      "openai",
		BaseURL:   session.BaseURL,
		Models:    models,
		Default:   models[0],
		APIKeyEnv: whaleStackKeyEnv,
		KeySet:    true,
	}
	if err := a.SaveProvider(view); err != nil {
		return ProviderView{}, err
	}
	if err := a.SetProviderKey(whaleStackKeyEnv, session.APIKey); err != nil {
		return ProviderView{}, err
	}
	rememberWhaleStackAccount(session)
	publishWhaleCatalog(session.BaseURL, session.APIKey, catalog)
	return view, nil
}

type whaleModel struct {
	ID         string `json:"id"`
	Display    string `json:"display_name"`
	Context    int    `json:"context_length"`
	Vision     *bool  `json:"vision"`
	Generation int64  `json:"generation"`
}
type whaleCatalog []whaleModel

func (c whaleCatalog) ids() []string {
	ids := make([]string, 0, len(c))
	for _, m := range c {
		ids = append(ids, m.ID)
	}
	return ids
}

var whaleLive struct {
	sync.RWMutex
	owner [32]byte
	rows  whaleCatalog
}

func whaleOwner(url, key string) [32]byte { return sha256.Sum256([]byte(url + "\x00" + key)) }
func publishWhaleCatalog(url, key string, c whaleCatalog) {
	whaleLive.Lock()
	defer whaleLive.Unlock()
	whaleLive.owner = whaleOwner(url, key)
	whaleLive.rows = c
}
func loadedWhaleCatalog(url, key string) whaleCatalog {
	whaleLive.RLock()
	defer whaleLive.RUnlock()
	if whaleLive.owner == whaleOwner(url, key) {
		return whaleLive.rows
	}
	return nil
}

// whaleStackModels reads the model list the account may use. An account with no
// model access is reported before anything is written, so the provider never
// appears half-configured.
func whaleStackModels(client *http.Client, baseURL, apiKey string) ([]string, error) {
	c, err := fetchWhaleCatalog(client, baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	if len(c) == 0 {
		return nil, errors.New("账号下暂无可用模型，请联系管理员")
	}
	return c.ids(), nil
}
func fetchWhaleCatalog(client *http.Client, baseURL, apiKey string) (whaleCatalog, error) {
	request, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("无法读取可用模型，请检查网络后重试")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("账号暂未开通模型访问权限，请联系管理员")
	}
	var payload struct {
		Data whaleCatalog `json:"data"`
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20+1))
	if err != nil || len(raw) > 2<<20 || json.Unmarshal(raw, &payload) != nil || payload.Data == nil {
		return nil, errors.New("模型列表格式异常，请稍后再试")
	}
	seen := make(map[string]bool, len(payload.Data))
	for _, item := range payload.Data {
		if item.ID == "" || item.ID != strings.TrimSpace(item.ID) || seen[item.ID] {
			return nil, errors.New("模型标识异常，请稍后再试")
		}
		seen[item.ID] = true
	}
	return payload.Data, nil
}

// WhaleStackRefreshModels re-reads the relay's model list for the signed-in
// account. The list is scoped to the key's group, so a swapped local deployment
// (or a group change on the account) reaches every picker without signing in
// again. Nothing is written when the answer matches what the config already
// holds, so an idle launch does not churn the config file.
func (a *App) WhaleStackRefreshModels() (ProviderView, error) {
	cfg, err := config.Load()
	if err != nil {
		return ProviderView{}, err
	}
	entry, ok := cfg.Provider(whaleStackProvider)
	if !ok || entry.APIKey() == "" {
		return ProviderView{}, errors.New("尚未登录鲸栈账号")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	url, key := entry.BaseURL, entry.APIKey()
	catalog, err := fetchWhaleCatalog(client, url, key)
	if err != nil {
		return ProviderView{}, err
	}
	current, err := config.Load()
	if err != nil {
		return ProviderView{}, err
	}
	owner, ok := current.Provider(whaleStackProvider)
	if !ok || owner.BaseURL != url || owner.APIKey() != key {
		return ProviderView{}, errors.New("登录账户已变化，请重新刷新")
	}
	models := catalog.ids()
	if len(models) > 0 && !sameModelSet(owner.ChatModelList(), models) {
		if err := a.UpdateProviderModels(whaleStackProvider, models, entry.DefaultModel()); err != nil {
			return ProviderView{}, err
		}
	}
	publishWhaleCatalog(url, key, catalog)
	a.emitWhaleStackModels(models)
	reloaded, err := config.Load()
	if err != nil {
		return ProviderView{}, err
	}
	updated, ok := reloaded.Provider(whaleStackProvider)
	if !ok {
		return ProviderView{}, errors.New("鲸栈供应商不存在")
	}
	return providerViewFromEntry(*updated, false, true), nil
}

// sameModelSet compares two model lists as sets: the relay is free to reorder
// its answer, and a reorder is not a change worth rewriting the config for.
func sameModelSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, m := range a {
		seen[m]++
	}
	for _, m := range b {
		seen[m]--
		if seen[m] < 0 {
			return false
		}
	}
	return true
}

func (a *App) emitWhaleStackModels(models []string) {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, "orca:whalestack-models", models)
}

// whaleStackRefreshOnStartup keeps the signed-in account's model list current
// without delaying the webview: the launch path must not wait on the network.
func (a *App) whaleStackRefreshOnStartup() {
	_, _ = a.WhaleStackRefreshModels()
}

// WhaleStackDeviceView is the browser hand-off: the app shows the code while
// the default browser asks the account to approve it.
type WhaleStackDeviceView struct {
	Device    string `json:"device"`
	Code      string `json:"code"`
	VerifyURL string `json:"verifyUrl"`
	ExpiresIn int    `json:"expiresIn"`
}

// WhaleStackStartSignIn asks the login service for a one-time code and opens the
// default browser on its approval page. The password never touches this process.
func (a *App) WhaleStackStartSignIn() (WhaleStackDeviceView, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	request, err := http.NewRequest(http.MethodPost, whaleStackOrigin+"/api/app/device", nil)
	if err != nil {
		return WhaleStackDeviceView{}, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return WhaleStackDeviceView{}, errors.New("网络不可用，请稍后再试")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return WhaleStackDeviceView{}, errors.New("登录服务暂不可用，请稍后再试")
	}
	var device WhaleStackDeviceView
	if err := json.NewDecoder(response.Body).Decode(&device); err != nil || device.Device == "" || device.VerifyURL == "" {
		return WhaleStackDeviceView{}, errors.New("登录响应不完整，请稍后再试")
	}
	if a.ctx != nil {
		wruntime.BrowserOpenURL(a.ctx, device.VerifyURL)
	}
	return device, nil
}

// WhaleStackFinishSignIn waits for the browser approval, then stores the relay
// credential and adds the provider exactly like a password sign-in would.
func (a *App) WhaleStackFinishSignIn(device string) (ProviderView, error) {
	device = strings.TrimSpace(device)
	if device == "" {
		return ProviderView{}, errors.New("登录已失效，请重新发起")
	}
	client := &http.Client{Timeout: 25 * time.Second}
	deadline := time.Now().Add(10*time.Minute + 30*time.Second)
	for time.Now().Before(deadline) {
		body, _ := json.Marshal(map[string]string{"device": device})
		request, err := http.NewRequest(http.MethodPost, whaleStackOrigin+"/api/app/device/poll", bytes.NewReader(body))
		if err != nil {
			return ProviderView{}, err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
		response, err := client.Do(request)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		var payload struct {
			Status  string `json:"status"`
			Email   string `json:"email"`
			Name    string `json:"name"`
			BaseURL string `json:"baseUrl"`
			APIKey  string `json:"apiKey"`
			KeyName string `json:"keyName"`
			Error   struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&payload)
		status := response.StatusCode
		response.Body.Close()
		if decodeErr != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		if status != http.StatusOK {
			if payload.Error.Message != "" {
				return ProviderView{}, errors.New(payload.Error.Message)
			}
			return ProviderView{}, fmt.Errorf("登录失败（%d），请稍后再试", status)
		}
		if payload.Status != "approved" {
			time.Sleep(2 * time.Second)
			continue
		}
		session := whaleStackLogin{Email: payload.Email, Name: payload.Name, BaseURL: payload.BaseURL, APIKey: payload.APIKey, KeyName: payload.KeyName}
		return a.completeWhaleStackSignIn(client, session)
	}
	return ProviderView{}, errors.New("登录等待超时，请重新发起")
}

// WhaleStackStatus is what the header capsule shows. The account name is kept
// beside the config (never the credential itself) so the capsule can name the
// signed-in account after a restart.
type WhaleStackStatus struct {
	SignedIn bool     `json:"signedIn"`
	Email    string   `json:"email,omitempty"`
	Name     string   `json:"name,omitempty"`
	Models   []string `json:"models,omitempty"`
}

type whaleStackAccountRecord struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	SavedAt int64  `json:"savedAt"`
}

func whaleStackAccountPath() string {
	return filepath.Join(filepath.Dir(config.UserConfigPath()), "whalestack-account.json")
}

func (a *App) WhaleStackStatus() WhaleStackStatus {
	cfg, err := config.Load()
	if err != nil {
		return WhaleStackStatus{}
	}
	entry, ok := cfg.Provider(whaleStackProvider)
	if !ok || !entry.Configured() || entry.APIKey() == "" {
		return WhaleStackStatus{}
	}
	status := WhaleStackStatus{SignedIn: true, Models: entry.ChatModelList()}
	if c := loadedWhaleCatalog(entry.BaseURL, entry.APIKey()); c != nil {
		status.Models = make([]string, 0, len(c))
		for _, m := range c {
			label := m.Display
			if label == "" {
				label = m.ID
			}
			status.Models = append(status.Models, label)
		}
	}
	if raw, err := os.ReadFile(whaleStackAccountPath()); err == nil {
		var record whaleStackAccountRecord
		if json.Unmarshal(raw, &record) == nil {
			status.Email = record.Email
			status.Name = record.Name
		}
	}
	return status
}

func rememberWhaleStackAccount(session whaleStackLogin) {
	record := whaleStackAccountRecord{Email: session.Email, Name: session.Name, SavedAt: time.Now().UnixMilli()}
	raw, err := json.Marshal(record)
	if err != nil {
		return
	}
	path := whaleStackAccountPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o600)
}
