package entity_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/graph"
	"github.com/LumabyteCo/aibutler/testutil"
)

// TestSaveExtractedLinksCooccurring: a person + project from the same text get a
// directed "works_on" edge, traversable via the graph read-side.
func TestSaveExtractedLinksCooccurring(t *testing.T) {
	ctx := context.Background()
	conn := testutil.TestDB(t).Conn()
	store := entity.NewStore(conn)
	g := graph.NewStore(conn)

	ex := entity.Extracted{People: []string{"Sarah"}, Projects: []string{"Q4 Roadmap"}}
	entCount, edgeCount, errs := store.SaveExtracted(ctx, ex, "s1")
	if len(errs) > 0 {
		t.Fatalf("errs: %v", errs)
	}
	if entCount != 2 {
		t.Errorf("entities = %d, want 2", entCount)
	}
	if edgeCount != 1 {
		t.Errorf("edges = %d, want 1", edgeCount)
	}

	node, err := g.FindByName(ctx, "Sarah")
	if err != nil {
		t.Fatalf("find Sarah: %v", err)
	}
	if len(node.Relationships) != 1 {
		t.Fatalf("Sarah relationships = %d, want 1", len(node.Relationships))
	}
	r := node.Relationships[0]
	if r.Relationship != "works_on" || r.Entity.Name != "Q4 Roadmap" || r.Direction != "outgoing" {
		t.Errorf("edge = %+v, want works_on -> Q4 Roadmap (outgoing)", r)
	}
}

// TestSaveExtractedStrengthensOnRepeat: re-extracting the same pair strengthens
// the existing edge rather than creating a duplicate (Hebbian, deduped).
func TestSaveExtractedStrengthensOnRepeat(t *testing.T) {
	ctx := context.Background()
	conn := testutil.TestDB(t).Conn()
	store := entity.NewStore(conn)
	g := graph.NewStore(conn)

	ex := entity.Extracted{People: []string{"Sarah"}, Projects: []string{"Q4 Roadmap"}}
	store.SaveExtracted(ctx, ex, "s1")
	store.SaveExtracted(ctx, ex, "s2")

	node, err := g.FindByName(ctx, "Sarah")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(node.Relationships) != 1 {
		t.Fatalf("relationships = %d, want 1 (deduped, not duplicated)", len(node.Relationships))
	}
	if node.Relationships[0].Confidence <= 0.5 {
		t.Errorf("confidence = %.2f, want > 0.5 after strengthening", node.Relationships[0].Confidence)
	}
}

// TestSaveExtractedSymmetricKnows: two people from the same text get one
// symmetric "knows" edge.
func TestSaveExtractedSymmetricKnows(t *testing.T) {
	ctx := context.Background()
	conn := testutil.TestDB(t).Conn()
	store := entity.NewStore(conn)
	g := graph.NewStore(conn)

	ex := entity.Extracted{People: []string{"Sarah", "Alex"}}
	_, edgeCount, _ := store.SaveExtracted(ctx, ex, "s1")
	if edgeCount != 1 {
		t.Errorf("edges = %d, want 1 (Sarah knows Alex)", edgeCount)
	}

	node, err := g.FindByName(ctx, "Sarah")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(node.Relationships) != 1 || node.Relationships[0].Relationship != "knows" {
		t.Errorf("expected one 'knows' edge, got %+v", node.Relationships)
	}
}

// TestSaveExtractedNoEdgeForSingleEntity: nothing to link with only one entity.
func TestSaveExtractedNoEdgeForSingleEntity(t *testing.T) {
	ctx := context.Background()
	conn := testutil.TestDB(t).Conn()
	store := entity.NewStore(conn)

	_, edgeCount, _ := store.SaveExtracted(ctx, entity.Extracted{People: []string{"Solo"}}, "s1")
	if edgeCount != 0 {
		t.Errorf("edges = %d, want 0 for a single entity", edgeCount)
	}
}
