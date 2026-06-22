package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/digest"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/graph"
	"github.com/LumabyteCo/aibutler/internal/memory/migration"
)

// modelSummarizer adapts agent.ModelAdapter to digest.Summarizer,
// enabling LLM-powered digest generation from a single completion call.
type modelSummarizer struct {
	model agent.ModelAdapter
}

func (s *modelSummarizer) Summarize(ctx context.Context, prompt string) (string, error) {
	resp, err := s.model.Complete(ctx, []agent.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// CmdMemory handles the `aibutler memory` subcommands.
func CmdMemory(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(w, "Usage: aibutler memory <subcommand>")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Subcommands:")
		fmt.Fprintln(w, "  import <format> <file>   Import from claude|chatgpt|plaintext")
		fmt.Fprintln(w, "  reindex                  Embed memory items missing a vector")
		fmt.Fprintln(w, "  digest [type] [--topic=X] Generate a memory digest")
		fmt.Fprintln(w, "  digests [type]            List existing digests")
		return nil
	}

	switch args[0] {
	case "import":
		return cmdMemoryImport(app, args[1:], w)
	case "reindex":
		return cmdMemoryReindex(app, w)
	case "digest":
		return cmdMemoryDigest(app, args[1:], w)
	case "digests":
		return cmdMemoryDigests(app, args[1:], w)
	default:
		return fmt.Errorf("unknown memory subcommand: %s", args[0])
	}
}

func cmdMemoryImport(app *App, args []string, w io.Writer) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: aibutler memory import <format> <file>")
	}

	format := args[0]
	filePath := args[1]

	var imp migration.Importer
	switch format {
	case "claude":
		imp = &migration.ClaudeImporter{}
	case "chatgpt":
		imp = &migration.ChatGPTImporter{}
	case "plaintext":
		imp = &migration.PlaintextImporter{Filename: filePath}
	default:
		return fmt.Errorf("unknown format: %s (supported: claude, chatgpt, plaintext)", format)
	}

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()

	conn := app.DB.Conn()
	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	ctx := context.Background()
	result, err := orch.Run(ctx, imp, f, migration.ImportOpts{Filename: filePath, Dedup: true})
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	fmt.Fprintf(w, "Import complete (%s):\n", format)
	fmt.Fprintf(w, "  Thoughts imported:  %d\n", result.ThoughtsImported)
	fmt.Fprintf(w, "  Entities extracted: %d\n", result.EntitiesExtracted)
	if result.Skipped > 0 {
		fmt.Fprintf(w, "  Skipped (dupes):    %d\n", result.Skipped)
	}
	if len(result.Errors) > 0 {
		fmt.Fprintf(w, "  Errors:             %d\n", len(result.Errors))
	}

	// Embed the imported (and any other un-embedded) items synchronously in
	// batches so semantic search can find them. We use the backfiller rather than
	// the live async indexer here so a bulk import can never shed embeddings the
	// way the bounded async queue would.
	fmt.Fprintln(w, "Embedding memory for semantic search (this also backfills any earlier un-embedded items)...")
	bf, _, bfErr := runBackfill(ctx, app)
	switch {
	case bf == nil && bfErr == nil:
		fmt.Fprintln(w, "  Embedded:           skipped — no embedding provider configured")
	default:
		embedded := 0
		if bf != nil {
			embedded = bf.Embedded
		}
		fmt.Fprintf(w, "  Embedded:           %d\n", embedded)
		if bf != nil && bf.Failed > 0 {
			fmt.Fprintf(w, "  Embed failures:     %d\n", bf.Failed)
		}
		if bfErr != nil {
			fmt.Fprintf(w, "  Embed error:        %v\n", bfErr)
		}
	}
	return nil
}

// runBackfill embeds every un-embedded memory item in batches. It returns a nil
// result with no error when no embedding provider is configured, so callers can
// distinguish "skipped" from "ran".
func runBackfill(ctx context.Context, app *App) (*memory.BackfillResult, string, error) {
	if app.VectorStore == nil {
		return nil, "", nil
	}
	embedder, provider := resolveEmbedder(app)
	if embedder == nil {
		return nil, "", nil
	}
	bf := memory.NewBackfiller(app.DB.Conn(), app.VectorStore, embedder.EmbedBatch, provider)
	res, err := bf.BackfillMissing(ctx)
	return res, provider, err
}

// cmdMemoryReindex embeds any memory items that lack a vector — for backfilling
// data imported before embeddings were wired, or items the live async indexer
// dropped under load.
func cmdMemoryReindex(app *App, w io.Writer) error {
	bf, provider, err := runBackfill(context.Background(), app)
	if bf == nil && err == nil {
		return fmt.Errorf("no embedding provider configured (set up Ollama or an embeddings API key)")
	}
	fmt.Fprintf(w, "Re-indexed memory with %s.\n", provider)
	if bf != nil {
		fmt.Fprintf(w, "  Embedded: %d", bf.Embedded)
		if bf.Failed > 0 {
			fmt.Fprintf(w, " (%d failed)", bf.Failed)
		}
		fmt.Fprintln(w)
		for src, n := range bf.BySource {
			fmt.Fprintf(w, "  %s: %d\n", src, n)
		}
	}
	if err != nil {
		return fmt.Errorf("reindex: %w", err)
	}
	return nil
}

func cmdMemoryDigest(app *App, args []string, w io.Writer) error {
	conn := app.DB.Conn()
	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	graphStore := graph.NewStore(conn)
	gen := digest.NewGenerator(conn, memStore, entityStore, graphStore)

	// Wire LLM summarizer if a model adapter is available.
	if adapter, _ := resolveModelAdapter(app); adapter != nil {
		gen.SetSummarizer(&modelSummarizer{model: adapter})
	}

	ctx := context.Background()

	dtype := "weekly"
	topic := ""
	if len(args) > 0 {
		dtype = args[0]
	}

	// Parse --topic=X from args.
	for _, arg := range args {
		if len(arg) > 8 && arg[:8] == "--topic=" {
			topic = arg[8:]
		}
	}

	var d *digest.Digest
	var err error

	switch dtype {
	case "weekly":
		d, err = gen.GenerateWeekly(ctx)
	case "topic":
		if topic == "" {
			return fmt.Errorf("topic digest requires --topic=X")
		}
		d, err = gen.GenerateTopicDigest(ctx, topic)
	case "entity":
		if topic == "" {
			return fmt.Errorf("entity digest requires --topic=X (entity name)")
		}
		d, err = gen.GenerateEntityDigest(ctx, topic)
	default:
		return fmt.Errorf("unknown digest type: %s (supported: weekly, topic, entity)", dtype)
	}

	if err != nil {
		return fmt.Errorf("generate digest: %w", err)
	}

	// Save the digest.
	if err := gen.Save(ctx, d); err != nil {
		return fmt.Errorf("save digest: %w", err)
	}

	fmt.Fprintf(w, "%s\n\n%s\n", d.Title, d.Content)
	return nil
}

func cmdMemoryDigests(app *App, args []string, w io.Writer) error {
	conn := app.DB.Conn()
	gen := digest.NewGenerator(conn, memory.NewStore(conn), entity.NewStore(conn), graph.NewStore(conn))

	ctx := context.Background()
	dtype := digest.DigestWeekly
	if len(args) > 0 {
		dtype = digest.DigestType(args[0])
	}

	digests, err := gen.List(ctx, dtype, 10)
	if err != nil {
		return fmt.Errorf("list digests: %w", err)
	}

	if len(digests) == 0 {
		fmt.Fprintln(w, "No digests found.")
		return nil
	}

	for _, d := range digests {
		fmt.Fprintf(w, "[%s] %s (%d thoughts)\n", d.CreatedAt, d.Title, d.SourceThoughtCount)
	}
	return nil
}
