package cli

import (
	"testing"

	"github.com/LumabyteCo/aibutler/internal/config"
)

func TestExpandVerifyHook(t *testing.T) {
	// Empty command disables.
	if hooks := expandVerifyHook(config.VerifyHookConfig{}); hooks != nil {
		t.Fatalf("empty command should produce no hooks, got %v", hooks)
	}
	// Default tool set covers the file-mutation tools.
	hooks := expandVerifyHook(config.VerifyHookConfig{Command: "true"})
	if len(hooks) != 1 || len(hooks[0].Tools) != 2 || hooks[0].Tools[0] != "file.write" || hooks[0].Tools[1] != "file.edit" {
		t.Fatalf("unexpected default expansion: %+v", hooks)
	}
	// Explicit tool list wins.
	hooks = expandVerifyHook(config.VerifyHookConfig{Command: "true", Tools: []string{"shell.exec"}})
	if len(hooks) != 1 || len(hooks[0].Tools) != 1 || hooks[0].Tools[0] != "shell.exec" {
		t.Fatalf("unexpected explicit expansion: %+v", hooks)
	}
}
