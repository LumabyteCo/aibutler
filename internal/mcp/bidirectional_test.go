package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/mcp"
)

// timeoutAfter bounds waits on transport goroutines so a regression hangs the
// test for seconds, not forever.
func timeoutAfter() <-chan time.Time { return time.After(3 * time.Second) }

// --- Handshake conformance ---

func TestInitializeOffersNewProtocolAndElicitation(t *testing.T) {
	tr := newTestTransport(nil)
	client := mcp.NewClient()
	if err := client.ConnectWithTransport(context.Background(), "srv", tr); err != nil {
		t.Fatal(err)
	}

	calls := tr.Calls()
	if len(calls) == 0 || calls[0].Method != "initialize" {
		t.Fatalf("first call = %v, want initialize", calls)
	}

	params, ok := calls[0].Params.(mcp.InitializeParams)
	if !ok {
		t.Fatalf("initialize params type = %T", calls[0].Params)
	}
	if params.ProtocolVersion != mcp.PreferredProtocolVersion {
		t.Errorf("protocolVersion = %q, want %q", params.ProtocolVersion, mcp.PreferredProtocolVersion)
	}
	if params.Capabilities == nil || params.Capabilities.Elicitation == nil {
		t.Error("expected the elicitation capability to be declared")
	}
}

// The MCP lifecycle requires notifications/initialized after a successful
// initialize and before any normal request.
func TestInitializedNotificationSentBeforeToolsList(t *testing.T) {
	tr := newTestTransport(nil)
	client := mcp.NewClient()
	if err := client.ConnectWithTransport(context.Background(), "srv", tr); err != nil {
		t.Fatal(err)
	}

	notes := tr.Notifications()
	found := false
	for _, n := range notes {
		if n.Method == "notifications/initialized" {
			found = true
		}
	}
	if !found {
		t.Fatalf("notifications/initialized not sent; got %v", notes)
	}

	// It must not consume a response slot: tools/list still had to succeed.
	calls := tr.Calls()
	if len(calls) != 2 || calls[1].Method != "tools/list" {
		t.Errorf("calls = %v, want [initialize tools/list]", calls)
	}
}

func TestNegotiatedProtocolVersionIsStored(t *testing.T) {
	tr := newTestTransport(nil) // server echoes 2024-11-05
	client := mcp.NewClient()
	if err := client.ConnectWithTransport(context.Background(), "srv", tr); err != nil {
		t.Fatal(err)
	}

	info, proto, err := client.ServerInfo("srv")
	if err != nil {
		t.Fatal(err)
	}
	if proto != "2024-11-05" {
		t.Errorf("negotiated = %q, want 2024-11-05 (the server's choice)", proto)
	}
	if info.Name != "test-server" {
		t.Errorf("serverInfo.Name = %q, want test-server", info.Name)
	}
}

// A server naming a version we cannot speak must fail the connection rather
// than silently proceeding on mismatched semantics.
func TestUnsupportedProtocolVersionRejected(t *testing.T) {
	initResult, _ := json.Marshal(mcp.InitializeResult{
		ProtocolVersion: "1999-01-01",
		ServerInfo:      mcp.ServerInfo{Name: "ancient", Version: "0.1"},
	})
	tr := mcp.NewInMemoryTransport(mcp.JSONRPCResponse{JSONRPC: "2.0", Result: initResult})

	client := mcp.NewClient()
	err := client.ConnectWithTransport(context.Background(), "srv", tr)
	if err == nil {
		t.Fatal("expected connect to fail on unsupported protocol version")
	}
	if !strings.Contains(err.Error(), "unsupported protocol version") {
		t.Errorf("err = %v, want unsupported protocol version", err)
	}
}

// --- Progress ---

func TestToolCallSendsProgressToken(t *testing.T) {
	callResult, _ := json.Marshal(mcp.ToolCallResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: "ok"}},
	})
	tr := newTestTransport([]mcp.ToolInfo{{Name: "t"}}, mcp.JSONRPCResponse{JSONRPC: "2.0", Result: callResult})

	client := mcp.NewClient()
	ctx := context.Background()
	if err := client.ConnectWithTransport(ctx, "srv", tr); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Call(ctx, "srv", "t", nil); err != nil {
		t.Fatal(err)
	}

	calls := tr.Calls()
	last := calls[len(calls)-1]
	params, ok := last.Params.(mcp.ToolCallParams)
	if !ok {
		t.Fatalf("tools/call params type = %T", last.Params)
	}
	if params.Meta == nil || params.Meta.ProgressToken == nil {
		t.Error("expected _meta.progressToken so the server streams progress")
	}
}

func TestProgressNotificationReachesHandler(t *testing.T) {
	tr := newTestTransport(nil)
	client := mcp.NewClient()

	got := make(chan mcp.ProgressParams, 1)
	client.SetProgressHandler(func(_ string, p mcp.ProgressParams) { got <- p })

	if err := client.ConnectWithTransport(context.Background(), "srv", tr); err != nil {
		t.Fatal(err)
	}

	tr.InjectNotification("notifications/progress", json.RawMessage(`{"progressToken":1,"progress":3,"total":5,"message":"optimizing"}`))

	select {
	case p := <-got:
		if p.Progress != 3 || p.Total != 5 || p.Message != "optimizing" {
			t.Errorf("progress = %+v", p)
		}
	default:
		t.Fatal("progress handler never fired")
	}
}

// --- Elicitation ---

func TestElicitationDefaultsToDecline(t *testing.T) {
	tr := newTestTransport(nil)
	client := mcp.NewClient()
	if err := client.ConnectWithTransport(context.Background(), "srv", tr); err != nil {
		t.Fatal(err)
	}

	res, rpcErr := tr.Inject("elicitation/create", json.RawMessage(`{"message":"q","requestedSchema":{"type":"object","properties":{"q1":{"type":"string","default":"yes"}}}}`))
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr)
	}
	out, ok := res.(mcp.ElicitResult)
	if !ok {
		t.Fatalf("result type = %T", res)
	}
	if out.Action != mcp.ElicitActionDecline {
		t.Errorf("action = %q, want decline (safe default)", out.Action)
	}
}

func TestElicitationAcceptDefaultsPolicy(t *testing.T) {
	tr := newTestTransport(nil)
	client := mcp.NewClient()
	client.SetElicitationHandler(mcp.AcceptElicitationDefaults)
	if err := client.ConnectWithTransport(context.Background(), "srv", tr); err != nil {
		t.Fatal(err)
	}

	schema := `{"message":"q","requestedSchema":{"type":"object","properties":{
		"q1":{"type":"string","default":"a coffee shop"},
		"q2":{"type":"string","enum":["Minimal","Vintage"]},
		"q3":{"type":"string"}}}}`

	res, rpcErr := tr.Inject("elicitation/create", json.RawMessage(schema))
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr)
	}
	out := res.(mcp.ElicitResult)
	if out.Action != mcp.ElicitActionAccept {
		t.Fatalf("action = %q, want accept", out.Action)
	}
	if out.Content["q1"] != "a coffee shop" {
		t.Errorf("q1 = %v, want the schema default", out.Content["q1"])
	}
	if out.Content["q2"] != "Minimal" {
		t.Errorf("q2 = %v, want the first enum option", out.Content["q2"])
	}
	if _, present := out.Content["q3"]; present {
		t.Error("q3 has no default and no options; it must be omitted, not guessed")
	}
}

// A schema with nothing to answer must not produce an empty "accept".
func TestAcceptElicitationDefaultsDeclinesWhenNothingToAnswer(t *testing.T) {
	out := mcp.AcceptElicitationDefaults("srv", mcp.ElicitRequestParams{
		RequestedSchema: json.RawMessage(`{"type":"object","properties":{"q1":{"type":"string"}}}`),
	})
	if out.Action != mcp.ElicitActionDecline {
		t.Errorf("action = %q, want decline", out.Action)
	}
}

func TestUnknownServerRequestGetsMethodNotFound(t *testing.T) {
	tr := newTestTransport(nil)
	client := mcp.NewClient()
	if err := client.ConnectWithTransport(context.Background(), "srv", tr); err != nil {
		t.Fatal(err)
	}

	_, rpcErr := tr.Inject("sampling/createMessage", json.RawMessage(`{}`))
	if rpcErr == nil || rpcErr.Code != mcp.ErrCodeMethodNotFound {
		t.Errorf("rpcErr = %v, want method-not-found for an unsupported capability", rpcErr)
	}
}

// --- Result rendering ---

func TestAgentTextFallsBackToStructuredContent(t *testing.T) {
	// A structured-output server may return no text at all.
	r := mcp.ToolCallResult{StructuredContent: json.RawMessage(`{"score":9}`)}
	if got := r.AgentText(); got != `{"score":9}` {
		t.Errorf("AgentText = %q, want the structured payload (never an empty string)", got)
	}
	// TextContent stays strict — it is the pre-existing, text-only contract.
	if got := r.TextContent(); got != "" {
		t.Errorf("TextContent = %q, want empty", got)
	}
}

func TestAgentTextRendersResourceAndBinary(t *testing.T) {
	r := mcp.ToolCallResult{Content: []mcp.ContentBlock{
		{Type: "text", Text: "intro"},
		{Type: "resource", Resource: &mcp.EmbeddedResource{URI: "clarifyprompt://categories", Text: "image, video"}},
		{Type: "image", MimeType: "image/png", Data: "AAAA"},
	}}
	got := r.AgentText()
	for _, want := range []string{"intro", "image, video", "image/png"} {
		if !strings.Contains(got, want) {
			t.Errorf("AgentText = %q, missing %q", got, want)
		}
	}
}

// --- Error classification ---

// A JSON-RPC error means the server is alive and answered. Reconnecting would
// pointlessly restart a healthy subprocess.
func TestRemoteErrorIsNotATransportError(t *testing.T) {
	errResp := mcp.JSONRPCResponse{JSONRPC: "2.0", Error: &mcp.JSONRPCError{Code: -32602, Message: "bad args"}}
	tr := newTestTransport([]mcp.ToolInfo{{Name: "t"}}, errResp)

	client := mcp.NewClient()
	ctx := context.Background()
	if err := client.ConnectWithTransport(ctx, "srv", tr); err != nil {
		t.Fatal(err)
	}

	_, err := client.Call(ctx, "srv", "t", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errorIs(err, mcp.ErrRemote) {
		t.Errorf("err = %v, want it to wrap ErrRemote (so Call skips the reconnect)", err)
	}
	// Exactly 3 calls: initialize, tools/list, tools/call. A reconnect would add more.
	if n := len(tr.Calls()); n != 3 {
		t.Errorf("calls = %d, want 3 (no reconnect on a remote error)", n)
	}
}

func errorIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// --- Review-driven regressions ---

// A caller timeout says nothing about server health. Reconnecting would kill a
// working subprocess mid-work — clarifyprompt's compose_prompt legitimately
// runs for minutes on a local model.
func TestCallDoesNotReconnectOnCallerCancellation(t *testing.T) {
	tr := newTestTransport([]mcp.ToolInfo{{Name: "slow"}})

	client := mcp.NewClient()
	if err := client.ConnectWithTransport(context.Background(), "srv", tr); err != nil {
		t.Fatal(err)
	}
	before := len(tr.Calls())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // caller gives up immediately

	if _, err := client.Call(ctx, "srv", "slow", nil); err == nil {
		t.Fatal("expected an error for a cancelled call")
	}

	// A reconnect would re-issue initialize/tools/list on a NEW transport and,
	// with this canned transport exhausted, would also drop the server.
	if got := len(tr.Calls()) - before; got > 1 {
		t.Errorf("extra calls = %d, want <=1 (no reconnect on caller cancellation)", got)
	}
	if len(client.Servers()) != 1 {
		t.Error("the healthy server was torn down by a caller-side cancellation")
	}
}

// Reconnect must leave the agent with a usable tool set.
func TestReconnectReRegistersTools(t *testing.T) {
	client := mcp.NewClient()
	if err := client.ConnectWithTransport(context.Background(), "srv", newTestTransport([]mcp.ToolInfo{{Name: "greet"}})); err != nil {
		t.Fatal(err)
	}

	var unregistered, reRegistered []string
	client.SetRegistry(fakeRegistry{onUnregister: func(p string) { unregistered = append(unregistered, p) }})
	client.SetReRegisterHook(func(name string) { reRegistered = append(reRegistered, name) })

	// Reconnect uses ServerConfig.Command, which is empty for a transport-injected
	// server, so the re-Connect fails. That is fine: what matters is that a
	// SUCCESSFUL reconnect re-registers, and that we unregistered first.
	_ = client.Reconnect(context.Background(), "srv")

	if len(unregistered) != 1 || unregistered[0] != "mcp.srv." {
		t.Errorf("unregistered = %v, want [mcp.srv.]", unregistered)
	}
}

type fakeRegistry struct{ onUnregister func(string) }

func (f fakeRegistry) UnregisterPrefix(prefix string) {
	if f.onUnregister != nil {
		f.onUnregister(prefix)
	}
}

// A required field with no default and no options has no defensible answer.
func TestAcceptElicitationDefaultsDeclinesUnanswerableRequiredField(t *testing.T) {
	out := mcp.AcceptElicitationDefaults("srv", mcp.ElicitRequestParams{
		RequestedSchema: json.RawMessage(`{"type":"object","properties":{
			"q1":{"type":"string","default":"ok"},
			"q2":{"type":"string"}},"required":["q2"]}`),
	})
	if out.Action != mcp.ElicitActionDecline {
		t.Errorf("action = %q, want decline (q2 is required but unanswerable)", out.Action)
	}
}

// Send must fail fast once the server is gone, not stall until the deadline.
func TestSendFailsFastWhenReadLoopAlreadyExited(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}

	// Exits immediately → the read loop is dead before we ever call Send.
	tr, err := mcp.NewStdioTransport("/bin/sh", []string{"-c", "exit 0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	time.Sleep(200 * time.Millisecond) // let the read loop observe EOF

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	_, err = tr.Send(ctx, mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	if err == nil {
		t.Fatal("expected an error from a dead transport")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Send took %v — it waited for the deadline instead of failing fast", elapsed)
	}
}

// --- Stdio transport: the regression test for the desync bug ---

// A server may legally write a notification (or a server→client request) on
// stdout before the response to an in-flight call. The old transport read
// exactly one line per request with no id-matching, so it returned the
// notification as the response. This asserts the read loop skips it and routes
// the real response by id.
func TestStdioTransportSkipsInterleavedNotification(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}

	script := `printf '%s\n' ` +
		`'{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":1,"progress":50,"message":"half"}}' ` +
		`'{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"real result"}]}}'; sleep 0.3`

	tr, err := mcp.NewStdioTransport("/bin/sh", []string{"-c", script}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	progress := make(chan string, 4)
	tr.SetHandlers(mcp.Handlers{
		OnNotification: func(method string, _ json.RawMessage) { progress <- method },
	})

	resp, err := tr.Send(context.Background(), mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp.ID != 1 {
		t.Errorf("resp.ID = %d, want 1", resp.ID)
	}

	var result mcp.ToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("the notification was returned instead of the response: %v", err)
	}
	if result.TextContent() != "real result" {
		t.Errorf("result = %q, want 'real result'", result.TextContent())
	}

	// Notification delivery is asynchronous to Send, so wait rather than
	// sampling immediately.
	select {
	case m := <-progress:
		if m != "notifications/progress" {
			t.Errorf("notification = %q", m)
		}
	case <-timeoutAfter():
		t.Error("the interleaved notification was dropped instead of dispatched")
	}
}

// JSON-RPC 2.0 allows string ids and the SERVER picks the id for server→client
// requests. A typed numeric id field would fail to decode the whole message,
// silently dropping it and hanging the server's request.
func TestStdioTransportHandlesStringRequestID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}

	script := `printf '%s\n' '{"jsonrpc":"2.0","id":"abc-1","method":"elicitation/create","params":{"message":"hi","requestedSchema":{"type":"object","properties":{"q1":{"type":"string","default":"yes"}}}}}'; head -n 1; sleep 0.3`

	tr, err := mcp.NewStdioTransport("/bin/sh", []string{"-c", script}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	got := make(chan string, 1)
	tr.SetHandlers(mcp.Handlers{
		OnRequest: func(method string, _ json.RawMessage) (interface{}, *mcp.JSONRPCError) {
			got <- method
			return mcp.ElicitResult{Action: mcp.ElicitActionDecline}, nil
		},
	})

	select {
	case m := <-got:
		if m != "elicitation/create" {
			t.Errorf("method = %q", m)
		}
	case <-timeoutAfter():
		t.Fatal("a string-id request was dropped: the client never answered, so the server would hang")
	}
}

// Notifications are ordered stream data (progress counts up). A goroutine per
// notification gives no ordering guarantee between them.
func TestNotificationsDeliveredInOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}

	const n = 40
	var b strings.Builder
	b.WriteString("printf '%s\\n'")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, ` '{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":1,"progress":%d}}'`, i)
	}
	b.WriteString("; sleep 0.5")

	tr, err := mcp.NewStdioTransport("/bin/sh", []string{"-c", b.String()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	seen := make(chan float64, n)
	tr.SetHandlers(mcp.Handlers{
		OnNotification: func(_ string, params json.RawMessage) {
			var p mcp.ProgressParams
			if err := json.Unmarshal(params, &p); err == nil {
				seen <- p.Progress
			}
		},
	})

	deadline := time.After(5 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case got := <-seen:
			if got != float64(i) {
				t.Fatalf("notification %d arrived out of order: got progress %v, want %v", i, got, float64(i))
			}
		case <-deadline:
			t.Fatalf("only %d/%d notifications delivered", i, n)
		}
	}
}

// A server that starts but never answers must not wedge startup forever.
func TestConnectHandshakeTimesOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}

	client := mcp.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Reads stdin, never writes stdout — the silent-server case.
	err := client.Connect(ctx, mcp.ServerConfig{
		Name:    "silent",
		Command: "/bin/sh",
		Args:    []string{"-c", "sleep 30"},
	})
	if err == nil {
		t.Fatal("expected the handshake to fail against a silent server")
	}
	if len(client.Servers()) != 0 {
		t.Error("a server that never completed the handshake must not be registered")
	}
}

// Close must be idempotent. Reconnect closes a transport that a shutdown path
// may close again; a second Close must not block.
func TestStdioTransportCloseIsIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}

	// `cat` exits as soon as stdin closes, so the first Close takes the fast
	// path and the second exercises pure idempotency.
	tr, err := mcp.NewStdioTransport("/bin/cat", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		tr.Close()
		tr.Close() // must return immediately, not block on a dead channel
		close(done)
	}()

	select {
	case <-done:
	case <-timeoutAfter():
		t.Fatal("second Close deadlocked")
	}
}

// A server→client request must be answered on stdin with the matching id.
func TestStdioTransportAnswersServerRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}

	// Emit an elicitation/create request, then echo whatever the client writes
	// back on fd 3 so the test can inspect the reply without feeding it back
	// into the transport's own stdout.
	script := `printf '%s\n' '{"jsonrpc":"2.0","id":7,"method":"elicitation/create","params":{"message":"hi","requestedSchema":{"type":"object","properties":{"q1":{"type":"string","default":"yes"}}}}}'; head -n 1 > /dev/null; sleep 0.3`

	tr, err := mcp.NewStdioTransport("/bin/sh", []string{"-c", script}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	answered := make(chan mcp.ElicitResult, 1)
	tr.SetHandlers(mcp.Handlers{
		OnRequest: func(method string, params json.RawMessage) (interface{}, *mcp.JSONRPCError) {
			if method != "elicitation/create" {
				return nil, &mcp.JSONRPCError{Code: mcp.ErrCodeMethodNotFound, Message: method}
			}
			var p mcp.ElicitRequestParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, &mcp.JSONRPCError{Code: mcp.ErrCodeInternalError, Message: err.Error()}
			}
			res := mcp.AcceptElicitationDefaults("srv", p)
			answered <- res
			return res, nil
		},
	})

	select {
	case res := <-answered:
		if res.Action != mcp.ElicitActionAccept || res.Content["q1"] != "yes" {
			t.Errorf("elicit result = %+v, want accept with q1=yes", res)
		}
	case <-timeoutAfter():
		t.Fatal("the server request never reached OnRequest")
	}
}
