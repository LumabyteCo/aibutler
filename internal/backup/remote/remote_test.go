package remote

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestUploadHTTP(t *testing.T) {
	var mu sync.Mutex
	stored := make(map[string][]byte)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			stored[r.URL.Path] = body
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			mu.Lock()
			data, ok := stored[r.URL.Path]
			mu.Unlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write(data)
		}
	}))
	defer srv.Close()

	c := NewClient(Config{
		Provider: ProviderHTTP,
		Endpoint: srv.URL,
	})

	ctx := context.Background()

	// Upload
	if err := c.Upload(ctx, "backup.db", []byte("hello world")); err != nil {
		t.Fatalf("upload: %v", err)
	}

	// Download
	data, err := c.Download(ctx, "backup.db")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("got %q, want %q", string(data), "hello world")
	}
}

func TestDownloadHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("test data"))
	}))
	defer srv.Close()

	c := NewClient(Config{Provider: ProviderHTTP, Endpoint: srv.URL})
	data, err := c.Download(context.Background(), "file.db")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(data) != "test data" {
		t.Errorf("got %q, want %q", string(data), "test data")
	}
}

func TestListS3(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult>
  <Contents>
    <Key>backup-2026-01-01.db</Key>
    <Size>1024</Size>
    <LastModified>2026-01-01T00:00:00Z</LastModified>
  </Contents>
  <Contents>
    <Key>backup-2026-01-02.db</Key>
    <Size>2048</Size>
    <LastModified>2026-01-02T00:00:00Z</LastModified>
  </Contents>
</ListBucketResult>`))
	}))
	defer srv.Close()

	c := NewClient(Config{
		Provider: ProviderS3,
		Endpoint: srv.URL,
		Bucket:   "test-bucket",
	})
	files, err := c.List(context.Background(), "backup-")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	if files[0].Name != "backup-2026-01-01.db" {
		t.Errorf("got name %q, want backup-2026-01-01.db", files[0].Name)
	}
	if files[1].Size != 2048 {
		t.Errorf("got size %d, want 2048", files[1].Size)
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	passphrase := "test-passphrase-2026"
	plaintext := []byte("secret backup data for testing encryption round-trip")

	encrypted, err := Encrypt(plaintext, passphrase)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if string(encrypted) == string(plaintext) {
		t.Error("encrypted data should differ from plaintext")
	}

	decrypted, err := Decrypt(encrypted, passphrase)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("got %q, want %q", string(decrypted), string(plaintext))
	}
}

func TestEncryptNoPassphrase(t *testing.T) {
	data := []byte("no encryption")
	encrypted, err := Encrypt(data, "")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if string(encrypted) != string(data) {
		t.Error("empty passphrase should return data unchanged")
	}
}
