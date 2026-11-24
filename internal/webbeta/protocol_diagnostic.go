package webbeta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ResponseShape contains no content, paths, URLs, tool arguments or reasoning.
// It describes only the response produced for this request, never stored chats.
type ResponseShape struct {
	Shape      string
	Characters int
	Bytes      int
	SHA256     string
}

func responseShape(raw string) ResponseShape {
	trimmed := strings.TrimSpace(raw)
	shape := "plain_text"
	switch {
	case trimmed == "":
		shape = "empty"
	case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
		shape = "markdown_fence"
	case strings.HasPrefix(trimmed, nativeJSONOpen):
		shape = "native_tool_envelope"
	case strings.HasPrefix(trimmed, dsmlCallsOpen):
		shape = "dsml_envelope"
	case strings.HasPrefix(trimmed, turnMarkerV3):
		shape = "v3_turn_record"
	case strings.HasPrefix(trimmed, strings.TrimSpace(turnMarkerV3)):
		shape = "v3_marker_separator"
	case strings.Contains(trimmed, "\n"+turnMarkerV3):
		if _, _, err := v3Preamble(trimmed); err == nil {
			shape = "v3_progress_record"
		} else {
			shape = "v3_embedded_record"
		}
	case strings.HasPrefix(trimmed, turnMarker):
		shape = "turn_record"
	case strings.HasPrefix(trimmed, progressMarker):
		shape = "progress_record"
	case strings.HasPrefix(trimmed, strings.TrimSpace(turnMarker)) || strings.HasPrefix(trimmed, strings.TrimSpace(progressMarker)):
		shape = "marker_separator"
	case strings.HasPrefix(trimmed, toolMarker(0)) || strings.HasPrefix(trimmed, toolMarker(1)):
		shape = "legacy_record"
	case strings.HasPrefix(trimmed, ">"):
		shape = "quoted_text"
	case (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && json.Valid([]byte(trimmed)):
		shape = "unframed_json"
	}
	return ResponseShape{Shape: shape, Characters: utf8.RuneCountInString(raw), Bytes: len(raw), SHA256: contentHash(raw)}
}

type ProtocolDiagnosticError struct {
	Cause           error
	Stage           string
	Original        ResponseShape
	OriginalCode    string
	Correction      *ResponseShape
	Corrections     int
	TextChunks      int
	ReasoningChunks int
}

func (e *ProtocolDiagnosticError) Unwrap() error { return e.Cause }
func protocolErrorCode(err error) string {
	if err == nil {
		return "none"
	}
	var budget *taskBudgetError
	if errors.As(err, &budget) {
		switch budget.reason {
		case "generations", "actions", "corrections", "active_time":
			return "task_limit_" + budget.reason
		default:
			return "task_limit_invalid_state"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, errToolReplay) {
		return "duplicate_tool_call"
	}
	if errors.Is(err, errV3RawArgument) {
		return "raw_argument_invalid"
	}
	var syntax *json.SyntaxError
	var fieldType *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &fieldType) {
		return "header_json_invalid"
	}
	switch err.Error() {
	case "missing explicit turn record":
		return "missing_explicit_turn_record"
	case "missing or repeated V3 raw marker":
		return "raw_markers_invalid"
	case "missing V3 raw start line", "missing V3 raw end line":
		return "raw_marker_line_invalid"
	case "invalid or trailing V3 raw content", "trailing V3 response content":
		return "trailing_content"
	case "invalid V3 tool fields", "invalid V3 final fields", "invalid V3 raw final", "empty V3 final", "unknown V3 turn type":
		return "record_fields_invalid"
	}
	return "invalid_record"
}

func (e *ProtocolDiagnosticError) Error() string {
	code := protocolErrorCode(e.Cause)
	prefix := "网页回答未通过协议校验；不会自动重发。"
	var budget *taskBudgetError
	if errors.As(e.Cause, &budget) {
		prefix = "网页回答需要纠正格式，但尚未发送纠正请求。" + budget.Error()
	}
	message := fmt.Sprintf("%s[web_protocol stage=%s code=%s original_shape=%s chars=%d bytes=%d sha256=%s text_chunks=%d reasoning_chunks=%d correction_attempts=%d",
		prefix,
		e.Stage, code, e.Original.Shape, e.Original.Characters, e.Original.Bytes, e.Original.SHA256, e.TextChunks, e.ReasoningChunks, e.Corrections)
	if e.OriginalCode != "" {
		message += " original_code=" + e.OriginalCode
	}
	if e.Correction != nil {
		message += fmt.Sprintf(" correction_shape=%s correction_chars=%d correction_bytes=%d correction_sha256=%s", e.Correction.Shape, e.Correction.Characters, e.Correction.Bytes, e.Correction.SHA256)
	}
	return message + "]"
}
