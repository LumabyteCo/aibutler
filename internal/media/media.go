package media

import (
	"context"
	"fmt"
)

// Result holds the output of processing a media attachment.
type Result struct {
	Type      string            // "text", "image", "code", "audio_transcript"
	Content   string            // Extracted text or description
	MimeType  string            // Original MIME type
	Language  string            // Programming language or spoken language
	ImageData []byte            // Raw image bytes for vision models (nil if text-only)
	Metadata  map[string]string // Additional info (page count, dimensions, etc.)
}

// Processor handles a specific category of media.
type Processor interface {
	CanProcess(mimeType string) bool
	Process(ctx context.Context, data []byte, mimeType, filename string) (*Result, error)
}

// Pipeline routes incoming media to the appropriate processor.
type Pipeline struct {
	processors []Processor
	maxSize    int64 // Maximum file size in bytes
}

// NewPipeline creates a media pipeline with the given maximum file size.
func NewPipeline(maxSize int64) *Pipeline {
	return &Pipeline{maxSize: maxSize}
}

// Register adds a processor to the pipeline.
func (p *Pipeline) Register(proc Processor) {
	p.processors = append(p.processors, proc)
}

// Process runs the appropriate processor for the given file data.
func (p *Pipeline) Process(ctx context.Context, data []byte, filename string) (*Result, error) {
	if int64(len(data)) > p.maxSize {
		return nil, fmt.Errorf("media: file too large (%d bytes, max %d)", len(data), p.maxSize)
	}

	mime := DetectMIME(data, filename)

	for _, proc := range p.processors {
		if proc.CanProcess(mime) {
			return proc.Process(ctx, data, mime, filename)
		}
	}

	return &Result{
		Type:     "file",
		MimeType: mime,
		Metadata: map[string]string{"filename": filename},
	}, nil
}
