package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/db"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/fts"
	"github.com/LumabyteCo/aibutler/internal/memory/hybrid"
	"github.com/LumabyteCo/aibutler/internal/ratelimit"
	"github.com/LumabyteCo/aibutler/internal/schedule"
	"github.com/LumabyteCo/aibutler/internal/swarm"
	"github.com/LumabyteCo/aibutler/testutil"
)

// benchDB creates an in-memory database for benchmarks.
func benchDB(b *testing.B) *db.DB {
	b.Helper()
	database, err := db.Open(db.Config{Path: ":memory:"})
	if err != nil {
		b.Fatalf("benchDB: open: %v", err)
	}
	if err := database.ApplySchema(context.Background()); err != nil {
		b.Fatalf("benchDB: schema: %v", err)
	}
	b.Cleanup(func() { database.Close() })
	return database
}

// BenchmarkHybridSearch benchmarks hybrid memory search.
func BenchmarkHybridSearch(b *testing.B) {
	database := benchDB(b)
	ctx := context.Background()
	conn := database.Conn()

	// Seed 100 thoughts.
	for i := 0; i < 100; i++ {
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO captured_thoughts (content, source) VALUES (?, 'bench')`,
			fmt.Sprintf("Thought number %d about topic %d and concept %d", i, i%10, i%5)); err != nil {
			b.Fatalf("insert thought %d: %v", i, err)
		}
	}

	ftsStore := fts.NewStore(conn)
	entityStore := entity.NewStore(conn)
	searcher := hybrid.NewSearcher(ftsStore, entityStore)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := searcher.Search(ctx, "topic concept", 10)
		if err != nil {
			b.Fatalf("search: %v", err)
		}
	}
}

// BenchmarkEntityExtraction benchmarks entity extraction from text.
func BenchmarkEntityExtraction(b *testing.B) {
	text := "My friend Sarah said the migration project is going well. " +
		"I met with John about the refactor. " +
		"I decided to use Go for the new service. " +
		"TODO: review the pull request by Friday. " +
		"I realized that caching improves latency significantly."

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = entity.Extract(text)
	}
}

// BenchmarkCronParsing benchmarks cron expression parsing.
func BenchmarkCronParsing(b *testing.B) {
	exprs := []string{
		"*/5 * * * *",
		"0 9 * * 1-5",
		"0 0 1 * *",
		"30 */2 * * *",
		"@daily",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		expr := exprs[i%len(exprs)]
		cron, err := schedule.ParseCron(expr)
		if err != nil {
			b.Fatalf("parse %q: %v", expr, err)
		}
		_ = cron.Next(time.Now())
	}
}

// BenchmarkA2ATaskExecution benchmarks A2A task execution.
func BenchmarkA2ATaskExecution(b *testing.B) {
	runner := &fakeTaskRunner{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = runner.RunTask(context.Background(), "benchmark task")
	}
}

// BenchmarkSwarmDecompose benchmarks goal decomposition.
func BenchmarkSwarmDecompose(b *testing.B) {
	database := benchDB(b)
	conn := database.Conn()

	decompositionJSON := `{"subtasks":[{"id":"sub-1","task":"subtask 1","depends_on":[]}]}`

	// Pre-create responses for b.N calls.
	responses := make([]agent.Response, b.N)
	for i := range responses {
		responses[i] = agent.Response{Content: decompositionJSON}
	}
	model := testutil.NewFakeModel(responses...)

	orch := swarm.New(conn, model, nil, &fakeTaskRunner{})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := orch.Decompose(context.Background(), "benchmark goal")
		if err != nil {
			b.Fatalf("decompose: %v", err)
		}
	}
}

// BenchmarkRateLimiterAllow benchmarks rate limiter check.
func BenchmarkRateLimiterAllow(b *testing.B) {
	limiter := ratelimit.New(1000000, 1*time.Hour) // High limit to avoid denials.

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(fmt.Sprintf("key-%d", i%100))
	}
}

// BenchmarkSQLiteBackup benchmarks a lightweight DB operation as a proxy for backup overhead.
// Real incremental backup requires on-disk databases and is tested separately.
func BenchmarkSQLiteBackup(b *testing.B) {
	database := benchDB(b)
	ctx := context.Background()
	conn := database.Conn()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var version int
		if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			b.Fatalf("pragma: %v", err)
		}
	}
}
