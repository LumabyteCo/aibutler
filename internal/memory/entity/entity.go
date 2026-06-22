// Package entity provides rule-based entity extraction from text.
// Extracts people, projects, decisions, action items, and insights
// without requiring an LLM call.
package entity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Type represents an entity category.
type Type string

const (
	TypePerson     Type = "person"
	TypeProject    Type = "project"
	TypeDecision   Type = "decision"
	TypeActionItem Type = "action_item"
	TypeInsight    Type = "insight"
)

// Entity represents an extracted entity.
type Entity struct {
	ID           int64             `json:"id"`
	Type         Type              `json:"type"`
	Name         string            `json:"name"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	SourceSession string           `json:"source_session,omitempty"`
	FirstSeen    string            `json:"first_seen"`
	LastSeen     string            `json:"last_seen"`
	MentionCount int               `json:"mention_count"`
}

// Relationship represents a connection between two entities.
type Relationship struct {
	ID             int64   `json:"id"`
	FromEntityID   int64   `json:"from_entity_id"`
	ToEntityID     int64   `json:"to_entity_id"`
	Relationship   string  `json:"relationship"`
	Confidence     float64 `json:"confidence"`
	SourceSession  string  `json:"source_session,omitempty"`
	CreatedAt      string  `json:"created_at"`
}

// Extracted is the result of entity extraction from text.
type Extracted struct {
	People      []string // Names of people mentioned
	Projects    []string // Project names mentioned
	Decisions   []string // Decisions described
	ActionItems []string // Action items found
	Insights    []string // Insights identified
}

// Store manages entity persistence.
type Store struct {
	db *sql.DB
}

// NewStore creates an entity store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Extract finds entities in text using rule-based pattern matching.
func Extract(text string) Extracted {
	return Extracted{
		People:      extractPeople(text),
		Projects:    extractProjects(text),
		Decisions:   extractDecisions(text),
		ActionItems: extractActionItems(text),
		Insights:    extractInsights(text),
	}
}

// SaveOrUpdate persists an entity, incrementing mention_count if it already exists.
func (s *Store) SaveOrUpdate(ctx context.Context, entityType Type, name, sessionID string, attrs map[string]string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	var attrsJSON string
	if len(attrs) > 0 {
		b, _ := json.Marshal(attrs)
		attrsJSON = string(b)
	}

	// Try to find existing entity with same type and name.
	var existingID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM entities WHERE type = ? AND LOWER(name) = LOWER(?)`,
		string(entityType), name).Scan(&existingID)

	if err == nil {
		// Update existing: bump mention count and last_seen.
		_, err = s.db.ExecContext(ctx,
			`UPDATE entities SET mention_count = mention_count + 1, last_seen = ?,
			 attributes = COALESCE(?, attributes) WHERE id = ?`,
			now, nullString(attrsJSON), existingID)
		if err != nil {
			return 0, fmt.Errorf("entity.update: %w", err)
		}
		return existingID, nil
	}

	// Insert new entity.
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO entities (type, name, attributes, source_session, first_seen, last_seen, mention_count)
		 VALUES (?, ?, ?, ?, ?, ?, 1)`,
		string(entityType), name, nullString(attrsJSON), sessionID, now, now)
	if err != nil {
		return 0, fmt.Errorf("entity.save: %w", err)
	}
	return result.LastInsertId()
}

// SaveRelationship creates a relationship between two entities.
func (s *Store) SaveRelationship(ctx context.Context, fromID, toID int64, rel string, confidence float64, sessionID string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO entity_relationships (from_entity_id, to_entity_id, relationship, confidence, source_session, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		fromID, toID, rel, confidence, sessionID, now)
	if err != nil {
		return 0, fmt.Errorf("entity.save_relationship: %w", err)
	}
	return result.LastInsertId()
}

// Relationship-strength constants for co-occurrence edges: a new edge starts at
// relBaseConfidence and each repeated co-occurrence strengthens it by
// relConfidenceStep up to relMaxConfidence ("fire together, wire together").
const (
	relBaseConfidence = 0.5
	relConfidenceStep = 0.1
	relMaxConfidence  = 1.0
)

// SaveOrStrengthenRelationship inserts a (from, to, relationship) edge, or — if
// one already exists — strengthens its confidence toward relMaxConfidence and
// refreshes its timestamp. entity_relationships has no UNIQUE constraint, so the
// upsert is performed in Go. Returns the edge id.
func (s *Store) SaveOrStrengthenRelationship(ctx context.Context, fromID, toID int64, rel, sessionID string) (int64, error) {
	var id int64
	var conf float64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, confidence FROM entity_relationships
		 WHERE from_entity_id = ? AND to_entity_id = ? AND relationship = ? LIMIT 1`,
		fromID, toID, rel).Scan(&id, &conf)
	switch {
	case err == nil:
		newConf := conf + relConfidenceStep
		if newConf > relMaxConfidence {
			newConf = relMaxConfidence
		}
		if _, err := s.db.ExecContext(ctx,
			`UPDATE entity_relationships SET confidence = ?, created_at = ? WHERE id = ?`,
			newConf, time.Now().UTC().Format(time.RFC3339), id); err != nil {
			return 0, fmt.Errorf("entity.strengthen_relationship: %w", err)
		}
		return id, nil
	case errors.Is(err, sql.ErrNoRows):
		return s.SaveRelationship(ctx, fromID, toID, rel, relBaseConfidence, sessionID)
	default:
		return 0, fmt.Errorf("entity.strengthen_relationship: lookup: %w", err)
	}
}

// EntityRef is a saved entity's id and type, used to link co-occurring entities.
type EntityRef struct {
	ID   int64
	Type Type
}

// SaveExtracted persists every entity from an extraction (via SaveOrUpdate) and
// links the ones that co-occurred in the same text with typed, deterministic
// edges (zero LLM — see cooccurrenceEdge). It is the single path both the
// runtime and import use, so the knowledge graph actually gets populated.
// Returns the number of entities saved, edges created/strengthened, and errors.
func (s *Store) SaveExtracted(ctx context.Context, ex Extracted, sessionID string) (entities, edges int, errs []string) {
	var refs []EntityRef
	save := func(typ Type, name string) {
		id, err := s.SaveOrUpdate(ctx, typ, name, sessionID, nil)
		if err != nil {
			errs = append(errs, fmt.Sprintf("entity %s %q: %s", typ, name, err))
			return
		}
		entities++
		refs = append(refs, EntityRef{ID: id, Type: typ})
	}
	for _, n := range ex.People {
		save(TypePerson, n)
	}
	for _, n := range ex.Projects {
		save(TypeProject, n)
	}
	for _, d := range ex.Decisions {
		save(TypeDecision, d)
	}
	for _, a := range ex.ActionItems {
		save(TypeActionItem, a)
	}
	for _, i := range ex.Insights {
		save(TypeInsight, i)
	}

	// Link every distinct co-occurring pair.
	for i := 0; i < len(refs); i++ {
		for j := i + 1; j < len(refs); j++ {
			if refs[i].ID == refs[j].ID {
				continue // same entity surfaced twice — no self-edge
			}
			fromID, toID, rel := cooccurrenceEdge(refs[i], refs[j])
			if _, err := s.SaveOrStrengthenRelationship(ctx, fromID, toID, rel, sessionID); err != nil {
				errs = append(errs, fmt.Sprintf("relationship %d->%d: %s", fromID, toID, err))
				continue
			}
			edges++
		}
	}
	return entities, edges, errs
}

// cooccurrenceEdge returns the directed edge (fromID, toID, relationship) for a
// co-occurring pair. Directional pairings orient the actor (person / decision)
// as the source; every other pair is symmetric and oriented by id so repeated
// co-occurrences resolve to the same edge and dedup.
func cooccurrenceEdge(a, b EntityRef) (int64, int64, string) {
	if rel, ok := directionalRel(a.Type, b.Type); ok {
		return a.ID, b.ID, rel
	}
	if rel, ok := directionalRel(b.Type, a.Type); ok {
		return b.ID, a.ID, rel
	}
	rel := "mentioned_with"
	if a.Type == TypePerson && b.Type == TypePerson {
		rel = "knows"
	}
	if a.ID <= b.ID {
		return a.ID, b.ID, rel
	}
	return b.ID, a.ID, rel
}

// directionalRel returns the relationship label when from->to is a known
// directed entity-type pairing, else ("", false).
func directionalRel(from, to Type) (string, bool) {
	switch {
	case from == TypePerson && to == TypeProject:
		return "works_on", true
	case from == TypePerson && to == TypeDecision:
		return "decided", true
	case from == TypePerson && to == TypeActionItem:
		return "assigned_to", true
	case from == TypeDecision && to == TypeProject:
		return "decided_for", true
	case from == TypeActionItem && to == TypeProject:
		return "part_of", true
	}
	return "", false
}

// GetByType returns entities of a specific type.
func (s *Store) GetByType(ctx context.Context, entityType Type, limit int) ([]Entity, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, name, COALESCE(attributes, ''), COALESCE(source_session, ''),
		        first_seen, last_seen, mention_count
		 FROM entities WHERE type = ?
		 ORDER BY mention_count DESC, last_seen DESC
		 LIMIT ?`, string(entityType), limit)
	if err != nil {
		return nil, fmt.Errorf("entity.get_by_type: %w", err)
	}
	defer rows.Close()
	return scanEntities(rows)
}

// GetAll returns all entities ordered by mention count.
func (s *Store) GetAll(ctx context.Context, limit int) ([]Entity, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, name, COALESCE(attributes, ''), COALESCE(source_session, ''),
		        first_seen, last_seen, mention_count
		 FROM entities
		 ORDER BY mention_count DESC, last_seen DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("entity.get_all: %w", err)
	}
	defer rows.Close()
	return scanEntities(rows)
}

// Search finds entities matching a query.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]Entity, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, name, COALESCE(attributes, ''), COALESCE(source_session, ''),
		        first_seen, last_seen, mention_count
		 FROM entities WHERE name LIKE ?
		 ORDER BY mention_count DESC
		 LIMIT ?`, "%"+query+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("entity.search: %w", err)
	}
	defer rows.Close()
	return scanEntities(rows)
}

// Summary returns a compact entity summary for Tier 1 awareness.
// Format: "Known: 5 people, 3 projects, 2 pending decisions, 4 action items"
func (s *Store) Summary(ctx context.Context) (string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT type, COUNT(*) FROM entities GROUP BY type`)
	if err != nil {
		return "", fmt.Errorf("entity.summary: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var t string
		var c int
		if err := rows.Scan(&t, &c); err != nil {
			return "", fmt.Errorf("entity.summary: scan: %w", err)
		}
		counts[t] = c
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	if len(counts) == 0 {
		return "", nil
	}

	var parts []string
	if n, ok := counts["person"]; ok && n > 0 {
		parts = append(parts, fmt.Sprintf("%d people", n))
	}
	if n, ok := counts["project"]; ok && n > 0 {
		parts = append(parts, fmt.Sprintf("%d projects", n))
	}
	if n, ok := counts["decision"]; ok && n > 0 {
		parts = append(parts, fmt.Sprintf("%d decisions", n))
	}
	if n, ok := counts["action_item"]; ok && n > 0 {
		parts = append(parts, fmt.Sprintf("%d action items", n))
	}
	if n, ok := counts["insight"]; ok && n > 0 {
		parts = append(parts, fmt.Sprintf("%d insights", n))
	}

	if len(parts) == 0 {
		return "", nil
	}
	return "Known: " + strings.Join(parts, ", "), nil
}

func scanEntities(rows *sql.Rows) ([]Entity, error) {
	var entities []Entity
	for rows.Next() {
		var e Entity
		var attrsStr string
		if err := rows.Scan(&e.ID, &e.Type, &e.Name, &attrsStr, &e.SourceSession,
			&e.FirstSeen, &e.LastSeen, &e.MentionCount); err != nil {
			return nil, fmt.Errorf("entity: scan: %w", err)
		}
		if attrsStr != "" {
			_ = json.Unmarshal([]byte(attrsStr), &e.Attributes)
		}
		entities = append(entities, e)
	}
	return entities, rows.Err()
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// --- Rule-based extraction patterns ---

var (
	// "My friend Sarah", "I met with John", "Talk to Alex about..."
	personPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:my\s+(?:friend|colleague|boss|wife|husband|partner|sister|brother|mom|dad|manager|coworker)\s+)([A-Z][a-z]+(?:\s+[A-Z][a-z]+)?)`),
		regexp.MustCompile(`(?i)\b(?:met\s+with|talk\s+to|called|emailed|messaged|told|asked)\s+([A-Z][a-z]+(?:\s+[A-Z][a-z]+)?)`),
		regexp.MustCompile(`(?i)([A-Z][a-z]+(?:\s+[A-Z][a-z]+)?)\s+(?:said|told|asked|mentioned|suggested|recommended|thinks|wants)`),
	}

	// "Project X", "working on the migration", "the refactor project"
	projectPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:project|initiative|sprint)\s+["']?([A-Za-z][\w\s-]{1,30})["']?`),
		regexp.MustCompile(`(?i)\bworking\s+on\s+(?:the\s+)?["']?([A-Za-z][\w\s-]{2,30})["']?`),
	}

	// "I decided to...", "We agreed to...", "The decision is..."
	decisionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:I\s+decided|we\s+decided|we\s+agreed|the\s+decision\s+is|I've\s+decided)\s+(?:to\s+)?(.{5,80}?)(?:\.|$)`),
		regexp.MustCompile(`(?i)\b(?:going\s+to|gonna|will)\s+(?:go\s+with|choose|pick|use)\s+(.{5,60}?)(?:\.|$)`),
	}

	// "I need to...", "TODO:", "Don't forget to...", "Remember to..."
	actionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:I\s+need\s+to|I\s+have\s+to|I\s+should|I\s+must|don't\s+forget\s+to|remember\s+to)\s+(.{5,80}?)(?:\.|$)`),
		regexp.MustCompile(`(?i)\bTODO:?\s*(.{5,80}?)(?:\.|$)`),
		regexp.MustCompile(`(?i)\b(?:make\s+sure\s+to|be\s+sure\s+to)\s+(.{5,80}?)(?:\.|$)`),
	}

	// "I realized that...", "It turns out...", "The key insight is..."
	insightPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:I\s+realized|I\s+learned|it\s+turns\s+out|the\s+key\s+(?:insight|takeaway|lesson)\s+is)\s+(?:that\s+)?(.{5,100}?)(?:\.|$)`),
		regexp.MustCompile(`(?i)\b(?:interesting\s+that|important\s+to\s+note|worth\s+noting)\s+(.{5,80}?)(?:\.|$)`),
	}
)

func extractPeople(text string) []string {
	raw := extractMatches(text, personPatterns)
	filtered := raw[:0]
	for _, name := range raw {
		if isPlausiblePerson(name) {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func extractProjects(text string) []string {
	raw := extractMatches(text, projectPatterns)
	// Normalize and dedup by canonical form. "Nimbus", "Nimbus that x",
	// "Nimbus - a weather-aware app" all collapse to "Nimbus" here.
	seen := make(map[string]bool)
	filtered := raw[:0]
	for _, name := range raw {
		normalized := normalizeProjectName(name)
		if !isPlausibleProject(normalized) {
			continue
		}
		key := strings.ToLower(normalized)
		if seen[key] {
			continue
		}
		seen[key] = true
		filtered = append(filtered, normalized)
	}
	return filtered
}

func extractDecisions(text string) []string {
	return extractMatches(text, decisionPatterns)
}

func extractActionItems(text string) []string {
	return extractMatches(text, actionPatterns)
}

func extractInsights(text string) []string {
	return extractMatches(text, insightPatterns)
}

func extractMatches(text string, patterns []*regexp.Regexp) []string {
	seen := make(map[string]bool)
	var results []string
	for _, p := range patterns {
		matches := p.FindAllStringSubmatch(text, -1)
		for _, m := range matches {
			if len(m) > 1 {
				name := strings.TrimSpace(m[1])
				lower := strings.ToLower(name)
				if !seen[lower] && name != "" {
					seen[lower] = true
					results = append(results, name)
				}
			}
		}
	}
	return results
}
