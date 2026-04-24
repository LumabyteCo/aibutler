package entity_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/testutil"
)

func newEntityStore(t *testing.T) *entity.Store {
	t.Helper()
	db := testutil.TestDB(t)
	return entity.NewStore(db.Conn())
}

// --- Extraction Tests ---

func TestExtractPeople(t *testing.T) {
	tests := []struct {
		text   string
		expect string
	}{
		{"My friend Sarah helped me", "Sarah"},
		{"I met with John yesterday", "John"},
		{"Talk to Alex about the project", "Alex"},
		{"My colleague Bob reviewed the code", "Bob"},
		{"Sarah mentioned it was urgent", "Sarah"},
	}
	for _, tt := range tests {
		result := entity.Extract(tt.text)
		found := false
		for _, p := range result.People {
			if strings.Contains(p, tt.expect) {
				found = true
			}
		}
		if !found {
			t.Errorf("Extract(%q).People = %v, want contains %q", tt.text, result.People, tt.expect)
		}
	}
}

func TestExtractProjects(t *testing.T) {
	tests := []struct {
		text   string
		expect string
	}{
		{"Working on the migration tool", "migration tool"},
		{"Project Phoenix is going well", "Phoenix"},
	}
	for _, tt := range tests {
		result := entity.Extract(tt.text)
		found := false
		for _, p := range result.Projects {
			if strings.Contains(strings.ToLower(p), strings.ToLower(tt.expect)) {
				found = true
			}
		}
		if !found {
			t.Errorf("Extract(%q).Projects = %v, want contains %q", tt.text, result.Projects, tt.expect)
		}
	}
}

func TestExtractDecisions(t *testing.T) {
	tests := []struct {
		text   string
		expect string
	}{
		{"I decided to use Go for this project.", "use Go"},
		{"We agreed to postpone the release.", "postpone the release"},
	}
	for _, tt := range tests {
		result := entity.Extract(tt.text)
		found := false
		for _, d := range result.Decisions {
			if strings.Contains(strings.ToLower(d), strings.ToLower(tt.expect)) {
				found = true
			}
		}
		if !found {
			t.Errorf("Extract(%q).Decisions = %v, want contains %q", tt.text, result.Decisions, tt.expect)
		}
	}
}

func TestExtractActionItems(t *testing.T) {
	tests := []struct {
		text   string
		expect string
	}{
		{"I need to review the PR by Friday.", "review the PR"},
		{"TODO: update the documentation.", "update the documentation"},
		{"Don't forget to send the email.", "send the email"},
		{"Remember to call the dentist.", "call the dentist"},
	}
	for _, tt := range tests {
		result := entity.Extract(tt.text)
		found := false
		for _, a := range result.ActionItems {
			if strings.Contains(strings.ToLower(a), strings.ToLower(tt.expect)) {
				found = true
			}
		}
		if !found {
			t.Errorf("Extract(%q).ActionItems = %v, want contains %q", tt.text, result.ActionItems, tt.expect)
		}
	}
}

func TestExtractInsights(t *testing.T) {
	tests := []struct {
		text   string
		expect string
	}{
		{"I realized that testing early saves time.", "testing early saves time"},
		{"It turns out the bug was in the parser.", "the bug was in the parser"},
	}
	for _, tt := range tests {
		result := entity.Extract(tt.text)
		found := false
		for _, i := range result.Insights {
			if strings.Contains(strings.ToLower(i), strings.ToLower(tt.expect)) {
				found = true
			}
		}
		if !found {
			t.Errorf("Extract(%q).Insights = %v, want contains %q", tt.text, result.Insights, tt.expect)
		}
	}
}

func TestExtractMultipleTypes(t *testing.T) {
	text := "My friend Sarah told me about Project Phoenix. I decided to join the team. I need to set up my dev environment."
	result := entity.Extract(text)

	if len(result.People) == 0 {
		t.Error("expected people")
	}
	if len(result.Projects) == 0 {
		t.Error("expected projects")
	}
	if len(result.Decisions) == 0 {
		t.Error("expected decisions")
	}
	if len(result.ActionItems) == 0 {
		t.Error("expected action items")
	}
}

func TestExtractDeduplication(t *testing.T) {
	// Same pattern match should be deduplicated — "friend Sarah" appears twice.
	text := "My friend Sarah is great. My friend Sarah helped me yesterday."
	result := entity.Extract(text)

	count := 0
	for _, p := range result.People {
		if p == "Sarah" {
			count++
		}
	}
	if count > 1 {
		t.Errorf("expected deduplicated result, got %d occurrences of Sarah in %v", count, result.People)
	}
}

func TestExtractEmptyText(t *testing.T) {
	result := entity.Extract("")
	if len(result.People) != 0 || len(result.Projects) != 0 ||
		len(result.Decisions) != 0 || len(result.ActionItems) != 0 ||
		len(result.Insights) != 0 {
		t.Errorf("expected empty extraction for empty text, got %+v", result)
	}
}

func TestExtractNoMatch(t *testing.T) {
	result := entity.Extract("The weather is nice today.")
	total := len(result.People) + len(result.Projects) + len(result.Decisions) +
		len(result.ActionItems) + len(result.Insights)
	if total != 0 {
		t.Errorf("expected no extraction, got %+v", result)
	}
}

// --- Store Tests ---

func TestSaveOrUpdateNewEntity(t *testing.T) {
	store := newEntityStore(t)
	ctx := context.Background()

	id, err := store.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "sess-1", nil)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	people, err := store.GetByType(ctx, entity.TypePerson, 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(people) != 1 {
		t.Fatalf("got %d, want 1", len(people))
	}
	if people[0].Name != "Sarah" {
		t.Errorf("name = %q, want Sarah", people[0].Name)
	}
	if people[0].MentionCount != 1 {
		t.Errorf("mention_count = %d, want 1", people[0].MentionCount)
	}
}

func TestSaveOrUpdateExisting(t *testing.T) {
	store := newEntityStore(t)
	ctx := context.Background()

	id1, _ := store.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "sess-1", nil)
	id2, _ := store.SaveOrUpdate(ctx, entity.TypePerson, "sarah", "sess-2", nil) // lowercase

	// Should return same ID (case-insensitive match).
	if id2 != id1 {
		t.Errorf("expected same ID, got %d and %d", id1, id2)
	}

	people, _ := store.GetByType(ctx, entity.TypePerson, 10)
	if len(people) != 1 {
		t.Fatalf("got %d people, want 1 (dedup)", len(people))
	}
	if people[0].MentionCount != 2 {
		t.Errorf("mention_count = %d, want 2", people[0].MentionCount)
	}
}

func TestSaveOrUpdateWithAttributes(t *testing.T) {
	store := newEntityStore(t)
	ctx := context.Background()

	attrs := map[string]string{"role": "engineer", "team": "backend"}
	id, _ := store.SaveOrUpdate(ctx, entity.TypePerson, "Bob", "sess-1", attrs)

	people, _ := store.GetByType(ctx, entity.TypePerson, 10)
	if len(people) != 1 {
		t.Fatalf("got %d, want 1", len(people))
	}
	if people[0].Attributes["role"] != "engineer" {
		t.Errorf("attributes = %v, want role=engineer", people[0].Attributes)
	}
	_ = id
}

func TestGetByType(t *testing.T) {
	store := newEntityStore(t)
	ctx := context.Background()

	store.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)
	store.SaveOrUpdate(ctx, entity.TypePerson, "Bob", "", nil)
	store.SaveOrUpdate(ctx, entity.TypeProject, "Phoenix", "", nil)
	store.SaveOrUpdate(ctx, entity.TypeDecision, "Use Go", "", nil)

	people, _ := store.GetByType(ctx, entity.TypePerson, 10)
	if len(people) != 2 {
		t.Errorf("people = %d, want 2", len(people))
	}

	projects, _ := store.GetByType(ctx, entity.TypeProject, 10)
	if len(projects) != 1 {
		t.Errorf("projects = %d, want 1", len(projects))
	}

	decisions, _ := store.GetByType(ctx, entity.TypeDecision, 10)
	if len(decisions) != 1 {
		t.Errorf("decisions = %d, want 1", len(decisions))
	}
}

func TestGetByTypeOrderedByMentionCount(t *testing.T) {
	store := newEntityStore(t)
	ctx := context.Background()

	store.SaveOrUpdate(ctx, entity.TypePerson, "Alice", "", nil)
	store.SaveOrUpdate(ctx, entity.TypePerson, "Bob", "", nil)
	store.SaveOrUpdate(ctx, entity.TypePerson, "Bob", "", nil) // Bob mentioned twice
	store.SaveOrUpdate(ctx, entity.TypePerson, "Bob", "", nil) // Bob mentioned three times

	people, _ := store.GetByType(ctx, entity.TypePerson, 10)
	if len(people) != 2 {
		t.Fatalf("got %d, want 2", len(people))
	}
	// Bob should be first (highest mention count).
	if people[0].Name != "Bob" {
		t.Errorf("first = %q, want Bob (most mentions)", people[0].Name)
	}
	if people[0].MentionCount != 3 {
		t.Errorf("Bob mention_count = %d, want 3", people[0].MentionCount)
	}
}

func TestGetAll(t *testing.T) {
	store := newEntityStore(t)
	ctx := context.Background()

	store.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)
	store.SaveOrUpdate(ctx, entity.TypeProject, "Phoenix", "", nil)
	store.SaveOrUpdate(ctx, entity.TypeDecision, "Use Go", "", nil)

	all, _ := store.GetAll(ctx, 10)
	if len(all) != 3 {
		t.Errorf("got %d, want 3", len(all))
	}
}

func TestGetAllDefaultLimit(t *testing.T) {
	store := newEntityStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		store.SaveOrUpdate(ctx, entity.TypeInsight, strings.Repeat("x", i+1), "", nil)
	}

	all, _ := store.GetAll(ctx, 0)
	if len(all) != 5 {
		t.Errorf("got %d, want 5", len(all))
	}
}

func TestSearch(t *testing.T) {
	store := newEntityStore(t)
	ctx := context.Background()

	store.SaveOrUpdate(ctx, entity.TypePerson, "Sarah Connor", "", nil)
	store.SaveOrUpdate(ctx, entity.TypePerson, "Bob Smith", "", nil)
	store.SaveOrUpdate(ctx, entity.TypeProject, "Sarah's Project", "", nil)

	results, _ := store.Search(ctx, "Sarah", 10)
	if len(results) != 2 {
		t.Errorf("got %d, want 2", len(results))
	}
}

func TestSummary(t *testing.T) {
	store := newEntityStore(t)
	ctx := context.Background()

	// Empty summary.
	summary, _ := store.Summary(ctx)
	if summary != "" {
		t.Errorf("empty summary = %q, want empty", summary)
	}

	// Add entities.
	store.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)
	store.SaveOrUpdate(ctx, entity.TypePerson, "Bob", "", nil)
	store.SaveOrUpdate(ctx, entity.TypeProject, "Phoenix", "", nil)
	store.SaveOrUpdate(ctx, entity.TypeDecision, "Use Go", "", nil)
	store.SaveOrUpdate(ctx, entity.TypeActionItem, "Review PR", "", nil)
	store.SaveOrUpdate(ctx, entity.TypeInsight, "Testing is good", "", nil)

	summary, err := store.Summary(ctx)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if !strings.HasPrefix(summary, "Known: ") {
		t.Errorf("summary = %q, want prefix 'Known: '", summary)
	}
	if !strings.Contains(summary, "2 people") {
		t.Errorf("summary = %q, want contains '2 people'", summary)
	}
	if !strings.Contains(summary, "1 projects") {
		t.Errorf("summary = %q, want contains '1 projects'", summary)
	}
}

func TestSaveRelationship(t *testing.T) {
	store := newEntityStore(t)
	ctx := context.Background()

	sarahID, _ := store.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)
	phoenixID, _ := store.SaveOrUpdate(ctx, entity.TypeProject, "Phoenix", "", nil)

	relID, err := store.SaveRelationship(ctx, sarahID, phoenixID, "works_on", 0.9, "sess-1")
	if err != nil {
		t.Fatalf("save_relationship: %v", err)
	}
	if relID == 0 {
		t.Fatal("expected non-zero relationship id")
	}
}
