package webbeta

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func nativeReadTools() []provider.ToolSchema {
	return []provider.ToolSchema{
		{Name: "read_file", ReadOnly: true, Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
		{Name: "ls", ReadOnly: true, Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
		{Name: "write_file", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`)},
	}
}

func TestV3NativeWriteRequiresCorrectedV3Record(t *testing.T) {
	raw := dsmlCallsOpen + "\n" + dsmlInvokeOpen + "write_file\">\n" + dsmlPathOpen + "x" + dsmlPathClose + "\n" + dsmlInvokeClose + "\n" + dsmlCallsClose
	turn, err := parseTurnV3(raw, v3TestToken, nativeReadTools(), nil)
	if !errors.Is(err, errNativeMutation) || len(turn.Calls) != 0 {
		t.Fatalf("native write was not held for V3 correction: %+v %v", turn, err)
	}
}

func TestV3StandaloneNativeReadOnlyEnvelopes(t *testing.T) {
	jsonCall := "<tool_call>\n" + `{"id":"read1","name":"read_file","arguments":{"path":"instructions.txt"}}` + "\n</tool_call>"
	dsmlCall := dsmlCallsOpen + "\n" + dsmlInvokeOpen + "read_file\">\n" + dsmlPathOpen + "instructions.txt" + dsmlPathClose + "\n" + dsmlInvokeClose + "\n" + dsmlCallsClose
	for name, raw := range map[string]string{"json": jsonCall, "dsml": dsmlCall} {
		turn, err := parseTurnV3(raw, v3TestToken, nativeReadTools(), nil)
		if err != nil || len(turn.Calls) != 1 || turn.Calls[0].Name != "read_file" || turn.Final != "" || turn.Progress != "" || !strings.Contains(turn.Calls[0].Arguments, `"path":"instructions.txt"`) {
			t.Fatalf("%s read-only envelope rejected: %+v %v", name, turn, err)
		}
		history := []provider.Message{{ToolCalls: []provider.ToolCall{{ID: turn.Calls[0].ID}}}}
		if repeated, err := parseTurnV3(raw, v3TestToken, nativeReadTools(), history); err == nil || len(repeated.Calls) != 0 {
			t.Fatalf("%s repeated tool accepted: %+v %v", name, repeated, err)
		}
	}
}

func TestV3NativeEnvelopeCannotWriteOrHideInText(t *testing.T) {
	writeJSON := "<tool_call>\n" + `{"id":"write1","name":"write_file","arguments":{"path":"x","content":"bad"}}` + "\n</tool_call>"
	writeDSML := dsmlCallsOpen + "\n" + dsmlInvokeOpen + "write_file\">\n" + dsmlPathOpen + "x" + dsmlPathClose + "\n" + dsmlInvokeClose + "\n" + dsmlCallsClose
	readJSON := "<tool_call>\n" + `{"id":"read1","name":"read_file","arguments":{"path":"x"}}` + "\n</tool_call>"
	readDSML := dsmlCallsOpen + "\n" + dsmlInvokeOpen + "read_file\">\n" + dsmlPathOpen + "x" + dsmlPathClose + "\n" + dsmlInvokeClose + "\n" + dsmlCallsClose
	for name, raw := range map[string]string{
		"json-write":     writeJSON,
		"dsml-write":     writeDSML,
		"json-quoted":    "> " + readJSON,
		"json-fenced":    "```\n" + readJSON + "\n```",
		"json-example":   "Example:\n" + readJSON,
		"json-trailing":  readJSON + "\nextra",
		"dsml-example":   "Example:\n" + readDSML,
		"dsml-duplicate": readDSML + "\n" + readDSML,
		"dsml-truncated": strings.TrimSuffix(readDSML, dsmlCallsClose),
	} {
		turn, err := parseTurnV3(raw, v3TestToken, nativeReadTools(), nil)
		if err == nil || len(turn.Calls) != 0 {
			t.Fatalf("%s executed: %+v %v", name, turn, err)
		}
	}
}

func TestV3NativeReadAliasRequiresActualReadOnlyCapability(t *testing.T) {
	tools := nativeReadTools()
	tools[0].ReadOnly = false
	raw := "<tool_call>\n" + `{"id":"read1","name":"read_file","arguments":{"path":"x"}}` + "\n</tool_call>"
	turn, err := parseTurnV3(raw, v3TestToken, tools, nil)
	if err == nil || len(turn.Calls) != 0 {
		t.Fatalf("writable replacement accepted under a read-only name: %+v %v", turn, err)
	}
	encoded, err := json.Marshal(nativeReadTools()[0])
	if err != nil || strings.Contains(string(encoded), "ReadOnly") || strings.Contains(string(encoded), "readOnly") {
		t.Fatalf("internal capability leaked into model-facing schema: %s %v", encoded, err)
	}
}
