package model

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	"github.com/LumabyteCo/aibutler/internal/tool"
)

// ToolsSetter is an optional interface that model adapters can implement
// to receive tool definitions before each agent run.
type ToolsSetter interface {
	SetTools(tools []agent.ToolDef)
}

// PostRunProcessor handles post-agent-run processing:
// saving conversation to session_transcripts for FTS5 indexing
// and extracting entities from tool outputs for the knowledge graph.
type PostRunProcessor interface {
	AfterAgentRun(ctx context.Context, sessionID, userMsg, assistantMsg, runStatus string, toolOutputs []agent.ToolOutput)
}

// FactoryConfig holds the dependencies for the agent factory.
type FactoryConfig struct {
	Composer      *prompt.Composer
	Model         agent.ModelAdapter
	Tools         *tool.Dispatcher
	Caps          *capability.CapabilitySet
	Tracker       *prompt.Tracker
	DB            *sql.DB
	Config        *config.Config
	RoleRouter    *agent.RoleRouter // Custom role routing (for ModeCustom)
	PostProcessor PostRunProcessor  // Optional: FTS5 indexing + entity extraction
	Compactor     *prompt.Compactor // Optional: context window compaction
	BatchExecutor *BatchExecutor    // Optional: parallel tool execution batching
}

// Factory implements channel.AgentFactory.
// It wires Router → Composer → Agent → Tools into a complete pipeline.
type Factory struct {
	composer      *prompt.Composer
	model         agent.ModelAdapter
	tools         *tool.Dispatcher
	caps          *capability.CapabilitySet
	tracker       *prompt.Tracker
	db            *sql.DB
	cfg           *config.Config
	roleRouter    *agent.RoleRouter
	postProcessor PostRunProcessor
	compactor     *prompt.Compactor
}

// NewFactory creates an agent factory.
func NewFactory(cfg FactoryConfig) *Factory {
	return &Factory{
		composer:      cfg.Composer,
		model:         cfg.Model,
		tools:         cfg.Tools,
		caps:          cfg.Caps,
		tracker:       cfg.Tracker,
		db:            cfg.DB,
		cfg:           cfg.Config,
		roleRouter:    cfg.RoleRouter,
		postProcessor: cfg.PostProcessor,
		compactor:     cfg.Compactor,
	}
}

// Run implements channel.AgentFactory. It composes the prompt, creates an agent,
// runs it, and records token usage.
func (f *Factory) Run(ctx context.Context, sessionID, task, channel string) (*agent.Result, error) {
	// 1. Compose the prompt (system message + skills + history).
	composed, err := f.composer.Compose(ctx, sessionID, task, channel)
	if err != nil {
		return nil, fmt.Errorf("factory: compose prompt: %w", err)
	}

	// 2. Resolve agent mode (check for per-turn override first).
	mode := agent.Mode(f.cfg.Settings.AgentMode)
	if mode == "" {
		mode = agent.ModeAuto
	}
	if override, cleaned := agent.ParseModeOverride(task, mode); override != "" {
		mode = override
		task = cleaned
	}

	// 3. Resolve custom role first (needed for tool filtering).
	var role *agent.CustomRole
	var rolePrompt string
	if mode == agent.ModeCustom && f.roleRouter != nil {
		r, err := f.roleRouter.Route(ctx, task, f.model)
		if err == nil {
			role = r
			rolePrompt = r.Prompt
		}
	}

	// 4. Get available tools, filtered by custom role if applicable.
	var toolDefs []agent.ToolDef
	if f.tools != nil {
		toolDefs = f.tools.AvailableTools(ctx, mode, f.caps)
	}
	if role != nil && len(role.Tools) > 0 {
		toolDefs = filterToolsByRole(toolDefs, role.Tools)
	}
	if setter, ok := f.model.(ToolsSetter); ok {
		setter.SetTools(toolDefs)
	}

	// 5. Build initial messages from composed prompt.
	var initMessages []agent.Message

	// System message = Tier 1 + Tier 2 (skill context) + role prompt.
	systemContent := composed.SystemMessage
	if composed.SkillContext != "" {
		systemContent += "\n\n" + composed.SkillContext
	}
	if rolePrompt != "" {
		systemContent += "\n\n" + rolePrompt
	}
	initMessages = append(initMessages, agent.Message{
		Role:    "system",
		Content: systemContent,
	})

	// Conversation history (Tier 3).
	initMessages = append(initMessages, composed.History...)

	// User message.
	initMessages = append(initMessages, agent.Message{
		Role:    "user",
		Content: task,
	})

	// 5b. Check if compaction is needed before creating the agent.
	if f.compactor != nil && f.compactor.ShouldCompact(initMessages) {
		compacted, meta, compactErr := f.compactor.Compact(initMessages)
		if compactErr == nil {
			initMessages = compacted
			// Log compaction metadata.
			if f.db != nil && meta != nil {
				logCompaction(ctx, f.db, sessionID, meta)
			}
		}
	}

	// 6. Create agent config.
	agentID := fmt.Sprintf("agent-%d", time.Now().UnixNano())
	agentCfg := agent.Config{
		ID:            agentID,
		SessionID:     sessionID,
		Task:          task,
		Type:          agent.TypePrimary,
		Model:         f.model,
		Caps:          f.caps,
		Mode:          mode,
		DB:            f.db,
		InitMessages:  initMessages,
		MaxToolCalls:  f.cfg.Options.Agents.MaxToolCalls,
		Timeout:       f.cfg.Options.Agents.SubagentTimeout,
		BudgetChecker: f.tracker,
		Autonomy:      f.resolveAutonomy(),
		// Wire the real per-provider cost estimator so budget caps and
		// cost tracking reflect actual model pricing, not the flat
		// placeholder rate. README claims "per-model pricing" and this
		// is the path that makes that claim true for agent-loop calls.
		CostEstimator: estimateCost,
		Provider:      resolveProvider(f.cfg.Settings.Model),
	}
	// Only set Tools if dispatcher is non-nil (avoids nil interface wrapper).
	if f.tools != nil {
		agentCfg.Tools = f.tools
	}
	if agentCfg.MaxToolCalls == 0 {
		agentCfg.MaxToolCalls = 50
	}
	if agentCfg.Timeout == 0 {
		agentCfg.Timeout = 5 * time.Minute
	}

	// 7. Run agent.
	a := agent.New(agentCfg)
	result, err := a.Run(ctx)

	// 8. Post-run processing: FTS5 indexing + entity extraction.
	// Run for both successful and failed/cancelled runs — partial tool outputs are still valuable.
	if f.postProcessor != nil {
		f.postProcessor.AfterAgentRun(ctx, sessionID, task, result.Output, string(result.Status), result.ToolOutputs)
	}

	if err != nil {
		return nil, fmt.Errorf("factory: agent run: %w", err)
	}

	// 9. Record token usage.
	if f.tracker != nil {
		provider := resolveProvider(f.cfg.Configurations.Models.Primary)
		costUSD := estimateCost(provider, result.TokensIn, result.TokensOut)
		_ = f.tracker.Record(ctx, prompt.UsageEntry{
			SessionID:    sessionID,
			Model:        f.cfg.Configurations.Models.Primary,
			Provider:     provider,
			InputTokens:  result.TokensIn,
			OutputTokens: result.TokensOut,
			CostUSD:      costUSD,
			SkillsLoaded: composed.SkillsLoaded,
			Tier2Tokens:  estimateTokenCount(composed.SkillContext),
		})
	}

	return &result, nil
}

// resolveAutonomy builds an AutonomyConfig from the current config.
func (f *Factory) resolveAutonomy() agent.AutonomyConfig {
	level := agent.AutonomyL1
	switch f.cfg.Options.Agents.AutonomyLevel {
	case "l2":
		level = agent.AutonomyL2
	case "l3":
		level = agent.AutonomyL3
	}

	ac := agent.AutonomyConfig{
		Level:       level,
		AutoActions: f.cfg.Options.Agents.L2AutoActions,
		AskActions:  f.cfg.Options.Agents.L2AskActions,
	}

	if level == agent.AutonomyL3 {
		l3 := agent.DefaultL3Config()
		if f.cfg.Options.Agents.L3TimeBound > 0 {
			l3.TimeBound = f.cfg.Options.Agents.L3TimeBound
		}
		if len(f.cfg.Options.Agents.L3SafetyActions) > 0 {
			l3.SafetyActions = f.cfg.Options.Agents.L3SafetyActions
		}
		l3.StartedAt = time.Now()
		ac.L3 = l3
	}

	return ac
}

// resolveProvider maps model names to provider strings.
func resolveProvider(model string) string {
	switch {
	case len(model) >= 6 && model[:6] == "claude":
		return "anthropic"
	case len(model) >= 3 && model[:3] == "gpt":
		return "openai"
	case len(model) >= 6 && model[:6] == "gemini":
		return "gemini"
	case len(model) >= 4 && model[:4] == "grok":
		return "xai"
	default:
		return "local"
	}
}

// EstimateCostPublic calculates approximate cost based on provider.
// Exported for use by the REPL and other CLI commands.
func EstimateCostPublic(provider string, tokensIn, tokensOut int) float64 {
	return estimateCost(provider, tokensIn, tokensOut)
}

// estimateCost calculates approximate cost based on provider.
func estimateCost(provider string, tokensIn, tokensOut int) float64 {
	switch provider {
	case "anthropic":
		// Claude Sonnet 4.6 pricing: $3/M input, $15/M output
		return float64(tokensIn)*3.0/1_000_000 + float64(tokensOut)*15.0/1_000_000
	case "openai":
		// GPT-4o pricing: $2.50/M input, $10/M output
		return float64(tokensIn)*2.5/1_000_000 + float64(tokensOut)*10.0/1_000_000
	case "gemini":
		// Gemini 2.0 Flash pricing: $0.10/M input, $0.40/M output
		return float64(tokensIn)*0.10/1_000_000 + float64(tokensOut)*0.40/1_000_000
	case "xai":
		// Grok-2 pricing: $2/M input, $10/M output
		return float64(tokensIn)*2.0/1_000_000 + float64(tokensOut)*10.0/1_000_000
	default:
		// Local models: $0.00
		return 0.0
	}
}

// filterToolsByRole returns only tools whose names appear in the allowlist.
func filterToolsByRole(tools []agent.ToolDef, allowed []string) []agent.ToolDef {
	allowSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowSet[name] = true
	}
	var filtered []agent.ToolDef
	for _, t := range tools {
		if allowSet[t.Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// logCompaction persists compaction metadata to the database (best-effort).
func logCompaction(ctx context.Context, db *sql.DB, sessionID string, meta *prompt.CompactionMetadata) {
	if _, err := db.ExecContext(ctx,
		`INSERT INTO session_compactions (session_id, original_count, compacted_count, removed_count, preserved_count, tokens_before, tokens_after)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sessionID, meta.OriginalCount, meta.CompactedCount, meta.RemovedCount,
		meta.PreservedCount, meta.EstTokensBefore, meta.EstTokensAfter); err != nil {
		log.Printf("factory: compaction metadata log failed: %v", err)
	}
}

// estimateTokenCount gives a rough token count (words × 1.3).
func estimateTokenCount(text string) int {
	if text == "" {
		return 0
	}
	words := 0
	for _, c := range text {
		if c == ' ' || c == '\n' || c == '\t' {
			words++
		}
	}
	return int(float64(words+1) * 1.3)
}
