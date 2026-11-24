package webbeta

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

func init() {
	provider.Register("deepseekweb-beta", func(c provider.Config) (provider.Provider, error) { return &Adapter{name: c.Name}, nil })
}

type Adapter struct{ name string }

func (a *Adapter) Name() string { return a.name }
func (a *Adapter) Stream(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	startupCtx, stopStartup := context.WithTimeout(ctx, 8*time.Second)
	e, err := engineForRequest(startupCtx)
	stopStartup()
	if err != nil {
		return nil, err
	}
	owner, err := requestOwner(r)
	if err != nil {
		return nil, err
	}
	if r.ParentTaskID == "" {
		return nil, errors.New("web request has no host parent task identity")
	}
	finish, err := e.beginOperation(owner)
	if err != nil {
		return nil, err
	}
	scope, _ := ctx.Value(taskScopeKey{}).(*taskScope)
	if scope != nil && scope.parentTaskID != r.ParentTaskID {
		finish()
		return nil, errors.New("web request parent task does not match its task scope")
	}
	releaseStream, err := e.acquireStream(owner, scope)
	if err != nil {
		finish()
		return nil, err
	}
	ctx, cancelRequest := e.requestContext(ctx)
	ctx, cancelTask, err := e.taskContext(ctx, r.ParentTaskID)
	if err != nil {
		cancelRequest()
		releaseStream()
		finish()
		return nil, err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			releaseStream()
			cancelTask()
			cancelRequest()
			finish()
		}
	}()
	waitCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	err = WaitUntilAvailable(waitCtx, e)
	cancel()
	if err != nil {
		return nil, err
	}
	req := Request{Owner: owner, ID: r.RequestID, TaskID: r.ParentTaskID, Epoch: r.ContextEpoch, Messages: r.Messages, Tools: r.Tools, DisableThinking: r.DisableThinking, Token: newID()}
	defer func() {
		if !handedOff {
			e.Vault().Release(req.Owner, req.ID)
		}
	}()
	start, fresh := e.AttachmentStart(req)
	for index, m := range r.Messages {
		if index < start {
			continue
		}
		if fresh && index < len(r.Messages)-1 && (len(m.Images) > 0 || len(m.Files) > 0) {
			return nil, errors.New("new web context requires explicit reattachment of historical files/images")
		}
		for _, im := range m.Images {
			if im.Data == "" {
				return nil, errors.New("image snapshot is not authorized or hydrated")
			}
			data, err := base64.StdEncoding.DecodeString(im.Data)
			if err != nil {
				return nil, err
			}
			id, err := e.Vault().Grant(req.Owner, req.ID, im.Name, data)
			if err != nil {
				e.Vault().Release(req.Owner, req.ID)
				return nil, err
			}
			req.Attachments = append(req.Attachments, id)
		}
		for _, file := range m.Files {
			if len(file.Data) == 0 {
				e.Vault().Release(req.Owner, req.ID)
				return nil, errors.New("file snapshot unavailable; reattach the file before sending")
			}
			id, err := e.Vault().Grant(req.Owner, req.ID, file.Name, file.Data)
			if err != nil {
				e.Vault().Release(req.Owner, req.ID)
				return nil, err
			}
			req.Attachments = append(req.Attachments, id)
		}
	}
	// One pending chunk lets a cancelled producer deliver a terminal failure
	// without holding identity teardown hostage to an abandoned consumer.
	out := make(chan provider.Chunk, 1)
	handedOff = true
	go func() {
		defer releaseStream()
		defer cancelTask()
		defer finish()
		defer cancelRequest()
		defer close(out)
		defer e.Vault().Release(req.Owner, req.ID)
		terminalDelivered := false
		var terminalErr error
		send := func(c provider.Chunk) error {
			// These errors leave a dispatched/acknowledged webpage operation.
			// Carry the policy across Agent continuation and compaction fallback,
			// even if the caller changes its request ID or auxiliary owner.
			if c.Type == provider.ChunkError {
				c.Err = provider.NoRequestReplay(c.Err)
				terminalErr = c.Err
			}
			if e.ctx.Err() != nil {
				return ErrIdentityClosed
			}
			select {
			case out <- c:
				if c.Type == provider.ChunkDone || c.Type == provider.ChunkError {
					terminalDelivered = true
				}
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		defer func() {
			if terminalDelivered {
				return
			}
			if terminalErr == nil {
				switch {
				case e.ctx.Err() != nil:
					terminalErr = ErrIdentityClosed
				case ctx.Err() != nil:
					terminalErr = ctx.Err()
				default:
					terminalErr = errors.New("web stream ended without a confirmed terminal result")
				}
			}
			failure := provider.Chunk{Type: provider.ChunkError, Err: provider.NoRequestReplay(terminalErr)}
			select {
			case out <- failure:
			default:
				// Only this goroutine produces chunks. Replacing one undelivered
				// chunk leaves a guaranteed slot for the error, never a success EOF.
				select {
				case <-out:
				default:
				}
				out <- failure
			}
		}()
		var body strings.Builder
		textChunks, reasoningChunks := 0, 0
		grammar := e.protocolFor(req)
		framed := grammar == latestGrammar || (len(r.Tools) > 0 && grammar == currentGrammar)
		progressSent := ""
		prefixPending := ""
		mode := "pending"
		err := e.Send(ctx, req, func(ev Event) error {
			switch ev.Kind {
			case "reasoning":
				reasoningChunks++
				return send(provider.Chunk{Type: provider.ChunkReasoning, Text: ev.Text})
			case "text":
				textChunks++
				body.WriteString(ev.Text)
				if framed {
					var progress string
					var parseErr error
					if grammar == latestGrammar {
						progress, _, parseErr = v3Preamble(body.String())
					} else {
						progress, _, parseErr = progressPrefix(body.String())
					}
					if parseErr != nil {
						return nil
					}
					if strings.HasPrefix(progress, progressSent) && len(progress) > len(progressSent) {
						delta := progress[len(progressSent):]
						progressSent = progress
						return send(provider.Chunk{Type: provider.ChunkText, Text: delta})
					}
					return nil
				}
				if mode == "tool" || mode == "json" {
					return nil
				}
				if mode == "text" {
					return send(provider.Chunk{Type: provider.ChunkText, Text: ev.Text})
				}
				prefixPending += ev.Text
				trimmed := strings.TrimLeft(prefixPending, " \n\r\t")
				if len(r.Tools) > 0 && strings.HasPrefix(trimmed, "{") {
					mode = "json"
					return nil
				}
				for _, marker := range []string{toolMarker(0), toolMarker(1)} {
					if strings.HasPrefix(marker, trimmed) {
						return nil
					}
					if strings.HasPrefix(trimmed, marker) {
						mode = "tool"
						return nil
					}
				}
				mode = "text"
				return send(provider.Chunk{Type: provider.ChunkText, Text: prefixPending})
			case "done":
				return nil
			}
			return nil
		})
		if err != nil {
			_ = send(provider.Chunk{Type: provider.ChunkError, Err: err})
			return
		}
		if len(r.Tools) == 0 && strings.TrimSpace(body.String()) == "" {
			_ = send(provider.Chunk{Type: provider.ChunkError, Err: errors.New("网页已结束但未返回有效正文；不会自动重发，请检查网页后再决定是否重试。")})
			return
		}
		if framed {
			var turn decodedTurn
			var parseErr error
			if grammar == latestGrammar {
				turn, parseErr = parseTurnV3(body.String(), req.Token, r.Tools, r.Messages)
			} else {
				turn, parseErr = parseTurn(body.String(), r.Tools, r.Messages, grammar)
			}
			diagnostic := ProtocolDiagnosticError{Stage: "confirmed_response", Original: responseShape(body.String()), OriginalCode: protocolErrorCode(parseErr), TextChunks: textChunks, ReasoningChunks: reasoningChunks}
			repairable := repairableTurn(body.String(), r.Tools, r.Messages)
			if grammar == latestGrammar {
				repairable = repairableTurnV3(body.String())
			}
			plainText := grammar == latestGrammar && !repairable && strings.TrimSpace(body.String()) != ""
			if parseErr != nil && !errors.Is(parseErr, errToolReplay) && (repairable || plainText) {
				diagnostic.Corrections = 1
				fixRequest := req
				fixRequest.Token = newID()
				fixRequest.CorrectionFinalOnly = (plainText && len(r.Tools) == 0) || (grammar == latestGrammar && v3FinalCandidate(body.String()))
				if errors.Is(parseErr, errNativeMutation) {
					fixRequest.CorrectionHint = "native_write"
				} else if errors.Is(parseErr, errV3RawArgument) {
					fixRequest.CorrectionHint = "raw_field"
				}
				corrected, fixErr := e.correctToolFormat(ctx, fixRequest)
				if fixErr != nil {
					diagnostic.Stage, diagnostic.Cause = "correction_failed", fixErr
					_ = send(provider.Chunk{Type: provider.ChunkError, Err: &diagnostic})
					return
				}
				shape := responseShape(corrected)
				diagnostic.Correction, diagnostic.Stage = &shape, "confirmed_correction"
				if grammar == latestGrammar {
					turn, parseErr = parseTurnV3(corrected, fixRequest.Token, r.Tools, r.Messages)
					if parseErr == nil && fixRequest.CorrectionFinalOnly && (turn.Final == "" || len(turn.Calls) != 0) {
						parseErr = errors.New("plain final correction cannot request tools")
					}
				} else {
					turn, parseErr = parseTurn(corrected, r.Tools, r.Messages, grammar)
				}
				// Progress already delivered cannot be erased or replayed by a repair.
				if !strings.HasPrefix(turn.Progress, progressSent) {
					turn.Progress = progressSent + turn.Progress
				}
			}
			if parseErr != nil {
				diagnostic.Cause = parseErr
				_ = send(provider.Chunk{Type: provider.ChunkError, Err: &diagnostic})
				return
			}
			if parseErr == nil {
				parseErr = e.recordTurn(req, turn)
			}
			if parseErr != nil {
				_ = send(provider.Chunk{Type: provider.ChunkError, Err: parseErr})
				return
			}
			if len(turn.Progress) > len(progressSent) {
				if send(provider.Chunk{Type: provider.ChunkText, Text: turn.Progress[len(progressSent):]}) != nil {
					return
				}
			}
			if turn.Final != "" && send(provider.Chunk{Type: provider.ChunkText, Text: turn.Final}) != nil {
				return
			}
			for i := range turn.Calls {
				if send(provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &turn.Calls[i]}) != nil {
					return
				}
			}
			releaseStream()
			_ = send(provider.Chunk{Type: provider.ChunkDone})
			return
		}
		candidate := mode == "tool"
		if mode == "json" {
			// A bare JSON answer is not permission to execute a tool. Ask for the
			// declared envelope only when the entire object is a valid tool shape.
			_, shapeErr := parseTool(toolMarker(0)+" "+body.String(), r.Tools, r.Messages)
			candidate = shapeErr == nil
		}
		if candidate {
			call, parseErr := parseTool(body.String(), r.Tools, r.Messages)
			err = parseErr
			if parseErr != nil && !errors.Is(parseErr, errToolReplay) {
				var corrected string
				corrected, err = e.correctToolFormat(ctx, req)
				if err == nil {
					call, err = parseTool(corrected, r.Tools, r.Messages)
				}
			}
			if err != nil {
				_ = send(provider.Chunk{Type: provider.ChunkError, Err: err})
				return
			}
			if err = e.recordTurn(req, decodedTurn{Calls: []provider.ToolCall{*call}}); err != nil {
				_ = send(provider.Chunk{Type: provider.ChunkError, Err: err})
				return
			}
			if err = send(provider.Chunk{Type: provider.ChunkToolCall, ToolCall: call}); err != nil {
				return
			}
		} else if mode == "pending" || mode == "json" {
			if err = send(provider.Chunk{Type: provider.ChunkText, Text: body.String()}); err != nil {
				return
			}
		}
		releaseStream()
		_ = send(provider.Chunk{Type: provider.ChunkDone})
	}()
	return out, nil
}

// Wait only for an enabled browser to load; never send, authorize or recover.
func WaitUntilAvailable(ctx context.Context, e *Engine) error {
	if e == nil {
		return AvailabilityError(e)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		s := e.Status()
		if s.Stopping || !s.Settings.Enabled || s.Blocked != "" || s.Browser.Blocked != "" || s.Browser.Ready || s.Browser.Busy {
			return AvailabilityError(e)
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return errors.New("网页仍在后台加载，请稍后重试；也可打开反向代理（Beta）查看登录或网络状态。消息尚未发送。")
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func AvailabilityError(e *Engine) error {
	if e == nil {
		return errors.New("网页接入尚未打开。请在“反向代理（Beta）”中确认登录并启用网页接入，然后重新发送。")
	}
	s := e.Status()
	if s.Stopping {
		return ErrTaskDraining
	}
	if s.Blocked != "" || s.Browser.Blocked != "" {
		return errors.New("网页接入已暂停。请打开“反向代理（Beta）”处理官网提示，再重新检查网页；不会自动重发。")
	}
	if !s.Settings.Enabled {
		return errors.New("网页接入尚未启用。已登录不等于已授权 Agent 发送，请在“反向代理（Beta）”点击“启用网页接入”。")
	}
	if !s.Browser.Ready && !s.Browser.Busy {
		return errors.New("网页正在连接或需要登录。请打开“反向代理（Beta）”确认页面就绪后重新发送。")
	}
	return nil
}
func requestOwner(r provider.Request) (string, error) {
	if r.RequestID == "" {
		return "", errors.New("web request is missing its operation identity")
	}
	// Auxiliary requests never append classifiers or summaries to a live task.
	// Only known, host-tagged requests get a separate, single-purpose binding.
	switch r.Purpose {
	case provider.RequestPurposeClassifier, provider.RequestPurposeCompaction, provider.RequestPurposeCompactionFallback:
		if len(r.Tools) != 0 {
			return "", errors.New("auxiliary web request must not expose tools")
		}
		return "internal:" + string(r.Purpose) + ":" + r.RequestID, nil
	default:
		if r.ConversationID != "" {
			return r.ConversationID, nil
		}
		return "", errors.New("web request has no conversation identity")
	}
}
func parseTool(text string, tools []provider.ToolSchema, history []provider.Message) (*provider.ToolCall, error) {
	raw := strings.TrimSpace(text)
	matched := false
	for _, marker := range []string{toolMarker(0), toolMarker(1)} {
		if strings.HasPrefix(raw, marker+" ") || strings.HasPrefix(raw, marker+"\n") {
			raw = strings.TrimSpace(strings.TrimPrefix(raw, marker))
			matched = true
			break
		}
	}
	if !matched {
		return nil, errors.New("invalid tool envelope")
	}
	if err := uniqueJSONKeys(raw); err != nil {
		return nil, err
	}
	var call struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&call); err != nil {
		return nil, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, errors.New("multiple tool payloads are not allowed")
	}
	if !json.Valid([]byte(raw)) || call.ID == "" || len(call.ID) > 128 || len(call.Arguments) == 0 || call.Arguments[0] != '{' {
		return nil, errors.New("invalid tool call")
	}
	known := false
	var parameters json.RawMessage
	for _, tool := range tools {
		if tool.Name == call.Name {
			known = true
			parameters = tool.Parameters
		}
	}
	if !known {
		return nil, fmt.Errorf("unavailable tool %q", call.Name)
	}
	if len(parameters) == 0 {
		parameters = json.RawMessage(`{"type":"object"}`)
	}
	compiler := jsonschema.NewCompiler()
	compiler.LoadURL = func(string) (io.ReadCloser, error) {
		return nil, errors.New("external schema references are not allowed")
	}
	if err := compiler.AddResource("https://orca.invalid/tool.json", strings.NewReader(string(parameters))); err != nil {
		return nil, errors.New("invalid tool schema")
	}
	schema, err := compiler.Compile("https://orca.invalid/tool.json")
	if err != nil {
		return nil, errors.New("tool schema cannot be compiled")
	}
	var arguments any
	decoder := json.NewDecoder(strings.NewReader(string(call.Arguments)))
	decoder.UseNumber()
	if err = decoder.Decode(&arguments); err != nil {
		return nil, errors.New("invalid tool arguments")
	}
	if err = schema.Validate(arguments); err != nil {
		return nil, errors.New("tool arguments do not match the declared schema")
	}
	for _, m := range history {
		for _, old := range m.ToolCalls {
			if old.ID == call.ID {
				return nil, errToolReplay
			}
		}
	}
	return &provider.ToolCall{ID: call.ID, Name: call.Name, Arguments: string(call.Arguments)}, nil
}
