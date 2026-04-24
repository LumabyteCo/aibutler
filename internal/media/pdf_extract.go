package media

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// ExtractPDFText does a best-effort text extraction from a PDF byte stream.
// It handles uncompressed and FlateDecode streams, extracts text operators
// (Tj, TJ, '), and concatenates them. For scanned/image-only PDFs it returns
// empty string with no error.
func ExtractPDFText(data []byte) (string, error) {
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		return "", fmt.Errorf("pdf: not a PDF file")
	}

	var texts []string
	streams := extractStreams(data)
	for _, raw := range streams {
		decoded := decodeStream(raw)
		if decoded == nil {
			continue
		}
		text := extractTextOps(decoded)
		if text != "" {
			texts = append(texts, text)
		}
	}

	return strings.Join(texts, "\n"), nil
}

// streamInfo holds a raw stream and whether it uses FlateDecode.
type streamInfo struct {
	data    []byte
	deflate bool
}

// extractStreams finds all stream..endstream blocks and whether they use FlateDecode.
func extractStreams(data []byte) []streamInfo {
	var result []streamInfo

	// Regex to find "stream\r?\n" markers.
	streamStart := []byte("stream\n")
	streamStartCR := []byte("stream\r\n")
	endStream := []byte("endstream")

	offset := 0
	for offset < len(data) {
		// Find next "stream" marker.
		idx := bytes.Index(data[offset:], streamStart)
		idxCR := bytes.Index(data[offset:], streamStartCR)

		var startIdx int
		var headerLen int
		if idx < 0 && idxCR < 0 {
			break
		} else if idx < 0 {
			startIdx = offset + idxCR
			headerLen = len(streamStartCR)
		} else if idxCR < 0 {
			startIdx = offset + idx
			headerLen = len(streamStart)
		} else if idxCR <= idx {
			startIdx = offset + idxCR
			headerLen = len(streamStartCR)
		} else {
			startIdx = offset + idx
			headerLen = len(streamStart)
		}

		contentStart := startIdx + headerLen
		endIdx := bytes.Index(data[contentStart:], endStream)
		if endIdx < 0 {
			break
		}

		streamData := data[contentStart : contentStart+endIdx]

		// Check ~200 bytes before stream marker for FlateDecode.
		lookBack := 200
		if startIdx < lookBack {
			lookBack = startIdx
		}
		header := data[startIdx-lookBack : startIdx]
		isDeflate := bytes.Contains(header, []byte("FlateDecode"))

		result = append(result, streamInfo{data: streamData, deflate: isDeflate})
		offset = contentStart + endIdx + len(endStream)
	}

	return result
}

// decodeStream decompresses a stream if needed.
func decodeStream(s streamInfo) []byte {
	if !s.deflate {
		return s.data
	}

	r := flate.NewReader(bytes.NewReader(s.data))
	defer r.Close()

	out, err := io.ReadAll(io.LimitReader(r, 1<<20)) // 1 MB limit per stream
	if err != nil {
		return nil
	}
	return out
}

var (
	// Matches (text) Tj and [(text)] TJ operators.
	tjPattern = regexp.MustCompile(`\(([^)]*)\)\s*Tj`)
	// TJ array: [(text1) num (text2)] TJ
	tjArrayPattern = regexp.MustCompile(`\[([^\]]*)\]\s*TJ`)
	// Parenthesized strings inside TJ arrays.
	parenPattern = regexp.MustCompile(`\(([^)]*)\)`)
)

// extractTextOps pulls text from PDF text operators in a content stream.
func extractTextOps(data []byte) string {
	var parts []string

	// Extract Tj strings.
	for _, m := range tjPattern.FindAllSubmatch(data, -1) {
		text := unescapePDF(m[1])
		if text != "" {
			parts = append(parts, text)
		}
	}

	// Extract TJ array strings.
	for _, m := range tjArrayPattern.FindAllSubmatch(data, -1) {
		for _, pm := range parenPattern.FindAllSubmatch(m[1], -1) {
			text := unescapePDF(pm[1])
			if text != "" {
				parts = append(parts, text)
			}
		}
	}

	return strings.TrimSpace(strings.Join(parts, ""))
}

// unescapePDF handles basic PDF string escapes.
func unescapePDF(b []byte) string {
	s := string(b)
	r := strings.NewReplacer(
		`\n`, "\n",
		`\r`, "\r",
		`\t`, "\t",
		`\\`, `\`,
		`\(`, "(",
		`\)`, ")",
	)
	return r.Replace(s)
}
