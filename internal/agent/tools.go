package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/LumabyteCo/aibutler/internal/capability"
)

const (
	TypeSubagent   AgentType = "subagent"
	TypeBackground AgentType = "background"
)

// DelegateConfig holds the dependencies for agent delegation tools.
type DelegateConfig struct {
	Model             ModelAdapter
	Tools             ToolExecutor
	Caps              *capability.CapabilitySet
	DB                *sql.DB
	Timeout           time.Duration
	MaxDepth          int     // Max nesting depth (default 3)
	CurrentDepth      int     // Current depth of the calling agent
	PerSubagentBudget float64 // Per-subagent budget cap (USD, 0 = unlimited)
	BackgroundMax     int     // Max concurrent background agents (default 3)
	Semaphore         *Semaphore
	UserSemaphore     *UserSemaphore // Per-user concurrency limiter
	UserID            string         // Current user ID for per-user tracking
}

// NewDelegateTool returns the name, description, schema, capability, and execute function
// for the agent.delegate tool. Uses this pattern to avoid circular import with tool package.
func NewDelegateTool(cfg DelegateConfig) (name, desc, schema, cap string, exec func(ctx context.Context, input string) (string, error)) {
	return "agent.delegate",
		"Delegate a task to a subagent that runs synchronously and returns the result. Use for focused subtasks that need their own tool execution context.",
		`{"type":"object","properties":{` +
			`"task":{"type":"string","description":"The task to delegate to the subagent"},` +
			`"timeout_seconds":{"type":"integer","description":"Max execution time in seconds (default: 120)"},` +
			`"max_cost":{"type":"number","description":"Max USD budget for this subagent (0 = use default)"},` +
			`"capabilities":{"type":"array","items":{"type":"string"},"description":"Restrict subagent to these capability resources (empty = inherit all)"}` +
			`},"required":["task"]}`,
		"agent.delegate",
		func(ctx context.Context, input string) (string, error) {
			// Enforce max nesting depth.
			maxDepth := cfg.MaxDepth
			if maxDepth <= 0 {
				maxDepth = 3
			}
			if cfg.CurrentDepth >= maxDepth {
				return "", fmt.Errorf("agent.delegate: max nesting depth %d reached", maxDepth)
			}

			var args struct {
				Task           string   `json:"task"`
				TimeoutSeconds int      `json:"timeout_seconds"`
				MaxCost        float64  `json:"max_cost"`
				Capabilities   []string `json:"capabilities"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("agent.delegate: invalid input: %w", err)
			}
			if args.Task == "" {
				return "", fmt.Errorf("agent.delegate: task is required")
			}

			timeout := time.Duration(args.TimeoutSeconds) * time.Second
			if timeout <= 0 {
				timeout = 2 * time.Minute
			}
			if cfg.Timeout > 0 && timeout > cfg.Timeout {
				timeout = cfg.Timeout
			}

			// Resolve capability subset.
			subCaps := cfg.Caps
			if len(args.Capabilities) > 0 {
				if err := capability.ValidateSubset(cfg.Caps, args.Capabilities); err != nil {
					return "", fmt.Errorf("agent.delegate: %w", err)
				}
				subCaps = capability.Subset(cfg.Caps, args.Capabilities)
			}

			// Resolve budget cap.
			budgetCap := args.MaxCost
			if budgetCap <= 0 {
				budgetCap = cfg.PerSubagentBudget
			}

			agentID := fmt.Sprintf("sub-%d", time.Now().UnixNano())

			// Record delegation in DB.
			recordDelegation(ctx, cfg.DB, "", agentID, "delegate", args.Task, budgetCap)

			subCfg := Config{
				ID:           agentID,
				Task:         args.Task,
				Type:         TypeSubagent,
				Model:        cfg.Model,
				Tools:        cfg.Tools,
				Caps:         subCaps,
				Mode:         ModeMulti,
				DB:           cfg.DB,
				MaxToolCalls: 25,
				Timeout:      timeout,
				BudgetCap:    budgetCap,
				Depth:        cfg.CurrentDepth + 1,
				MaxDepth:     maxDepth,
			}

			// Acquire per-user semaphore (includes global check), or fall back to global only.
			if cfg.UserSemaphore != nil {
				if err := cfg.UserSemaphore.Acquire(ctx, cfg.UserID); err != nil {
					return "", fmt.Errorf("agent.delegate: concurrency limit reached: %w", err)
				}
				defer cfg.UserSemaphore.Release(cfg.UserID)
			} else if cfg.Semaphore != nil {
				if err := cfg.Semaphore.Acquire(ctx); err != nil {
					return "", fmt.Errorf("agent.delegate: concurrency limit reached: %w", err)
				}
				defer cfg.Semaphore.Release()
			}

			a := New(subCfg)
			result, err := a.Run(ctx)
			if err != nil {
				updateDelegation(ctx, cfg.DB, agentID, "failed")
				return "", fmt.Errorf("agent.delegate: %w", err)
			}

			updateDelegation(ctx, cfg.DB, agentID, string(result.Status))

			out, _ := json.Marshal(map[string]interface{}{
				"status":     string(result.Status),
				"output":     result.Output,
				"tool_calls": result.ToolCalls,
				"tokens_in":  result.TokensIn,
				"tokens_out": result.TokensOut,
				"cost_usd":   result.CostUSD,
				"duration":   result.Duration.String(),
			})
			return string(out), nil
		}
}

// NewSpawnTool returns the name, description, schema, capability, and execute function
// for the agent.spawn tool.
func NewSpawnTool(cfg DelegateConfig) (name, desc, schema, cap string, exec func(ctx context.Context, input string) (string, error)) {
	return "agent.spawn",
		"Spawn a background agent that runs asynchronously. Use for tasks that don't need an immediate result (e.g., scheduled analysis, batch processing).",
		`{"type":"object","properties":{` +
			`"task":{"type":"string","description":"The task for the background agent"},` +
			`"timeout_seconds":{"type":"integer","description":"Max execution time in seconds (default: 300)"},` +
			`"max_cost":{"type":"number","description":"Max USD budget for this background agent (0 = use default)"}` +
			`},"required":["task"]}`,
		"agent.delegate",
		func(ctx context.Context, input string) (string, error) {
			// Enforce max nesting depth.
			maxDepth := cfg.MaxDepth
			if maxDepth <= 0 {
				maxDepth = 3
			}
			if cfg.CurrentDepth >= maxDepth {
				return "", fmt.Errorf("agent.spawn: max nesting depth %d reached", maxDepth)
			}

			var args struct {
				Task           string  `json:"task"`
				TimeoutSeconds int     `json:"timeout_seconds"`
				MaxCost        float64 `json:"max_cost"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("agent.spawn: invalid input: %w", err)
			}
			if args.Task == "" {
				return "", fmt.Errorf("agent.spawn: task is required")
			}

			timeout := time.Duration(args.TimeoutSeconds) * time.Second
			if timeout <= 0 {
				timeout = 5 * time.Minute
			}
			if cfg.Timeout > 0 && timeout > cfg.Timeout {
				timeout = cfg.Timeout
			}

			// Budget cap.
			budgetCap := args.MaxCost
			if budgetCap <= 0 {
				budgetCap = cfg.PerSubagentBudget
			}

			agentID := fmt.Sprintf("bg-%d", time.Now().UnixNano())

			// Record delegation and background agent in DB.
			recordDelegation(ctx, cfg.DB, "", agentID, "spawn", args.Task, budgetCap)
			recordBackgroundAgent(ctx, cfg.DB, agentID, "", args.Task, int(timeout.Seconds()), budgetCap)

			subCfg := Config{
				ID:           agentID,
				Task:         args.Task,
				Type:         TypeBackground,
				Model:        cfg.Model,
				Tools:        cfg.Tools,
				Caps:         cfg.Caps,
				Mode:         ModeMulti,
				DB:           cfg.DB,
				MaxToolCalls: 25,
				Timeout:      timeout,
				BudgetCap:    budgetCap,
				Depth:        cfg.CurrentDepth + 1,
				MaxDepth:     maxDepth,
			}

			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("PANIC recovered in background-agent %s: %v", agentID, r)
					}
				}()
				bgCtx, cancel := context.WithTimeout(context.Background(), timeout)
				defer cancel()

				// Acquire per-user semaphore (includes global check), or fall back to global only.
				if cfg.UserSemaphore != nil {
					if err := cfg.UserSemaphore.Acquire(bgCtx, cfg.UserID); err != nil {
						updateBackgroundAgent(context.Background(), cfg.DB, agentID, "failed")
						return
					}
					defer cfg.UserSemaphore.Release(cfg.UserID)
				} else if cfg.Semaphore != nil {
					if err := cfg.Semaphore.Acquire(bgCtx); err != nil {
						updateBackgroundAgent(context.Background(), cfg.DB, agentID, "failed")
						return
					}
					defer cfg.Semaphore.Release()
				}

				a := New(subCfg)
				result, _ := a.Run(bgCtx)
				updateDelegation(context.Background(), cfg.DB, agentID, string(result.Status))
				updateBackgroundAgent(context.Background(), cfg.DB, agentID, string(result.Status))
			}()

			return fmt.Sprintf(`{"agent_id":%q,"status":"spawned","task":%q}`, agentID, args.Task), nil
		}
}

// NewStatusTool returns a tool for querying background agent status.
func NewStatusTool(db *sql.DB) (name, desc, schema, cap string, exec func(ctx context.Context, input string) (string, error)) {
	return "agent.status",
		"Query the status of a background agent by ID.",
		`{"type":"object","properties":{"agent_id":{"type":"string","description":"The agent ID to query"}},"required":["agent_id"]}`,
		"agent.delegate",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				AgentID string `json:"agent_id"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("agent.status: invalid input: %w", err)
			}
			if args.AgentID == "" {
				return "", fmt.Errorf("agent.status: agent_id is required")
			}

			if db == nil {
				return `{"error":"no database configured"}`, nil
			}

			var state, task string
			var costUSD sql.NullFloat64
			var tokensIn, tokensOut sql.NullInt64
			var durationMs sql.NullInt64
			var errorMsg sql.NullString

			err := db.QueryRowContext(ctx,
				`SELECT state, task, cost_usd, tokens_input, tokens_output, duration_ms, error
				 FROM agents WHERE id = ?`, args.AgentID).Scan(
				&state, &task, &costUSD, &tokensIn, &tokensOut, &durationMs, &errorMsg)
			if err == sql.ErrNoRows {
				return `{"error":"agent not found"}`, nil
			}
			if err != nil {
				return "", fmt.Errorf("agent.status: %w", err)
			}

			out, _ := json.Marshal(map[string]interface{}{
				"agent_id":    args.AgentID,
				"status":      state,
				"task":        task,
				"cost_usd":    costUSD.Float64,
				"tokens_in":   tokensIn.Int64,
				"tokens_out":  tokensOut.Int64,
				"duration_ms": durationMs.Int64,
				"error":       errorMsg.String,
			})
			return string(out), nil
		}
}

// NewCancelTool returns a tool for cancelling a running background agent.
func NewCancelTool(db *sql.DB) (name, desc, schema, cap string, exec func(ctx context.Context, input string) (string, error)) {
	return "agent.cancel",
		"Cancel a running background agent by ID.",
		`{"type":"object","properties":{"agent_id":{"type":"string","description":"The background agent ID to cancel"}},"required":["agent_id"]}`,
		"agent.delegate",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				AgentID string `json:"agent_id"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("agent.cancel: invalid input: %w", err)
			}
			if args.AgentID == "" {
				return "", fmt.Errorf("agent.cancel: agent_id is required")
			}

			if db == nil {
				return `{"error":"no database configured"}`, nil
			}

			// Mark agent as cancelled in both tables.
			res, err := db.ExecContext(ctx,
				`UPDATE agents SET state = 'cancelled', updated_at = datetime('now') WHERE id = ? AND state NOT IN ('completed', 'failed', 'cancelled')`,
				args.AgentID)
			if err != nil {
				return "", fmt.Errorf("agent.cancel: %w", err)
			}
			rows, _ := res.RowsAffected()

			db.ExecContext(ctx,
				`UPDATE background_agents SET status = 'cancelled' WHERE agent_id = ? AND status = 'running'`,
				args.AgentID)
			db.ExecContext(ctx,
				`UPDATE agent_delegations SET status = 'cancelled', completed_at = datetime('now') WHERE child_id = ? AND status IN ('pending', 'running')`,
				args.AgentID)

			if rows == 0 {
				return `{"status":"not_found_or_already_terminal"}`, nil
			}
			return fmt.Sprintf(`{"status":"cancelled","agent_id":%q}`, args.AgentID), nil
		}
}

// NewListBackgroundTool returns a tool for listing background agents.
func NewListBackgroundTool(db *sql.DB) (name, desc, schema, cap string, exec func(ctx context.Context, input string) (string, error)) {
	return "agent.list",
		"List background agents, optionally filtered by status.",
		`{"type":"object","properties":{"status":{"type":"string","description":"Filter by status (running, completed, failed, cancelled). Empty = all."}}}`,
		"agent.delegate",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Status string `json:"status"`
			}
			json.Unmarshal([]byte(input), &args)

			if db == nil {
				return `{"error":"no database configured"}`, nil
			}

			var rows *sql.Rows
			var err error
			if args.Status != "" {
				rows, err = db.QueryContext(ctx,
					`SELECT b.agent_id, b.task, b.status, b.created_at, b.expires_at
					 FROM background_agents b WHERE b.status = ? ORDER BY b.created_at DESC LIMIT 20`,
					args.Status)
			} else {
				rows, err = db.QueryContext(ctx,
					`SELECT b.agent_id, b.task, b.status, b.created_at, b.expires_at
					 FROM background_agents b ORDER BY b.created_at DESC LIMIT 20`)
			}
			if err != nil {
				return "", fmt.Errorf("agent.list: %w", err)
			}
			defer rows.Close()

			var agents []map[string]interface{}
			for rows.Next() {
				var agentID, task, status, createdAt string
				var expiresAt sql.NullString
				if err := rows.Scan(&agentID, &task, &status, &createdAt, &expiresAt); err != nil {
					continue
				}
				agents = append(agents, map[string]interface{}{
					"agent_id":   agentID,
					"task":       task,
					"status":     status,
					"created_at": createdAt,
					"expires_at": expiresAt.String,
				})
			}

			out, _ := json.Marshal(map[string]interface{}{
				"agents": agents,
				"count":  len(agents),
			})
			return string(out), nil
		}
}

// recordDelegation persists a delegation record.
func recordDelegation(ctx context.Context, db *sql.DB, parentID, childID, delegationType, task string, maxCost float64) {
	if db == nil {
		return
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO agent_delegations (parent_id, child_id, delegation_type, status, max_cost, task)
		 VALUES (?, ?, ?, 'running', ?, ?)`,
		parentID, childID, delegationType, maxCost, task); err != nil {
		log.Printf("recordDelegation: %v", err)
	}
}

// updateDelegation updates the status of a delegation record.
func updateDelegation(ctx context.Context, db *sql.DB, childID, status string) {
	if db == nil {
		return
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE agent_delegations SET status = ?, completed_at = datetime('now') WHERE child_id = ?`,
		status, childID); err != nil {
		log.Printf("updateDelegation: %v", err)
	}
}

// recordBackgroundAgent persists a background agent record.
func recordBackgroundAgent(ctx context.Context, db *sql.DB, agentID, session, task string, maxDuration int, maxCost float64) {
	if db == nil {
		return
	}
	var expiresAt interface{}
	if maxDuration > 0 {
		expiresAt = time.Now().Add(time.Duration(maxDuration) * time.Second).UTC().Format(time.RFC3339)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO background_agents (agent_id, owner_session, task, max_duration, max_cost, status, expires_at)
		 VALUES (?, ?, ?, ?, ?, 'running', ?)`,
		agentID, session, task, maxDuration, maxCost, expiresAt); err != nil {
		log.Printf("recordBackgroundAgent: %v", err)
	}
}

// updateBackgroundAgent updates a background agent's status.
func updateBackgroundAgent(ctx context.Context, db *sql.DB, agentID, status string) {
	if db == nil {
		return
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE background_agents SET status = ? WHERE agent_id = ?`,
		status, agentID); err != nil {
		log.Printf("updateBackgroundAgent: %v", err)
	}
}

// CleanupExpiredBackgroundAgents marks expired background agents as "expired".
func CleanupExpiredBackgroundAgents(ctx context.Context, db *sql.DB) (int64, error) {
	if db == nil {
		return 0, nil
	}
	res, err := db.ExecContext(ctx,
		`UPDATE background_agents SET status = 'expired'
		 WHERE status = 'running' AND expires_at IS NOT NULL AND expires_at < datetime('now')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
