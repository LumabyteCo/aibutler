package prompt

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/memory/bank"
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

// WorkingStateFunc returns a one-line summary of what is currently in flight
// for a session (active multi-step task, work paused awaiting the user), so
// the model knows the working state every turn without a retrieval call.
type WorkingStateFunc func(ctx context.Context, sessionID string) string

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
	workingStateFn   WorkingStateFunc
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

// SetWorkingState sets a function that summarizes in-flight session work for
// Tier-1 injection.
func (c *Composer) SetWorkingState(fn WorkingStateFunc) {
	c.workingStateFn = fn
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

	// 4. Key Facts — the bounded core-memory layer (~350 tokens by default).
	// "scored" ranks by pinned flag, importance, usage frequency, and recency
	// within a fixed token budget; "recency" is the legacy newest-first top-10.
	var facts []string
	if c.cfg.Options.Prompts.CoreMemorySelection == "recency" {
		facts = c.loadKeyFacts(ctx, 10)
	} else {
		facts = c.loadCoreFacts(ctx, c.coreMemoryBudget())
	}
	if len(facts) > 0 {
		parts = append(parts, "Key facts: "+strings.Join(facts, ". ")+".")
	}

	// 4b. Working state (~30 tokens): what's currently in flight, so the
	// model never starts a turn blind to unfinished work.
	if c.workingStateFn != nil {
		if ws := c.workingStateFn(ctx, sessionID); ws != "" {
			parts = append(parts, "Working state: "+ws)
		}
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

	// Shrink facts before dropping them: cut lowest-scored facts (they sit
	// at the end of the section) until the prompt fits, so overflow degrades
	// the core-memory layer gradually instead of zeroing it.
	overhead := estimateTokens(strings.Join(parts, "\n\n")) - maxTokens
	var trimmed []string
	for _, p := range parts {
		if !strings.HasPrefix(p, "Key facts:") {
			trimmed = append(trimmed, p)
			continue
		}
		if shrunk, ok := shrinkFactsPart(p, overhead); ok {
			trimmed = append(trimmed, shrunk)
		}
		// ok=false: even one fact doesn't fit — drop the section entirely.
	}
	result = strings.Join(trimmed, "\n\n")
	if estimateTokens(result) <= maxTokens {
		return result
	}

	// Still over: drop the facts section wholesale.
	var trimmedNoFacts []string
	for _, p := range trimmed {
		if !strings.HasPrefix(p, "Key facts:") {
			trimmedNoFacts = append(trimmedNoFacts, p)
		}
	}
	trimmed = trimmedNoFacts
	result = strings.Join(trimmed, "\n\n")
	if estimateTokens(result) <= maxTokens {
		return result
	}

	// Remove the working-state line.
	var trimmedWS []string
	for _, p := range trimmed {
		if !strings.HasPrefix(p, "Working state:") {
			trimmedWS = append(trimmedWS, p)
		}
	}
	trimmed = trimmedWS
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

// shrinkFactsPart drops trailing facts (lowest-scored — selection emits them
// in score order) from a "Key facts: a. b. c." section until roughly
// overhead tokens are saved. Returns ok=false when nothing survives.
func shrinkFactsPart(part string, overhead int) (string, bool) {
	body := strings.TrimSuffix(strings.TrimPrefix(part, "Key facts: "), ".")
	facts := strings.Split(body, ". ")
	saved := 0
	for len(facts) > 0 && saved < overhead {
		last := facts[len(facts)-1]
		saved += estimateTokens(last) + 1
		facts = facts[:len(facts)-1]
	}
	if len(facts) == 0 {
		return "", false
	}
	return "Key facts: " + strings.Join(facts, ". ") + ".", true
}

// coreMemoryBudget returns the token budget for the scored fact set.
func (c *Composer) coreMemoryBudget() int {
	b := c.cfg.Options.Prompts.MaxCoreMemoryTokens
	if b <= 0 {
		b = 350
	}
	return b
}

// coreFactCandidateLimit bounds the scoring scan. Facts are short rows; a few
// hundred candidates cover any realistic active set while keeping the
// per-turn query cheap and index-friendly.
const coreFactCandidateLimit = 200

// loadCoreFacts selects the always-in-context fact set by score rather than
// recency alone. A fact earns its place through any mix of:
//
//   - being pinned by the user (absolute priority),
//   - importance (explicitly stated facts carry higher salience),
//   - usage frequency (facts that keep getting retrieved or re-asserted), and
//   - recency of confirmation (a statement from January should not outrank
//     one from yesterday just because both exist).
//
// The score is computed in Go — the decay and log terms have no SQLite
// built-ins — over an indexed candidate query, and the winners fill a fixed
// token budget so the core layer is cheap and predictable every turn.
func (c *Composer) loadCoreFacts(ctx context.Context, budgetTokens int) []string {
	if c.db == nil {
		return nil
	}

	// pinned DESC first: pinned facts are exactly the ones that are old and
	// never re-asserted, so they must never age out of the candidate window
	// as active-fact volume grows. id DESC breaks same-second timestamp ties
	// deterministically.
	rows, err := c.db.QueryContext(ctx,
		`SELECT fact, importance, access_count, pinned, extracted_at, COALESCE(last_accessed, '')
		 FROM key_facts WHERE status = 'active' AND bank = ?
		 ORDER BY pinned DESC, extracted_at DESC, id DESC LIMIT ?`, bank.FromContext(ctx), coreFactCandidateLimit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	type scored struct {
		fact  string
		score float64
	}
	var candidates []scored
	now := time.Now().UTC()
	for rows.Next() {
		var fact, extractedAt, lastAccessed string
		var importance, accessCount, pinned int
		if err := rows.Scan(&fact, &importance, &accessCount, &pinned, &extractedAt, &lastAccessed); err != nil {
			continue
		}
		candidates = append(candidates, scored{
			fact:  fact,
			score: coreFactScore(now, importance, accessCount, pinned != 0, extractedAt, lastAccessed),
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })

	// Fill the budget skipping facts that don't fit — one oversized
	// top-scored fact must not starve the whole layer. The header and
	// trailing period the caller adds are charged up front.
	var out []string
	used := estimateTokens("Key facts: .")
	for _, cand := range candidates {
		cost := estimateTokens(cand.fact) + 1 // +1 for the joining separator
		if used+cost > budgetTokens {
			continue
		}
		out = append(out, cand.fact)
		used += cost
	}
	return out
}

// Core-memory scoring weights. Importance dominates (a fact the user cares
// about beats a fact that is merely recent), recency second (beliefs go
// stale), frequency third (habitual relevance).
const (
	coreWeightImportance = 0.40
	coreWeightRecency    = 0.35
	coreWeightFrequency  = 0.25
	coreRecencyHalfLife  = 90 * 24 * time.Hour
)

// coreFactScore combines the promotion signals into one [0,1]-ish score;
// pinned facts get an absolute offset that no unpinned combination reaches.
func coreFactScore(now time.Time, importance, accessCount int, pinned bool, extractedAt, lastAccessed string) float64 {
	// Recency counts from the latest confirmation — extraction or last use.
	newest := parseFactTime(extractedAt)
	if la := parseFactTime(lastAccessed); la.After(newest) {
		newest = la
	}
	recency := 0.0
	if !newest.IsZero() {
		age := now.Sub(newest)
		if age < 0 {
			age = 0
		}
		recency = math.Pow(0.5, float64(age)/float64(coreRecencyHalfLife))
	}

	// log1p(access)/log1p(100): 0 uses → 0, ~10 uses → ~0.52, 100+ → 1.
	frequency := math.Log1p(float64(accessCount)) / math.Log1p(100)
	if frequency > 1 {
		frequency = 1
	}

	score := coreWeightImportance*(float64(importance)/10.0) +
		coreWeightRecency*recency +
		coreWeightFrequency*frequency
	if pinned {
		score += 10 // absolute priority over any unpinned score
	}
	return score
}

// parseFactTime parses the stored RFC3339 / SQLite datetime formats,
// returning the zero time on failure.
func parseFactTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// loadKeyFacts loads the most recent key facts from the database.
func (c *Composer) loadKeyFacts(ctx context.Context, limit int) []string {
	if c.db == nil {
		return nil
	}

	// Only active facts: a fact that was superseded by a newer statement or
	// retracted by the user must never re-enter the prompt.
	rows, err := c.db.QueryContext(ctx,
		`SELECT fact FROM key_facts WHERE status = 'active' AND bank = ? ORDER BY extracted_at DESC LIMIT ?`,
		bank.FromContext(ctx), limit)
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
