package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Handlers receives peer-initiated traffic that arrives while the transport is
// open. Both are optional; a nil hook means "ignore that traffic".
type Handlers struct {
	// OnRequest answers a server→client request (e.g. elicitation/create).
	// Returning a non-nil *JSONRPCError sends an error response instead.
	OnRequest func(method string, params json.RawMessage) (interface{}, *JSONRPCError)
	// OnNotification handles a server→client notification (e.g. notifications/progress).
	OnNotification func(method string, params json.RawMessage)
}

// Transport is the communication layer to an MCP server.
type Transport interface {
	// Send issues a request and blocks until the response with the matching id arrives.
	Send(ctx context.Context, req JSONRPCRequest) (JSONRPCResponse, error)
	// Notify sends a notification (no id, no response expected).
	Notify(ctx context.Context, method string, params interface{}) error
	// SetHandlers installs hooks for peer-initiated traffic.
	SetHandlers(h Handlers)
	Close() error
}

// ErrTransportClosed is returned to callers waiting on a response when the
// transport shuts down or the server exits.
var ErrTransportClosed = errors.New("mcp: transport closed")

// StdioTransport communicates with an MCP server via stdin/stdout JSON-RPC.
//
// A single background read loop owns stdout and demultiplexes the stream:
// responses are routed to the waiting caller by JSON-RPC id, while
// server-initiated requests and notifications are dispatched to Handlers. This
// is what makes bidirectional features (elicitation, progress) possible — a
// naive write-one/read-one transport would mistake an interleaved
// server→client message for the response to its own request.
type StdioTransport struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	stdin  io.WriteCloser
	stdout *bufio.Reader

	writeMu sync.Mutex // serializes writes to stdin

	mu      sync.Mutex
	pending map[int]chan JSONRPCResponse
	handler Handlers
	closed  bool

	// closeOnce makes Close idempotent: it must guard the whole shutdown, not
	// just cmd.Wait. Guarding only the Wait goroutine would leave a second
	// Close waiting on a channel nobody ever writes to.
	closeOnce sync.Once
	closeErr  error

	notif chan notification // serialized notification delivery

	done    chan struct{} // closed when the read loop exits
	readErr error         // why the read loop exited (guarded by mu)
}

// notification is one inbound server→client notification awaiting delivery.
type notification struct {
	method string
	params json.RawMessage
}

// notifyBuffer bounds how far notification delivery may lag the read loop.
// Handlers are expected not to block; if one does, backpressure stalls the
// stream rather than spawning unbounded goroutines or reordering updates.
const notifyBuffer = 256

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

	t := &StdioTransport{
		cmd:     cmd,
		cancel:  cancel,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		pending: make(map[int]chan JSONRPCResponse),
		notif:   make(chan notification, notifyBuffer),
		done:    make(chan struct{}),
	}

	go t.readLoop()
	go t.notifyLoop()

	return t, nil
}

// SetHandlers installs hooks for peer-initiated traffic.
func (t *StdioTransport) SetHandlers(h Handlers) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handler = h
}

// readLoop owns stdout for the life of the transport.
func (t *StdioTransport) readLoop() {
	defer close(t.done)
	// Closing notif retires the notify worker once queued notifications drain.
	// Safe because dispatch — the only sender — runs on this goroutine.
	defer close(t.notif)

	for {
		line, err := t.stdout.ReadBytes('\n')
		if len(line) > 0 {
			t.dispatch(line)
		}
		if err != nil {
			t.mu.Lock()
			t.readErr = err
			// Wake every caller still waiting for a response.
			for id, ch := range t.pending {
				close(ch)
				delete(t.pending, id)
			}
			t.mu.Unlock()
			return
		}
	}
}

// dispatch classifies one inbound line and routes it.
func (t *StdioTransport) dispatch(line []byte) {
	var env rpcEnvelope
	if err := json.Unmarshal(line, &env); err != nil {
		// Not JSON-RPC. Well-behaved servers keep stdout clean and log to
		// stderr, but a stray line must not kill the session.
		return
	}

	switch {
	case env.Method != "" && env.hasID():
		// Server→client request. Answer it off the read loop so handler work
		// never blocks the stream. Requests are id-matched, so serving them
		// concurrently is safe.
		go t.serveRequest(env.ID, env.Method, env.Params)

	case env.Method != "":
		// Notification: must not be answered. Hand it to the single notify
		// worker rather than a goroutine per message — notifications are
		// ordered stream data (progress counts up), and goroutines have no
		// ordering guarantee between them.
		t.notif <- notification{method: env.Method, params: env.Params}

	case env.hasID():
		// Response to one of our requests — route by id. A non-numeric id is
		// never ours (we only send ints), so it is ignored rather than
		// mis-routed.
		id, ok := env.intID()
		if !ok {
			return
		}
		t.mu.Lock()
		ch, found := t.pending[id]
		if found {
			delete(t.pending, id)
		}
		t.mu.Unlock()
		if !found {
			// Late or unknown id (e.g. a call we already gave up on).
			return
		}
		ch <- JSONRPCResponse{
			JSONRPC: env.JSONRPC,
			ID:      id,
			Result:  env.Result,
			Error:   env.Error,
		}
		close(ch)
	}
}

// notifyLoop delivers notifications one at a time, in arrival order.
func (t *StdioTransport) notifyLoop() {
	for n := range t.notif {
		t.mu.Lock()
		h := t.handler.OnNotification
		t.mu.Unlock()
		if h != nil {
			h(n.method, n.params)
		}
	}
}

// serveRequest runs the OnRequest hook and writes the JSON-RPC reply, echoing
// the request's id bytes verbatim.
func (t *StdioTransport) serveRequest(id json.RawMessage, method string, params json.RawMessage) {
	t.mu.Lock()
	h := t.handler.OnRequest
	t.mu.Unlock()

	resp := rawResponse{JSONRPC: "2.0", ID: id}

	if h == nil {
		resp.Error = &JSONRPCError{Code: ErrCodeMethodNotFound, Message: "method not supported by client: " + method}
	} else if result, rpcErr := h(method, params); rpcErr != nil {
		resp.Error = rpcErr
	} else {
		encoded, err := json.Marshal(result)
		if err != nil {
			resp.Error = &JSONRPCError{Code: ErrCodeInternalError, Message: "marshal result: " + err.Error()}
		} else {
			resp.Result = encoded
		}
	}

	if err := t.writeJSON(resp); err != nil {
		// Nothing useful to do: the peer is gone and the read loop will notice.
		return
	}
}

// writeJSON marshals v and writes it as one newline-delimited frame.
func (t *StdioTransport) writeJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("mcp: marshal: %w", err)
	}
	data = append(data, '\n')

	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if _, err := t.stdin.Write(data); err != nil {
		return fmt.Errorf("mcp: write: %w", err)
	}
	return nil
}

// Send issues a request and waits for the response with the matching id.
func (t *StdioTransport) Send(ctx context.Context, req JSONRPCRequest) (JSONRPCResponse, error) {
	ch := make(chan JSONRPCResponse, 1)

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return JSONRPCResponse{}, ErrTransportClosed
	}
	t.pending[req.ID] = ch
	t.mu.Unlock()

	unregister := func() {
		t.mu.Lock()
		delete(t.pending, req.ID)
		t.mu.Unlock()
	}

	if err := t.writeJSON(req); err != nil {
		unregister()
		return JSONRPCResponse{}, fmt.Errorf("mcp: write request: %w", err)
	}

	select {
	case <-ctx.Done():
		unregister()
		return JSONRPCResponse{}, fmt.Errorf("mcp: request cancelled: %w", ctx.Err())

	case <-t.done:
		// The read loop already exited, so nobody will ever answer this
		// request. Without this case the caller would block until its deadline
		// waiting on a channel that cannot be delivered to.
		unregister()
		t.mu.Lock()
		err := t.readErr
		t.mu.Unlock()
		if err == nil {
			err = ErrTransportClosed
		}
		return JSONRPCResponse{}, fmt.Errorf("mcp: read response: %w", err)

	case resp, ok := <-ch:
		if !ok {
			// Channel closed without a value: the read loop died.
			t.mu.Lock()
			err := t.readErr
			t.mu.Unlock()
			if err == nil {
				err = ErrTransportClosed
			}
			return JSONRPCResponse{}, fmt.Errorf("mcp: read response: %w", err)
		}
		return resp, nil
	}
}

// Notify sends a notification. It does not wait for (or expect) a response.
func (t *StdioTransport) Notify(_ context.Context, method string, params interface{}) error {
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return ErrTransportClosed
	}
	return t.writeJSON(JSONRPCNotification{JSONRPC: "2.0", Method: method, Params: params})
}

// Close shuts down the subprocess gracefully with a 5s timeout.
//
// It is idempotent and safe to call concurrently: Reconnect closes a transport
// that a shutdown path may also close.
func (t *StdioTransport) Close() error {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closed = true
		t.mu.Unlock()

		t.stdin.Close()

		done := make(chan error, 1)
		go func() { done <- t.cmd.Wait() }()

		select {
		case err := <-done:
			t.cancel()
			t.closeErr = err
		case <-time.After(5 * time.Second):
			t.cancel() // cancel context to signal the process
			if t.cmd.Process != nil {
				t.cmd.Process.Kill()
			}
			t.closeErr = <-done
		}
	})
	return t.closeErr
}

// InMemoryTransport is a test double that returns canned responses in order.
//
// Notifications are recorded but never consume a canned response, so a test's
// response list stays aligned with its requests even as the client adds
// lifecycle notifications such as notifications/initialized.
type InMemoryTransport struct {
	mu            sync.Mutex
	responses     []JSONRPCResponse
	calls         []JSONRPCRequest
	notifications []JSONRPCNotification
	handler       Handlers
	idx           int
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

// Notifications returns all notifications sent through this transport.
func (t *InMemoryTransport) Notifications() []JSONRPCNotification {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]JSONRPCNotification, len(t.notifications))
	copy(out, t.notifications)
	return out
}

// SetHandlers installs hooks for peer-initiated traffic.
func (t *InMemoryTransport) SetHandlers(h Handlers) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handler = h
}

// Inject simulates a server-initiated request, returning the client's reply.
// It lets tests exercise the elicitation path without a real subprocess.
func (t *InMemoryTransport) Inject(method string, params json.RawMessage) (interface{}, *JSONRPCError) {
	t.mu.Lock()
	h := t.handler.OnRequest
	t.mu.Unlock()
	if h == nil {
		return nil, &JSONRPCError{Code: ErrCodeMethodNotFound, Message: "no handler"}
	}
	return h(method, params)
}

// InjectNotification simulates a server-initiated notification.
func (t *InMemoryTransport) InjectNotification(method string, params json.RawMessage) {
	t.mu.Lock()
	h := t.handler.OnNotification
	t.mu.Unlock()
	if h != nil {
		h(method, params)
	}
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

func (t *InMemoryTransport) Notify(_ context.Context, method string, params interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.notifications = append(t.notifications, JSONRPCNotification{JSONRPC: "2.0", Method: method, Params: params})
	return nil
}

func (t *InMemoryTransport) Close() error { return nil }
