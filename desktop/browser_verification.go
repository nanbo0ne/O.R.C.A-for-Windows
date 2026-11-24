package main

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// Kind is a fixed diagnostic label, never response text or page content.
type browserVerificationError struct{ Kind string }

func (e *browserVerificationError) Error() string {
	return "browser completion is not confirmed by fresh evidence: " + e.Kind
}

type browserResponseError struct{ Kind string }

func (e *browserResponseError) Error() string { return "browser response rejected: " + e.Kind }

func browserCheckFinishReason(reason string) error {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "", "stop", "tool_calls":
		return nil
	case "length":
		return &browserResponseError{Kind: "output_limit"}
	case "content_filter":
		return &browserResponseError{Kind: "content_filter"}
	case "repetition_truncation":
		return &browserResponseError{Kind: "repetition_truncation"}
	default:
		return &browserResponseError{Kind: "abnormal_finish"}
	}
}

func browserVerifyVerdict(body string, calls []provider.ToolCall) error {
	fail := func(kind string) error { return &browserVerificationError{Kind: kind} }
	if len(calls) != 0 {
		return fail("unexpected_tool")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return fail("empty_response")
	}
	if len(body) > 4096 {
		return fail("verdict_too_large")
	}
	// Accept one complete JSON fence only, never extract a positive fragment
	// from prose/thinking or discard a contradictory trailing answer.
	if strings.HasPrefix(body, "```") {
		header, rest, ok := strings.Cut(body, "\n")
		header = strings.TrimSpace(header)
		if !ok || (header != "```json" && header != "```") || !strings.HasSuffix(rest, "\n```") {
			return fail("invalid_fence")
		}
		body = strings.TrimSpace(strings.TrimSuffix(rest, "\n```"))
	}
	if !strings.HasPrefix(body, "{") {
		return fail("non_json_response")
	}
	d := json.NewDecoder(strings.NewReader(body))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return fail("invalid_json")
	}
	seen := map[string]bool{}
	var satisfied *bool
	var reason string
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return fail("invalid_json")
		}
		if seen[key] {
			return fail("duplicate_field")
		}
		seen[key] = true
		switch key {
		case "satisfied":
			err = d.Decode(&satisfied)
		case "reason":
			err = d.Decode(&reason)
		default:
			return fail("unknown_field")
		}
		if err != nil {
			return fail("invalid_json")
		}
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return fail("invalid_json")
	}
	if _, err := d.Token(); err != io.EOF {
		return fail("trailing_content")
	}
	if satisfied == nil || strings.TrimSpace(reason) == "" {
		return fail("missing_fields")
	}
	if len(reason) > 2048 {
		return fail("reason_too_large")
	}
	if !*satisfied {
		return fail("negative_evidence")
	}
	return nil
}

const browserStaleReceipt = "The previous observation expired before dispatch; no action from that response was taken. Use the new observation to decide again with a new tool-call ID. Do not replay the discarded action."

func (s *browserService) replanStale(t *browserTask, callID string, err error) bool {
	if !errors.Is(err, browserhost.ErrStaleObservation) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checkTaskLocked(t) != nil || t.budget.staleReplans >= browserMaxStaleReplans {
		return false
	}
	t.budget.staleReplans++
	if callID != "" {
		t.budget.seen["action:"+callID] = true
	}
	return true
}
