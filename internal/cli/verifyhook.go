package cli

import (
	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/hook"
)

// expandVerifyHook turns the configurations.hooks.verify shorthand into a
// post-tool hook: after the listed tools mutate files, the configured checker
// command runs and its findings are appended (sanitized, marked untrusted) to
// the tool output the model sees. The model then corrects against real
// checker results instead of its own assessment of the edit.
//
// Example config:
//
//	configurations:
//	  hooks:
//	    verify:
//	      command: "go build ./... 2>&1 | head -20"
//	      tools: ["file.write", "file.edit"]
func expandVerifyHook(v config.VerifyHookConfig) []hook.HookConfig {
	if v.Command == "" {
		return nil
	}
	tools := v.Tools
	if len(tools) == 0 {
		tools = []string{"file.write", "file.edit"}
	}
	return []hook.HookConfig{{Command: v.Command, Tools: tools}}
}
