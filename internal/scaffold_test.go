package internal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVSCodeScaffoldExists(t *testing.T) {
	// Determine project root relative to this test file.
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Dir(filepath.Dir(filename))

	pkgPath := filepath.Join(projectRoot, "vscode-extension", "package.json")
	if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
		t.Fatalf("vscode-extension/package.json does not exist at %s", pkgPath)
	}

	extPath := filepath.Join(projectRoot, "vscode-extension", "src", "extension.ts")
	if _, err := os.Stat(extPath); os.IsNotExist(err) {
		t.Fatalf("vscode-extension/src/extension.ts does not exist at %s", extPath)
	}
}
