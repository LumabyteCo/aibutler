// Package archive provides ZIP and TAR.GZ listing/extraction using stdlib only.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// ListZip returns the names of all files in a ZIP archive.
func ListZip(_ context.Context, path string) ([]string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("archive: open zip %q: %w", path, err)
	}
	defer r.Close()

	names := make([]string, 0, len(r.File))
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	return names, nil
}

// maxDecompressionRatio is the maximum allowed ratio of decompressed size to
// compressed size. Archives exceeding this ratio are rejected as potential zip bombs.
const maxDecompressionRatio = 100

// maxTotalExtractedSize is the maximum total decompressed size (1 GB).
const maxTotalExtractedSize = 1 << 30

// ExtractZip extracts all files from a ZIP archive into destDir.
// It rejects archives with excessive decompression ratios (zip bomb protection)
// and skips symlinks to prevent symlink-based path traversal.
func ExtractZip(_ context.Context, zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("archive: open zip %q: %w", zipPath, err)
	}
	defer r.Close()

	// Calculate compressed size from file info for ratio check.
	fi, err := os.Stat(zipPath)
	if err != nil {
		return fmt.Errorf("archive: stat %q: %w", zipPath, err)
	}
	compressedSize := fi.Size()
	if compressedSize == 0 {
		compressedSize = 1 // avoid division by zero
	}

	// Sum declared uncompressed sizes and check for zip bomb.
	var totalUncompressed uint64
	for _, f := range r.File {
		totalUncompressed += f.UncompressedSize64
	}
	if totalUncompressed > maxTotalExtractedSize {
		return fmt.Errorf("archive: total uncompressed size %d exceeds limit %d", totalUncompressed, maxTotalExtractedSize)
	}
	if int64(totalUncompressed)/compressedSize > maxDecompressionRatio {
		return fmt.Errorf("archive: decompression ratio %d exceeds limit %d (potential zip bomb)",
			int64(totalUncompressed)/compressedSize, maxDecompressionRatio)
	}

	for _, f := range r.File {
		if err := extractZipFile(f, destDir); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, destDir string) error {
	// Sanitize path to prevent zip-slip.
	target := filepath.Join(destDir, f.Name) //nolint:gosec
	if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), filepath.Clean(destDir)+string(os.PathSeparator)) {
		return fmt.Errorf("archive: illegal path in zip: %q", f.Name)
	}

	// Skip symlinks to prevent symlink-based path traversal.
	if f.FileInfo().Mode()&os.ModeSymlink != 0 {
		return nil // silently skip symlinks
	}

	if f.FileInfo().IsDir() {
		return os.MkdirAll(target, 0750)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
		return fmt.Errorf("archive: create dir: %w", err)
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("archive: open zip entry %q: %w", f.Name, err)
	}
	defer rc.Close()

	out, err := os.Create(target) //nolint:gosec
	if err != nil {
		return fmt.Errorf("archive: create file %q: %w", target, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, io.LimitReader(rc, 100*1024*1024)); err != nil { // 100 MB limit per file
		return fmt.Errorf("archive: extract %q: %w", f.Name, err)
	}
	return nil
}

// ListTar returns the names of all files in a .tar.gz archive.
func ListTar(_ context.Context, path string) ([]string, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("archive: open tar %q: %w", path, err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("archive: open gzip %q: %w", path, err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("archive: read tar %q: %w", path, err)
		}
		names = append(names, hdr.Name)
	}
	return names, nil
}

// RegisterArchiveTools registers media.archive.list and media.archive.extract.
func RegisterArchiveTools(registry toolRegistry) {
	registry.Register(
		"media.archive.list",
		"List files in a ZIP or TAR.GZ archive.",
		`{"type":"object","properties":{"path":{"type":"string","description":"Path to the archive file"}},"required":["path"]}`,
		"tool.file.read",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("media.archive.list: invalid input: %w", err)
			}
			if args.Path == "" {
				return "", fmt.Errorf("media.archive.list: path is required")
			}

			var names []string
			var listErr error

			if strings.HasSuffix(strings.ToLower(args.Path), ".zip") {
				names, listErr = ListZip(ctx, args.Path)
			} else {
				names, listErr = ListTar(ctx, args.Path)
			}
			if listErr != nil {
				return "", listErr
			}

			out, _ := json.Marshal(map[string]interface{}{
				"files": names,
				"count": len(names),
			})
			return string(out), nil
		},
	)

	registry.Register(
		"media.archive.extract",
		"Extract a ZIP archive to a destination directory.",
		`{"type":"object","properties":{"path":{"type":"string","description":"Path to the ZIP archive"},"dest":{"type":"string","description":"Destination directory for extracted files"}},"required":["path","dest"]}`,
		"tool.file.write",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Path string `json:"path"`
				Dest string `json:"dest"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("media.archive.extract: invalid input: %w", err)
			}
			if args.Path == "" || args.Dest == "" {
				return "", fmt.Errorf("media.archive.extract: path and dest are required")
			}
			if err := ExtractZip(ctx, args.Path, args.Dest); err != nil {
				return "", err
			}
			return fmt.Sprintf(`{"status":"extracted","path":%q,"dest":%q}`, args.Path, args.Dest), nil
		},
	)
}
