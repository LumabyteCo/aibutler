package media

import (
	"context"
	"fmt"
	"strings"
)

const maxTextLines = 500

// ImageProcessor handles image/* MIME types.
type ImageProcessor struct{}

func (p *ImageProcessor) CanProcess(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/")
}

func (p *ImageProcessor) Process(_ context.Context, data []byte, mimeType, filename string) (*Result, error) {
	return &Result{
		Type:      "image",
		MimeType:  mimeType,
		ImageData: data,
		Metadata:  map[string]string{"filename": filename, "size": fmt.Sprintf("%d", len(data))},
	}, nil
}

// TextProcessor handles text/* and code MIME types.
type TextProcessor struct{}

func (p *TextProcessor) CanProcess(mimeType string) bool {
	return strings.HasPrefix(mimeType, "text/") || mimeType == "application/json"
}

func (p *TextProcessor) Process(_ context.Context, data []byte, mimeType, filename string) (*Result, error) {
	content := string(data)

	// Truncate at maxTextLines.
	lines := strings.Split(content, "\n")
	truncated := false
	if len(lines) > maxTextLines {
		lines = lines[:maxTextLines]
		truncated = true
	}
	content = strings.Join(lines, "\n")

	lang := LanguageForExtension(filename)
	resultType := "text"
	if lang != "" && lang != "markdown" && lang != "csv" {
		resultType = "code"
	}

	meta := map[string]string{
		"filename":   filename,
		"line_count": fmt.Sprintf("%d", len(lines)),
	}
	if truncated {
		meta["truncated"] = "true"
	}

	return &Result{
		Type:     resultType,
		Content:  content,
		MimeType: mimeType,
		Language: lang,
		Metadata: meta,
	}, nil
}

// PDFProcessor extracts text from PDF files using a pure-Go parser.
// Falls back to metadata-only result for scanned/image PDFs.
type PDFProcessor struct{}

func (p *PDFProcessor) CanProcess(mimeType string) bool {
	return mimeType == "application/pdf"
}

func (p *PDFProcessor) Process(_ context.Context, data []byte, mimeType, filename string) (*Result, error) {
	text, err := ExtractPDFText(data)
	if err != nil {
		// Not a valid PDF — return error.
		return nil, fmt.Errorf("media: pdf: %w", err)
	}

	meta := map[string]string{
		"filename": filename,
		"size":     fmt.Sprintf("%d", len(data)),
	}

	if text == "" {
		// Scanned/image-only PDF — no extractable text.
		meta["scanned"] = "true"
		return &Result{
			Type:     "text",
			Content:  fmt.Sprintf("[PDF: %s — scanned/image-only, no extractable text]", filename),
			MimeType: mimeType,
			Metadata: meta,
		}, nil
	}

	// Truncate at maxTextLines.
	lines := strings.Split(text, "\n")
	if len(lines) > maxTextLines {
		lines = lines[:maxTextLines]
		meta["truncated"] = "true"
	}
	meta["line_count"] = fmt.Sprintf("%d", len(lines))

	return &Result{
		Type:     "text",
		Content:  strings.Join(lines, "\n"),
		MimeType: mimeType,
		Metadata: meta,
	}, nil
}

// STTFunc is a function that transcribes audio data to text.
// Matches the signature of voice.Pipeline.ProcessVoiceInput (simplified).
type STTFunc func(ctx context.Context, audio []byte, mimeType string) (string, error)

// AudioProcessor handles audio/* MIME types.
// If STT is non-nil, it routes audio to the voice pipeline for transcription.
// Otherwise it returns metadata-only.
type AudioProcessor struct {
	STT STTFunc
}

func (p *AudioProcessor) CanProcess(mimeType string) bool {
	return strings.HasPrefix(mimeType, "audio/")
}

func (p *AudioProcessor) Process(ctx context.Context, data []byte, mimeType, filename string) (*Result, error) {
	meta := map[string]string{
		"filename": filename,
		"size":     fmt.Sprintf("%d", len(data)),
	}

	if p.STT != nil {
		text, err := p.STT(ctx, data, mimeType)
		if err != nil {
			// Graceful degradation — return metadata.
			meta["stt_error"] = err.Error()
			return &Result{
				Type:     "audio_transcript",
				Content:  fmt.Sprintf("[Audio: %s — transcription failed: %v]", filename, err),
				MimeType: mimeType,
				Metadata: meta,
			}, nil
		}
		return &Result{
			Type:     "audio_transcript",
			Content:  text,
			MimeType: mimeType,
			Metadata: meta,
		}, nil
	}

	// No STT provider — metadata-only.
	return &Result{
		Type:     "audio_transcript",
		Content:  fmt.Sprintf("[Audio file: %s (%d bytes). Transcription requires voice pipeline.]", filename, len(data)),
		MimeType: mimeType,
		Metadata: meta,
	}, nil
}

// NewDefaultPipeline creates a pipeline with all built-in processors registered.
func NewDefaultPipeline(maxSizeMB int64) *Pipeline {
	p := NewPipeline(maxSizeMB * 1024 * 1024)
	p.Register(&ImageProcessor{})
	p.Register(&TextProcessor{})
	p.Register(&PDFProcessor{})
	p.Register(&AudioProcessor{})
	return p
}
