package webbeta

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

const v3TestToken = "0123456789abcdef0123456789abcdef"

func v3TestTools() []provider.ToolSchema {
	return []provider.ToolSchema{{Name: "write_file", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`)}}
}

func TestV3RawArgumentBodyIsNeverTreatedAsFieldName(t *testing.T) {
	header := `ORCA_TURN_V3 {"type":"tool","id":"write-1","name":"write_file","arguments":{"path":"x"},"raw_argument":"file body"}`
	for _, tail := range []string{"", "\nORCA_RAW_V3_" + v3TestToken + "\nfile body", "\nORCA_RAW_V3_" + v3TestToken + "\nfile body\nORCA_END_V3_" + v3TestToken} {
		turn, err := parseTurnV3(header+tail, v3TestToken, v3TestTools(), nil)
		if !errors.Is(err, errV3RawArgument) || len(turn.Calls) != 0 {
			t.Fatalf("invalid raw argument executed or misclassified: %+v %v", turn, err)
		}
	}
}

func TestV3RawToolPreservesExactContentAndProgress(t *testing.T) {
	content := "package main\n\tvar path = `C:\\src\\x.go`\n\n# 中文 😀\n"
	raw := "先检查了输入文件。\nORCA_TURN_V3 {\"type\":\"tool\",\"id\":\"write-1\",\"name\":\"write_file\",\"arguments\":{\"path\":\"src/x.go\"},\"raw_argument\":\"content\"}\nORCA_RAW_V3_" + v3TestToken + "\n" + content + "\nORCA_END_V3_" + v3TestToken
	turn, err := parseTurnV3(raw, v3TestToken, v3TestTools(), nil)
	if err != nil || turn.Progress != "先检查了输入文件。" || len(turn.Calls) != 1 || turn.Calls[0].ID != "write-1" {
		t.Fatalf("V3 tool did not parse: %+v %v", turn, err)
	}
	var args struct{ Path, Content string }
	if err := json.Unmarshal([]byte(turn.Calls[0].Arguments), &args); err != nil || args.Path != "src/x.go" || args.Content != content {
		t.Fatalf("raw content changed: %+v %v", args, err)
	}
}

func TestV3RawFinalPreservesCodeAndJSON(t *testing.T) {
	content := "{\"ok\":true}\n```go\nfunc main() {}\n```\n"
	raw := "ORCA_TURN_V3 {\"type\":\"final\",\"raw\":true}\nORCA_RAW_V3_" + v3TestToken + "\n" + content + "\nORCA_END_V3_" + v3TestToken
	turn, err := parseTurnV3(raw, v3TestToken, nil, nil)
	if err != nil || turn.Final != content || len(turn.Calls) != 0 {
		t.Fatalf("V3 final changed: %+v %v", turn, err)
	}
}

func TestV3VerboseLeadInIsAcceptedOnlyForFinal(t *testing.T) {
	lead := "已完成修复并通过项目校验。\n\n- 缺陷已定位。\n- 文件已修复。\n- 校验结果为 PASS。"
	final := lead + "\nORCA_TURN_V3 {\"type\":\"final\",\"text\":\"任务已完成\"}"
	turn, err := parseTurnV3(final, v3TestToken, v3TestTools(), nil)
	if err != nil || turn.Final != "任务已完成" || turn.Progress != "" || len(turn.Calls) != 0 || !v3FinalCandidate(final) {
		t.Fatalf("verbose final not safely accepted: %+v %v", turn, err)
	}
	tool := lead + "\nORCA_TURN_V3 {\"type\":\"tool\",\"id\":\"one\",\"name\":\"write_file\",\"arguments\":{\"path\":\"x\",\"content\":\"bad\"}}"
	for name, raw := range map[string]string{
		"verbose-tool":    tool,
		"quoted-final":    "> " + final,
		"fenced-final":    "```text\n" + final + "\n```",
		"duplicate-final": final + "\n" + final,
	} {
		turn, err := parseTurnV3(raw, v3TestToken, v3TestTools(), nil)
		if err == nil || len(turn.Calls) != 0 || v3FinalCandidate(raw) {
			t.Fatalf("%s accepted: %+v %v", name, turn, err)
		}
	}
}

func TestV3RejectsIncompleteAmbiguousOrUntrustedCalls(t *testing.T) {
	base := `ORCA_TURN_V3 {"type":"tool","id":"one","name":"write_file","arguments":{"path":"src/x.go"},"raw_argument":"content"}` + "\nORCA_RAW_V3_" + v3TestToken + "\nhello\nORCA_END_V3_" + v3TestToken
	for name, raw := range map[string]string{
		"missing-end":       strings.TrimSuffix(base, "\nORCA_END_V3_"+v3TestToken),
		"duplicate-end":     base + "\nORCA_END_V3_" + v3TestToken,
		"wrong-token":       strings.ReplaceAll(base, v3TestToken, "ffffffffffffffffffffffffffffffff"),
		"conflicting-field": strings.Replace(base, `"path":"src/x.go"`, `"path":"src/x.go","content":"different"`, 1),
		"quoted":            "> " + base,
		"fenced":            "```text\n" + base + "\n```",
		"think":             "<think>" + base + "</think>",
		"example":           "Example:\n" + base,
		"chinese-example":   "下面是示例：\n" + base,
		"unknown-tool":      strings.Replace(base, `"name":"write_file"`, `"name":"not_allowed"`, 1),
		"duplicate-id":      base,
	} {
		history := []provider.Message(nil)
		if name == "duplicate-id" {
			history = []provider.Message{{ToolCalls: []provider.ToolCall{{ID: "one"}}}}
		}
		turn, err := parseTurnV3(raw, v3TestToken, v3TestTools(), history)
		if err == nil || len(turn.Calls) != 0 {
			t.Fatalf("%s accepted: %+v %v", name, turn, err)
		}
	}
}
