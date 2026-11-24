package webbeta

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

const (
	nativeJSONOpen  = "<tool_call>"
	nativeJSONClose = "</tool_call>"
	dsmlCallsOpen   = "<\uFF5C\uFF5CDSML\uFF5C\uFF5C calls>"
	dsmlCallsClose  = "</\uFF5C\uFF5CDSML\uFF5C\uFF5C calls>"
	dsmlInvokeOpen  = "<\uFF5C\uFF5CDSML\uFF5C\uFF5C invoke name=\""
	dsmlInvokeClose = "</\uFF5C\uFF5CDSML\uFF5C\uFF5C invoke>"
	dsmlPathOpen    = "<\uFF5C\uFF5CDSML\uFF5C\uFF5C parameter name=\"path\" string=\"true\">"
	dsmlPathClose   = "</\uFF5C\uFF5CDSML\uFF5C\uFF5C parameter>"
)

var errNativeMutation = errors.New("native tool envelope cannot modify state")

// Only complete, standalone native envelopes for known read-only tools are
// accepted. File writes and all other actions still require the V3 contract.
func parseNativeReadOnly(raw string, tools []provider.ToolSchema, history []provider.Message) (decodedTurn, bool, error) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	var payload string
	switch {
	case strings.HasPrefix(trimmed, nativeJSONOpen):
		if !strings.HasSuffix(trimmed, nativeJSONClose) || strings.Count(trimmed, nativeJSONOpen) != 1 || strings.Count(trimmed, nativeJSONClose) != 1 {
			return decodedTurn{}, true, errors.New("incomplete native tool envelope")
		}
		payload = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, nativeJSONOpen), nativeJSONClose))
	case strings.HasPrefix(trimmed, dsmlCallsOpen):
		if !strings.HasSuffix(trimmed, dsmlCallsClose) || strings.Count(trimmed, dsmlCallsOpen) != 1 || strings.Count(trimmed, dsmlCallsClose) != 1 {
			return decodedTurn{}, true, errors.New("incomplete DSML envelope")
		}
		body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, dsmlCallsOpen), dsmlCallsClose))
		if !strings.HasPrefix(body, dsmlInvokeOpen) || !strings.HasSuffix(body, dsmlInvokeClose) {
			return decodedTurn{}, true, errors.New("invalid DSML invocation")
		}
		body = strings.TrimPrefix(body, dsmlInvokeOpen)
		name, rest, ok := strings.Cut(body, "\">")
		if !ok {
			return decodedTurn{}, true, errors.New("invalid DSML tool name")
		}
		if name == "write_file" {
			return decodedTurn{}, true, errNativeMutation
		}
		if !nativeReadOnlyAllowed(name, tools) {
			return decodedTurn{}, true, errors.New("unavailable DSML read-only tool")
		}
		rest = strings.TrimSpace(strings.TrimSuffix(rest, dsmlInvokeClose))
		if !strings.HasPrefix(rest, dsmlPathOpen) || !strings.HasSuffix(rest, dsmlPathClose) {
			return decodedTurn{}, true, errors.New("invalid DSML path parameter")
		}
		path := strings.TrimSuffix(strings.TrimPrefix(rest, dsmlPathOpen), dsmlPathClose)
		if path == "" || strings.TrimSpace(path) != path || strings.ContainsAny(path, "<>\r\n") {
			return decodedTurn{}, true, errors.New("invalid DSML path value")
		}
		args, _ := json.Marshal(map[string]string{"path": path})
		hash := sha256.Sum256([]byte(trimmed))
		payloadBytes, _ := json.Marshal(struct {
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}{"native-" + hex.EncodeToString(hash[:12]), name, args})
		payload = string(payloadBytes)
	default:
		return decodedTurn{}, false, nil
	}
	call, err := parseTool(toolMarker(0)+" "+payload, tools, history)
	if err != nil {
		return decodedTurn{}, true, err
	}
	if !nativeReadOnlyAllowed(call.Name, tools) {
		if call.Name == "write_file" {
			return decodedTurn{}, true, errNativeMutation
		}
		return decodedTurn{}, true, errors.New("native tool envelope is not read-only")
	}
	return decodedTurn{Calls: []provider.ToolCall{*call}}, true, nil
}

func nativeReadOnlyAllowed(name string, tools []provider.ToolSchema) bool {
	if name != "read_file" && name != "ls" {
		return false
	}
	for _, candidate := range tools {
		if candidate.Name == name {
			return candidate.ReadOnly
		}
	}
	return false
}
