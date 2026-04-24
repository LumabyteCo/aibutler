package memory

import (
	"context"

	"github.com/LumabyteCo/aibutler/internal/memory/vector"
)

// EmbedFunc is the signature of an embedding function — typically the Embed
// method of a vector.Embedder bound as a method value. We accept a function
// rather than the full Embedder interface so tests and the cli wiring can
// pass a closure without constructing a full Embedder.
type EmbedFunc func(ctx context.Context, text string) ([]float32, error)

// NewVectorIndexer creates a VectorIndexer that computes embeddings via
// embed and persists them to store using Upsert (so re-indexing the same
// content replaces the old embedding rather than accumulating duplicates).
//
// modelName is recorded alongside each row in memory_vectors for audit and
// to enable "wipe old embeddings when the model changes" in the future.
func NewVectorIndexer(store *vector.Store, embed EmbedFunc, modelName string) VectorIndexer {
	if store == nil || embed == nil {
		return nil
	}
	return &vectorIndexer{store: store, embed: embed, model: modelName}
}

type vectorIndexer struct {
	store *vector.Store
	embed EmbedFunc
	model string
}

func (v *vectorIndexer) IndexContent(ctx context.Context, source string, sourceID int64, content string) error {
	if content == "" {
		return nil
	}
	emb, err := v.embed(ctx, content)
	if err != nil {
		return err
	}
	if len(emb) == 0 {
		return nil
	}
	return v.store.Upsert(ctx, source, sourceID, emb, v.model)
}
