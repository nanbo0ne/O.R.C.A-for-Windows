package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/permission"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type classifierScopeKey struct{}
type classifierScopeProvider struct {
	owner, parent             string
	begins, releases, streams int
	admissionErr              error
	hang                      bool
	started                   chan struct{}
	text                      string
}

func (*classifierScopeProvider) Name() string { return "synthetic-scoped-classifier" }
func (p *classifierScopeProvider) BeginTask(ctx context.Context, owner, parent string) (context.Context, func(), error) {
	p.begins++
	p.owner, p.parent = owner, parent
	if p.admissionErr != nil {
		return nil, nil, p.admissionErr
	}
	return context.WithValue(ctx, classifierScopeKey{}, p), func() { p.releases++ }, nil
}
func (p *classifierScopeProvider) Stream(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	p.streams++
	if ctx.Value(classifierScopeKey{}) != p || p.owner == "" || p.parent == "" || r.ParentTaskID != p.parent {
		return nil, errors.New("classifier escaped task scope")
	}
	ch := make(chan provider.Chunk, 2)
	if p.hang {
		close(p.started)
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: p.text}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestClassifiersOwnScopedProviderLifetime(t *testing.T) {
	for _, kind := range []string{"risk", "plan"} {
		for _, mode := range []string{"success", "cancel", "rejected"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				p := &classifierScopeProvider{started: make(chan struct{}), text: `{"needs_plan":true,"level":"low","reason":"synthetic"}`}
				if kind == "risk" {
					p.text = `{"level":"low","reason":"synthetic"}`
				}
				if mode == "cancel" {
					p.hang = true
				}
				if mode == "rejected" {
					p.admissionErr = errors.New("synthetic admission denied")
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan error, 1)
				go func() {
					if kind == "risk" {
						_, err := NewProviderRiskClassifier(p).AssessWithParentTurn(ctx, permission.RiskInput{Tool: "synthetic"}, "parent-turn")
						done <- err
					} else {
						_, _, err := NewProviderAutoPlanClassifier(p).NeedsPlanWithParentTurn(ctx, "synthetic", 1, "parent-turn")
						done <- err
					}
				}()
				if mode == "cancel" {
					select {
					case <-p.started:
						cancel()
					case err := <-done:
						t.Fatalf("stream never admitted: %v", err)
					case <-time.After(time.Second):
						t.Fatal("stream did not start")
					}
				}
				select {
				case err := <-done:
					if mode == "success" && err != nil {
						t.Fatal(err)
					}
					if mode == "cancel" && !errors.Is(err, context.Canceled) {
						t.Fatalf("cancel: %v", err)
					}
					if mode == "rejected" {
						if !errors.Is(err, p.admissionErr) || p.streams != 0 || p.releases != 0 {
							t.Fatalf("rejected admission leaked: %v", err)
						}
					} else if p.streams != 1 || p.releases != 1 {
						t.Fatal("scope not released exactly once")
					}
					if p.begins != 1 || p.parent != "parent-turn" {
						t.Fatal("parent scope mismatch")
					}
				case <-time.After(time.Second):
					t.Fatal("classifier ignored cancellation")
				}
			})
		}
	}
}
