package main

// whalestack_app.go —— 用鲸栈账号登录桌面端，自动获得中转站的模型访问权限。
//
// 登录由统一登录服务完成（它负责账号校验与凭证签发），这里只做两件事：把
// 返回的凭证写进全局凭据文件，并把「鲸栈」注册成一个模型供应商。凭证按账号
// 派生、可重复登录复用，不需要用户手动复制 Key。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
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
	if session.BaseURL == "" || session.APIKey == "" {
		return ProviderView{}, errors.New("登录响应不完整，请稍后再试")
	}

	models, err := whaleStackModels(client, session.BaseURL, session.APIKey)
	if err != nil {
		return ProviderView{}, err
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
	return view, nil
}

// whaleStackModels reads the model list the account may use. An account with no
// model access is reported before anything is written, so the provider never
// appears half-configured.
func whaleStackModels(client *http.Client, baseURL, apiKey string) ([]string, error) {
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
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, errors.New("模型列表格式异常，请稍后再试")
	}
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("账号下暂无可用模型，请联系管理员")
	}
	return models, nil
}
