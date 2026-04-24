//go:build integration

package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/fts"
	"github.com/LumabyteCo/aibutler/internal/memory/hybrid"
	"github.com/LumabyteCo/aibutler/internal/memory/vector"
	"github.com/LumabyteCo/aibutler/internal/model"
	"github.com/LumabyteCo/aibutler/testutil"
)

const (
	ollamaBaseURL  = "http://localhost:11434"
	ollamaEmbedURL = "http://localhost:11434/api/embed"
	embedModel     = "nomic-embed-text:v1.5"
)

// skipIfNoOllama skips the test if Ollama is not running locally.
func skipIfNoOllama(t *testing.T) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(ollamaBaseURL + "/api/tags")
	if err != nil {
		t.Skipf("Ollama not running at %s: %v", ollamaBaseURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Skipf("Ollama returned status %d", resp.StatusCode)
	}
}

// TestE2EOllamaEmbedSingle verifies that the Ollama adapter can generate
// a single embedding from a real local model.
func TestE2EOllamaEmbedSingle(t *testing.T) {
	skipIfNoOllama(t)

	adapter := model.NewEmbeddingOllamaWithURL(ollamaEmbedURL, embedModel, 30*time.Second)

	vec, err := adapter.Embed(context.Background(), "Go concurrency patterns")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(vec) == 0 {
		t.Fatal("expected non-empty embedding vector")
	}

	// nomic-embed-text:v1.5 outputs 768-dimensional vectors.
	if len(vec) != 768 {
		t.Errorf("expected 768 dimensions, got %d", len(vec))
	}

	// Dimension should be cached.
	if adapter.Dimension() != 768 {
		t.Errorf("expected cached dimension 768, got %d", adapter.Dimension())
	}

	t.Logf("embedding dimension: %d, first 5 values: %v", len(vec), vec[:5])
}

// TestE2EOllamaEmbedBatch verifies batch embedding with a real Ollama model.
func TestE2EOllamaEmbedBatch(t *testing.T) {
	skipIfNoOllama(t)

	adapter := model.NewEmbeddingOllamaWithURL(ollamaEmbedURL, embedModel, 30*time.Second)

	texts := []string{
		"Go concurrency patterns",
		"Rust memory safety",
		"Python machine learning",
	}
	vecs, err := adapter.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}

	if len(vecs) != 3 {
		t.Fatalf("expected 3 embeddings, got %d", len(vecs))
	}

	for i, v := range vecs {
		if len(v) != 768 {
			t.Errorf("embedding[%d] has %d dimensions, expected 768", i, len(v))
		}
	}

	// Embeddings for different texts should be different.
	if vecs[0][0] == vecs[1][0] && vecs[0][1] == vecs[1][1] {
		t.Error("embeddings for different texts should differ")
	}
}

// TestE2EVectorStoreRoundTrip tests save → search → delete with real embeddings.
func TestE2EVectorStoreRoundTrip(t *testing.T) {
	skipIfNoOllama(t)

	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	store := vector.NewStore(conn)
	adapter := model.NewEmbeddingOllamaWithURL(ollamaEmbedURL, embedModel, 30*time.Second)

	// Generate embeddings for three different topics.
	texts := []string{
		"Go is a statically typed, compiled language designed for concurrency",
		"Python is popular for machine learning and data science",
		"Rust provides memory safety without garbage collection",
	}

	for i, text := range texts {
		vec, err := adapter.Embed(ctx, text)
		if err != nil {
			t.Fatalf("Embed(%q): %v", text, err)
		}
		if err := store.Save(ctx, "thought", int64(i+1), vec, embedModel); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	// Verify count.
	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 vectors, got %d", count)
	}

	// Search for "concurrent programming in Go" — should rank the Go text highest.
	queryVec, err := adapter.Embed(ctx, "concurrent programming in Go")
	if err != nil {
		t.Fatalf("Embed query: %v", err)
	}

	results, err := store.Search(ctx, queryVec, 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// The Go text (source_id=1) should be most relevant (lowest distance).
	if results[0].SourceID != 1 {
		t.Errorf("expected Go text (id=1) as top result, got id=%d (distance=%f)",
			results[0].SourceID, results[0].Distance)
	}

	t.Logf("search results:")
	for i, r := range results {
		t.Logf("  [%d] source_id=%d distance=%.4f (%s)", i, r.SourceID, r.Distance, texts[r.SourceID-1][:40])
	}

	// Delete and verify.
	if err := store.Delete(ctx, "thought", 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	count, _ = store.Count(ctx)
	if count != 2 {
		t.Errorf("expected 2 vectors after delete, got %d", count)
	}
}

// TestE2EHybridSearchWithOllama tests the full three-source hybrid search
// pipeline with real Ollama embeddings: FTS5 + entity + vector.
func TestE2EHybridSearchWithOllama(t *testing.T) {
	skipIfNoOllama(t)

	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	// Set up all stores.
	ftsStore := fts.NewStore(conn)
	entityStore := entity.NewStore(conn)
	vecStore := vector.NewStore(conn)
	searcher := hybrid.NewSearcher(ftsStore, entityStore)

	// Create Ollama embedding adapter.
	adapter := model.NewEmbeddingOllamaWithURL(ollamaEmbedURL, embedModel, 30*time.Second)

	// Wire vector search into hybrid searcher.
	searcher.SetVectorSearch(vecStore, adapter.Embed)

	// Populate data: thoughts, entities, and embeddings.
	thoughts := []string{
		"Sarah presented the Q4 project roadmap today",
		"We decided to migrate the database to PostgreSQL",
		"The team agreed on using Go for the new microservice",
	}
	for i, thought := range thoughts {
		// Insert into captured_thoughts (triggers FTS5 sync).
		_, err := conn.ExecContext(ctx,
			`INSERT INTO captured_thoughts (content, source, created_at) VALUES (?, 'user', '2026-01-01T00:00:00Z')`,
			thought)
		if err != nil {
			t.Fatalf("insert thought: %v", err)
		}

		// Generate and store embedding.
		vec, err := adapter.Embed(ctx, thought)
		if err != nil {
			t.Fatalf("Embed thought: %v", err)
		}
		if err := vecStore.Save(ctx, "thought", int64(i+1), vec, embedModel); err != nil {
			t.Fatalf("Save vector: %v", err)
		}
	}

	// Add entities.
	entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)
	entityStore.SaveOrUpdate(ctx, entity.TypeProject, "Q4 Roadmap", "", nil)
	entityStore.SaveOrUpdate(ctx, entity.TypeDecision, "Migrate to PostgreSQL", "", nil)

	// --- Test 1: Search for "Sarah" — should get FTS + entity + vector results ---
	results, err := searcher.Search(ctx, "Sarah", 20)
	if err != nil {
		t.Fatalf("search Sarah: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'Sarah'")
	}

	sources := map[string]bool{}
	for _, r := range results {
		// Normalize source prefix for checking.
		src := r.Source
		if strings.HasPrefix(src, "vector:") {
			src = "vector"
		}
		sources[src] = true
	}

	t.Logf("'Sarah' search: %d results, sources: %v", len(results), sources)
	if !sources["thought"] {
		t.Error("expected FTS 'thought' source in Sarah results")
	}
	if !sources["entity"] {
		t.Error("expected 'entity' source in Sarah results")
	}
	if !sources["vector"] {
		t.Error("expected 'vector' source in Sarah results")
	}

	// Results should be sorted by score descending.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted: [%d].Score=%f > [%d].Score=%f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}

	// --- Test 2: Search for "database migration" — vector should find the PostgreSQL thought ---
	results2, err := searcher.Search(ctx, "database migration", 10)
	if err != nil {
		t.Fatalf("search database migration: %v", err)
	}
	if len(results2) == 0 {
		t.Fatal("expected results for 'database migration'")
	}

	t.Logf("'database migration' search: %d results", len(results2))
	for i, r := range results2 {
		t.Logf("  [%d] score=%.4f source=%s content=%q", i, r.Score, r.Source, r.Content)
	}

	// --- Test 3: Search for "Go microservice" — should find the Go thought ---
	results3, err := searcher.Search(ctx, "Go microservice architecture", 10)
	if err != nil {
		t.Fatalf("search Go microservice: %v", err)
	}
	if len(results3) == 0 {
		t.Fatal("expected results for 'Go microservice'")
	}

	t.Logf("'Go microservice architecture' search: %d results", len(results3))
	for i, r := range results3 {
		t.Logf("  [%d] score=%.4f source=%s content=%q", i, r.Score, r.Source, r.Content)
	}
}

// TestE2EOllamaCompatEndpoint tests the OpenAI-compatible endpoint on Ollama.
func TestE2EOllamaCompatEndpoint(t *testing.T) {
	skipIfNoOllama(t)

	adapter := model.NewEmbeddingCompat(
		ollamaBaseURL+"/v1/embeddings",
		"",
		embedModel,
		30*time.Second,
	)

	vec, err := adapter.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed via compat endpoint: %v", err)
	}

	if len(vec) != 768 {
		t.Errorf("expected 768 dimensions, got %d", len(vec))
	}

	t.Logf("compat endpoint: %d dimensions, first 3: %v", len(vec), vec[:3])
}
