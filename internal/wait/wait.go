// Package wait provides a generic "wait until condition is true" primitive.
//
// Without this, agents that drive real systems race conditions and fail
// non-deterministically — clicking a button before the window has loaded,
// hitting an HTTP endpoint before the service has bound its port, calling
// a script before its dependency process is up.
//
// Five condition types are supported, all OS-agnostic and dependency-free:
//
//   - file_exists      — os.Stat the path
//   - process_running  — pgrep on Unix, tasklist on Windows
//   - port_open        — TCP DialTimeout against host:port
//   - http_ready       — HTTP request returns any non-network-error response
//   - duration         — plain time.Sleep (no polling)
//
// Each call has a hard total timeout (default 60s, max 600s) and a
// minimum poll interval (50ms) so an agent can't accidentally wait
// forever or hammer the system. The tool returns a JSON status indicating
// whether the condition was satisfied, how long it took, and how many
// polls fired — useful state for the supervisor to decide what to do next.
//
// Capability: tool.wait.until. The probes are read-only (Stat, TCP connect,
// HTTP request, process listing); none mutate state.
package wait

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeoutSeconds = 60
	maxTimeoutSeconds     = 600
	defaultPollIntervalMS = 200
	minPollIntervalMS     = 50
	maxPollIntervalMS     = 60_000

	// httpProbeTimeout caps each individual HTTP request inside the poll loop.
	httpProbeTimeout = 3 * time.Second
	// tcpProbeTimeout caps each TCP DialTimeout in port_open.
	tcpProbeTimeout = 1 * time.Second
)

// Condition types accepted by the wait.until tool.
const (
	TypeFileExists      = "file_exists"
	TypeProcessRunning  = "process_running"
	TypePortOpen        = "port_open"
	TypeHTTPReady       = "http_ready"
	TypeDuration        = "duration"
)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Result is the JSON payload returned by wait.until.
type Result struct {
	Satisfied  bool   `json:"satisfied"`
	Type       string `json:"type"`
	ElapsedMS  int64  `json:"elapsed_ms"`
	Checks     int    `json:"checks"`
	Reason     string `json:"reason,omitempty"`
	LastStatus string `json:"last_status,omitempty"`
}

// Input is the JSON payload accepted by wait.until.
type Input struct {
	Type           string  `json:"type"`
	TimeoutSeconds float64 `json:"timeout_seconds"`
	PollIntervalMS int     `json:"poll_interval_ms"`

	// Type-specific fields.
	Path    string  `json:"path"`    // file_exists
	Name    string  `json:"name"`    // process_running
	Host    string  `json:"host"`    // port_open
	Port    int     `json:"port"`    // port_open
	URL     string  `json:"url"`     // http_ready
	Seconds float64 `json:"seconds"` // duration
}

// Waiter holds optional dependencies that tests can override.
type Waiter struct {
	httpClient *http.Client
	// processRunner returns whether the named process is running. Tests can
	// override this to avoid shelling out.
	processRunner func(ctx context.Context, name string) bool
}

// NewWaiter creates a Waiter using real system probes.
func NewWaiter() *Waiter {
	return &Waiter{
		httpClient: &http.Client{
			Timeout: httpProbeTimeout,
			// Don't follow redirects — we just want to know the service responds.
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		processRunner: defaultProcessRunner,
	}
}

// Until polls the condition described by `in` until it's satisfied or the
// timeout expires. Returns a Result describing the outcome.
func (w *Waiter) Until(ctx context.Context, in Input) Result {
	start := time.Now()
	res := Result{Type: in.Type}

	timeout := in.TimeoutSeconds
	if timeout <= 0 {
		timeout = defaultTimeoutSeconds
	}
	if timeout > maxTimeoutSeconds {
		timeout = maxTimeoutSeconds
	}
	pollMS := in.PollIntervalMS
	if pollMS < minPollIntervalMS {
		pollMS = defaultPollIntervalMS
	}
	if pollMS > maxPollIntervalMS {
		pollMS = maxPollIntervalMS
	}

	deadline := start.Add(time.Duration(timeout * float64(time.Second)))
	pollInterval := time.Duration(pollMS) * time.Millisecond

	// Special case: duration is a one-shot sleep, not a poll loop.
	if in.Type == TypeDuration {
		secs := in.Seconds
		if secs < 0 {
			secs = 0
		}
		if secs > timeout {
			secs = timeout
		}
		select {
		case <-time.After(time.Duration(secs * float64(time.Second))):
			res.Satisfied = true
			res.Checks = 1
			res.ElapsedMS = time.Since(start).Milliseconds()
			return res
		case <-ctx.Done():
			res.Satisfied = false
			res.Reason = "context cancelled"
			res.ElapsedMS = time.Since(start).Milliseconds()
			res.Checks = 1
			return res
		}
	}

	for {
		res.Checks++
		ok, status := w.probe(ctx, in)
		res.LastStatus = status
		if ok {
			res.Satisfied = true
			res.ElapsedMS = time.Since(start).Milliseconds()
			return res
		}

		// Timeout?
		if time.Now().After(deadline) {
			res.Satisfied = false
			res.Reason = fmt.Sprintf("timeout after %.1fs", timeout)
			res.ElapsedMS = time.Since(start).Milliseconds()
			return res
		}

		// Wait a poll interval, but break early if context cancels.
		select {
		case <-time.After(pollInterval):
		case <-ctx.Done():
			res.Satisfied = false
			res.Reason = "context cancelled"
			res.ElapsedMS = time.Since(start).Milliseconds()
			return res
		}
	}
}

// probe runs a single check and returns (satisfied, status-string).
func (w *Waiter) probe(ctx context.Context, in Input) (bool, string) {
	switch in.Type {
	case TypeFileExists:
		if in.Path == "" {
			return false, "path is required"
		}
		_, err := os.Stat(in.Path)
		if err == nil {
			return true, "file exists"
		}
		if os.IsNotExist(err) {
			return false, "file not found"
		}
		return false, err.Error()

	case TypeProcessRunning:
		if in.Name == "" {
			return false, "name is required"
		}
		if w.processRunner(ctx, in.Name) {
			return true, "process running"
		}
		return false, "process not running"

	case TypePortOpen:
		if in.Host == "" {
			return false, "host is required"
		}
		if in.Port <= 0 || in.Port > 65535 {
			return false, "invalid port"
		}
		addr := net.JoinHostPort(in.Host, strconv.Itoa(in.Port))
		conn, err := net.DialTimeout("tcp", addr, tcpProbeTimeout)
		if err != nil {
			return false, "tcp dial: " + err.Error()
		}
		_ = conn.Close()
		return true, "port open"

	case TypeHTTPReady:
		if in.URL == "" {
			return false, "url is required"
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
		if err != nil {
			return false, "build request: " + err.Error()
		}
		resp, err := w.httpClient.Do(req)
		if err != nil {
			return false, "http: " + err.Error()
		}
		_ = resp.Body.Close()
		// Any non-error response counts as "service is up." 5xx specifically
		// could mean "up but unhealthy" — caller decides whether to keep
		// waiting based on last_status. For "up" purposes 5xx counts as ready.
		return true, fmt.Sprintf("http %d", resp.StatusCode)

	default:
		return false, "unknown condition type: " + in.Type
	}
}

// defaultProcessRunner shells out to pgrep (Unix) or tasklist (Windows).
// Both are OS built-ins; no allowlist required.
//
// Known quirk: on macOS, pgrep does not match PID 1 (launchd) for non-root
// callers. Use a user-mode process name (e.g. "Finder") rather than
// "launchd" if you need a presence probe.
func defaultProcessRunner(ctx context.Context, name string) bool {
	switch runtime.GOOS {
	case "windows":
		// tasklist /FI "IMAGENAME eq <name>" — but exact-match a process name
		// is fiddly because Windows uses .exe extensions. Match either form.
		candidates := []string{name, name + ".exe"}
		for _, c := range candidates {
			out, err := exec.CommandContext(ctx, "tasklist", "/NH", "/FI", "IMAGENAME eq "+c).Output() //nolint:gosec
			if err == nil && strings.Contains(strings.ToLower(string(out)), strings.ToLower(c)) {
				return true
			}
		}
		return false
	default:
		// pgrep returns exit 0 if any process matches, exit 1 otherwise.
		err := exec.CommandContext(ctx, "pgrep", "-x", name).Run() //nolint:gosec
		return err == nil
	}
}

// RegisterTool registers the wait.until tool.
func RegisterTool(registry toolRegistry, w *Waiter) {
	registry.Register(
		"wait.until",
		"Block until a condition is true (or timeout). Conditions: "+
			"file_exists (path), process_running (name), port_open (host, port), "+
			"http_ready (url), duration (seconds — plain sleep). "+
			"timeout_seconds defaults to 60, capped at 600. "+
			"poll_interval_ms defaults to 200, minimum 50.",
		`{"type":"object","properties":{`+
			`"type":{"type":"string","enum":["file_exists","process_running","port_open","http_ready","duration"],"description":"Condition type"},`+
			`"path":{"type":"string","description":"Required for file_exists"},`+
			`"name":{"type":"string","description":"Required for process_running"},`+
			`"host":{"type":"string","description":"Required for port_open"},`+
			`"port":{"type":"integer","minimum":1,"maximum":65535,"description":"Required for port_open"},`+
			`"url":{"type":"string","description":"Required for http_ready"},`+
			`"seconds":{"type":"number","minimum":0,"description":"Required for duration"},`+
			`"timeout_seconds":{"type":"number","minimum":0.1,"maximum":600,"description":"Total wait timeout (default 60)"},`+
			`"poll_interval_ms":{"type":"integer","minimum":50,"maximum":60000,"description":"Time between polls (default 200)"}`+
			`},"required":["type"]}`,
		"tool.wait.until",
		func(ctx context.Context, input string) (string, error) {
			var in Input
			if err := json.Unmarshal([]byte(input), &in); err != nil {
				return "", fmt.Errorf("wait.until: invalid input: %w", err)
			}
			if in.Type == "" {
				return "", fmt.Errorf("wait.until: type is required")
			}
			res := w.Until(ctx, in)
			out, err := json.Marshal(res)
			if err != nil {
				return "", fmt.Errorf("wait.until: marshal result: %w", err)
			}
			return string(out), nil
		},
	)
}
