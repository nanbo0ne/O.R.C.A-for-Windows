package webbeta

import (
	"context"
	"errors"
	"fmt"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// DispatchDiagnosticError contains only host-owned enums and aggregate counts.
// Cause remains inspectable, but its potentially sensitive text is never rendered.
type DispatchDiagnosticError struct {
	Cause                                                       error
	Operation                                                   string
	Stage                                                       string
	Sequence                                                    int
	ElapsedMS                                                   int64
	Ready, Ack, SendAttempted, GenerationObserved, ResponseSeen bool
	TextChunks, ReasoningChunks, TextBytes, ReasoningBytes      int
	Stop                                                        string
	Trigger                                                     string
	StopFailureObserved                                         bool
	StopFailure                                                 StopDrainDiagnostic
}

func (e *DispatchDiagnosticError) Unwrap() error { return e.Cause }

// The provider slot receives a value copy, never the live mutable tracker.
func (e DispatchDiagnosticError) CancellationError(cause error) error {
	e.Cause = cause
	return provider.NoRequestReplay(&e)
}
func (e *DispatchDiagnosticError) Error() string {
	op := "task"
	if e.Operation == "opening" {
		op = "opening"
	}
	stage := safeDispatchStage(e.Stage)
	stop := "not_requested"
	if e.Stop == "confirmed" || e.Stop == "unconfirmed" {
		stop = e.Stop
	}
	code := "page_failure"
	switch {
	case errors.Is(e.Cause, context.DeadlineExceeded):
		code = "deadline_exceeded"
	case errors.Is(e.Cause, context.Canceled):
		code = "cancelled"
	default:
		var failure *Failure
		if errors.As(e.Cause, &failure) {
			switch failure.Code {
			case "dispatch_ack_timeout", "dispatch_ack_missing", "event_channel_closed", "adapter_missing", "adapter_generation_mismatch", "dispatch_exception", "dispatch_rejected", "dispatch_revoked", "navigation_timeout", "stop_unconfirmed":
				code = failure.Code
			}
		}
	}
	drain := ""
	if e.StopFailureObserved {
		drain = fmt.Sprintf(" stop_http_done=%t stop_ui_idle=%t stop_task_exited=%t stop_revoked=%t stop_output_drained=%t", e.StopFailure.HTTPDone, e.StopFailure.UIIdle, e.StopFailure.TaskExited, e.StopFailure.Revoked, e.StopFailure.OutputDrained)
	}
	return fmt.Sprintf("网页请求未完成；不会自动重发。[web_dispatch operation=%s stage=%s seq=%d elapsed_ms=%d code=%s trigger=%s ready=%t ack=%t send_attempted=%t generation_observed=%t response_seen=%t text_chunks=%d reasoning_chunks=%d text_bytes=%d reasoning_bytes=%d stop=%s%s]", op, stage, e.Sequence, e.ElapsedMS, code, safeDispatchTrigger(e.Trigger), e.Ready, e.Ack, e.SendAttempted, e.GenerationObserved, e.ResponseSeen, e.TextChunks, e.ReasoningChunks, e.TextBytes, e.ReasoningBytes, stop, drain)
}

// Keep the first failure separate from stop/drain failures without exposing page text.
func (e *DispatchDiagnosticError) RecordTrigger(cause error) {
	if e.Trigger != "" {
		return
	}
	switch {
	case errors.Is(cause, context.Canceled):
		e.Trigger = "cancelled"
	case errors.Is(cause, context.DeadlineExceeded):
		e.Trigger = "deadline_exceeded"
	default:
		var failure *Failure
		if errors.As(cause, &failure) {
			e.Trigger = safeDispatchTrigger(failure.Code)
		} else {
			e.Trigger = "unknown"
		}
	}
}

func safeDispatchTrigger(code string) string {
	switch code {
	case "cancelled", "deadline_exceeded", "dispatch_ack_timeout", "dispatch_ack_missing", "event_channel_closed",
		"manual_intervention", "restricted", "challenge", "rate_limited", "signed_out", "page_not_ready",
		"ambiguous_response", "response_identity_changed", "response_rewritten", "completion_unconfirmed",
		"mode_control_unavailable", "mode_state_unconfirmed", "existing_draft", "send_unavailable",
		"raw_adapter_unavailable", "raw_batch_invalid", "raw_batch_unknown", "raw_binding_unavailable",
		"raw_busy", "raw_closed", "raw_content_unknown", "raw_duplicate_snapshot", "raw_event_unknown",
		"raw_fragment_missing", "raw_fragment_unknown", "raw_fragments_invalid", "raw_frame_limit",
		"raw_fragment_invalid", "raw_fragment_type_tool", "raw_fragment_type_tool_call", "raw_fragment_type_tool_result",
		"raw_fragment_type_search", "raw_fragment_type_answer", "raw_fragment_type_unknown",
		"raw_fragment_type_missing", "raw_fragment_type_null", "raw_fragment_type_invalid", "raw_fragment_type_empty",
		"raw_fragment_type_request", "raw_fragment_type_template_response", "raw_fragment_type_file",
		"raw_fragment_type_tool_search", "raw_fragment_type_tool_open", "raw_fragment_type_tool_find",
		"raw_fragment_type_tip", "raw_fragment_type_read_link",
		"raw_fragment_content_missing", "raw_fragment_content_null", "raw_fragment_content_invalid", "raw_observation_failed",
		"raw_http_failed", "raw_identity_changed", "raw_invalid_json", "raw_invalid_unicode",
		"raw_line_limit", "raw_metadata_invalid", "raw_multiple_matches", "raw_op_invalid", "raw_op_unknown",
		"raw_patch_invalid", "raw_path_invalid", "raw_request_limit", "raw_response_rewritten", "raw_size_limit",
		"raw_sse_unknown", "raw_status_unknown", "raw_status_incomplete", "raw_status_content_filter",
		"raw_status_context_length", "raw_status_timeout", "raw_stream_truncated", "raw_xhr_type":
		return code
	default:
		return "unknown"
	}
}

func safeDispatchStage(stage string) string {
	switch stage {
	case "navigation", "ready", "dispatch_queued", "dispatch_ack", "modes_checked", "uploads_ready", "input_set", "send_attempted", "generation_observed", "response_seen", "done":
		return stage
	default:
		return "unknown"
	}
}

// Observe ignores arbitrary page strings and stores no payload or identifiers.
func (e *DispatchDiagnosticError) Observe(ev Event) {
	if ev.Kind == "error" {
		e.RecordTrigger(&Failure{Code: ev.Code})
	}
	if ev.Kind == "stop_failed" && ev.StopDiagnostic != nil {
		e.StopFailureObserved = true
		e.StopFailure = *ev.StopDiagnostic
		return
	}
	if ev.Kind == "phase" {
		stage := safeDispatchStage(ev.Code)
		if stage == "unknown" {
			return
		}
		e.Stage = stage
		e.Sequence++
		switch stage {
		case "ready":
			e.Ready = true
		case "dispatch_ack":
			e.Ack = true
		case "send_attempted":
			e.SendAttempted = true
		case "generation_observed":
			e.GenerationObserved = true
		case "response_seen":
			e.ResponseSeen = true
		}
	}
	if ev.Kind == "text" {
		e.TextChunks++
		e.TextBytes += len(ev.Text)
	}
	if ev.Kind == "reasoning" {
		e.ReasoningChunks++
		e.ReasoningBytes += len(ev.Text)
	}
}
