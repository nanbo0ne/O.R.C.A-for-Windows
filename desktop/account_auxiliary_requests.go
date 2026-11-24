package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

const (
	auxiliaryMemory  provider.RequestPurpose = "memory"
	auxiliaryTitle   provider.RequestPurpose = "title"
	auxiliaryWelcome provider.RequestPurpose = "welcome"
	auxiliaryProbe   provider.RequestPurpose = "credential_probe"
)

func exactAuxiliaryProvider(cfg *config.Config, ref string) (*config.ProviderEntry, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("auxiliary request has no selected model source")
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return nil, fmt.Errorf("selected auxiliary model %q is unavailable; no other source was used", ref)
	}
	if isWebBetaModel(entry.Name+"/") && entry.Kind != "deepseekweb-beta" {
		return nil, errors.New("selected web model has an invalid provider kind; no API source was used")
	}
	return entry, nil
}

func automaticAuxiliaryAllowed(entry *config.ProviderEntry) error {
	if entry.Kind == "deepseekweb-beta" || isWebBetaModel(entry.Name+"/") {
		return errors.New("automatic auxiliary requests are unavailable for web models; no other source was used")
	}
	return nil
}

func bindAuxiliaryRequest(ctx context.Context, purpose provider.RequestPurpose, req provider.Request) provider.Request {
	if req.RequestID == "" {
		req.RequestID = event.NewRequestID()
	}
	if req.ParentTaskID == "" {
		req.ParentTaskID, _ = agent.ParentTurn(ctx)
		if req.ParentTaskID == "" {
			req.ParentTaskID = event.NewRequestID()
		}
	}
	req.Purpose = purpose
	return req
}

func collectAuxiliaryText(ctx context.Context, stream <-chan provider.Chunk) (string, error) {
	var text strings.Builder
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case chunk, ok := <-stream:
			if !ok {
				if err := ctx.Err(); err != nil {
					return "", err
				}
				return "", provider.NoRequestReplay(errors.New("auxiliary stream ended without completion; request was not replayed"))
			}
			switch chunk.Type {
			case provider.ChunkText:
				text.WriteString(chunk.Text)
			case provider.ChunkDone:
				if err := ctx.Err(); err != nil {
					return "", err
				}
				return text.String(), nil
			case provider.ChunkToolCallStart, provider.ChunkToolCall:
				return "", provider.NoRequestReplay(errors.New("auxiliary requests do not allow tool calls; request was not replayed"))
			case provider.ChunkError:
				if chunk.Err != nil {
					return "", chunk.Err
				}
				return "", provider.NoRequestReplay(errors.New("auxiliary stream failed; request was not replayed"))
			}
		}
	}
}
