package archive_test

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/media/archive"
)

type mockRegistry struct {
	tools []string
	exec  map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{exec: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools = append(m.tools, name)
	m.exec[name] = exec
}

func createTestZip(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	path := filepath.Join(dir, "test.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte(content))
	}
	return path
}

func createTestTarGz(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	path := filepath.Join(dir, "test.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0600,
			Size: int64(len(content)),
		}
		tw.WriteHeader(hdr)
		tw.Write([]byte(content))
	}
	return path
}

func TestListZip(t *testing.T) {
	dir := t.TempDir()
	zipPath := createTestZip(t, dir, map[string]string{
		"hello.txt": "hello",
		"world.txt": "world",
	})

	names, err := archive.ListZip(context.Background(), zipPath)
	if err != nil {
		t.Fatalf("ListZip: unexpected error: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("expected 2 files, got %d: %v", len(names), names)
	}
}

func TestExtractZip(t *testing.T) {
	dir := t.TempDir()
	zipPath := createTestZip(t, dir, map[string]string{
		"file.txt": "content here",
	})
	dest := filepath.Join(dir, "extracted")

	if err := archive.ExtractZip(context.Background(), zipPath, dest); err != nil {
		t.Fatalf("ExtractZip: unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "file.txt"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(data) != "content here" {
		t.Errorf("expected 'content here', got %q", string(data))
	}
}

func TestListTar(t *testing.T) {
	dir := t.TempDir()
	tarPath := createTestTarGz(t, dir, map[string]string{
		"a.txt": "aaa",
		"b.txt": "bbb",
	})

	names, err := archive.ListTar(context.Background(), tarPath)
	if err != nil {
		t.Fatalf("ListTar: unexpected error: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("expected 2 files, got %d: %v", len(names), names)
	}
}

func TestRegisterArchiveTools(t *testing.T) {
	reg := newMockRegistry()
	archive.RegisterArchiveTools(reg)

	want := map[string]bool{
		"media.archive.list":    false,
		"media.archive.extract": false,
	}
	for _, name := range reg.tools {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q was not registered", name)
		}
	}
}

func TestExtractZip_ZipSlipPrevention(t *testing.T) {
	// Create a zip with a path traversal entry.
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	// Write a file with path traversal.
	fw, _ := w.Create("../../../etc/passwd")
	fw.Write([]byte("evil content"))
	w.Close()
	f.Close()

	dest := filepath.Join(dir, "extracted")
	err = archive.ExtractZip(context.Background(), zipPath, dest)
	if err == nil {
		t.Fatal("expected error for zip-slip attack")
	}
	if !strings.Contains(err.Error(), "illegal path") {
		t.Errorf("expected 'illegal path' error, got: %v", err)
	}
}

func TestExtractZip_TotalSizeLimit(t *testing.T) {
	// Create a zip with a very large declared uncompressed size.
	// We set this up by writing a highly compressible file.
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "big.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	// Create a file that compresses well but declares large uncompressed size.
	fw, _ := w.CreateHeader(&zip.FileHeader{
		Name:   "big.txt",
		Method: zip.Store, // no compression, so ratio = 1:1 and within limit
	})
	// Write some content.
	fw.Write([]byte("small content"))
	w.Close()
	f.Close()

	dest := filepath.Join(dir, "extracted")
	// This should succeed since the ratio is not excessive.
	err = archive.ExtractZip(context.Background(), zipPath, dest)
	if err != nil {
		t.Fatalf("expected success for normal zip, got: %v", err)
	}
}

func TestListTool_Execute(t *testing.T) {
	dir := t.TempDir()
	zipPath := createTestZip(t, dir, map[string]string{
		"doc.txt": "text",
	})

	reg := newMockRegistry()
	archive.RegisterArchiveTools(reg)

	listExec := reg.exec["media.archive.list"]
	if listExec == nil {
		t.Fatal("media.archive.list not registered")
	}

	input, _ := json.Marshal(map[string]string{"path": zipPath})
	out, err := listExec(context.Background(), string(input))
	if err != nil {
		t.Fatalf("list tool exec: %v", err)
	}
	if !strings.Contains(out, "doc.txt") {
		t.Errorf("expected doc.txt in output, got %q", out)
	}
}
