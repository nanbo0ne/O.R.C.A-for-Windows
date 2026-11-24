package webbeta

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestBudgetRejectionReportsExactReasonWithoutChangingLedger(t *testing.T) {
	for _, tc := range []struct {
		name, reason, label               string
		mutate                            func(*TaskBudget)
		generations, actions, corrections int
	}{
		{"generations", "generations", "已用 35 次，上限 35 次", func(b *TaskBudget) { b.Generations = taskGenerationLimit }, 1, 0, 0},
		{"actions", "actions", "已用 30 次，上限 30 次", func(b *TaskBudget) { b.Actions = 30 }, 0, 1, 0},
		{"corrections", "corrections", "已用 3 次，上限 3 次", func(b *TaskBudget) { b.Corrections = taskCorrectionLimit }, 0, 0, 1},
		{"time", "active_time", "不含审批和回答等待", func(b *TaskBudget) { b.StartedAt = time.Now().Add(-taskLifetime - time.Second).UnixMilli() }, 0, 0, 0},
		{"negative-counter", "invalid_state", "状态异常", func(b *TaskBudget) { b.Generations = -1 }, 1, 0, 0},
		{"negative-request", "invalid_state", "状态异常", func(*TaskBudget) {}, -1, 0, 0},
		{"future-start", "invalid_state", "状态异常", func(b *TaskBudget) { b.StartedAt = time.Now().Add(time.Minute).UnixMilli() }, 0, 0, 0},
		{"invalid-wait", "invalid_state", "状态异常", func(b *TaskBudget) { b.HumanWaitMillis = int64(time.Hour / time.Millisecond) }, 0, 0, 0},
		{"counter-overflow", "generations", "模型生成次数", func(b *TaskBudget) { b.Generations = math.MaxInt }, 1, 0, 0},
		{"request-overflow", "actions", "工具动作次数", func(b *TaskBudget) { b.Actions = 1 }, 0, math.MaxInt, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := testEngine(t, &fakeBrowser{})
			budget := TaskBudget{GenerationHash: contentHash(e.state.Generation), StartedAt: time.Now().Add(-time.Second).UnixMilli()}
			tc.mutate(&budget)
			e.mu.Lock()
			defer e.mu.Unlock()
			e.state.Tasks = map[string]TaskBudget{"synthetic-parent": budget}
			err := e.reserveTaskLocked("synthetic-parent", tc.generations, tc.actions, tc.corrections)
			var detail *taskBudgetError
			if !errors.Is(err, ErrTaskBudget) || !errors.As(err, &detail) || detail.reason != tc.reason || !strings.Contains(err.Error(), tc.label) {
				t.Fatalf("unexpected budget diagnostic: %v", err)
			}
			if e.state.Tasks["synthetic-parent"] != budget {
				t.Fatal("rejection changed counters")
			}
			if strings.Contains(err.Error(), "synthetic-parent") || strings.Contains(err.Error(), e.state.Generation) {
				t.Fatal("diagnostic exposed task/identity metadata")
			}
		})
	}
}

func TestBudgetLongApprovalUsesGenerationReasonNotWallTime(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state.Tasks = map[string]TaskBudget{"parent": {
		GenerationHash:  contentHash(e.state.Generation),
		StartedAt:       time.Now().Add(-52 * time.Minute).UnixMilli(),
		HumanWaitMillis: int64(48 * time.Minute / time.Millisecond),
		Generations:     taskGenerationLimit, Actions: 8, Corrections: 1,
	}}
	err := e.reserveTaskLocked("parent", 1, 0, 0)
	var detail *taskBudgetError
	if !errors.As(err, &detail) || detail.reason != "generations" {
		t.Fatalf("approval wait was counted as active execution: %v", err)
	}
}

func TestSlowWebTaskCanContinuePastFormerLimits(t *testing.T) {
	e := testEngine(t, &fakeBrowser{})
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state.Tasks = map[string]TaskBudget{"parent": {
		GenerationHash: contentHash(e.state.Generation),
		StartedAt:      time.Now().Add(-12 * time.Minute).UnixMilli(),
		Generations:    10, Actions: 9,
	}}
	if err := e.reserveTaskLocked("parent", 1, 1, 0); err != nil {
		t.Fatalf("slow valid task stopped at former limits: %v", err)
	}
	got := e.state.Tasks["parent"]
	if got.Generations != 11 || got.Actions != 10 {
		t.Fatalf("wrong reservation: %+v", got)
	}
	got.Generations = taskGenerationLimit
	e.state.Tasks["parent"] = got
	if err := e.reserveTaskLocked("parent", 1, 0, 0); !errors.Is(err, ErrTaskBudget) {
		t.Fatalf("new generation boundary was not enforced: %v", err)
	}
}
