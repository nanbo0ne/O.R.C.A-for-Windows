package control

import (
	"context"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// Auxiliary providers can differ from the main model. Keep their lifetime and
// parent attribution without appending classification to the main web binding.
func beginClassifierTask(ctx context.Context, p provider.Provider, parent, requestID string) (context.Context, func(), string, error) {
	parent = strings.TrimSpace(parent)
	scoped, ok := p.(provider.TaskProvider)
	if !ok {
		return ctx, func() {}, parent, nil
	}
	if parent == "" {
		parent, _ = agent.ParentTurn(ctx)
		if parent == "" {
			parent = requestID
		}
	}
	ctx, cancel := context.WithCancel(provider.WithCancellationDiagnostic(ctx))
	taskCtx, release, err := scoped.BeginTask(ctx, "classifier:"+requestID, parent)
	if err != nil {
		cancel()
		return nil, nil, parent, err
	}
	return taskCtx, func() { cancel(); release() }, parent, nil
}
