package digest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/graph"
)

// DigestType identifies the kind of digest.
type DigestType string

const (
	DigestWeekly DigestType = "weekly"
	DigestTopic  DigestType = "topic"
	DigestEntity DigestType = "entity"
)

// Digest is a generated memory summary.
type Digest struct {
	ID                 int64      `json:"id"`
	Type               DigestType `json:"type"`
	Title              string     `json:"title"`
	Content            string     `json:"content"`
	PeriodStart        string     `json:"period_start,omitempty"`
	PeriodEnd          string     `json:"period_end,omitempty"`
	SourceThoughtCount int        `json:"source_thought_count"`
	CreatedAt          string     `json:"created_at,omitempty"`
}

// Summarizer generates LLM-powered text summaries from structured prompts.
// When nil, the Generator falls back to rule-based content.
type Summarizer interface {
	Summarize(ctx context.Context, prompt string) (string, error)
}

// Generator creates digests from memory stores.
// When a Summarizer is set, it produces LLM-powered summaries;
// otherwise it produces rule-based output.
type Generator struct {
	db         *sql.DB
	mem        *memory.Store
	entity     *entity.Store
	graph      *graph.Store
	summarizer Summarizer
}

// NewGenerator creates a digest generator.
func NewGenerator(db *sql.DB, mem *memory.Store, ent *entity.Store, g *graph.Store) *Generator {
	return &Generator{db: db, mem: mem, entity: ent, graph: g}
}

// SetSummarizer wires an LLM-backed summarizer. When set, Generate* methods
// produce LLM-powered summaries; otherwise they fall back to rule-based output.
func (g *Generator) SetSummarizer(s Summarizer) {
	g.summarizer = s
}

// GenerateWeekly creates a digest of the past 7 days.
func (g *Generator) GenerateWeekly(ctx context.Context) (*Digest, error) {
	now := time.Now().UTC()
	weekAgo := now.AddDate(0, 0, -7)
	periodStart := weekAgo.Format(time.RFC3339)
	periodEnd := now.Format(time.RFC3339)

	thoughts, err := g.mem.GetThoughts(ctx, memory.ThoughtQuery{Since: periodStart, Limit: 1000})
	if err != nil {
		return nil, fmt.Errorf("digest: get thoughts: %w", err)
	}

	if len(thoughts) == 0 {
		return &Digest{
			Type:        DigestWeekly,
			Title:       fmt.Sprintf("Weekly Digest: %s to %s", weekAgo.Format("Jan 2"), now.Format("Jan 2")),
			Content:     "No activity recorded this week.",
			PeriodStart: periodStart,
			PeriodEnd:   periodEnd,
		}, nil
	}

	var sections []string
	sections = append(sections, fmt.Sprintf("Thoughts captured: %d", len(thoughts)))

	// People.
	if g.entity != nil {
		people, _ := g.entity.GetByType(ctx, entity.TypePerson, 10)
		if len(people) > 0 {
			names := entityNames(people, 5)
			sections = append(sections, "People mentioned: "+strings.Join(names, ", "))
		}

		projects, _ := g.entity.GetByType(ctx, entity.TypeProject, 10)
		if len(projects) > 0 {
			names := entityNames(projects, 5)
			sections = append(sections, "Projects discussed: "+strings.Join(names, ", "))
		}

		decisions, _ := g.entity.GetByType(ctx, entity.TypeDecision, 5)
		if len(decisions) > 0 {
			names := entityNames(decisions, 5)
			sections = append(sections, "Decisions made: "+strings.Join(names, ", "))
		}
	}

	// Graph stats.
	if g.graph != nil {
		stats, _ := g.graph.Stats(ctx)
		if len(stats) > 0 {
			var statParts []string
			for k, v := range stats {
				statParts = append(statParts, fmt.Sprintf("%s: %d", k, v))
			}
			sections = append(sections, "Knowledge graph: "+strings.Join(statParts, ", "))
		}
	}

	content := strings.Join(sections, "\n")
	if g.summarizer != nil {
		var excerpts []string
		for i, t := range thoughts {
			if i >= 20 {
				break
			}
			excerpts = append(excerpts, "- "+truncate(t.Content, 150))
		}
		prompt := fmt.Sprintf(
			"Write a concise weekly memory digest (2-3 paragraphs) from the following activity data.\n"+
				"Focus on themes, patterns, and insights rather than listing every item.\n\n"+
				"Summary stats: %s\n\nRecent thoughts:\n%s",
			strings.Join(sections, "; "),
			strings.Join(excerpts, "\n"),
		)
		if summary, err := g.summarizer.Summarize(ctx, prompt); err == nil && summary != "" {
			content = summary
		}
	}

	return &Digest{
		Type:               DigestWeekly,
		Title:              fmt.Sprintf("Weekly Digest: %s to %s", weekAgo.Format("Jan 2"), now.Format("Jan 2")),
		Content:            content,
		PeriodStart:        periodStart,
		PeriodEnd:          periodEnd,
		SourceThoughtCount: len(thoughts),
	}, nil
}

// GenerateTopicDigest creates a digest about a specific topic.
func (g *Generator) GenerateTopicDigest(ctx context.Context, topic string) (*Digest, error) {
	thoughts, err := g.mem.GetThoughts(ctx, memory.ThoughtQuery{Contains: topic, Limit: 100})
	if err != nil {
		return nil, fmt.Errorf("digest: get thoughts for topic %q: %w", topic, err)
	}

	var content string
	if len(thoughts) == 0 {
		content = fmt.Sprintf("No thoughts found about %q.", topic)
	} else {
		var lines []string
		lines = append(lines, fmt.Sprintf("Related thoughts: %d", len(thoughts)))
		for i, t := range thoughts {
			if i >= 10 {
				break
			}
			summary := truncate(t.Content, 100)
			lines = append(lines, fmt.Sprintf("- %s", summary))
		}
		content = strings.Join(lines, "\n")

		if g.summarizer != nil {
			var excerpts []string
			for i, t := range thoughts {
				if i >= 15 {
					break
				}
				excerpts = append(excerpts, "- "+truncate(t.Content, 150))
			}
			prompt := fmt.Sprintf(
				"Summarize the following thoughts about %q into a concise paragraph highlighting key themes and insights.\n\n%s",
				topic,
				strings.Join(excerpts, "\n"),
			)
			if summary, err := g.summarizer.Summarize(ctx, prompt); err == nil && summary != "" {
				content = summary
			}
		}
	}

	return &Digest{
		Type:               DigestTopic,
		Title:              fmt.Sprintf("Topic: %s", topic),
		Content:            content,
		SourceThoughtCount: len(thoughts),
	}, nil
}

// GenerateEntityDigest creates a digest about a specific entity.
func (g *Generator) GenerateEntityDigest(ctx context.Context, entityName string) (*Digest, error) {
	thoughts, err := g.mem.GetThoughts(ctx, memory.ThoughtQuery{Contains: entityName, Limit: 100})
	if err != nil {
		return nil, fmt.Errorf("digest: get thoughts for entity %q: %w", entityName, err)
	}

	var content string
	if len(thoughts) == 0 {
		content = fmt.Sprintf("No thoughts found mentioning %q.", entityName)
	} else {
		var lines []string
		lines = append(lines, fmt.Sprintf("Mentioned in %d thoughts", len(thoughts)))
		for i, t := range thoughts {
			if i >= 10 {
				break
			}
			summary := truncate(t.Content, 100)
			lines = append(lines, fmt.Sprintf("- %s", summary))
		}
		content = strings.Join(lines, "\n")

		if g.summarizer != nil {
			var excerpts []string
			for i, t := range thoughts {
				if i >= 15 {
					break
				}
				excerpts = append(excerpts, "- "+truncate(t.Content, 150))
			}
			prompt := fmt.Sprintf(
				"Summarize the following thoughts mentioning %q into a concise paragraph highlighting key insights.\n\n%s",
				entityName,
				strings.Join(excerpts, "\n"),
			)
			if summary, err := g.summarizer.Summarize(ctx, prompt); err == nil && summary != "" {
				content = summary
			}
		}
	}

	return &Digest{
		Type:               DigestEntity,
		Title:              fmt.Sprintf("Entity: %s", entityName),
		Content:            content,
		SourceThoughtCount: len(thoughts),
	}, nil
}

// Save persists a digest to the database.
func (g *Generator) Save(ctx context.Context, d *Digest) error {
	result, err := g.db.ExecContext(ctx,
		`INSERT INTO memory_digests (digest_type, title, content, period_start, period_end, source_thought_count)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		string(d.Type), d.Title, d.Content,
		nullStr(d.PeriodStart), nullStr(d.PeriodEnd), d.SourceThoughtCount)
	if err != nil {
		return fmt.Errorf("digest: save: %w", err)
	}
	id, _ := result.LastInsertId()
	d.ID = id
	return nil
}

// List retrieves digests by type, newest first.
func (g *Generator) List(ctx context.Context, dtype DigestType, limit int) ([]Digest, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := g.db.QueryContext(ctx,
		`SELECT id, digest_type, title, content, COALESCE(period_start,''), COALESCE(period_end,''),
		 source_thought_count, created_at
		 FROM memory_digests WHERE digest_type = ? ORDER BY created_at DESC LIMIT ?`,
		string(dtype), limit)
	if err != nil {
		return nil, fmt.Errorf("digest: list: %w", err)
	}
	defer rows.Close()

	var digests []Digest
	for rows.Next() {
		var d Digest
		var dtype string
		if err := rows.Scan(&d.ID, &dtype, &d.Title, &d.Content, &d.PeriodStart, &d.PeriodEnd, &d.SourceThoughtCount, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("digest: list scan: %w", err)
		}
		d.Type = DigestType(dtype)
		digests = append(digests, d)
	}
	return digests, rows.Err()
}

func entityNames(entities []entity.Entity, max int) []string {
	var names []string
	for i, e := range entities {
		if i >= max {
			break
		}
		names = append(names, e.Name)
	}
	return names
}

func truncate(s string, maxLen int) string {
	// Replace newlines with spaces for summary.
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
