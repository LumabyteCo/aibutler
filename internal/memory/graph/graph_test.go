package graph_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/graph"
	"github.com/LumabyteCo/aibutler/testutil"
)

type testStores struct {
	entity *entity.Store
	graph  *graph.Store
	conn   *sql.DB
}

func setup(t *testing.T) testStores {
	t.Helper()
	db := testutil.TestDB(t)
	conn := db.Conn()
	return testStores{
		entity: entity.NewStore(conn),
		graph:  graph.NewStore(conn),
		conn:   conn,
	}
}

func TestGetNodeEmpty(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	_, err := s.graph.GetNode(ctx, 999)
	if err == nil {
		t.Fatal("expected error for non-existent node")
	}
}

func TestGetNodeWithRelationships(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	sarahID, _ := s.entity.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)
	bobID, _ := s.entity.SaveOrUpdate(ctx, entity.TypePerson, "Bob", "", nil)
	phoenixID, _ := s.entity.SaveOrUpdate(ctx, entity.TypeProject, "Phoenix", "", nil)

	// Sarah works_on Phoenix, Sarah knows Bob.
	s.entity.SaveRelationship(ctx, sarahID, phoenixID, "works_on", 0.9, "")
	s.entity.SaveRelationship(ctx, sarahID, bobID, "knows", 0.8, "")

	node, err := s.graph.GetNode(ctx, sarahID)
	if err != nil {
		t.Fatalf("get_node: %v", err)
	}
	if node.Entity.Name != "Sarah" {
		t.Errorf("name = %q, want Sarah", node.Entity.Name)
	}
	if len(node.Relationships) != 2 {
		t.Fatalf("relationships = %d, want 2", len(node.Relationships))
	}

	// Both should be outgoing.
	for _, r := range node.Relationships {
		if r.Direction != "outgoing" {
			t.Errorf("direction = %q, want outgoing", r.Direction)
		}
	}
}

func TestGetNodeIncomingRelationships(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	sarahID, _ := s.entity.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)
	phoenixID, _ := s.entity.SaveOrUpdate(ctx, entity.TypeProject, "Phoenix", "", nil)

	// Sarah → works_on → Phoenix
	s.entity.SaveRelationship(ctx, sarahID, phoenixID, "works_on", 0.9, "")

	// Query Phoenix — should have incoming relationship from Sarah.
	node, err := s.graph.GetNode(ctx, phoenixID)
	if err != nil {
		t.Fatalf("get_node: %v", err)
	}
	if len(node.Relationships) != 1 {
		t.Fatalf("relationships = %d, want 1", len(node.Relationships))
	}
	if node.Relationships[0].Direction != "incoming" {
		t.Errorf("direction = %q, want incoming", node.Relationships[0].Direction)
	}
	if node.Relationships[0].Entity.Name != "Sarah" {
		t.Errorf("related entity = %q, want Sarah", node.Relationships[0].Entity.Name)
	}
	if node.Relationships[0].Relationship != "works_on" {
		t.Errorf("relationship = %q, want works_on", node.Relationships[0].Relationship)
	}
}

func TestFindByName(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	s.entity.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)
	s.entity.SaveOrUpdate(ctx, entity.TypePerson, "Bob", "", nil)

	node, err := s.graph.FindByName(ctx, "Sarah")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if node.Entity.Name != "Sarah" {
		t.Errorf("name = %q, want Sarah", node.Entity.Name)
	}
}

func TestFindByNameCaseInsensitive(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	s.entity.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)

	node, err := s.graph.FindByName(ctx, "sarah")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if node.Entity.Name != "Sarah" {
		t.Errorf("name = %q, want Sarah", node.Entity.Name)
	}
}

func TestFindByNameNotFound(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	_, err := s.graph.FindByName(ctx, "Nobody")
	if err == nil {
		t.Fatal("expected error for non-existent name")
	}
}

func TestRelatedTo(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	sarahID, _ := s.entity.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)
	bobID, _ := s.entity.SaveOrUpdate(ctx, entity.TypePerson, "Bob", "", nil)
	phoenixID, _ := s.entity.SaveOrUpdate(ctx, entity.TypeProject, "Phoenix", "", nil)

	s.entity.SaveRelationship(ctx, sarahID, bobID, "knows", 0.8, "")
	s.entity.SaveRelationship(ctx, sarahID, phoenixID, "works_on", 0.9, "")

	related, err := s.graph.RelatedTo(ctx, sarahID)
	if err != nil {
		t.Fatalf("related_to: %v", err)
	}
	if len(related) != 2 {
		t.Errorf("got %d related, want 2", len(related))
	}
}

func TestStatsEmpty(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	stats, err := s.graph.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats["entities"] != 0 {
		t.Errorf("entities = %d, want 0", stats["entities"])
	}
	if stats["relationships"] != 0 {
		t.Errorf("relationships = %d, want 0", stats["relationships"])
	}
}

func TestStatsWithData(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	sarahID, _ := s.entity.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)
	bobID, _ := s.entity.SaveOrUpdate(ctx, entity.TypePerson, "Bob", "", nil)
	s.entity.SaveOrUpdate(ctx, entity.TypeProject, "Phoenix", "", nil)

	s.entity.SaveRelationship(ctx, sarahID, bobID, "knows", 0.8, "")

	stats, err := s.graph.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats["entities"] != 3 {
		t.Errorf("entities = %d, want 3", stats["entities"])
	}
	if stats["relationships"] != 1 {
		t.Errorf("relationships = %d, want 1", stats["relationships"])
	}
	if stats["person"] != 2 {
		t.Errorf("person = %d, want 2", stats["person"])
	}
	if stats["project"] != 1 {
		t.Errorf("project = %d, want 1", stats["project"])
	}
}
