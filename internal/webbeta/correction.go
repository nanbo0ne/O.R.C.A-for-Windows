package webbeta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// A format correction is a new, budgeted request in the same confirmed chat.
// It never resubmits the original task or changes the user's history checkpoint.
func (e *Engine) correctToolFormat(ctx context.Context, r Request) (string, error) {
	ctx, cancel, err := e.taskContext(ctx, r.TaskID)
	if err != nil {
		return "", err
	}
	defer cancel()
	var text strings.Builder
	err = e.queue.Do(ctx, false, func(call context.Context) error {
		e.mu.Lock()
		var c *Conversation
		for i := range e.state.Conversations {
			v := &e.state.Conversations[i]
			if v.Owner == r.Owner && v.Phase == "bound" && v.Epoch == r.Epoch {
				c = v
				break
			}
		}
		hashes := fingerprints(r.Messages)
		if c == nil || c.Inflight != "" || len(c.Prefix) != len(hashes) || !prefixMatch(c.Prefix, hashes) {
			e.mu.Unlock()
			return errors.New("tool correction has no matching confirmed conversation")
		}
		id, requestID := c.ID, newID()
		if err := e.reserveTaskLocked(r.TaskID, 0, 0, 1); err != nil {
			e.mu.Unlock()
			return err
		}
		c.Inflight = requestID
		marker := toolMarker(c.Grammar)
		s := e.state.Settings
		cmd := Command{ID: requestID, URL: c.URL, Action: "send", Thinking: s.Thinking && !r.DisableThinking, Search: s.Search,
			Text: fmt.Sprintf("上一条工具请求的格式未通过校验，没有执行任何工具。只修正格式，不重复任务、不声称工具已完成。必须输出字面前缀 %s、一个空格、再输出原来的 JSON 对象。不要 Markdown 或额外文字。格式示例：%s {\"id\":\"唯一编号\",\"name\":\"工具名称\",\"arguments\":{}}", marker, marker)}
		if c.Grammar == currentGrammar {
			cmd.Text = "上一条回答的格式未通过校验，没有执行本次工具。只补充一个完整的工具或最终答复记录，不重复已给出的进展，不声称工具已完成。\n" + protocolInstructions()
		}
		if c.Grammar == latestGrammar {
			if _, _, err := v3Markers(r.Token); err != nil {
				e.mu.Unlock()
				return err
			}
			cmd.Text = "上一条回答没有可执行的 V3 记录，因此没有执行该回答中提到的工具。请根据当前任务和已返回的工具回执，只输出一个明确的 V3 tool 或 final 记录；不要重放已执行工具，也不要把普通文字当作执行回执。\n" + protocolInstructionsV3(r.Token)
			if r.CorrectionHint == "native_write" {
				cmd.Text = "上一条写入请求用了非 V3 格式，因此尚未执行。只把那一次写入改写成完整 V3 tool 记录，不要重新读取或声称已经写入。短文本放在 arguments.content 的 JSON 字符串中；若使用原文块，raw_argument 必须是参数名 content，不得填入文件正文，并且原文块必须有完整结束标记。\n" + protocolInstructionsV3(r.Token)
			}
			if r.CorrectionHint == "raw_field" {
				cmd.Text = "上一条写入请求没有执行：raw_argument 的值必须精确为 content，不能是文件正文。这次只用一种格式纠正：仅输出一行 ORCA_TURN_V3 tool JSON 记录，将先前的文件正文作为正确转义的 arguments.content 字符串，并保留原路径。不要使用 raw_argument、原文块、Markdown、说明文字或第二条记录。不要重新读取文件，也不要声称已经写入。"
			}
			if len(r.Tools) > 0 && r.CorrectionHint != "raw_field" {
				toolsJSON, _ := json.Marshal(r.Tools)
				cmd.Text += "\n本轮仍可使用的宿主工具定义：" + string(toolsJSON) + "\n若任务需要这些工具，只输出 tool 记录并等待宿主返回真实回执。"
			}
			if r.CorrectionFinalOnly {
				cmd.Text = "上一条普通文字没有可执行工具调用。只能输出 final 记录，将上一条最终文字完整放入 text 或原文块；不得新增工具调用、事实或进展。\n" + protocolInstructionsV3(r.Token)
			}
		}
		err := saveState(e.path, e.state)
		e.mu.Unlock()
		if err != nil {
			return err
		}
		done := false
		err = e.run(call, cmd, func(ev Event) error {
			if ev.ID != requestID {
				return errors.New("stale correction event")
			}
			if ev.Kind == "text" {
				if text.Len()+len(ev.Text) > 1<<20 {
					return errors.New("tool correction too large")
				}
				text.WriteString(ev.Text)
			}
			if ev.Kind == "done" {
				done = ev.URL == cmd.URL
			}
			return nil
		})
		e.mu.Lock()
		defer e.mu.Unlock()
		c = e.find(id)
		if preflightErr, unsent := e.clearUnsentLocked(c, err); unsent {
			return preflightErr
		}
		if err != nil || !done {
			e.uncertainLocked(c)
			if err != nil {
				return err
			}
			return provider.NoRequestReplay(errors.New("tool correction completion not confirmed"))
		}
		c.Inflight = ""
		c.Reply = completedReply(text.String(), r)
		return e.saveCompletionLocked(c)
	})
	if err != nil {
		e.fail(err)
	}
	return text.String(), err
}
