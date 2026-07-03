package memory_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/testutil"
)

func newStore(t *testing.T) *memory.Store {
	t.Helper()
	db := testutil.TestDB(t)
	return memory.NewStore(db.Conn())
}

func TestSaveAndGetThought(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	id, err := store.SaveThought(ctx, "test thought", "terminal", "sess-1", nil)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	thoughts, err := store.GetThoughts(ctx, memory.ThoughtQuery{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(thoughts) != 1 {
		t.Fatalf("got %d thoughts, want 1", len(thoughts))
	}
	if thoughts[0].Content != "test thought" {
		t.Errorf("content = %q, want %q", thoughts[0].Content, "test thought")
	}
	if thoughts[0].Source != "terminal" {
		t.Errorf("source = %q, want %q", thoughts[0].Source, "terminal")
	}
}

func TestSaveThoughtWithTags(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	tags := []string{"career", "decision"}
	_, err := store.SaveThought(ctx, "changing jobs", "user", "", tags)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	thoughts, err := store.GetThoughts(ctx, memory.ThoughtQuery{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(thoughts) != 1 {
		t.Fatalf("got %d, want 1", len(thoughts))
	}
	if len(thoughts[0].Tags) != 2 || thoughts[0].Tags[0] != "career" {
		t.Errorf("tags = %v, want [career decision]", thoughts[0].Tags)
	}
}

func TestGetThoughtsByTag(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.SaveThought(ctx, "first", "user", "", []string{"work"})
	store.SaveThought(ctx, "second", "user", "", []string{"personal"})
	store.SaveThought(ctx, "third", "user", "", []string{"work", "urgent"})

	thoughts, err := store.GetThoughts(ctx, memory.ThoughtQuery{Tags: []string{"work"}})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(thoughts) != 2 {
		t.Fatalf("got %d, want 2", len(thoughts))
	}
}

func TestGetThoughtsByMultipleTags(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.SaveThought(ctx, "a", "user", "", []string{"work"})
	store.SaveThought(ctx, "b", "user", "", []string{"personal"})
	store.SaveThought(ctx, "c", "user", "", []string{"health"})

	// OR logic: work OR personal
	thoughts, err := store.GetThoughts(ctx, memory.ThoughtQuery{Tags: []string{"work", "personal"}})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(thoughts) != 2 {
		t.Fatalf("got %d, want 2", len(thoughts))
	}
}

func TestGetThoughtsByContent(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.SaveThought(ctx, "learn Go programming", "user", "", nil)
	store.SaveThought(ctx, "buy groceries", "user", "", nil)
	store.SaveThought(ctx, "Go concurrency patterns", "user", "", nil)

	thoughts, err := store.GetThoughts(ctx, memory.ThoughtQuery{Contains: "Go"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(thoughts) != 2 {
		t.Fatalf("got %d, want 2", len(thoughts))
	}
}

func TestThoughtCount(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	count, _ := store.ThoughtCount(ctx)
	if count != 0 {
		t.Errorf("initial count = %d, want 0", count)
	}

	store.SaveThought(ctx, "first", "user", "", nil)
	store.SaveThought(ctx, "second", "user", "", nil)

	count, _ = store.ThoughtCount(ctx)
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestSaveAndGetKeyFact(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	id, err := store.SaveKeyFact(ctx, "User prefers dark mode", "preference", "sess-1")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	facts, err := store.GetKeyFacts(ctx, "", 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("got %d, want 1", len(facts))
	}
	if facts[0].Fact != "User prefers dark mode" {
		t.Errorf("fact = %q", facts[0].Fact)
	}
	if facts[0].Category != "preference" {
		t.Errorf("category = %q, want preference", facts[0].Category)
	}
}

// TestSaveKeyFactDedupes is the regression test for the QA finding:
// the same preference fact ("dark mode") was stored 3 times, and the
// "AI Butler" project fact was stored 7 times. Dedup should collapse
// identical facts (same canonical form + same category) to one row.
func TestSaveKeyFactDedupes(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	// Save the "same" fact five times with surface variations.
	variants := []string{
		"User prefers dark mode",
		"user prefers dark mode",     // case diff
		"User prefers dark mode.",    // trailing punct
		"  User prefers dark mode  ", // leading/trailing whitespace
		"User  prefers  dark  mode",  // collapsed whitespace
	}

	ids := make(map[int64]bool)
	for _, v := range variants {
		id, err := store.SaveKeyFact(ctx, v, "preference", "sess-1")
		if err != nil {
			t.Fatalf("save %q: %v", v, err)
		}
		ids[id] = true
	}

	// All five calls should have returned the SAME id.
	if len(ids) != 1 {
		t.Errorf("expected 1 unique id across %d variants, got %d: %v",
			len(variants), len(ids), ids)
	}

	// And the table should have exactly one row in this category.
	facts, err := store.GetKeyFacts(ctx, "preference", 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(facts) != 1 {
		t.Errorf("expected 1 fact row after dedup, got %d: %v", len(facts), facts)
	}
}

// TestSaveKeyFactSeparatesCategories — two facts with identical text but
// different categories ARE legitimately distinct and should NOT dedup.
func TestSaveKeyFactSeparatesCategories(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	id1, _ := store.SaveKeyFact(ctx, "Cairo", "location", "")
	id2, _ := store.SaveKeyFact(ctx, "Cairo", "project_name", "")

	if id1 == id2 {
		t.Errorf("expected different IDs for same text in different categories, got %d for both", id1)
	}

	all, _ := store.GetKeyFacts(ctx, "", 10)
	if len(all) != 2 {
		t.Errorf("expected 2 facts across categories, got %d", len(all))
	}
}

func TestGetKeyFactsByCategory(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.SaveKeyFact(ctx, "User prefers dark mode", "preference", "")
	store.SaveKeyFact(ctx, "User's name is Alex", "identity", "")
	store.SaveKeyFact(ctx, "User prefers Go", "preference", "")

	facts, err := store.GetKeyFacts(ctx, "preference", 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(facts) != 2 {
		t.Fatalf("got %d, want 2", len(facts))
	}
}

// --- Extraction Tests ---

func TestExtractPreference(t *testing.T) {
	results := memory.ExtractKeyFacts("I prefer dark mode.")
	if len(results) == 0 {
		t.Fatal("expected extraction")
	}
	found := false
	for _, r := range results {
		if r.Category == "preference" && strings.Contains(r.Fact, "dark mode") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected preference about dark mode, got %v", results)
	}
}

func TestExtractIdentity(t *testing.T) {
	results := memory.ExtractKeyFacts("My name is Alex.")
	if len(results) == 0 {
		t.Fatal("expected extraction")
	}
	found := false
	for _, r := range results {
		if r.Category == "identity" && strings.Contains(r.Fact, "Alex") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected identity about Alex, got %v", results)
	}
}

func TestExtractDecision(t *testing.T) {
	results := memory.ExtractKeyFacts("I've decided to use Go for this project.")
	if len(results) == 0 {
		t.Fatal("expected extraction")
	}
	found := false
	for _, r := range results {
		if r.Category == "decision" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected decision, got %v", results)
	}
}

func TestExtractProject(t *testing.T) {
	results := memory.ExtractKeyFacts("I'm working on AI Butler.")
	if len(results) == 0 {
		t.Fatal("expected extraction")
	}
	found := false
	for _, r := range results {
		if r.Category == "project" && strings.Contains(r.Fact, "AI Butler") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected project about AI Butler, got %v", results)
	}
}

func TestExtractMultipleFacts(t *testing.T) {
	text := "My name is Alex. I prefer dark mode. I'm working on AI Butler."
	results := memory.ExtractKeyFacts(text)
	if len(results) < 3 {
		t.Errorf("expected at least 3 facts, got %d: %v", len(results), results)
	}

	categories := make(map[string]bool)
	for _, r := range results {
		categories[r.Category] = true
	}
	for _, cat := range []string{"identity", "preference", "project"} {
		if !categories[cat] {
			t.Errorf("missing category %q", cat)
		}
	}
}

func TestExtractNoMatch(t *testing.T) {
	results := memory.ExtractKeyFacts("The weather is nice today.")
	if len(results) != 0 {
		t.Errorf("expected no extraction, got %v", results)
	}
}

func TestExtractEmptyText(t *testing.T) {
	results := memory.ExtractKeyFacts("")
	if results != nil {
		t.Errorf("expected nil, got %v", results)
	}
}

// --- Tool Tests ---

func TestCaptureToolRoundTrip(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	registry := setupRegistry(store)
	tool, ok := registry.Get("memory.capture")
	if !ok {
		t.Fatal("memory.capture not registered")
	}

	input := `{"content":"Remember to review the PR","tags":["work","code"]}`
	result, err := tool.Execute(ctx, input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result, "captured") {
		t.Errorf("result = %q, want contains 'captured'", result)
	}

	// Verify stored.
	thoughts, _ := store.GetThoughts(ctx, memory.ThoughtQuery{})
	if len(thoughts) != 1 {
		t.Fatalf("got %d thoughts, want 1", len(thoughts))
	}
	if thoughts[0].Content != "Remember to review the PR" {
		t.Errorf("content = %q", thoughts[0].Content)
	}
}

func TestCaptureToolExtractsFacts(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	registry := setupRegistry(store)
	tool, _ := registry.Get("memory.capture")

	input := `{"content":"My name is Alex. I prefer dark mode."}`
	result, err := tool.Execute(ctx, input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result, "key fact") {
		t.Errorf("result = %q, want contains 'key fact'", result)
	}

	// Verify facts extracted.
	facts, _ := store.GetKeyFacts(ctx, "", 10)
	if len(facts) < 2 {
		t.Errorf("expected at least 2 facts, got %d", len(facts))
	}
}

func TestSearchToolByTag(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.SaveThought(ctx, "go patterns", "user", "", []string{"code"})
	store.SaveThought(ctx, "dinner plans", "user", "", []string{"personal"})

	registry := setupRegistry(store)
	tool, _ := registry.Get("memory.thoughts")

	input := `{"tags":["code"]}`
	result, err := tool.Execute(ctx, input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var thoughts []memory.Thought
	if err := json.Unmarshal([]byte(result), &thoughts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(thoughts) != 1 {
		t.Fatalf("got %d, want 1", len(thoughts))
	}
	if thoughts[0].Content != "go patterns" {
		t.Errorf("content = %q", thoughts[0].Content)
	}
}

func TestFactsTool(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.SaveKeyFact(ctx, "User prefers Go", "preference", "")
	store.SaveKeyFact(ctx, "User's name is Alex", "identity", "")

	registry := setupRegistry(store)
	tool, _ := registry.Get("memory.facts")

	input := `{"category":"preference"}`
	result, err := tool.Execute(ctx, input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var facts []memory.KeyFact
	if err := json.Unmarshal([]byte(result), &facts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("got %d, want 1", len(facts))
	}
}

func TestUTF8ArabicRoundTrip(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	arabic := "مرحبا بالعالم"
	_, err := store.SaveThought(ctx, arabic, "telegram", "", []string{"عربي"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	thoughts, err := store.GetThoughts(ctx, memory.ThoughtQuery{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(thoughts) != 1 || thoughts[0].Content != arabic {
		t.Errorf("content = %q, want %q", thoughts[0].Content, arabic)
	}
	if len(thoughts[0].Tags) != 1 || thoughts[0].Tags[0] != "عربي" {
		t.Errorf("tags = %v", thoughts[0].Tags)
	}
}

func TestDefaultSource(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.SaveThought(ctx, "test", "", "", nil)

	thoughts, _ := store.GetThoughts(ctx, memory.ThoughtQuery{})
	if len(thoughts) != 1 || thoughts[0].Source != "user" {
		t.Errorf("source = %q, want 'user'", thoughts[0].Source)
	}
}

// --- Helpers ---

func setupRegistry(store *memory.Store) *toolRegistry {
	r := &toolRegistry{tools: make(map[string]toolIface)}
	r.tools["memory.capture"] = &captureToolWrapper{store: store}
	r.tools["memory.thoughts"] = &searchToolWrapper{store: store}
	r.tools["memory.facts"] = &factsToolWrapper{store: store}

	// Use the actual registration to get the real tools.
	// Since we can't import tool.Registry in this test package without
	// a circular dependency, we use a simple wrapper.
	return r
}

// Minimal tool interface for testing without importing tool package.
type toolIface interface {
	Execute(ctx context.Context, input string) (string, error)
}

type toolRegistry struct {
	tools map[string]toolIface
}

func (r *toolRegistry) Get(name string) (toolIface, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Wrapper tools that delegate to the store directly (same logic as real tools).
type captureToolWrapper struct{ store *memory.Store }

func (t *captureToolWrapper) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
		Source  string   `json:"source"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", err
	}
	id, err := t.store.SaveThought(ctx, args.Content, args.Source, "", args.Tags)
	if err != nil {
		return "", err
	}
	extracted := memory.ExtractKeyFacts(args.Content)
	for _, e := range extracted {
		t.store.SaveKeyFact(ctx, e.Fact, e.Category, "")
	}
	result := "Thought captured (id=" + itoa(id) + ")."
	if len(extracted) > 0 {
		result += " Extracted key fact(s)."
	}
	return result, nil
}

type searchToolWrapper struct{ store *memory.Store }

func (t *searchToolWrapper) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Tags  []string `json:"tags"`
		Query string   `json:"query"`
		Limit int      `json:"limit"`
	}
	json.Unmarshal([]byte(input), &args)
	thoughts, err := t.store.GetThoughts(ctx, memory.ThoughtQuery{Tags: args.Tags, Contains: args.Query, Limit: args.Limit})
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(thoughts)
	return string(data), nil
}

type factsToolWrapper struct{ store *memory.Store }

func (t *factsToolWrapper) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Category string `json:"category"`
		Limit    int    `json:"limit"`
	}
	json.Unmarshal([]byte(input), &args)
	facts, err := t.store.GetKeyFacts(ctx, args.Category, args.Limit)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(facts)
	return string(data), nil
}

func itoa(n int64) string {
	return fmt.Sprintf("%d", n)
}
