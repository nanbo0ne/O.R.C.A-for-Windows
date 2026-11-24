package webbeta

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

const latestGrammar = 3
const turnMarkerV3 = "ORCA_TURN_V3 "

var errV3RawArgument = errors.New("V3 raw argument must name one undeclared string parameter")

type v3Record struct {
	Type        string          `json:"type"`
	ID          string          `json:"id,omitempty"`
	Name        string          `json:"name,omitempty"`
	Arguments   json.RawMessage `json:"arguments,omitempty"`
	RawArgument string          `json:"raw_argument,omitempty"`
	Raw         bool            `json:"raw,omitempty"`
	Text        *string         `json:"text,omitempty"`
}

func v3Markers(token string) (string, string, error) {
	if len(token) != 32 {
		return "", "", errors.New("invalid V3 response token")
	}
	for _, ch := range token {
		if !strings.ContainsRune("0123456789abcdef", ch) {
			return "", "", errors.New("invalid V3 response token")
		}
	}
	return "ORCA_RAW_V3_" + token, "ORCA_END_V3_" + token, nil
}

func v3Preamble(raw string) (string, string, error) {
	trimmed := strings.TrimLeft(raw, " \t\r\n")
	if strings.HasPrefix(trimmed, turnMarkerV3) {
		return "", trimmed, nil
	}
	idx := strings.Index(trimmed, "\n"+turnMarkerV3)
	if idx < 0 {
		return "", "", errors.New("missing V3 turn record")
	}
	progress := strings.TrimSpace(trimmed[:idx])
	if progress == "" || len(progress) > 2048 || strings.Count(progress, "\n") > 2 ||
		strings.Contains(progress, "ORCA_") || strings.Contains(progress, "```") ||
		strings.Contains(strings.ToLower(progress), "<think") ||
		strings.HasSuffix(progress, ":") || strings.HasSuffix(progress, "：") ||
		strings.Contains(strings.ToLower(progress), "example") ||
		strings.Contains(progress, "示例") || strings.Contains(progress, "例如") ||
		strings.Contains(progress, "引用") {
		return "", "", errors.New("invalid V3 progress prefix")
	}
	for _, line := range strings.Split(progress, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, ">") || strings.HasPrefix(line, "[") || line == "" {
			return "", "", errors.New("quoted or empty V3 progress prefix")
		}
	}
	return progress, trimmed[idx+1:], nil
}

// A verbose lead-in is never a tool request. Accept it only when the single
// explicit record is a final answer, and discard the lead-in rather than
// exposing an unvalidated progress stream.
func v3VerboseFinal(raw string) (string, error) {
	trimmed := strings.TrimLeft(raw, " \t\r\n")
	marker := "\n" + turnMarkerV3
	if strings.Count(trimmed, marker) != 1 {
		return "", errors.New("missing or repeated V3 final record")
	}
	idx := strings.Index(trimmed, marker)
	prefix := strings.TrimSpace(trimmed[:idx])
	if prefix == "" || len(prefix) > 4096 || strings.Count(prefix, "\n") > 16 ||
		strings.Contains(prefix, "ORCA_") || strings.Contains(prefix, "```") ||
		strings.Contains(prefix, "~~~") || strings.Contains(strings.ToLower(prefix), "<think") {
		return "", errors.New("invalid verbose V3 final prefix")
	}
	for _, line := range strings.Split(prefix, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, ">") || strings.HasPrefix(line, "[") {
			return "", errors.New("quoted V3 final prefix")
		}
	}
	rest := trimmed[idx+1:]
	header := strings.SplitN(rest, "\n", 2)[0]
	var record v3Record
	if err := decodeStrict(strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(header, "\r"), turnMarkerV3)), &record); err != nil || record.Type != "final" {
		return "", errors.New("verbose V3 prefix is not a final answer")
	}
	return rest, nil
}

func v3RawBody(rest, token string) (string, error) {
	begin, end, err := v3Markers(token)
	if err != nil {
		return "", err
	}
	if strings.Count(rest, begin) != 1 || strings.Count(rest, end) != 1 {
		return "", errors.New("missing or repeated V3 raw marker")
	}
	lineEnd := strings.IndexByte(rest, '\n')
	if lineEnd < 0 || strings.TrimSuffix(rest[:lineEnd], "\r") != begin {
		return "", errors.New("missing V3 raw start line")
	}
	contentAndEnd := rest[lineEnd+1:]
	endAt := strings.LastIndex(contentAndEnd, end)
	if endAt < 1 || contentAndEnd[endAt-1] != '\n' {
		return "", errors.New("missing V3 raw end line")
	}
	contentEnd := endAt - 1
	if contentEnd > 0 && contentAndEnd[contentEnd-1] == '\r' {
		contentEnd--
	}
	content := contentAndEnd[:contentEnd]
	if !utf8.ValidString(content) || strings.TrimSpace(contentAndEnd[endAt+len(end):]) != "" {
		return "", errors.New("invalid or trailing V3 raw content")
	}
	return content, nil
}

func v3RawFieldAllowed(tools []provider.ToolSchema, name, field string) bool {
	for _, tool := range tools {
		if tool.Name != name {
			continue
		}
		var schema struct {
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
		}
		if json.Unmarshal(tool.Parameters, &schema) != nil {
			return false
		}
		return schema.Properties[field].Type == "string"
	}
	return false
}

func parseTurnV3(raw, token string, tools []provider.ToolSchema, history []provider.Message) (decodedTurn, error) {
	var result decodedTurn
	if len(raw) > 2<<20 || !utf8.ValidString(raw) {
		return result, errors.New("V3 response exceeds limit or has invalid UTF-8")
	}
	if _, _, err := v3Markers(token); err != nil {
		return result, err
	}
	if native, recognized, err := parseNativeReadOnly(raw, tools, history); recognized {
		return native, err
	}
	progress, rest, err := v3Preamble(raw)
	if err != nil {
		rest, err = v3VerboseFinal(raw)
		if err != nil {
			return result, err
		}
	}
	lineEnd := strings.IndexByte(rest, '\n')
	header := rest
	tail := ""
	if lineEnd >= 0 {
		header, tail = rest[:lineEnd], rest[lineEnd+1:]
	}
	header = strings.TrimSuffix(header, "\r")
	if len(header) > 64<<10 || !strings.HasPrefix(header, turnMarkerV3) {
		return result, errors.New("invalid V3 turn header")
	}
	var record v3Record
	if err := decodeStrict(strings.TrimSpace(strings.TrimPrefix(header, turnMarkerV3)), &record); err != nil {
		return result, err
	}
	// Diagnose a body mistaken for a field name before looking for raw markers.
	// Otherwise a missing block masks the actionable error and repeats the wrong format.
	if record.Type == "tool" && record.RawArgument != "" && !v3RawFieldAllowed(tools, record.Name, record.RawArgument) {
		return result, errV3RawArgument
	}
	var content string
	if record.Raw || record.RawArgument != "" {
		content, err = v3RawBody(tail, token)
		if err != nil {
			return result, err
		}
	} else if strings.TrimSpace(tail) != "" {
		return result, errors.New("trailing V3 response content")
	}
	switch record.Type {
	case "final":
		if record.ID != "" || record.Name != "" || record.Arguments != nil || record.RawArgument != "" {
			return result, errors.New("invalid V3 final fields")
		}
		if record.Raw {
			if record.Text != nil || strings.TrimSpace(content) == "" {
				return result, errors.New("invalid V3 raw final")
			}
			result.Final = content
		} else {
			if record.Text == nil || strings.TrimSpace(*record.Text) == "" {
				return result, errors.New("empty V3 final")
			}
			result.Final = *record.Text
		}
	case "tool":
		if record.Raw || record.Text != nil || record.ID == "" || record.Name == "" || len(record.Arguments) == 0 {
			return result, errors.New("invalid V3 tool fields")
		}
		if err := uniqueJSONKeys(string(record.Arguments)); err != nil {
			return result, err
		}
		var args map[string]json.RawMessage
		if json.Unmarshal(record.Arguments, &args) != nil || args == nil {
			return result, errors.New("V3 tool arguments must be an object")
		}
		if record.RawArgument != "" {
			if !v3RawFieldAllowed(tools, record.Name, record.RawArgument) || args[record.RawArgument] != nil {
				return result, errV3RawArgument
			}
			encoded, _ := json.Marshal(content)
			args[record.RawArgument] = encoded
		}
		arguments, _ := json.Marshal(args)
		callJSON, _ := json.Marshal(struct {
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}{record.ID, record.Name, arguments})
		call, err := parseTool(toolMarker(0)+" "+string(callJSON), tools, history)
		if err != nil {
			return decodedTurn{}, fmt.Errorf("V3 tool rejected: %w", err)
		}
		result.Calls = []provider.ToolCall{*call}
	default:
		return result, errors.New("unknown V3 turn type")
	}
	result.Progress = progress
	return result, nil
}

func repairableTurnV3(raw string) bool {
	_, rest, err := v3Preamble(raw)
	if err != nil {
		rest, err = v3VerboseFinal(raw)
	}
	return err == nil && strings.HasPrefix(rest, turnMarkerV3)
}

func v3FinalCandidate(raw string) bool {
	_, rest, err := v3Preamble(raw)
	if err != nil {
		rest, err = v3VerboseFinal(raw)
	}
	if err != nil {
		return false
	}
	header := strings.SplitN(rest, "\n", 2)[0]
	var record v3Record
	return decodeStrict(strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(header, "\r"), turnMarkerV3)), &record) == nil && record.Type == "final"
}
