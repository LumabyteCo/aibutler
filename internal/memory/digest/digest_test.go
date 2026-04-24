package digest_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/digest"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/graph"
	"github.com/LumabyteCo/aibutler/testutil"
)

// fakeSummarizer is a test implementation of digest.Summarizer.
type fakeSummarizer struct {
	called bool
	prompt string
	output string
	err    error
}

func (s *fakeSummarizer) Summarize(_ context.Context, prompt string) (string, error) {
	s.called = true
	s.prompt = prompt
	return s.output, s.err
}

func TestNewGenerator(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	g := digest.NewGenerator(conn, memory.NewStore(conn), entity.NewStore(conn), graph.NewStore(conn))
	if g == nil {
		t.Fatal("expected non-nil generator")
	}
}

func TestWeeklyDigestWithData(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	ent := entity.NewStore(conn)
	g := digest.NewGenerator(conn, mem, ent, graph.NewStore(conn))

	// Seed thoughts.
	mem.SaveThought(ctx, "Meeting with Alice about Project Alpha", "user", "s1", []string{"work"})
	mem.SaveThought(ctx, "Decided to use Go for the backend", "user", "s1", []string{"tech"})
	mem.SaveThought(ctx, "Bob mentioned the deadline is Friday", "user", "s1", nil)
	mem.SaveThought(ctx, "Need to review the PR", "user", "s2", []string{"tech"})
	mem.SaveThought(ctx, "Lunch with Carol at the cafe", "user", "s2", nil)

	// Seed entities.
	ent.SaveOrUpdate(ctx, entity.TypePerson, "Alice", "s1", nil)
	ent.SaveOrUpdate(ctx, entity.TypeProject, "Project Alpha", "s1", nil)

	d, err := g.GenerateWeekly(ctx)
	if err != nil {
		t.Fatalf("weekly: %v", err)
	}
	if d.Type != digest.DigestWeekly {
		t.Errorf("type = %q", d.Type)
	}
	if d.SourceThoughtCount != 5 {
		t.Errorf("thought count = %d, want 5", d.SourceThoughtCount)
	}
	if !strings.Contains(d.Content, "Thoughts captured: 5") {
		t.Errorf("content missing thought count: %q", d.Content)
	}
	if !strings.Contains(d.Content, "Alice") {
		t.Errorf("content missing Alice: %q", d.Content)
	}
}

func TestWeeklyDigestEmpty(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	g := digest.NewGenerator(conn, memory.NewStore(conn), entity.NewStore(conn), graph.NewStore(conn))

	d, err := g.GenerateWeekly(ctx)
	if err != nil {
		t.Fatalf("weekly: %v", err)
	}
	if !strings.Contains(d.Content, "No activity") {
		t.Errorf("content = %q, expected 'No activity'", d.Content)
	}
	if d.SourceThoughtCount != 0 {
		t.Errorf("thought count = %d, want 0", d.SourceThoughtCount)
	}
}

func TestTopicDigest(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	g := digest.NewGenerator(conn, mem, entity.NewStore(conn), graph.NewStore(conn))

	mem.SaveThought(ctx, "Learning golang concurrency patterns", "user", "s1", nil)
	mem.SaveThought(ctx, "Go's golang goroutines are lightweight", "user", "s1", nil)
	mem.SaveThought(ctx, "Python is also great", "user", "s1", nil)

	d, err := g.GenerateTopicDigest(ctx, "golang")
	if err != nil {
		t.Fatalf("topic: %v", err)
	}
	if d.Type != digest.DigestTopic {
		t.Errorf("type = %q", d.Type)
	}
	if d.SourceThoughtCount != 2 {
		t.Errorf("thought count = %d, want 2", d.SourceThoughtCount)
	}
	if !strings.Contains(d.Title, "golang") {
		t.Errorf("title = %q", d.Title)
	}
}

func TestTopicDigestNoMatch(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	g := digest.NewGenerator(conn, memory.NewStore(conn), entity.NewStore(conn), graph.NewStore(conn))

	d, err := g.GenerateTopicDigest(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("topic: %v", err)
	}
	if !strings.Contains(d.Content, "No thoughts found") {
		t.Errorf("content = %q", d.Content)
	}
}

func TestEntityDigest(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	g := digest.NewGenerator(conn, mem, entity.NewStore(conn), graph.NewStore(conn))

	mem.SaveThought(ctx, "Alice presented the quarterly results", "user", "s1", nil)
	mem.SaveThought(ctx, "Discussed budget with Alice", "user", "s2", nil)

	d, err := g.GenerateEntityDigest(ctx, "Alice")
	if err != nil {
		t.Fatalf("entity: %v", err)
	}
	if d.Type != digest.DigestEntity {
		t.Errorf("type = %q", d.Type)
	}
	if d.SourceThoughtCount != 2 {
		t.Errorf("thought count = %d, want 2", d.SourceThoughtCount)
	}
}

func TestSaveDigest(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	g := digest.NewGenerator(conn, memory.NewStore(conn), entity.NewStore(conn), graph.NewStore(conn))

	d := &digest.Digest{
		Type:               digest.DigestWeekly,
		Title:              "Test Digest",
		Content:            "Test content",
		PeriodStart:        "2024-01-01T00:00:00Z",
		PeriodEnd:          "2024-01-08T00:00:00Z",
		SourceThoughtCount: 5,
	}
	if err := g.Save(ctx, d); err != nil {
		t.Fatalf("save: %v", err)
	}
	if d.ID == 0 {
		t.Error("expected non-zero ID after save")
	}

	// Verify in DB.
	var title string
	conn.QueryRowContext(ctx, "SELECT title FROM memory_digests WHERE id = ?", d.ID).Scan(&title)
	if title != "Test Digest" {
		t.Errorf("title = %q", title)
	}
}

func TestListDigests(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	g := digest.NewGenerator(conn, memory.NewStore(conn), entity.NewStore(conn), graph.NewStore(conn))

	for i := 0; i < 3; i++ {
		g.Save(ctx, &digest.Digest{Type: digest.DigestWeekly, Title: "W", Content: "C", SourceThoughtCount: i})
	}

	list, err := g.List(ctx, digest.DigestWeekly, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("count = %d, want 3", len(list))
	}
}

func TestListDigestsFilterByType(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	g := digest.NewGenerator(conn, memory.NewStore(conn), entity.NewStore(conn), graph.NewStore(conn))

	g.Save(ctx, &digest.Digest{Type: digest.DigestWeekly, Title: "W1", Content: "C"})
	g.Save(ctx, &digest.Digest{Type: digest.DigestTopic, Title: "T1", Content: "C"})
	g.Save(ctx, &digest.Digest{Type: digest.DigestWeekly, Title: "W2", Content: "C"})

	list, err := g.List(ctx, digest.DigestWeekly, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("weekly count = %d, want 2", len(list))
	}
}

func TestDigestPeriodDates(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	g := digest.NewGenerator(conn, mem, entity.NewStore(conn), graph.NewStore(conn))

	// Seed a thought so digest generates real content.
	mem.SaveThought(ctx, "test thought", "user", "s1", nil)

	d, err := g.GenerateWeekly(ctx)
	if err != nil {
		t.Fatalf("weekly: %v", err)
	}
	if d.PeriodStart == "" {
		t.Error("PeriodStart is empty")
	}
	if d.PeriodEnd == "" {
		t.Error("PeriodEnd is empty")
	}
	if d.PeriodStart >= d.PeriodEnd {
		t.Errorf("PeriodStart (%s) should be before PeriodEnd (%s)", d.PeriodStart, d.PeriodEnd)
	}
}

func TestWeeklyDigestWithSummarizer(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	mem.SaveThought(ctx, "Working on the AI Butler project", "user", "s1", nil)
	mem.SaveThought(ctx, "Had a productive meeting today", "user", "s1", nil)

	fake := &fakeSummarizer{output: "This week focused on AI Butler development and productive meetings."}
	g := digest.NewGenerator(conn, mem, entity.NewStore(conn), graph.NewStore(conn))
	g.SetSummarizer(fake)

	d, err := g.GenerateWeekly(ctx)
	if err != nil {
		t.Fatalf("weekly: %v", err)
	}
	if !fake.called {
		t.Error("expected summarizer to be called")
	}
	if d.Content != "This week focused on AI Butler development and productive meetings." {
		t.Errorf("content = %q", d.Content)
	}
	if !strings.Contains(fake.prompt, "Recent thoughts") {
		t.Errorf("prompt missing 'Recent thoughts': %q", fake.prompt)
	}
}

func TestWeeklyDigestSummarizerFallback(t *testing.T) {
	// If LLM fails, the content falls back to rule-based output.
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	mem.SaveThought(ctx, "test thought", "user", "s1", nil)

	fake := &fakeSummarizer{err: fmt.Errorf("LLM unavailable")}
	g := digest.NewGenerator(conn, mem, entity.NewStore(conn), graph.NewStore(conn))
	g.SetSummarizer(fake)

	d, err := g.GenerateWeekly(ctx)
	if err != nil {
		t.Fatalf("weekly: %v", err)
	}
	if !strings.Contains(d.Content, "Thoughts captured") {
		t.Errorf("expected fallback content, got: %q", d.Content)
	}
}

func TestSummarizerNotCalledWhenNoThoughts(t *testing.T) {
	// LLM should not be called when there are no thoughts to summarize.
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	fake := &fakeSummarizer{output: "should not appear"}
	g := digest.NewGenerator(conn, memory.NewStore(conn), entity.NewStore(conn), graph.NewStore(conn))
	g.SetSummarizer(fake)

	d, err := g.GenerateWeekly(ctx)
	if err != nil {
		t.Fatalf("weekly: %v", err)
	}
	if fake.called {
		t.Error("summarizer should not be called when there are no thoughts")
	}
	if !strings.Contains(d.Content, "No activity") {
		t.Errorf("expected 'No activity', got: %q", d.Content)
	}
}

func TestTopicDigestWithSummarizer(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	mem.SaveThought(ctx, "Go is great for concurrency", "user", "s1", nil)

	fake := &fakeSummarizer{output: "Go is praised for its concurrency primitives."}
	g := digest.NewGenerator(conn, mem, entity.NewStore(conn), graph.NewStore(conn))
	g.SetSummarizer(fake)

	d, err := g.GenerateTopicDigest(ctx, "Go")
	if err != nil {
		t.Fatalf("topic: %v", err)
	}
	if !fake.called {
		t.Error("expected summarizer to be called")
	}
	if d.Content != "Go is praised for its concurrency primitives." {
		t.Errorf("content = %q", d.Content)
	}
}

func TestEntityDigestWithSummarizer(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	mem.SaveThought(ctx, "Alice presented the quarterly results", "user", "s1", nil)

	fake := &fakeSummarizer{output: "Alice is a key figure in quarterly reviews."}
	g := digest.NewGenerator(conn, mem, entity.NewStore(conn), graph.NewStore(conn))
	g.SetSummarizer(fake)

	d, err := g.GenerateEntityDigest(ctx, "Alice")
	if err != nil {
		t.Fatalf("entity: %v", err)
	}
	if !fake.called {
		t.Error("expected summarizer to be called")
	}
	if d.Content != "Alice is a key figure in quarterly reviews." {
		t.Errorf("content = %q", d.Content)
	}
}

func TestTopicDigestSummarizerFallback(t *testing.T) {
	// When LLM fails, falls back to rule-based content.
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	mem.SaveThought(ctx, "Python is great for scripting", "user", "s1", nil)

	fake := &fakeSummarizer{err: fmt.Errorf("timeout")}
	g := digest.NewGenerator(conn, mem, entity.NewStore(conn), graph.NewStore(conn))
	g.SetSummarizer(fake)

	d, err := g.GenerateTopicDigest(ctx, "Python")
	if err != nil {
		t.Fatalf("topic: %v", err)
	}
	if !strings.Contains(d.Content, "Related thoughts") {
		t.Errorf("expected fallback content, got: %q", d.Content)
	}
}

func TestFactoryRunnerInCLI(t *testing.T) {
	// factoryRunner is tested indirectly via SetSummarizer being wired.
	// Here we verify that SetSummarizer accepts a nil summarizer
	// and falls back to rule-based output.
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	mem.SaveThought(ctx, "a thought", "user", "s1", nil)

	g := digest.NewGenerator(conn, mem, entity.NewStore(conn), graph.NewStore(conn))
	g.SetSummarizer(nil) // explicit nil: rule-based

	d, err := g.GenerateWeekly(ctx)
	if err != nil {
		t.Fatalf("weekly: %v", err)
	}
	if !strings.Contains(d.Content, "Thoughts captured") {
		t.Errorf("expected rule-based content, got: %q", d.Content)
	}
}
