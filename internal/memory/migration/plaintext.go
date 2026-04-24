package migration

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// PlaintextImporter imports plain text files, splitting on double newlines.
type PlaintextImporter struct {
	Filename string
}

func (p *PlaintextImporter) Name() string { return "plaintext" }

func (p *PlaintextImporter) Parse(ctx context.Context, r io.Reader, save SaveFunc) error {
	data, err := io.ReadAll(LimitedReader(r))
	if err != nil {
		return fmt.Errorf("plaintext: read: %w", err)
	}

	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}

	tags := []string{"import", "plaintext"}
	if p.Filename != "" {
		tags = append(tags, p.Filename)
	}

	// Split on double newlines (paragraph separator).
	paragraphs := strings.Split(text, "\n\n")
	for _, para := range paragraphs {
		if err := ctx.Err(); err != nil {
			return err
		}
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if err := save(ctx, para, "plaintext", tags); err != nil {
			return err
		}
	}

	return nil
}
