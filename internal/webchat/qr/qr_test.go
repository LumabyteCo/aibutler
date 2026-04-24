package qr

import (
	"testing"
)

func TestGenerateQR_ReturnsBytes(t *testing.T) {
	data, err := GenerateQR("https://butler.local:8080")
	if err != nil {
		t.Fatalf("GenerateQR failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty PNG data")
	}

	// Check PNG signature.
	if len(data) < 8 {
		t.Fatal("data too short to be a PNG")
	}
	pngSig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	for i, b := range pngSig {
		if data[i] != b {
			t.Fatalf("invalid PNG signature at byte %d: got %02x, expected %02x", i, data[i], b)
		}
	}
}

func TestGenerateQR_EmptyURL(t *testing.T) {
	_, err := GenerateQR("")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestGenerateQR_LongURL(t *testing.T) {
	// Create a URL longer than max capacity.
	longURL := "https://example.com/" + string(make([]byte, 100))
	_, err := GenerateQR(longURL)
	if err == nil {
		t.Fatal("expected error for URL exceeding max capacity")
	}
}
