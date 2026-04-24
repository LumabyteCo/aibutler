package media_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/media"
)

func TestDetectMIMEJPEG(t *testing.T) {
	// JPEG magic bytes: FF D8 FF
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	mime := media.DetectMIME(data, "photo.jpg")
	if mime != "image/jpeg" {
		t.Errorf("got %q, want image/jpeg", mime)
	}
}

func TestDetectMIMEPNG(t *testing.T) {
	// PNG magic bytes
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	mime := media.DetectMIME(data, "image.png")
	if mime != "image/png" {
		t.Errorf("got %q, want image/png", mime)
	}
}

func TestDetectMIMEByExtension(t *testing.T) {
	// Go source file — extension override takes priority.
	data := []byte("package main\n")
	mime := media.DetectMIME(data, "main.go")
	if mime != "text/x-go" {
		t.Errorf("got %q, want text/x-go", mime)
	}
}

func TestDetectMIMEPDF(t *testing.T) {
	mime := media.DetectMIME(nil, "document.pdf")
	if mime != "application/pdf" {
		t.Errorf("got %q, want application/pdf", mime)
	}
}

func TestDetectMIMEFallback(t *testing.T) {
	mime := media.DetectMIME(nil, "unknown.xyz")
	if mime != "application/octet-stream" {
		t.Errorf("got %q, want application/octet-stream", mime)
	}
}

func TestLanguageForExtension(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"main.go", "go"},
		{"app.py", "python"},
		{"index.ts", "typescript"},
		{"unknown.xyz", ""},
	}
	for _, tt := range tests {
		got := media.LanguageForExtension(tt.filename)
		if got != tt.want {
			t.Errorf("LanguageForExtension(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}
}

func TestPipelineRouting(t *testing.T) {
	ctx := context.Background()
	p := media.NewDefaultPipeline(20) // 20 MB

	// JPEG → ImageProcessor
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	result, err := p.Process(ctx, jpeg, "photo.jpg")
	if err != nil {
		t.Fatalf("JPEG: %v", err)
	}
	if result.Type != "image" {
		t.Errorf("JPEG type = %q, want image", result.Type)
	}

	// .go → TextProcessor
	goSrc := []byte("package main\nfunc main() {}\n")
	result, err = p.Process(ctx, goSrc, "main.go")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if result.Type != "code" {
		t.Errorf("Go type = %q, want code", result.Type)
	}
	if result.Language != "go" {
		t.Errorf("Go language = %q, want go", result.Language)
	}
}

func TestImageProcessor(t *testing.T) {
	ctx := context.Background()
	p := &media.ImageProcessor{}
	data := []byte{0xFF, 0xD8, 0xFF}

	if !p.CanProcess("image/jpeg") {
		t.Fatal("expected CanProcess(image/jpeg) = true")
	}
	if p.CanProcess("text/plain") {
		t.Fatal("expected CanProcess(text/plain) = false")
	}

	result, err := p.Process(ctx, data, "image/jpeg", "test.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if result.ImageData == nil {
		t.Error("expected ImageData to be set")
	}
}

func TestTextProcessor(t *testing.T) {
	ctx := context.Background()
	p := &media.TextProcessor{}

	if !p.CanProcess("text/plain") {
		t.Fatal("expected CanProcess(text/plain) = true")
	}

	data := []byte("line1\nline2\nline3\n")
	result, err := p.Process(ctx, data, "text/plain", "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != "text" {
		t.Errorf("type = %q, want text", result.Type)
	}
	if !strings.Contains(result.Content, "line1") {
		t.Error("expected content to contain 'line1'")
	}
}

func TestTextProcessorTruncation(t *testing.T) {
	ctx := context.Background()
	p := &media.TextProcessor{}

	// Build 600 lines.
	var lines []string
	for i := 0; i < 600; i++ {
		lines = append(lines, "line")
	}
	data := []byte(strings.Join(lines, "\n"))

	result, err := p.Process(ctx, data, "text/plain", "big.txt")
	if err != nil {
		t.Fatal(err)
	}
	if result.Metadata["truncated"] != "true" {
		t.Error("expected truncated = true")
	}
	// Should be 500 lines.
	outputLines := strings.Split(result.Content, "\n")
	if len(outputLines) != 500 {
		t.Errorf("line count = %d, want 500", len(outputLines))
	}
}

func TestCodeFileProcessor(t *testing.T) {
	ctx := context.Background()
	p := &media.TextProcessor{}

	data := []byte("package main\n\nfunc main() {}\n")
	result, err := p.Process(ctx, data, "text/x-go", "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != "code" {
		t.Errorf("type = %q, want code", result.Type)
	}
	if result.Language != "go" {
		t.Errorf("language = %q, want go", result.Language)
	}
}

func TestPDFProcessorScanned(t *testing.T) {
	ctx := context.Background()
	p := &media.PDFProcessor{}

	if !p.CanProcess("application/pdf") {
		t.Fatal("expected CanProcess(application/pdf) = true")
	}

	// Minimal valid PDF with no text streams → treated as scanned.
	result, err := p.Process(ctx, []byte("%PDF-1.4\n%%EOF"), "application/pdf", "doc.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if result.Metadata["scanned"] != "true" {
		t.Errorf("expected scanned = true, got metadata %v", result.Metadata)
	}
}

func TestPDFProcessorWithText(t *testing.T) {
	ctx := context.Background()
	p := &media.PDFProcessor{}

	// Build a minimal PDF with uncompressed text stream.
	pdf := buildTestPDF("Hello World")
	result, err := p.Process(ctx, pdf, "application/pdf", "hello.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "Hello World") {
		t.Errorf("expected 'Hello World' in content, got %q", result.Content)
	}
}

// buildTestPDF creates a minimal valid PDF with a single text stream.
func buildTestPDF(text string) []byte {
	stream := fmt.Sprintf("BT /F1 12 Tf (%s) Tj ET", text)
	return []byte(fmt.Sprintf(`%%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R /Contents 4 0 R >>
endobj
4 0 obj
<< /Length %d >>
stream
%s
endstream
endobj
%%%%EOF`, len(stream)+1, stream))
}

func TestAudioProcessorNoSTT(t *testing.T) {
	ctx := context.Background()
	p := &media.AudioProcessor{} // No STT provider

	if !p.CanProcess("audio/ogg") {
		t.Fatal("expected CanProcess(audio/ogg) = true")
	}

	result, err := p.Process(ctx, []byte("fake-audio"), "audio/ogg", "voice.ogg")
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != "audio_transcript" {
		t.Errorf("type = %q, want audio_transcript", result.Type)
	}
	if !strings.Contains(result.Content, "voice pipeline") {
		t.Error("expected fallback message mentioning voice pipeline")
	}
}

func TestAudioProcessorWithSTT(t *testing.T) {
	ctx := context.Background()
	stt := func(_ context.Context, _ []byte, _ string) (string, error) {
		return "transcribed text", nil
	}
	p := &media.AudioProcessor{STT: stt}

	result, err := p.Process(ctx, []byte("fake-audio"), "audio/ogg", "voice.ogg")
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "transcribed text" {
		t.Errorf("content = %q, want 'transcribed text'", result.Content)
	}
}

func TestAudioProcessorSTTError(t *testing.T) {
	ctx := context.Background()
	stt := func(_ context.Context, _ []byte, _ string) (string, error) {
		return "", fmt.Errorf("stt failed")
	}
	p := &media.AudioProcessor{STT: stt}

	result, err := p.Process(ctx, []byte("fake-audio"), "audio/ogg", "voice.ogg")
	if err != nil {
		t.Fatal("expected graceful degradation, got error")
	}
	if result.Metadata["stt_error"] != "stt failed" {
		t.Errorf("expected stt_error in metadata, got %v", result.Metadata)
	}
}

func TestDetectMIME_MismatchedExtension(t *testing.T) {
	// ELF binary (executable) disguised as .jpg — content sniffing should override extension.
	elfHeader := []byte{0x7f, 0x45, 0x4c, 0x46, 0x02, 0x01, 0x01, 0x00}
	mime := media.DetectMIME(elfHeader, "malware.jpg")

	// Should NOT return image/jpeg since content is not a JPEG.
	if strings.HasPrefix(mime, "image/") {
		t.Errorf("expected non-image MIME for ELF binary in .jpg, got %q", mime)
	}
}

func TestDetectMIME_MatchingExtension(t *testing.T) {
	// Real JPEG data with .jpg extension — should agree.
	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	mime := media.DetectMIME(jpegData, "photo.jpg")
	if mime != "image/jpeg" {
		t.Errorf("expected image/jpeg for real JPEG with .jpg, got %q", mime)
	}
}

func TestPipelineSizeLimit(t *testing.T) {
	ctx := context.Background()
	p := media.NewPipeline(100) // 100 bytes max

	data := make([]byte, 200)
	_, err := p.Process(ctx, data, "test.bin")
	if err == nil {
		t.Error("expected size limit error")
	}
}
