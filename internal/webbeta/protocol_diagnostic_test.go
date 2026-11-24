package webbeta

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type diagnosticBrowser struct {
	fakeBrowser
	raw           string
	corrected     string
	requests      int
	correctionErr error
}

func TestProtocolErrorCodeDoesNotExposeContent(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errV3RawArgument, "raw_argument_invalid"},
		{&taskBudgetError{reason: "corrections", used: 3, limit: 3}, "task_limit_corrections"},
		{&taskBudgetError{reason: "private-value"}, "task_limit_invalid_state"},
		{context.Canceled, "cancelled"},
		{context.DeadlineExceeded, "deadline_exceeded"},
		{errors.New("missing or repeated V3 raw marker"), "raw_markers_invalid"},
		{errors.New("missing V3 raw end line"), "raw_marker_line_invalid"},
		{errors.New("invalid V3 tool fields"), "record_fields_invalid"},
		{errors.New("secret-123 unknown field or file path"), "invalid_record"},
	} {
		if got := protocolErrorCode(tc.err); got != tc.want {
			t.Fatalf("got %s, want %s", got, tc.want)
		}
	}
	diagnostic := ProtocolDiagnosticError{Cause: errors.New("missing or repeated V3 raw marker"), OriginalCode: protocolErrorCode(errV3RawArgument)}
	if text := diagnostic.Error(); !strings.Contains(text, "original_code=raw_argument_invalid") || !strings.Contains(text, "code=raw_markers_invalid") {
		t.Fatal("original and correction failures were mixed")
	}
}

func TestProtocolCorrectionBudgetDiagnosticIsActionable(t *testing.T) {
	err := &ProtocolDiagnosticError{Stage: "correction_failed", Cause: &taskBudgetError{reason: "corrections", used: 3, limit: 3}}
	text := err.Error()
	if !strings.Contains(text, "尚未发送纠正请求") || !strings.Contains(text, "已用 3 次，上限 3 次") || !errors.Is(err, ErrTaskBudget) {
		t.Fatal(text)
	}
}

func (b *diagnosticBrowser) Run(ctx context.Context, c Command, emit func(Event) error) error {
	if c.URL == "" {
		return b.fakeBrowser.Run(ctx, c, emit)
	}
	b.requests++
	if b.requests > 1 && b.correctionErr != nil {
		return b.correctionErr
	}
	raw := b.raw
	if b.requests > 1 {
		raw = b.corrected
	}
	if err := emit(Event{ID: c.ID, Kind: "reasoning", Text: "synthetic reasoning; not diagnostic content"}); err != nil {
		return err
	}
	for _, part := range []string{raw[:len(raw)/2], raw[len(raw)/2:]} {
		if err := emit(Event{ID: c.ID, Kind: "text", Text: part}); err != nil {
			return err
		}
	}
	return emit(Event{ID: c.ID, Kind: "done", URL: c.URL})
}

func TestConfirmedMalformedResponseHasSafeDiagnosticWithoutReplay(t *testing.T) {
	for _, tc := range []struct {
		name, raw, corrected, shape string
		requests                    int
	}{
		{"numeric-answer", "42", "", "plain_text", 1},
		{"natural-answer", "Synthetic private-shaped canary: secret-123", "", "plain_text", 1},
		{"markdown-wrapper", "```json\nORCA_TURN_V2 {\"type\":\"final\",\"text\":\"42\"}\n```", "", "markdown_fence", 1},
		{"marker-newline", "ORCA_TURN_V2\n{\"type\":\"final\",\"text\":\"42\"}", "", "marker_separator", 1},
		{"bare-final-json", "{\"type\":\"final\",\"text\":\"42\"}", "", "unframed_json", 1},
		{"correction-still-plain", "ORCA_TURN_V2 {\"type\":\"bad\"}", "Synthetic private-shaped canary: secret-123", "turn_record", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &diagnosticBrowser{raw: tc.raw, corrected: tc.corrected}
			e := testEngine(t, b)
			old := CurrentEngine()
			SetEngine(e)
			defer SetEngine(old)
			ch, err := (&Adapter{}).Stream(context.Background(), provider.Request{ParentTaskID: "diagnostic-parent", ConversationID: "diagnostic-owner", RequestID: "diagnostic-request", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic task"}}, Tools: []provider.ToolSchema{{Name: "echo"}}})
			if err != nil {
				t.Fatal(err)
			}
			var failure error
			for chunk := range ch {
				if chunk.Type == provider.ChunkToolCall || chunk.Type == provider.ChunkDone || (chunk.Type == provider.ChunkText && chunk.Text != "") {
					t.Fatal("invalid response was treated as executable or completed")
				}
				if chunk.Err != nil {
					failure = chunk.Err
				}
			}
			if failure == nil || !provider.ReplayForbidden(failure) {
				t.Fatal("missing non-replayable failure", failure)
			}
			var diagnostic *ProtocolDiagnosticError
			if !errors.As(failure, &diagnostic) {
				t.Fatal("typed diagnostic lost through no-replay wrapper")
			}
			if diagnostic.Original.Bytes != len(tc.raw) || diagnostic.Original.Characters != utf8.RuneCountInString(tc.raw) || diagnostic.Original.SHA256 != contentHash(tc.raw) || diagnostic.TextChunks != 2 || diagnostic.ReasoningChunks != 1 || diagnostic.Corrections != tc.requests-1 {
				t.Fatal("diagnostic counters do not describe this response")
			}
			if tc.corrected == "" {
				if diagnostic.Stage != "confirmed_response" || diagnostic.Correction != nil {
					t.Fatal("invented correction evidence")
				}
			} else if diagnostic.Stage != "confirmed_correction" || diagnostic.Correction == nil || diagnostic.Correction.Shape != "plain_text" || diagnostic.Correction.SHA256 != contentHash(tc.corrected) {
				t.Fatal("original and corrected response evidence mixed")
			}
			if !strings.Contains(failure.Error(), "web_protocol") || !strings.Contains(failure.Error(), "original_shape="+tc.shape) {
				t.Errorf("missing safe format diagnostic: %v", failure)
			}
			if strings.Contains(failure.Error(), "secret-123") || strings.Contains(failure.Error(), "synthetic reasoning") {
				t.Fatal("diagnostic leaked response content")
			}
			if b.requests != tc.requests {
				t.Fatalf("unexpected requests/replay: %d", b.requests)
			}
		})
	}
}

func TestResponseShapeCountsUnicodeAndDoesNotInterpretText(t *testing.T) {
	raw := "合成结果🙂"
	got := responseShape(raw)
	if got.Characters != 5 || got.Bytes != len(raw) || got.Shape != "plain_text" {
		t.Fatalf("incorrect metadata: %+v", got)
	}
	for _, text := range []string{"```json\nORCA_TURN_V2 {\"type\":\"final\",\"text\":\"42\"}\n```", "ORCA_TURN_V2\n{\"type\":\"final\",\"text\":\"42\"}", "42"} {
		if _, err := parseTurn(text, nil, nil, currentGrammar); err == nil || repairableTurn(text, nil, nil) {
			t.Fatal("diagnostics widened execution or correction policy")
		}
	}
}

func TestResponseShapeDistinguishesV3RecordsFromPlainText(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"<tool_call>\n{}\n</tool_call>", "native_tool_envelope"},
		{dsmlCallsOpen + dsmlCallsClose, "dsml_envelope"},
		{`ORCA_TURN_V3 {"type":"final","text":"done"}`, "v3_turn_record"},
		{"先读取文件。\nORCA_TURN_V3 {\"type\":\"tool\"}", "v3_progress_record"},
		{"引用示例：\nORCA_TURN_V3 {\"type\":\"tool\"}", "v3_embedded_record"},
		{"ORCA_TURN_V3\n{}", "v3_marker_separator"},
	} {
		if got := responseShape(tc.raw).Shape; got != tc.want {
			t.Errorf("shape %q, want %q", got, tc.want)
		}
	}
}

func TestCorrectionTransportFailureDiagnosticKeepsCauseAndNoBody(t *testing.T) {
	cause := errors.New("synthetic transport detail secret-456")
	b := &diagnosticBrowser{raw: `ORCA_TURN_V2 {"type":"bad"}`, correctionErr: cause}
	e := testEngine(t, b)
	old := CurrentEngine()
	SetEngine(e)
	defer SetEngine(old)
	ch, err := (&Adapter{}).Stream(context.Background(), provider.Request{ParentTaskID: "parent", ConversationID: "owner", RequestID: "request", Messages: []provider.Message{{Role: provider.RoleUser, Content: "synthetic"}}, Tools: []provider.ToolSchema{{Name: "echo"}}})
	if err != nil {
		t.Fatal(err)
	}
	var failure error
	for chunk := range ch {
		if chunk.Type == provider.ChunkDone || chunk.Type == provider.ChunkToolCall {
			t.Fatal("correction failure executed or completed")
		}
		if chunk.Err != nil {
			failure = chunk.Err
		}
	}
	var diagnostic *ProtocolDiagnosticError
	if !errors.As(failure, &diagnostic) || !errors.Is(failure, cause) || !provider.ReplayForbidden(failure) {
		t.Fatal("lost correction cause or non-replay boundary", failure)
	}
	if diagnostic.Stage != "correction_failed" || diagnostic.Corrections != 1 || diagnostic.Correction != nil || diagnostic.Original.Shape != "turn_record" {
		t.Fatal("unconfirmed correction reported as completed")
	}
	if strings.Contains(failure.Error(), "secret-456") || b.requests != 2 {
		t.Fatal("diagnostic exposed upstream details or replayed request")
	}
}

// This documents current framing order, not a proven cause of a live failure.
func TestRenderKeepsTransportContractAndResultOnlyTaskDistinct(t *testing.T) {
	system := "Synthetic system: answer naturally."
	user := "19+23等于多少？请只给出结果。"
	rendered := render([]provider.Message{{Role: provider.RoleSystem, Content: system}, {Role: provider.RoleUser, Content: user}}, []provider.ToolSchema{{Name: "echo"}}, currentGrammar, true)
	protocol, systemAt, userAt := strings.Index(rendered, protocolInstructions()), strings.Index(rendered, system), strings.Index(rendered, user)
	if protocol < 0 || systemAt <= protocol || userAt <= systemAt {
		t.Fatal("render ordering changed; reassess protocol placement deliberately")
	}
	if strings.Count(rendered, protocolInstructions()) != 1 {
		t.Fatal("missing or duplicated transport contract")
	}
}
