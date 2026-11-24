package webbeta

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

const taskLifetime = 30 * time.Minute

const (
	taskActionLimit     = 30
	taskCorrectionLimit = 3
	// Each response is repaired at most once; separate replies share this task cap.
	taskGenerationLimit = taskActionLimit + taskCorrectionLimit + 2
)

var ErrTaskBudget = errors.New("网页任务达到运行限额（35 次生成、30 次工具动作、3 次格式纠正或 30 分钟实际运行时间），已停止；已完成结果保留，不会重放工具")
var ErrTaskIdentityChanged = errors.New("网页父任务属于之前的登录身份，请发起新的用户回合；不会接续旧任务")

type taskBudgetError struct {
	reason string
	used   int64
	limit  int64
}

func (e *taskBudgetError) Unwrap() error { return ErrTaskBudget }

func (e *taskBudgetError) Error() string {
	var detail string
	switch e.reason {
	case "generations":
		detail = fmt.Sprintf("模型生成次数不足（已用 %d 次，上限 %d 次；含开场和格式纠正）", e.used, e.limit)
	case "actions":
		detail = fmt.Sprintf("工具动作次数不足（已用 %d 次，上限 %d 次）", e.used, e.limit)
	case "corrections":
		detail = fmt.Sprintf("格式纠正次数不足（已用 %d 次，上限 %d 次）", e.used, e.limit)
	case "active_time":
		detail = fmt.Sprintf("实际运行时间达到上限（已用 %d 秒，上限 %d 秒；不含审批和回答等待）", e.used, e.limit)
	default:
		detail = "任务计数状态异常"
	}
	return "网页任务已停止：" + detail + "。已完成的工具结果保留，不会自动重发"
}

type TaskBudget struct {
	GenerationHash  string `json:"generationHash"`
	StartedAt       int64  `json:"startedAt"`
	HumanWaitMillis int64  `json:"humanWaitMillis,omitempty"`
	Generations     int    `json:"generations"`
	Actions         int    `json:"actions"`
	Corrections     int    `json:"corrections"`
}
type taskContextKey struct{}

type notSentError struct{ error }

func (e *notSentError) Unwrap() error { return e.error }

// KnownUnsent is for a trusted browser-host rejection that occurs before any
// page dispatch. It must never wrap a timeout or an error after navigation or
// sending, where the website outcome may be unknown.
func KnownUnsent(err error) error {
	if err == nil {
		return nil
	}
	return &notSentError{err}
}

// A local budget/readiness rejection is known not to have sent anything. Do
// not label it an ambiguous upstream delivery or force account recovery.
func (e *Engine) clearUnsentLocked(c *Conversation, err error) (error, bool) {
	var unsent *notSentError
	if !errors.As(err, &unsent) {
		return nil, false
	}
	c.Inflight = ""
	if c.Phase == "preparing" {
		c.Phase = "failed"
	}
	if saveErr := e.saveCompletionLocked(c); saveErr != nil {
		e.lastError = saveErr.Error()
		return saveErr, true
	}
	return err, true
}

func taskFromContext(ctx context.Context) string {
	id, _ := ctx.Value(taskContextKey{}).(string)
	return id
}

func (e *Engine) taskContext(ctx context.Context, id string) (context.Context, context.CancelFunc, error) {
	if bound := taskFromContext(ctx); bound != "" && bound != id {
		return nil, nil, errors.New("web request parent task does not match its active task scope")
	}
	if id == "" {
		return ctx, func() {}, nil
	} // Non-task reserve preparation only.
	if clock, _ := ctx.Value(taskClockKey{}).(*taskClock); clock != nil && clock.engine == e && clock.id == id {
		return ctx, func() {}, ctx.Err()
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, nil, ErrIdentityClosed
	}
	if e.state.Tasks == nil {
		e.state.Tasks = make(map[string]TaskBudget)
	}
	b, exists := e.state.Tasks[id]
	if !exists {
		b.GenerationHash = contentHash(e.state.Generation)
		b.StartedAt = time.Now().UnixMilli()
		e.state.Tasks[id] = b
		if err := saveState(e.path, e.state); err != nil {
			delete(e.state.Tasks, id)
			e.mu.Unlock()
			return nil, nil, err
		}
	}
	err := e.reserveTaskLocked(id, 0, 0, 0)
	e.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	remaining := taskLifetime - time.Since(time.UnixMilli(b.StartedAt)) + time.Duration(b.HumanWaitMillis)*time.Millisecond
	ctx, clock := newTaskClock(context.WithValue(ctx, taskContextKey{}, id), remaining, func(wait time.Duration) error {
		e.mu.Lock()
		defer e.mu.Unlock()
		current := e.state.Tasks[id]
		if e.closed || current.GenerationHash != contentHash(e.state.Generation) {
			return ErrTaskIdentityChanged
		}
		before := current
		current.HumanWaitMillis += wait.Milliseconds()
		e.state.Tasks[id] = current
		if err := saveState(e.path, e.state); err != nil {
			e.state.Tasks[id] = before
			return err
		}
		return nil
	})
	clock.engine, clock.id = e, id
	ctx = context.WithValue(ctx, taskClockKey{}, clock)
	return provider.WithTaskHumanWait(ctx, clock.pause), clock.close, nil
}

// Caller persists the reservation before initiating the corresponding effect.
func (e *Engine) reserveTaskLocked(id string, generations, actions, corrections int) error {
	if id == "" {
		return nil
	}
	b, ok := e.state.Tasks[id]
	if !ok {
		return errors.New("missing parent task budget")
	}
	if b.GenerationHash != contentHash(e.state.Generation) {
		return ErrTaskIdentityChanged
	}
	now := time.Now().UnixMilli()
	if b.StartedAt <= 0 || b.StartedAt > now || b.HumanWaitMillis < 0 || b.HumanWaitMillis > now-b.StartedAt ||
		b.Generations < 0 || b.Actions < 0 || b.Corrections < 0 || generations < 0 || actions < 0 || corrections < 0 {
		return &taskBudgetError{reason: "invalid_state"}
	}
	activeMillis := now - b.StartedAt - b.HumanWaitMillis
	if activeMillis >= taskLifetime.Milliseconds() {
		return &taskBudgetError{reason: "active_time", used: activeMillis / 1000, limit: int64(taskLifetime / time.Second)}
	}
	// Compare remaining capacity before addition, so corrupt counters cannot wrap.
	for _, quota := range []struct {
		reason            string
		used, next, limit int
	}{
		{"generations", b.Generations, generations, taskGenerationLimit},
		{"actions", b.Actions, actions, taskActionLimit},
		{"corrections", b.Corrections, corrections, taskCorrectionLimit},
	} {
		if quota.used > quota.limit || quota.next > quota.limit-quota.used {
			return &taskBudgetError{reason: quota.reason, used: int64(quota.used), limit: int64(quota.limit)}
		}
	}
	b.Generations += generations
	b.Actions += actions
	b.Corrections += corrections
	e.state.Tasks[id] = b
	return nil
}
