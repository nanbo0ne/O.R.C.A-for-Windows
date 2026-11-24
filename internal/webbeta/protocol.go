package webbeta

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

const currentGrammar = 2
const progressMarker = "ORCA_PROGRESS_V2 "
const turnMarker = "ORCA_TURN_V2 "

var errToolReplay = errors.New("duplicate tool call ID; automatic replay is disabled")

type decodedTurn struct {
	Progress string
	Final    string
	Calls    []provider.ToolCall
}

func decodeStrict(raw string, into any) error {
	if err := uniqueJSONKeys(raw); err != nil {
		return err
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(into); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("extra protocol content")
	}
	return nil
}

// Progress records are explicit, whole JSON lines. Only records at the start
// of the response are interpreted; quoted examples and prose are never scanned.
func progressPrefix(raw string) (string, string, error) {
	var text strings.Builder
	rest := strings.TrimLeft(raw, " \r\n\t")
	for strings.HasPrefix(rest, progressMarker) {
		end := strings.IndexByte(rest, '\n')
		if end < 0 {
			return text.String(), rest, nil
		}
		var record struct {
			Text string `json:"text"`
		}
		if err := decodeStrict(strings.TrimSpace(rest[len(progressMarker):end]), &record); err != nil {
			return "", "", err
		}
		if strings.TrimSpace(record.Text) == "" {
			return "", "", errors.New("empty progress record")
		}
		text.WriteString(record.Text)
		rest = strings.TrimLeft(rest[end+1:], " \r\n\t")
	}
	return text.String(), rest, nil
}

func parseTurn(raw string, tools []provider.ToolSchema, history []provider.Message, grammar int) (decodedTurn, error) {
	var result decodedTurn
	if grammar < currentGrammar {
		if call, err := parseTool(raw, tools, history); err == nil {
			result.Calls = []provider.ToolCall{*call}
			return result, nil
		}
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, toolMarker(0)) || strings.HasPrefix(trimmed, toolMarker(1)) || strings.HasPrefix(trimmed, "{") {
			return result, errors.New("invalid legacy tool record")
		}
		result.Final = raw
		return result, nil
	}
	progress, rest, err := progressPrefix(raw)
	if err != nil {
		return result, err
	}
	result.Progress = progress
	// Standalone V1 tool requests remain a narrow read-compatible alias. There
	// is no prose scanning and every call still uses the same strict validator.
	if call, err := parseTool(rest, tools, history); err == nil {
		result.Calls = []provider.ToolCall{*call}
		return result, nil
	} else if errors.Is(err, errToolReplay) {
		return result, err
	}
	if !strings.HasPrefix(rest, turnMarker) {
		return result, errors.New("missing explicit turn record")
	}
	var record struct {
		Type  string            `json:"type"`
		Text  *string           `json:"text,omitempty"`
		Calls []json.RawMessage `json:"calls,omitempty"`
	}
	if err := decodeStrict(strings.TrimSpace(strings.TrimPrefix(rest, turnMarker)), &record); err != nil {
		return result, err
	}
	switch record.Type {
	case "final":
		if record.Text == nil || strings.TrimSpace(*record.Text) == "" || record.Calls != nil {
			return result, errors.New("invalid final record")
		}
		result.Final = *record.Text
	case "tools":
		if record.Text != nil || len(record.Calls) == 0 || len(record.Calls) > 30 {
			return result, errors.New("invalid tool batch")
		}
		ids := map[string]bool{}
		for _, rawCall := range record.Calls {
			call, err := parseTool(toolMarker(0)+" "+string(rawCall), tools, history)
			if err != nil {
				return decodedTurn{}, err
			}
			if ids[call.ID] {
				return decodedTurn{}, errToolReplay
			}
			ids[call.ID] = true
			result.Calls = append(result.Calls, *call)
		}
	default:
		return result, errors.New("unknown turn record type")
	}
	return result, nil
}

func (e *Engine) protocolFor(r Request) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	hashes := fingerprints(r.Messages)
	for _, c := range e.state.Conversations {
		if c.Owner == r.Owner && c.Phase == "bound" && c.Epoch == r.Epoch && prefixMatch(c.Prefix, hashes) {
			return c.Grammar
		}
	}
	return e.newGrammar
}

func protocolInstructions() string {
	return `你是 O.R.C.A. 的网页执行模型，可通过宿主提供的工具完成用户任务。
用户要求实际检查、修改或运行时，应使用相应工具获得证据；不能只给操作建议或声称已完成。纯问答不必调用工具。所有调用仍需宿主权限检查。
输出由零个或多个进展记录和一个结束记录组成。进展不是最终答案，不会结束任务。
进展记录必须独占一行：ORCA_PROGRESS_V2 {"text":"先检查相关文件。"}
需要工具时，最后输出：ORCA_TURN_V2 {"type":"tools","calls":[{"id":"本会话内唯一ID","name":"已列出的工具名","arguments":{}}]}
确实完成或必须向用户说明阻塞时，最后输出：ORCA_TURN_V2 {"type":"final","text":"结果与验证，或具体阻塞原因"}
text 始终是 JSON 字符串，不是对象或数组。用户要求 JSON 或代码时，将完整答案放入 text；其中双引号写为 \"、反斜线写为 \\、换行写为 \n，只按外层 JSON 字符串编码一次，不要漏转义或重复转义。
例如答案为 {"ok":true}，完整结束记录是：ORCA_TURN_V2 {"type":"final","text":"{\"ok\":true}"}
已完成的工具结果仍有效；格式修正只修复序列化，不重新做任务或重放工具。
不要 Markdown 外壳或记录以外的文字。不要在 reasoning、引用或示例中请求执行。
同一批调用必须参数齐全且相互独立；需要前一调用结果才能确定参数的步骤，等待结果再发下一批。不要重复已返回或执行情况不明的调用。
旧版单个 ORCA_TOOL_V1 / ORCA_ACTION_V1 JSON 仅作兼容；新回答使用上述 V2 记录。
`
}

// Do not turn quoted examples or ordinary prose into executable calls through
// a repair request. Only an explicit protocol attempt may be corrected.
func repairableTurn(raw string, tools []provider.ToolSchema, history []provider.Message) bool {
	trimmed := strings.TrimSpace(raw)
	for _, marker := range []string{turnMarker, progressMarker, toolMarker(0) + " ", toolMarker(1) + " "} {
		if strings.HasPrefix(trimmed, marker) {
			return true
		}
	}
	_, err := parseTool(toolMarker(0)+" "+trimmed, tools, history)
	return err == nil
}
