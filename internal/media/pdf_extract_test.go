package media

import (
	"bytes"
	"compress/flate"
	"fmt"
	"strings"
	"testing"
)

func TestExtractPDFTextNotPDF(t *testing.T) {
	_, err := ExtractPDFText([]byte("not a pdf"))
	if err == nil {
		t.Error("expected error for non-PDF")
	}
}

func TestExtractPDFTextEmpty(t *testing.T) {
	text, err := ExtractPDFText([]byte("%PDF-1.4\n%%EOF"))
	if err != nil {
		t.Fatal(err)
	}
	if text != "" {
		t.Errorf("expected empty text for PDF with no streams, got %q", text)
	}
}

func TestExtractPDFTextTj(t *testing.T) {
	stream := "BT /F1 12 Tf (Hello World) Tj ET"
	pdf := makePDF(stream, false)
	text, err := ExtractPDFText(pdf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Hello World") {
		t.Errorf("expected 'Hello World', got %q", text)
	}
}

func TestExtractPDFTextTJArray(t *testing.T) {
	stream := "BT /F1 12 Tf [(Hel) -10 (lo )] TJ [(World)] TJ ET"
	pdf := makePDF(stream, false)
	text, err := ExtractPDFText(pdf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Hello") {
		t.Errorf("expected 'Hello', got %q", text)
	}
	if !strings.Contains(text, "World") {
		t.Errorf("expected 'World', got %q", text)
	}
}

func TestExtractPDFTextFlateDecoded(t *testing.T) {
	stream := "BT (Compressed Text) Tj ET"
	pdf := makePDF(stream, true)
	text, err := ExtractPDFText(pdf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Compressed Text") {
		t.Errorf("expected 'Compressed Text', got %q", text)
	}
}

func TestExtractPDFTextEscapes(t *testing.T) {
	stream := `BT (Hello\nWorld) Tj ET`
	pdf := makePDF(stream, false)
	text, err := ExtractPDFText(pdf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Hello\nWorld") {
		t.Errorf("expected escaped newline, got %q", text)
	}
}

// makePDF builds a minimal PDF with a single stream.
func makePDF(content string, deflated bool) []byte {
	if !deflated {
		return []byte(fmt.Sprintf(`%%PDF-1.4
4 0 obj
<< /Length %d >>
stream
%s
endstream
endobj
%%%%EOF`, len(content)+1, content))
	}

	// FlateDecode compressed stream.
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.DefaultCompression)
	w.Write([]byte(content))
	w.Close()
	compressed := buf.Bytes()

	return []byte(fmt.Sprintf("%%PDF-1.4\n4 0 obj\n<< /Filter /FlateDecode /Length %d >>\nstream\n%sendstream\nendobj\n%%%%EOF",
		len(compressed), string(compressed)))
}
