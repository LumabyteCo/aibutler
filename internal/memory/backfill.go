package memory

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/LumabyteCo/aibutler/internal/memory/vector"
)

// BatchEmbedFunc embeds a batch of texts, returning one vector per input in the
// SAME order — typically vector.Embedder.EmbedBatch bound as a method value.
// Implementations MUST preserve input order: the backfiller maps results to
// items positionally (chunk[i] -> vecs[i]). A length mismatch is caught and the
// chunk skipped, but silent reordering would mis-associate embeddings.
type BatchEmbedFunc func(ctx context.Context, texts []string) ([][]float32, error)

// defaultBackfillBatch is how many items are embedded per provider call.
const defaultBackfillBatch = 64

// Backfiller embeds memory items that have no vector yet. It is the synchronous,
// batched counterpart to the live async indexer (Store.enqueueIndex): used by
// `memory import` — so a bulk load cannot shed embeddings the way the bounded
// async queue would — and by `memory reindex`, to embed pre-existing or
// previously-dropped items. It is idempotent: only items absent from
// memory_vectors are embedded, and writes go through Upsert.
type Backfiller struct {
	db    *sql.DB
	vec   *vector.Store
	embed BatchEmbedFunc
	model string
	batch int
}

// NewBackfiller creates a Backfiller. db, vec, and embed must be non-nil. model
// is recorded alongside each embedding (matching the live indexer's value).
func NewBackfiller(db *sql.DB, vec *vector.Store, embed BatchEmbedFunc, model string) *Backfiller {
	return &Backfiller{db: db, vec: vec, embed: embed, model: model, batch: defaultBackfillBatch}
}

// BackfillResult summarizes a backfill run.
type BackfillResult struct {
	Embedded int            // items successfully embedded
	Failed   int            // items whose batch (or upsert) failed
	BySource map[string]int // per source type, items embedded
}

// missingQuery selects items of one source type that lack an embedding. Each is
// a complete SELECT literal (no table-name interpolation) so it stays clear of
// the no-raw-SQL-concatenation guard while remaining injection-free.
type missingQuery struct {
	source string
	query  string
}

var backfillSources = []missingQuery{
	{source: "thought", query: `
		SELECT t.id, t.content FROM captured_thoughts t
		LEFT JOIN memory_vectors mv ON mv.source_type = 'thought' AND mv.source_id = t.id
		WHERE mv.source_id IS NULL AND t.content != ''
		ORDER BY t.id`},
	{source: "transcript", query: `
		SELECT t.id, t.content FROM session_transcripts t
		LEFT JOIN memory_vectors mv ON mv.source_type = 'transcript' AND mv.source_id = t.id
		WHERE mv.source_id IS NULL AND t.content != ''
		ORDER BY t.id`},
}

type backfillItem struct {
	id      int64
	content string
}

// BackfillMissing embeds every memory item that has no vector yet, in batches of
// b.batch. A failed batch is logged and counted but does not abort the run, so
// one bad chunk never blocks the rest. Returns partial results on a query error.
func (b *Backfiller) BackfillMissing(ctx context.Context) (*BackfillResult, error) {
	res := &BackfillResult{BySource: map[string]int{}}
	for _, src := range backfillSources {
		if err := b.backfillSource(ctx, src, res); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (b *Backfiller) backfillSource(ctx context.Context, src missingQuery, res *BackfillResult) error {
	items, err := b.loadMissing(ctx, src.query)
	if err != nil {
		return fmt.Errorf("backfill: %s: %w", src.source, err)
	}

	for start := 0; start < len(items); start += b.batch {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + b.batch
		if end > len(items) {
			end = len(items)
		}
		chunk := items[start:end]

		texts := make([]string, len(chunk))
		for i, it := range chunk {
			texts[i] = it.content
		}
		vecs, err := b.embed(ctx, texts)
		if err != nil {
			res.Failed += len(chunk)
			log.Printf("memory: backfill embed %s [%d:%d] failed: %v", src.source, start, end, err)
			continue
		}
		if len(vecs) != len(chunk) {
			res.Failed += len(chunk)
			log.Printf("memory: backfill embed %s [%d:%d] returned %d vectors for %d inputs; skipping chunk",
				src.source, start, end, len(vecs), len(chunk))
			continue
		}
		for i, it := range chunk {
			if len(vecs[i]) == 0 {
				res.Failed++
				continue
			}
			if err := b.vec.Upsert(ctx, src.source, it.id, vecs[i], b.model); err != nil {
				res.Failed++
				log.Printf("memory: backfill upsert %s/%d failed: %v", src.source, it.id, err)
				continue
			}
			res.Embedded++
			res.BySource[src.source]++
		}
	}
	return nil
}

func (b *Backfiller) loadMissing(ctx context.Context, query string) ([]backfillItem, error) {
	rows, err := b.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []backfillItem
	for rows.Next() {
		var it backfillItem
		if err := rows.Scan(&it.id, &it.content); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}
