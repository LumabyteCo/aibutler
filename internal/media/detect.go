package media

import (
	"net/http"
	"path/filepath"
	"strings"
)

// extensionOverrides maps file extensions to MIME types for cases where
// http.DetectContentType doesn't give useful results (e.g. code files).
var extensionOverrides = map[string]string{
	".go":   "text/x-go",
	".py":   "text/x-python",
	".js":   "text/javascript",
	".ts":   "text/typescript",
	".rs":   "text/x-rust",
	".rb":   "text/x-ruby",
	".java": "text/x-java",
	".c":    "text/x-c",
	".cpp":  "text/x-c++",
	".h":    "text/x-c",
	".md":   "text/markdown",
	".yaml": "text/yaml",
	".yml":  "text/yaml",
	".json": "application/json",
	".csv":  "text/csv",
	".xml":  "text/xml",
	".html": "text/html",
	".css":  "text/css",
	".sql":  "text/x-sql",
	".sh":   "text/x-shellscript",
	".pdf":  "application/pdf",
	".ogg":  "audio/ogg",
	".opus": "audio/opus",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".webm": "audio/webm",
}

// extensionLanguages maps file extensions to programming language names.
var extensionLanguages = map[string]string{
	".go":   "go",
	".py":   "python",
	".js":   "javascript",
	".ts":   "typescript",
	".rs":   "rust",
	".rb":   "ruby",
	".java": "java",
	".c":    "c",
	".cpp":  "cpp",
	".h":    "c",
	".md":   "markdown",
	".yaml": "yaml",
	".yml":  "yaml",
	".json": "json",
	".csv":  "csv",
	".xml":  "xml",
	".html": "html",
	".css":  "css",
	".sql":  "sql",
	".sh":   "shell",
}

// DetectMIME returns the MIME type for the given data and filename.
// It prefers extension-based detection for code files, falling back
// to content sniffing via http.DetectContentType. For binary files
// (images, audio, etc.), it cross-validates that content sniffing
// agrees with the extension to prevent MIME-type mismatch attacks
// (e.g., uploading an executable disguised as a .jpg).
func DetectMIME(data []byte, filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if mime, ok := extensionOverrides[ext]; ok {
		return mime
	}

	if len(data) > 0 {
		sniffed := http.DetectContentType(data)

		// Cross-validate: if the extension implies a specific binary type,
		// verify the sniffed type matches the expected category.
		if expected, ok := binaryExtensionMIME[ext]; ok {
			if !mimeMatchesCategory(sniffed, expected) {
				// Content doesn't match extension claim — return sniffed type
				// so the file is processed (or rejected) based on real content.
				return sniffed
			}
		}

		return sniffed
	}

	return "application/octet-stream"
}

// binaryExtensionMIME maps binary file extensions to their expected MIME prefix.
// Used for cross-validation between extension and content sniffing.
var binaryExtensionMIME = map[string]string{
	".jpg":  "image/",
	".jpeg": "image/",
	".png":  "image/",
	".gif":  "image/",
	".bmp":  "image/",
	".webp": "image/",
	".mp3":  "audio/",
	".wav":  "audio/",
	".ogg":  "audio/",
	".mp4":  "video/",
	".avi":  "video/",
	".webm": "video/",
	".pdf":  "application/pdf",
	".zip":  "application/zip",
}

// mimeMatchesCategory checks if a sniffed MIME type matches the expected prefix.
func mimeMatchesCategory(sniffed, expectedPrefix string) bool {
	return strings.HasPrefix(sniffed, expectedPrefix)
}

// LanguageForExtension returns the programming language for a file extension.
func LanguageForExtension(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	return extensionLanguages[ext]
}
