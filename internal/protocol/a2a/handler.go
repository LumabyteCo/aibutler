package a2a

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// TaskRunner is the narrow interface for executing delegated tasks.
type TaskRunner interface {
	RunTask(ctx context.Context, task string) (string, error)
}

// Handler processes inbound A2A requests.
type Handler struct {
	db        *sql.DB
	runner    TaskRunner
	tokens    map[string]bool // SHA-256 hashes of allowed bearer tokens
	card      AgentCard
	maxDepth  int    // max swarm delegation depth (0 = use default 4)
	agentName string // own agent name for loop detection
}

// NewHandler creates an A2A handler.
func NewHandler(db *sql.DB, runner TaskRunner, card AgentCard, allowedTokenHashes []string) *Handler {
	tokens := make(map[string]bool, len(allowedTokenHashes))
	for _, h := range allowedTokenHashes {
		tokens[h] = true
	}
	return &Handler{db: db, runner: runner, tokens: tokens, card: card, agentName: card.Name}
}

// SetMaxDepth configures the maximum swarm delegation depth.
func (h *Handler) SetMaxDepth(depth int) {
	h.maxDepth = depth
}

// SetRunner replaces the task runner at runtime.
// Called once after bootstrap completes when the real Factory becomes available.
func (h *Handler) SetRunner(runner TaskRunner) {
	h.runner = runner
}

// ServeHTTP routes requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/.well-known/agent.json":
		h.serveAgentCard(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/a2a/tasks":
		h.handleTask(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/a2a/tasks/") && strings.HasSuffix(r.URL.Path, "/stream"):
		h.handleStreamTask(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/a2a/tasks/") && strings.HasSuffix(r.URL.Path, "/cancel"):
		h.handleCancelTask(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/a2a/tasks/"):
		h.handleGetTask(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) serveAgentCard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(h.card); err != nil {
		log.Printf("a2a: serveAgentCard: encode: %v", err)
	}
}

func (h *Handler) handleTask(w http.ResponseWriter, r *http.Request) {
	// Verify auth.
	tokenHash, ok := h.verifyAuth(r)
	if !ok {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		} else {
			http.Error(w, "Forbidden", http.StatusForbidden)
		}
		return
	}

	// Swarm depth limit: check X-Swarm-Depth header.
	maxDepth := h.maxDepth
	if maxDepth <= 0 {
		maxDepth = 4
	}
	depthStr := r.Header.Get("X-Swarm-Depth")
	depth := 0
	if depthStr != "" {
		if _, err := fmt.Sscanf(depthStr, "%d", &depth); err != nil {
			depth = 0
		}
	}
	depth++ // Increment for this hop.
	if depth > maxDepth {
		http.Error(w, "Too Many Requests: swarm depth limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Swarm loop detection: check X-Swarm-Agent-Chain header.
	agentChain := r.Header.Get("X-Swarm-Agent-Chain")
	if agentChain != "" && h.agentName != "" {
		for _, name := range strings.Split(agentChain, ",") {
			if strings.TrimSpace(name) == h.agentName {
				http.Error(w, "Conflict: agent loop detected", http.StatusConflict)
				return
			}
		}
	}

	// Build updated chain for downstream propagation (stored in context).
	newChain := h.agentName
	if agentChain != "" {
		newChain = agentChain + "," + h.agentName
	}

	// Extract trace ID from header (A2A v2 swarm tracing).
	traceID := r.Header.Get("X-Swarm-Trace-ID")

	// Parse request.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	var req TaskRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Bad Request: invalid task", http.StatusBadRequest)
		return
	}

	// A2A v2: if task is empty but messages provided, extract text from last user message.
	if req.Task == "" && len(req.Messages) > 0 {
		for i := len(req.Messages) - 1; i >= 0; i-- {
			msg := req.Messages[i]
			if msg.Role == "user" {
				for _, part := range msg.Parts {
					if part.Text != "" {
						req.Task = part.Text
						break
					}
				}
				if req.Task != "" {
					break
				}
			}
		}
	}

	if req.Task == "" {
		http.Error(w, "Bad Request: invalid task", http.StatusBadRequest)
		return
	}

	// Use trace ID from header or request body.
	if traceID == "" {
		traceID = req.TraceID
	}

	// Record delegation.
	peer := r.RemoteAddr
	delegationID, err := h.recordDelegation(r.Context(), "inbound", peer, req.Task, tokenHash, "running", traceID)
	if err != nil {
		log.Printf("a2a: recordDelegation: %v", err)
	}

	// Execute task with swarm context.
	taskCtx := context.WithValue(r.Context(), swarmDepthKey{}, depth)
	taskCtx = context.WithValue(taskCtx, swarmChainKey{}, newChain)
	output, execErr := h.runner.RunTask(taskCtx, req.Task)

	result := TaskResult{ID: req.ID}
	if execErr != nil {
		result.Status = "failed"
		result.Error = execErr.Error()
		h.completeDelegation(r.Context(), delegationID, "failed", execErr.Error())
	} else {
		result.Status = "completed"
		result.Output = output
		h.completeDelegation(r.Context(), delegationID, "completed", truncateResult(output))
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("a2a: handleTask: encode result: %v", err)
	}
}

func (h *Handler) verifyAuth(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return "", false
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	if token == "" {
		return "", false
	}
	hash := hashToken(token)

	// Timing-safe: iterate all tokens with constant-time compare
	// to avoid leaking information about which tokens exist.
	found := false
	for storedHash := range h.tokens {
		if subtle.ConstantTimeCompare([]byte(storedHash), []byte(hash)) == 1 {
			found = true
		}
	}
	return hash, found
}

// HashToken returns the SHA-256 hex digest of a bearer token.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}

// hashToken is the internal alias for backward compatibility.
func hashToken(token string) string { return HashToken(token) }

func (h *Handler) recordDelegation(ctx context.Context, direction, peer, task, tokenHash, status, traceID string) (int64, error) {
	res, err := h.db.ExecContext(ctx,
		`INSERT INTO a2a_delegations (direction, peer_agent, task_summary, status, token_auth_hash, trace_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		direction, peer, truncateResult(task), status, tokenHash, traceID, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (h *Handler) completeDelegation(ctx context.Context, id int64, status, result string) error {
	_, err := h.db.ExecContext(ctx,
		`UPDATE a2a_delegations SET status = ?, result_summary = ?, completed_at = ? WHERE id = ?`,
		status, result, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func truncateResult(s string) string {
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}

func (h *Handler) handleGetTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.verifyAuth(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	taskID := strings.TrimPrefix(r.URL.Path, "/a2a/tasks/")
	taskID = strings.TrimSuffix(taskID, "/")
	if taskID == "" {
		http.Error(w, "Bad Request: missing task id", http.StatusBadRequest)
		return
	}
	var resp TaskStatusResponse
	var lifecycleState, output string
	err := h.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(lifecycle_state, status), COALESCE(result_summary,''), created_at, COALESCE(completed_at,'')
		 FROM a2a_delegations WHERE id = ?`,
		taskID).Scan(&lifecycleState, &output, &resp.CreatedAt, &resp.CompletedAt)
	if err == sql.ErrNoRows {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	resp.ID = taskID
	resp.LifecycleState = TaskLifecycleState(lifecycleState)
	resp.Output = output
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("a2a: handleGetTask: encode resp: %v", err)
	}
}

func (h *Handler) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.verifyAuth(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/a2a/tasks/")
	taskID := strings.TrimSuffix(path, "/cancel")
	if taskID == "" {
		http.Error(w, "Bad Request: missing task id", http.StatusBadRequest)
		return
	}
	if _, dbErr := h.db.ExecContext(r.Context(),
		`UPDATE a2a_delegations SET status = 'canceled', lifecycle_state = 'canceled', completed_at = ?
		 WHERE id = ? AND status NOT IN ('completed','failed','canceled')`,
		time.Now().UTC().Format(time.RFC3339), taskID); dbErr != nil {
		log.Printf("a2a: handleCancelTask: update %s: %v", taskID, dbErr)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "canceled", "id": taskID}); err != nil {
		log.Printf("a2a: handleCancelTask: encode resp: %v", err)
	}
}

// swarmDepthKey is the context key for swarm delegation depth.
type swarmDepthKey struct{}

// swarmChainKey is the context key for the agent chain (loop detection).
type swarmChainKey struct{}

// SwarmDepthFromContext returns the swarm delegation depth from the context.
func SwarmDepthFromContext(ctx context.Context) int {
	if v, ok := ctx.Value(swarmDepthKey{}).(int); ok {
		return v
	}
	return 0
}

// SwarmChainFromContext returns the agent chain from the context.
func SwarmChainFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(swarmChainKey{}).(string); ok {
		return v
	}
	return ""
}

func (h *Handler) handleStreamTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.verifyAuth(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/a2a/tasks/")
	taskID := strings.TrimSuffix(path, "/stream")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(30 * time.Second)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-deadline:
			fmt.Fprintf(w, "data: {\"id\":%q,\"lifecycle_state\":\"timeout\"}\n\n", taskID)
			flusher.Flush()
			return
		case <-ticker.C:
			var status, result string
			err := h.db.QueryRowContext(r.Context(),
				`SELECT COALESCE(lifecycle_state, status), COALESCE(result_summary,'')
				 FROM a2a_delegations WHERE id = ?`, taskID).Scan(&status, &result)
			if err != nil {
				fmt.Fprintf(w, "data: {\"id\":%q,\"lifecycle_state\":\"pending\"}\n\n", taskID)
			} else {
				out, _ := json.Marshal(TaskStatusResponse{
					ID:             taskID,
					LifecycleState: TaskLifecycleState(status),
					Output:         result,
				})
				fmt.Fprintf(w, "data: %s\n\n", out)
				if status == "completed" || status == "failed" || status == "canceled" {
					flusher.Flush()
					return
				}
			}
			flusher.Flush()
		}
	}
}
