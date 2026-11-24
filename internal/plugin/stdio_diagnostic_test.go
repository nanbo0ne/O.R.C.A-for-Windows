package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const diagnosticMarker = "ORCA_SYNTHETIC_STDERR_LOCAL_ONLY_20261108"
const diagnosticPayload = "stderr: ordinary MCP content " + diagnosticMarker

type diagnosticCapture struct {
	mu      sync.Mutex
	data    bytes.Buffer
	entered chan struct{}
	once    sync.Once
}

func (s *diagnosticCapture) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.data.Write(p)
	if strings.Contains(s.data.String(), diagnosticMarker) {
		s.once.Do(func() { close(s.entered) })
	}
	return n, err
}

func diagnosticFixture(t *testing.T, mode string) (*remoteTool, *stdioTransport, *diagnosticCapture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	capture := &diagnosticCapture{entered: make(chan struct{})}
	host, tools, err := StartAll(ctx, []Spec{{
		Name: "diagnostic", Command: os.Args[0],
		Args: []string{"-test.run=^TestStdioDiagnosticHelper$"},
		Env:  map[string]string{"ORCA_STDIO_DIAGNOSTIC_HELPER": mode}, Stderr: capture,
	}})
	if err != nil {
		t.Fatalf("real stdio handshake failed: %v", err)
	}
	t.Cleanup(host.Close)
	if len(tools) != 1 || tools[0].Name() != "mcp__diagnostic__probe" {
		t.Fatal("real stdio discovery did not return the expected tool")
	}
	remote, ok := tools[0].(*remoteTool)
	if !ok {
		t.Fatal("fixture did not return the production remoteTool")
	}
	host.mu.RLock()
	defer host.mu.RUnlock()
	if len(host.clients) != 1 {
		t.Fatal("fixture host does not own exactly one client")
	}
	tr, ok := host.clients[0].t.(*stdioTransport)
	if !ok {
		t.Fatal("fixture did not use production stdio transport")
	}
	return remote, tr, capture
}

func TestRemoteToolStdioFailureHidesLocalStderr(t *testing.T) {
	remote, tr, _ := diagnosticFixture(t, "exit")
	out, modelErr := remote.Execute(context.Background(), json.RawMessage(`{}`))
	if modelErr == nil || out != "" {
		t.Fatal("tool call must fail after successful handshake and discovery")
	}
	if !strings.Contains(tr.stderr.String(), diagnosticMarker) {
		t.Fatal("local stderr tail lost the diagnostic marker")
	}
	// Local transport callers still receive the diagnostic-rich error.
	_, localErr := tr.call(context.Background(), "tools/call", map[string]any{})
	if localErr == nil || !strings.Contains(localErr.Error(), diagnosticMarker) {
		t.Fatal("local transport error lost the diagnostic marker")
	}
	tr.mu.Lock()
	cause := tr.readErr
	tr.mu.Unlock()
	if cause == nil || !errors.Is(localErr, cause) || !errors.Is(modelErr, cause) {
		t.Fatal("local/model errors lost the original transport cause identity")
	}
	if strings.Contains(modelErr.Error(), diagnosticMarker) {
		t.Fatal("model-facing tools/call error leaked local stderr marker")
	}
}

func TestRemoteToolStdioPayloadsPreserved(t *testing.T) {
	for _, mode := range []string{"success", "tool_error", "rpc_error"} {
		t.Run(mode, func(t *testing.T) {
			remote, _, _ := diagnosticFixture(t, mode)
			out, err := remote.Execute(context.Background(), json.RawMessage(`{}`))
			switch mode {
			case "success":
				if err != nil || out != diagnosticPayload {
					t.Fatalf("normal MCP content changed: %q, %v", out, err)
				}
			case "tool_error":
				if out != diagnosticPayload || err == nil || !strings.Contains(err.Error(), diagnosticPayload) {
					t.Fatal("normal MCP IsError content was filtered")
				}
			case "rpc_error":
				if out != "" || err == nil || !strings.Contains(err.Error(), diagnosticPayload) {
					t.Fatal("normal MCP RPC error was filtered")
				}
			}
		})
	}
}

func TestRemoteToolStdioCancellationIdentity(t *testing.T) {
	remote, _, capture := diagnosticFixture(t, "wait")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := remote.Execute(ctx, json.RawMessage(`{}`))
		done <- err
	}()
	select {
	case <-capture.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("stdio helper never entered tools/call")
	}
	select {
	case <-done:
		t.Fatal("tool call finished before client cancellation")
	default:
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), diagnosticMarker) {
			t.Fatalf("cancellation identity changed: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("client cancellation did not converge")
	}
}

func TestStdioDiagnosticCauseIdentity(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, io.EOF} {
		t.Run(cause.Error(), func(t *testing.T) {
			tail := &tailBuffer{limit: 1024}
			_, _ = tail.Write([]byte(diagnosticMarker))
			tr := &stdioTransport{stderr: tail}
			local := tr.withStderr(fmt.Errorf("original transport cause: %w", cause))
			var diagnostic *stdioDiagnosticError
			if !errors.As(local, &diagnostic) || !errors.Is(local, cause) || !strings.Contains(local.Error(), diagnosticMarker) {
				t.Fatal("typed local diagnostic lost stderr or original cause")
			}
			visible := withoutStdioDiagnostic(fmt.Errorf("local wrapper: %w", local))
			if !errors.Is(visible, cause) || strings.Contains(visible.Error(), diagnosticMarker) {
				t.Fatal("model boundary lost cause identity or retained local stderr")
			}
		})
	}
	plain := errors.New(diagnosticPayload)
	if withoutStdioDiagnostic(plain) != plain || withoutStdioDiagnostic(nil) != nil {
		t.Fatal("untyped MCP errors must not be filtered")
	}
}

// A real subprocess fixture; the parent never passes a real server/config.
func TestStdioDiagnosticHelper(t *testing.T) {
	mode := os.Getenv("ORCA_STDIO_DIAGNOSTIC_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "exit", "success", "tool_error", "rpc_error", "wait":
	default:
		os.Exit(30)
	}
	defer os.Exit(0)
	in := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	for in.Scan() {
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(in.Bytes(), &req) != nil {
			os.Exit(31)
		}
		if req.ID == nil {
			continue
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": protocolVersion, "serverInfo": map[string]string{"name": "diagnostic", "version": "1"}, "capabilities": map[string]any{}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "probe", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			_, _ = os.Stderr.WriteString(diagnosticMarker + "\n")
			if mode == "exit" {
				os.Exit(23)
			}
			if mode == "wait" {
				time.Sleep(500 * time.Millisecond)
			}
			if mode == "rpc_error" {
				_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "error": map[string]any{"code": -32000, "message": diagnosticPayload}})
				continue
			}
			result = map[string]any{"isError": mode == "tool_error", "content": []any{map[string]string{"type": "text", "text": diagnosticPayload}}}
		default:
			os.Exit(32)
		}
		if out.Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}) != nil {
			os.Exit(33)
		}
	}
}
