package prompt

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/session"
)

// Prompt is the assembled three-tier prompt ready for model consumption.
type Prompt struct {
	SystemMessage string          // Tier 1 (always present, ~600 tokens)
	SkillContext  string          // Tier 2 (on-demand, 0-2000 tokens)
	History       []agent.Message // Tier 3 (sliding window)
	UserMessage   string          // The new message
	SkillsLoaded  []string        // Names of skills activated
	EstTokens     int             // Estimated total tokens
}

// ThoughtCounter provides thought count for the Tier 1 awareness pointer.
type ThoughtCounter interface {
	ThoughtCount(ctx context.Context) (int, error)
}

// EntitySummarizer provides a compact entity summary for Tier 1 awareness.
type EntitySummarizer interface {
	Summary(ctx context.Context) (string, error)
}

// InstructionEntry holds a single learned instruction for prompt injection.
type InstructionEntry struct {
	Content  string
	Category string
	Priority int
}

// InstructionProvider supplies active instructions for system prompt injection.
type InstructionProvider interface {
	ActiveForPrompt(ctx context.Context, channel, sessionID string) ([]InstructionEntry, error)
	Count(ctx context.Context) (int, error)
}

// GitContextFunc returns git context string for system prompt injection.
type GitContextFunc func(ctx context.Context) string

// Composer assembles the three-tier prompt for model consumption.
type Composer struct {
	cfg              *config.Config
	skills           []*Skill
	session          *session.Manager
	tracker          *Tracker
	db               *sql.DB
	memoryStore      ThoughtCounter
	instructionStore InstructionProvider
	entityStore      EntitySummarizer
	gitContextFn     GitContextFunc
}

// NewComposer creates a prompt composer.
func NewComposer(cfg *config.Config, sm *session.Manager, tracker *Tracker, db *sql.DB) *Composer {
	return &Composer{
		cfg:     cfg,
		session: sm,
		tracker: tracker,
		db:      db,
	}
}

// SetMemoryStore sets the memory store for Tier 1 awareness pointers.
func (c *Composer) SetMemoryStore(tc ThoughtCounter) {
	c.memoryStore = tc
}

// SetInstructionStore sets the instruction store for Tier 1 injection.
func (c *Composer) SetInstructionStore(ip InstructionProvider) {
	c.instructionStore = ip
}

// SetEntityStore sets the entity store for Tier 1 awareness pointers.
func (c *Composer) SetEntityStore(es EntitySummarizer) {
	c.entityStore = es
}

// SetGitContext sets a function that provides git context for system prompt injection.
func (c *Composer) SetGitContext(fn GitContextFunc) {
	c.gitContextFn = fn
}

// LoadSkills loads skill files from the configured skills directory + embedded defaults.
func (c *Composer) LoadSkills() error {
	// Load defaults first.
	defaults, err := LoadDefaultSkills()
	if err != nil {
		return err
	}
	c.skills = defaults

	// Load user skills (override defaults by name).
	dir := c.cfg.SkillsDir()
	if dir != "" {
		userSkills, err := LoadSkillsDir(dir)
		if err != nil {
			return err
		}
		// Merge: user skills override defaults with same name.
		nameMap := make(map[string]*Skill)
		for _, s := range c.skills {
			nameMap[s.Name] = s
		}
		for _, s := range userSkills {
			nameMap[s.Name] = s
		}
		c.skills = nil
		for _, s := range nameMap {
			c.skills = append(c.skills, s)
		}
	}

	return nil
}

// Skills returns the loaded skills.
func (c *Composer) Skills() []*Skill {
	return c.skills
}

// Compose assembles the full prompt for a given session and user message.
// The channel parameter enables channel-scoped instruction loading.
func (c *Composer) Compose(ctx context.Context, sessionID, userMessage, channel string) (*Prompt, error) {
	p := &Prompt{
		UserMessage: userMessage,
	}

	// Tier 1: System message (awareness pointers).
	systemMsg, err := c.buildTier1(ctx, channel, sessionID)
	if err != nil {
		return nil, fmt.Errorf("prompt.compose: tier1: %w", err)
	}
	p.SystemMessage = systemMsg

	// Tier 2: On-demand skill loading.
	maxSkills := c.cfg.Options.Prompts.MaxSkillsPerTurn
	matched := MatchSkills(c.skills, userMessage, maxSkills)
	if len(matched) > 0 {
		var parts []string
		for _, s := range matched {
			parts = append(parts, s.Body)
			p.SkillsLoaded = append(p.SkillsLoaded, s.Name)
		}
		p.SkillContext = strings.Join(parts, "\n\n---\n\n")
	}

	// Tier 3: Sliding window.
	if c.session != nil && sessionID != "" {
		history, err := c.session.SlidingWindow(ctx, sessionID)
		if err != nil {
			return nil, fmt.Errorf("prompt.compose: tier3: %w", err)
		}
		p.History = history
	}

	// Estimate total tokens (word count × 1.3 rough estimate).
	p.EstTokens = estimateTokens(p.SystemMessage) +
		estimateTokens(p.SkillContext) +
		estimateMessageTokens(p.History) +
		estimateTokens(p.UserMessage)

	return p, nil
}

// buildTier1 assembles the static system prompt (~600 tokens target, 700 hard ceiling).
func (c *Composer) buildTier1(ctx context.Context, channel, sessionID string) (string, error) {
	var parts []string
	maxTokens := c.cfg.Options.Prompts.MaxTier1Tokens

	// 1. Base system prompt (~200 tokens).
	base := fmt.Sprintf("You are %s, a personal AI assistant. You help with tasks, answer questions, and manage daily activities. Be concise and helpful.", c.cfg.Settings.PersonaName)
	parts = append(parts, base)

	// 2. Learned Instructions (~200 tokens, priority-ordered).
	instrBlock := c.loadInstructions(ctx, channel, sessionID)
	if instrBlock != "" {
		parts = append(parts, instrBlock)
	}

	// 3. Skill Index (~50 tokens).
	var skillSummaries []string
	for _, s := range c.skills {
		if s.Enabled && s.Summary != "" {
			skillSummaries = append(skillSummaries, fmt.Sprintf("%s (%s)", s.Name, s.Summary))
		}
	}
	if len(skillSummaries) > 0 {
		parts = append(parts, "Available skills: "+strings.Join(skillSummaries, ", ")+".")
	}

	// 4. Key Facts (~100 tokens).
	facts := c.loadKeyFacts(ctx, 10)
	if len(facts) > 0 {
		parts = append(parts, "Key facts: "+strings.Join(facts, ". ")+".")
	}

	// 5. Living Memory awareness pointer (~10 tokens).
	if c.memoryStore != nil {
		count, _ := c.memoryStore.ThoughtCount(ctx)
		if count > 0 {
			parts = append(parts, fmt.Sprintf("Living Memory: %d thoughts captured.", count))
		}
	}

	// 5b. Instruction count pointer (~10 tokens).
	if c.instructionStore != nil {
		count, _ := c.instructionStore.Count(ctx)
		if count > 0 {
			parts = append(parts, fmt.Sprintf("Learned Instructions: %d active.", count))
		}
	}

	// 5c. Entity awareness pointer (~50 tokens).
	if c.entityStore != nil {
		summary, _ := c.entityStore.Summary(ctx)
		if summary != "" {
			parts = append(parts, summary)
		}
	}

	// 5d. Git context (~50 tokens).
	if c.gitContextFn != nil {
		gitCtx := c.gitContextFn(ctx)
		if gitCtx != "" {
			parts = append(parts, "Git:\n"+gitCtx)
		}
	}

	// 6. Agent Mode (~10 tokens).
	parts = append(parts, fmt.Sprintf("Mode: %s.", c.cfg.Settings.AgentMode))

	// Assemble and check token ceiling.
	result := strings.Join(parts, "\n\n")

	// Truncate if over ceiling: remove facts first, then reduce instructions to top 3, then remove skill index.
	if estimateTokens(result) > maxTokens {
		result = c.truncateTier1(parts, maxTokens)
	}

	return result, nil
}

// loadInstructions loads active instructions and formats them for system prompt injection.
func (c *Composer) loadInstructions(ctx context.Context, channel, sessionID string) string {
	if c.instructionStore == nil {
		return ""
	}

	entries, err := c.instructionStore.ActiveForPrompt(ctx, channel, sessionID)
	if err != nil || len(entries) == 0 {
		return ""
	}

	maxTokens := c.cfg.Options.Prompts.MaxInstructionTokens
	if maxTokens <= 0 {
		maxTokens = 200
	}

	var lines []string
	totalTokens := estimateTokens("User instructions (always follow these):")

	included := 0
	for _, e := range entries {
		line := "- " + e.Content
		lineTokens := estimateTokens(line)
		if totalTokens+lineTokens > maxTokens {
			remaining := len(entries) - included
			if remaining > 0 {
				lines = append(lines, fmt.Sprintf("(+ %d more lower-priority instructions omitted)", remaining))
			}
			break
		}
		lines = append(lines, line)
		totalTokens += lineTokens
		included++
	}

	if len(lines) == 0 {
		return ""
	}

	return "User instructions (always follow these):\n" + strings.Join(lines, "\n")
}

// truncateTier1 rebuilds Tier 1 content within the token budget by removing lower-priority sections.
func (c *Composer) truncateTier1(parts []string, maxTokens int) string {
	// Strategy: try progressively shorter content.
	// Already assembled in parts — remove from the middle (facts before skills before instructions).
	result := strings.Join(parts, "\n\n")
	if estimateTokens(result) <= maxTokens {
		return result
	}

	// Remove facts (find and remove "Key facts:" part).
	var trimmed []string
	for _, p := range parts {
		if !strings.HasPrefix(p, "Key facts:") {
			trimmed = append(trimmed, p)
		}
	}
	result = strings.Join(trimmed, "\n\n")
	if estimateTokens(result) <= maxTokens {
		return result
	}

	// Remove skill index.
	var trimmed2 []string
	for _, p := range trimmed {
		if !strings.HasPrefix(p, "Available skills:") {
			trimmed2 = append(trimmed2, p)
		}
	}
	return strings.Join(trimmed2, "\n\n")
}

// loadKeyFacts loads the most recent key facts from the database.
func (c *Composer) loadKeyFacts(ctx context.Context, limit int) []string {
	if c.db == nil {
		return nil
	}

	rows, err := c.db.QueryContext(ctx,
		`SELECT fact FROM key_facts ORDER BY extracted_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var facts []string
	for rows.Next() {
		var fact string
		if err := rows.Scan(&fact); err != nil {
			continue
		}
		facts = append(facts, fact)
	}
	return facts
}

// EstimateTokens gives a rough token count for arbitrary text using a hybrid
// approach. English text averages ~4 characters per token; non-ASCII text
// (Arabic, CJK) uses ~2-3 characters per token. Uses byte-length / 3.5 as a
// compromise that's more accurate than simple word count across languages.
//
// Returns the higher of the byte-based and word-based estimates to avoid
// undercount (which would lead to over-budget surprises).
func EstimateTokens(text string) int {
	return estimateTokens(text)
}

// estimateTokens is the unexported implementation. Kept separate so the
// existing call sites within this package don't need to migrate.
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// Byte-based estimation: ~3.5 bytes per token on average.
	// This accounts for multi-byte Unicode (Arabic, CJK) better than word count.
	byteEst := int(float64(len(text)) / 3.5)
	// Word-based estimation: ~1.3 tokens per word (English-centric).
	wordEst := int(float64(len(strings.Fields(text))) * 1.3)
	// Return the higher estimate for safety (avoids undercount).
	if byteEst > wordEst {
		return byteEst
	}
	return wordEst
}

// estimateMessageTokens estimates tokens for a slice of messages.
func estimateMessageTokens(msgs []agent.Message) int {
	total := 0
	for _, m := range msgs {
		total += estimateTokens(m.Content) + 4 // +4 for role overhead
	}
	return total
}
