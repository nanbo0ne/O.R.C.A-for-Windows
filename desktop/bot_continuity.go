package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/boot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/bot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func (a *App) decideBotContinuity(ctx context.Context, modelRef string, previous bot.SessionChoice, current string) (bool, error) {
	cfg, err := config.LoadForRoot(previous.WorkspaceRoot)
	if err != nil {
		return false, err
	}
	entry, err := exactAuxiliaryProvider(cfg, modelRef)
	if err != nil {
		return false, err
	}
	if err := automaticAuxiliaryAllowed(entry); err != nil {
		return false, err
	}
	prov, err := boot.NewProviderWithProxy(entry, cfg.NetworkProxySpec())
	if err != nil {
		return false, err
	}
	userTail, assistantTail := continuitySessionTail(previous.Path)
	prompt := fmt.Sprintf("Previous segment title: %s\nRecent user message: %s\nPrevious final answer: %s\nCurrent message: %s\n\nReply only RELATED or NEW.", previous.Title, userTail, assistantTail, strings.TrimSpace(current))
	decisionCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	stream, err := prov.Stream(decisionCtx, bindAuxiliaryRequest(decisionCtx, provider.RequestPurposeClassifier, provider.Request{Temperature: 0, MaxTokens: 12, Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "Decide whether a new mobile assistant message continues the prior conversation segment. RELATED only when carrying over prior context is clearly useful; otherwise NEW."},
		{Role: provider.RoleUser, Content: prompt},
	}}))
	if err != nil {
		return false, err
	}
	out, err := collectAuxiliaryText(decisionCtx, stream)
	if err != nil {
		return false, err
	}
	answer := strings.ToUpper(strings.TrimSpace(out))
	if strings.HasPrefix(answer, "RELATED") {
		return true, nil
	}
	if strings.HasPrefix(answer, "NEW") {
		return false, nil
	}
	return false, fmt.Errorf("invalid continuity decision %q", answer)
}

func continuitySessionTail(path string) (string, string) {
	session, err := agent.LoadSession(path)
	if err != nil {
		return "", ""
	}
	var user, assistant string
	for _, message := range session.Snapshot() {
		content := strings.TrimSpace(message.Content)
		switch message.Role {
		case provider.RoleUser:
			content = control.StripComposePrefixes(content)
			if content != "" && !control.IsSyntheticUserMessage(content) {
				user = brokerOneLine(content, 600)
			}
		case provider.RoleAssistant:
			if content != "" {
				assistant = brokerOneLine(content, 800)
			}
		}
	}
	return user, assistant
}
