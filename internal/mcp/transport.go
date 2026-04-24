package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Transport is the communication layer to an MCP server.
type Transport interface {
	Send(ctx context.Context, req JSONRPCRequest) (JSONRPCResponse, error)
	Close() error
}

// StdioTransport communicates with an MCP server via stdin/stdout JSON-RPC.
type StdioTransport struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
}

// NewStdioTransport starts a subprocess and returns a transport.
// The subprocess is bound to a cancellable context so Close() can signal it.
func NewStdioTransport(command string, args []string, env []string) (*StdioTransport, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, command, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("mcp: stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("mcp: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("mcp: start %s: %w", command, err)
	}

	return &StdioTransport{
		cmd:    cmd,
		cancel: cancel,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}, nil
}

func (t *StdioTransport) Send(ctx context.Context, req JSONRPCRequest) (JSONRPCResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		return JSONRPCResponse{}, fmt.Errorf("mcp: marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := t.stdin.Write(data); err != nil {
		return JSONRPCResponse{}, fmt.Errorf("mcp: write request: %w", err)
	}

	// Read with context cancellation so hanging servers don't block forever.
	type readResult struct {
		line []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, readErr := t.stdout.ReadBytes('\n')
		ch <- readResult{line, readErr}
	}()

	select {
	case <-ctx.Done():
		return JSONRPCResponse{}, fmt.Errorf("mcp: read cancelled: %w", ctx.Err())
	case res := <-ch:
		if res.err != nil {
			return JSONRPCResponse{}, fmt.Errorf("mcp: read response: %w", res.err)
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal(res.line, &resp); err != nil {
			return JSONRPCResponse{}, fmt.Errorf("mcp: unmarshal response: %w", err)
		}

		return resp, nil
	}
}

// Close shuts down the subprocess gracefully with a 5s timeout.
func (t *StdioTransport) Close() error {
	t.stdin.Close()

	done := make(chan error, 1)
	go func() { done <- t.cmd.Wait() }()

	select {
	case err := <-done:
		t.cancel()
		return err
	case <-time.After(5 * time.Second):
		t.cancel() // cancel context to signal the process
		t.cmd.Process.Kill()
		return <-done
	}
}

// InMemoryTransport is a test double that returns canned responses.
type InMemoryTransport struct {
	mu        sync.Mutex
	responses []JSONRPCResponse
	calls     []JSONRPCRequest
	idx       int
}

// NewInMemoryTransport creates a transport with pre-configured responses.
func NewInMemoryTransport(responses ...JSONRPCResponse) *InMemoryTransport {
	return &InMemoryTransport{responses: responses}
}

// Calls returns all requests sent through this transport.
func (t *InMemoryTransport) Calls() []JSONRPCRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]JSONRPCRequest, len(t.calls))
	copy(out, t.calls)
	return out
}

func (t *InMemoryTransport) Send(_ context.Context, req JSONRPCRequest) (JSONRPCResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, req)

	if t.idx >= len(t.responses) {
		return JSONRPCResponse{}, fmt.Errorf("mcp: no more canned responses (got %d calls)", len(t.calls))
	}

	resp := t.responses[t.idx]
	resp.ID = req.ID
	t.idx++
	return resp, nil
}

func (t *InMemoryTransport) Close() error { return nil }
