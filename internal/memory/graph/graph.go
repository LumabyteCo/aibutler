// Package graph provides knowledge graph traversal over the entity-relationship model.
package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/memory/bank"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
)

// Node represents an entity with its relationships.
type Node struct {
	Entity        entity.Entity   `json:"entity"`
	Relationships []RelatedEntity `json:"relationships,omitempty"`
}

// RelatedEntity is an entity connected via a relationship.
type RelatedEntity struct {
	Entity       entity.Entity `json:"entity"`
	Relationship string        `json:"relationship"`
	Direction    string        `json:"direction"` // "outgoing" or "incoming"
	Confidence   float64       `json:"confidence"`
}

// Store provides graph traversal queries.
type Store struct {
	db *sql.DB
}

// NewStore creates a graph store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// GetNode returns an entity with all its direct relationships.
func (s *Store) GetNode(ctx context.Context, entityID int64) (*Node, error) {
	// Get the entity itself.
	var e entity.Entity
	var attrsStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, name, COALESCE(attributes, ''), COALESCE(source_session, ''),
		        first_seen, last_seen, mention_count
		 FROM entities WHERE id = ? AND bank = ?`, entityID, bank.FromContext(ctx)).
		Scan(&e.ID, &e.Type, &e.Name, &attrsStr, &e.SourceSession,
			&e.FirstSeen, &e.LastSeen, &e.MentionCount)
	if err != nil {
		return nil, fmt.Errorf("graph.get_node: %w", err)
	}
	if attrsStr != "" {
		_ = json.Unmarshal([]byte(attrsStr), &e.Attributes)
	}

	node := &Node{Entity: e}

	// Get outgoing relationships.
	outRows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.type, e.name, COALESCE(e.attributes, ''), COALESCE(e.source_session, ''),
		        e.first_seen, e.last_seen, e.mention_count,
		        r.relationship, r.confidence
		 FROM entity_relationships r
		 JOIN entities e ON e.id = r.to_entity_id
		 WHERE r.from_entity_id = ?`, entityID)
	if err != nil {
		return nil, fmt.Errorf("graph.get_node: outgoing: %w", err)
	}
	defer outRows.Close()

	for outRows.Next() {
		re, err := scanRelatedEntity(outRows, "outgoing")
		if err != nil {
			return nil, err
		}
		node.Relationships = append(node.Relationships, re)
	}
	if err := outRows.Err(); err != nil {
		return nil, err
	}

	// Get incoming relationships.
	inRows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.type, e.name, COALESCE(e.attributes, ''), COALESCE(e.source_session, ''),
		        e.first_seen, e.last_seen, e.mention_count,
		        r.relationship, r.confidence
		 FROM entity_relationships r
		 JOIN entities e ON e.id = r.from_entity_id
		 WHERE r.to_entity_id = ?`, entityID)
	if err != nil {
		return nil, fmt.Errorf("graph.get_node: incoming: %w", err)
	}
	defer inRows.Close()

	for inRows.Next() {
		re, err := scanRelatedEntity(inRows, "incoming")
		if err != nil {
			return nil, err
		}
		node.Relationships = append(node.Relationships, re)
	}
	if err := inRows.Err(); err != nil {
		return nil, err
	}

	return node, nil
}

// FindByName finds an entity by name (case-insensitive) and returns its graph node.
func (s *Store) FindByName(ctx context.Context, name string) (*Node, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM entities WHERE LOWER(name) = LOWER(?) AND bank = ? LIMIT 1`, name, bank.FromContext(ctx)).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("graph.find_by_name: %w", err)
	}
	return s.GetNode(ctx, id)
}

// RelatedTo returns all entities related to a given entity (1-hop).
func (s *Store) RelatedTo(ctx context.Context, entityID int64) ([]RelatedEntity, error) {
	node, err := s.GetNode(ctx, entityID)
	if err != nil {
		return nil, err
	}
	return node.Relationships, nil
}

// Stats returns graph statistics.
func (s *Store) Stats(ctx context.Context) (map[string]int, error) {
	stats := make(map[string]int)

	var entities, relationships int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entities WHERE bank = ?`, bank.FromContext(ctx)).Scan(&entities)
	if err != nil {
		return nil, fmt.Errorf("graph.stats: entities: %w", err)
	}
	stats["entities"] = entities

	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entity_relationships`).Scan(&relationships)
	if err != nil {
		return nil, fmt.Errorf("graph.stats: relationships: %w", err)
	}
	stats["relationships"] = relationships

	// Count by type.
	rows, err := s.db.QueryContext(ctx, `SELECT type, COUNT(*) FROM entities WHERE bank = ? GROUP BY type`, bank.FromContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("graph.stats: by_type: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		var c int
		if err := rows.Scan(&t, &c); err != nil {
			return nil, err
		}
		stats[t] = c
	}
	return stats, rows.Err()
}

func scanRelatedEntity(rows *sql.Rows, direction string) (RelatedEntity, error) {
	var e entity.Entity
	var attrsStr string
	var rel string
	var conf float64
	if err := rows.Scan(&e.ID, &e.Type, &e.Name, &attrsStr, &e.SourceSession,
		&e.FirstSeen, &e.LastSeen, &e.MentionCount,
		&rel, &conf); err != nil {
		return RelatedEntity{}, fmt.Errorf("graph: scan: %w", err)
	}
	if attrsStr != "" {
		_ = json.Unmarshal([]byte(attrsStr), &e.Attributes)
	}
	return RelatedEntity{
		Entity:       e,
		Relationship: rel,
		Direction:    direction,
		Confidence:   conf,
	}, nil
}
