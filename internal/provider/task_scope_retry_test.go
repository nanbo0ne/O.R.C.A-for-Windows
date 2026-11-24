package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type taskScopeRetryKey struct{}
type taskScopeRetryTransport func(*http.Request) (*http.Response, error)

func (f taskScopeRetryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// SendWithRetry is the production retry layer; it does not wrap Provider.
// A custom transport keeps this regression entirely offline.
func TestTaskScopeSendWithRetryPreservesContext(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "retry-success", true: "revoked-during-backoff"}[revoke], func(t *testing.T) {
			parent, cancelParent := context.WithCancel(context.Background())
			defer cancelParent()
			scope, cancelScope := context.WithCancel(context.WithValue(parent, taskScopeRetryKey{}, "synthetic-task"))
			defer cancelScope()
			var attempts, notices int
			ctx := WithRetryNotify(scope, func(RetryInfo) {
				notices++
				if revoke {
					cancelScope()
				}
			})
			client := &http.Client{Transport: taskScopeRetryTransport(func(req *http.Request) (*http.Response, error) {
				attempts++
				if req.Context().Value(taskScopeRetryKey{}) != "synthetic-task" || req.Context().Done() != scope.Done() {
					t.Error("retry transport lost task values or cancellation")
				}
				status := http.StatusOK
				if attempts == 1 {
					status = http.StatusServiceUnavailable
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("synthetic"))}, nil
			})}
			resp, err := SendWithRetry(ctx, client, "synthetic", "", func(requestCtx context.Context) (*http.Request, error) {
				return http.NewRequestWithContext(requestCtx, http.MethodPost, "https://synthetic.invalid/completions", nil)
			})
			if resp != nil {
				resp.Body.Close()
			}
			if revoke {
				if !errors.Is(err, context.Canceled) || attempts != 1 {
					t.Errorf("revoked scope retried: error=%v attempts=%d", err, attempts)
				}
			} else if err != nil || attempts != 2 || scope.Err() != nil {
				t.Errorf("ordinary retry changed scope lifetime: error=%v attempts=%d scope=%v", err, attempts, scope.Err())
			}
			if notices != 1 || parent.Err() != nil {
				t.Errorf("retry affected parent: notices=%d parent=%v", notices, parent.Err())
			}
		})
	}
}
