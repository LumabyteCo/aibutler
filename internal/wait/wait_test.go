package wait

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type mockRegistry struct {
	tools []string
	exec  map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{exec: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, _, _, _ string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools = append(m.tools, name)
	m.exec[name] = exec
}

func TestRegisterTool(t *testing.T) {
	reg := newMockRegistry()
	RegisterTool(reg, NewWaiter())
	if _, ok := reg.exec["wait.until"]; !ok {
		t.Fatal("wait.until not registered")
	}
}

func TestUntil_FileExists_Immediate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exists.txt")
	if err := os.WriteFile(path, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}

	w := NewWaiter()
	res := w.Until(context.Background(), Input{
		Type:           TypeFileExists,
		Path:           path,
		TimeoutSeconds: 5,
		PollIntervalMS: 50,
	})
	if !res.Satisfied {
		t.Errorf("expected satisfied=true, got %+v", res)
	}
	if res.Checks != 1 {
		t.Errorf("expected 1 check (file already exists), got %d", res.Checks)
	}
}

func TestUntil_FileExists_AppearsLater(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "later.txt")

	// Create the file after a short delay.
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = os.WriteFile(path, []byte("ok"), 0o600)
	}()

	w := NewWaiter()
	res := w.Until(context.Background(), Input{
		Type:           TypeFileExists,
		Path:           path,
		TimeoutSeconds: 3,
		PollIntervalMS: 50,
	})
	if !res.Satisfied {
		t.Errorf("expected satisfied=true, got %+v", res)
	}
	if res.Checks < 2 {
		t.Errorf("expected multiple polls, got %d", res.Checks)
	}
	if res.ElapsedMS < 100 {
		t.Errorf("expected elapsed >= ~150ms (file appeared after delay), got %dms", res.ElapsedMS)
	}
}

func TestUntil_FileExists_Timeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "never.txt")

	w := NewWaiter()
	res := w.Until(context.Background(), Input{
		Type:           TypeFileExists,
		Path:           path,
		TimeoutSeconds: 0.3, // 300ms total
		PollIntervalMS: 50,
	})
	if res.Satisfied {
		t.Errorf("expected satisfied=false (timeout), got %+v", res)
	}
	if !strings.Contains(res.Reason, "timeout") {
		t.Errorf("expected reason to mention timeout, got %q", res.Reason)
	}
	if res.LastStatus != "file not found" {
		t.Errorf("expected last_status='file not found', got %q", res.LastStatus)
	}
}

func TestUntil_FileExists_MissingPath(t *testing.T) {
	w := NewWaiter()
	res := w.Until(context.Background(), Input{
		Type:           TypeFileExists,
		TimeoutSeconds: 0.2,
		PollIntervalMS: 50,
	})
	if res.Satisfied {
		t.Errorf("expected satisfied=false when path is empty, got %+v", res)
	}
	if !strings.Contains(res.LastStatus, "path is required") {
		t.Errorf("expected last_status to mention missing path, got %q", res.LastStatus)
	}
}

func TestUntil_PortOpen_RealListener(t *testing.T) {
	// Start a listener on a free port — wait.until should detect it.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	addr := l.Addr().(*net.TCPAddr)

	w := NewWaiter()
	res := w.Until(context.Background(), Input{
		Type:           TypePortOpen,
		Host:           "127.0.0.1",
		Port:           addr.Port,
		TimeoutSeconds: 2,
		PollIntervalMS: 50,
	})
	if !res.Satisfied {
		t.Errorf("expected satisfied=true for live listener, got %+v", res)
	}
}

func TestUntil_PortOpen_NoListener(t *testing.T) {
	w := NewWaiter()
	res := w.Until(context.Background(), Input{
		Type:           TypePortOpen,
		Host:           "127.0.0.1",
		Port:           1, // privileged + almost certainly closed
		TimeoutSeconds: 0.4,
		PollIntervalMS: 50,
	})
	if res.Satisfied {
		t.Errorf("expected satisfied=false for closed port, got %+v", res)
	}
}

func TestUntil_PortOpen_InvalidPort(t *testing.T) {
	w := NewWaiter()
	res := w.Until(context.Background(), Input{
		Type:           TypePortOpen,
		Host:           "127.0.0.1",
		Port:           99999, // out of range
		TimeoutSeconds: 0.3,
		PollIntervalMS: 50,
	})
	if res.Satisfied {
		t.Error("expected satisfied=false for invalid port")
	}
	if !strings.Contains(res.LastStatus, "invalid port") {
		t.Errorf("expected last_status='invalid port', got %q", res.LastStatus)
	}
}

func TestUntil_HTTPReady_LiveServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	w := NewWaiter()
	res := w.Until(context.Background(), Input{
		Type:           TypeHTTPReady,
		URL:            srv.URL,
		TimeoutSeconds: 2,
		PollIntervalMS: 50,
	})
	if !res.Satisfied {
		t.Errorf("expected satisfied=true for live server, got %+v", res)
	}
	if !strings.HasPrefix(res.LastStatus, "http 200") {
		t.Errorf("expected last_status to start with 'http 200', got %q", res.LastStatus)
	}
}

func TestUntil_HTTPReady_BadURL(t *testing.T) {
	w := NewWaiter()
	res := w.Until(context.Background(), Input{
		Type:           TypeHTTPReady,
		URL:            "http://127.0.0.1:1/nope", // closed
		TimeoutSeconds: 0.4,
		PollIntervalMS: 50,
	})
	if res.Satisfied {
		t.Errorf("expected satisfied=false, got %+v", res)
	}
}

func TestUntil_ProcessRunning_Mocked(t *testing.T) {
	w := NewWaiter()
	w.processRunner = func(_ context.Context, name string) bool {
		return name == "magic-process"
	}
	res := w.Until(context.Background(), Input{
		Type:           TypeProcessRunning,
		Name:           "magic-process",
		TimeoutSeconds: 1,
		PollIntervalMS: 50,
	})
	if !res.Satisfied {
		t.Errorf("expected mocked process to be 'running', got %+v", res)
	}
}

func TestUntil_ProcessRunning_MockedAbsent(t *testing.T) {
	w := NewWaiter()
	w.processRunner = func(_ context.Context, _ string) bool { return false }
	res := w.Until(context.Background(), Input{
		Type:           TypeProcessRunning,
		Name:           "ghost",
		TimeoutSeconds: 0.3,
		PollIntervalMS: 50,
	})
	if res.Satisfied {
		t.Error("expected satisfied=false when mocked process is absent")
	}
}

func TestUntil_Duration(t *testing.T) {
	w := NewWaiter()
	start := time.Now()
	res := w.Until(context.Background(), Input{
		Type:           TypeDuration,
		Seconds:        0.15,
		TimeoutSeconds: 5,
	})
	elapsed := time.Since(start)
	if !res.Satisfied {
		t.Errorf("expected duration to satisfy, got %+v", res)
	}
	if elapsed < 140*time.Millisecond {
		t.Errorf("expected elapsed >= 140ms, got %v", elapsed)
	}
	if elapsed > 1*time.Second {
		t.Errorf("expected elapsed << 1s, got %v", elapsed)
	}
	if res.Checks != 1 {
		t.Errorf("duration should count as 1 check, got %d", res.Checks)
	}
}

func TestUntil_Duration_ClampedToTimeout(t *testing.T) {
	w := NewWaiter()
	start := time.Now()
	res := w.Until(context.Background(), Input{
		Type:           TypeDuration,
		Seconds:        10, // request long sleep
		TimeoutSeconds: 0.2, // but timeout is short
	})
	elapsed := time.Since(start)
	if !res.Satisfied {
		t.Errorf("expected duration to complete (clamped), got %+v", res)
	}
	if elapsed > 600*time.Millisecond {
		t.Errorf("expected duration to be clamped to ~200ms, got %v", elapsed)
	}
}

func TestUntil_UnknownType(t *testing.T) {
	w := NewWaiter()
	res := w.Until(context.Background(), Input{
		Type:           "ufo_lands",
		TimeoutSeconds: 0.3,
		PollIntervalMS: 50,
	})
	if res.Satisfied {
		t.Errorf("expected satisfied=false for unknown type, got %+v", res)
	}
	if !strings.Contains(res.LastStatus, "unknown condition type") {
		t.Errorf("expected last_status to flag unknown type, got %q", res.LastStatus)
	}
}

func TestUntil_ContextCancellation(t *testing.T) {
	w := NewWaiter()
	dir := t.TempDir()
	path := filepath.Join(dir, "never.txt")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	res := w.Until(ctx, Input{
		Type:           TypeFileExists,
		Path:           path,
		TimeoutSeconds: 5, // would normally wait 5s
		PollIntervalMS: 50,
	})
	elapsed := time.Since(start)
	if res.Satisfied {
		t.Error("expected satisfied=false when context cancelled")
	}
	if !strings.Contains(res.Reason, "cancel") {
		t.Errorf("expected reason to mention cancel, got %q", res.Reason)
	}
	if elapsed > 1*time.Second {
		t.Errorf("expected fast cancellation, took %v", elapsed)
	}
}

func TestRegisteredTool_RoundTripJSON(t *testing.T) {
	reg := newMockRegistry()
	RegisterTool(reg, NewWaiter())

	tool := reg.exec["wait.until"]
	if tool == nil {
		t.Fatal("wait.until not registered")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "tooltest.txt")
	if err := os.WriteFile(path, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}

	in := map[string]interface{}{
		"type":             "file_exists",
		"path":             path,
		"timeout_seconds":  2,
		"poll_interval_ms": 50,
	}
	inJSON, _ := json.Marshal(in)
	out, err := tool(context.Background(), string(inJSON))
	if err != nil {
		t.Fatalf("tool exec: %v", err)
	}
	var got Result
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output not valid JSON: %v\noutput: %s", err, out)
	}
	if !got.Satisfied {
		t.Errorf("expected satisfied=true, got %+v", got)
	}
}

func TestRegisteredTool_MissingType(t *testing.T) {
	reg := newMockRegistry()
	RegisterTool(reg, NewWaiter())
	tool := reg.exec["wait.until"]
	_, err := tool(context.Background(), `{"path":"/tmp/x"}`)
	if err == nil {
		t.Fatal("expected error when type is missing")
	}
}

func TestRegisteredTool_InvalidJSON(t *testing.T) {
	reg := newMockRegistry()
	RegisterTool(reg, NewWaiter())
	tool := reg.exec["wait.until"]
	_, err := tool(context.Background(), `not json`)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}
