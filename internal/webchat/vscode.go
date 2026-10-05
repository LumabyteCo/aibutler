package webchat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AgentRunner runs one prompt through the agent pipeline and returns the
// final output. It is satisfied by the model Factory (via a small adapter)
// so the editor routes exercise the full agent loop — memory, tools,
// capability engine — exactly like a chat message would.
type AgentRunner interface {
	Run(ctx context.Context, sessionID, task, channel string) (output string, err error)
}

// VSCodeHandler serves /api/vscode/{action} for the editor extension.
//
// Contract (vscode-extension/src/extension.ts):
//
//	POST /api/vscode/ask     {"message": "..."}                          -> {"output": "..."}
//	POST /api/vscode/explain {"code": "...", "language": "go"}           -> {"output": "..."}
//	POST /api/vscode/fix     {"code": "...", "language": "go"}           -> {"output": "..."}
//	POST /api/vscode/tests   {"code": "...", "language": "go", "path": ""} -> {"output": "..."}
//
// Before this handler existed the extension shipped pointing at a route
// that was never implemented — every command 404'd. This closes that gap
// with the smallest possible surface: one route family, one response shape.
type VSCodeHandler struct {
	Runner AgentRunner
}

// SetRunner injects the agent runner (called by cmd_run once the model
// adapter has resolved). Safe to call at any time.
func (h *VSCodeHandler) SetRunner(r AgentRunner) {
	h.Runner = r
}

type vscodeRequest struct {
	Message  string `json:"message"`
	Code     string `json:"code"`
	Language string `json:"language"`
	Path     string `json:"path"`
}

func (h *VSCodeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/api/vscode/")
	if h.Runner == nil {
		http.Error(w, `{"error": "no agent runner wired — is a model configured?"}`, http.StatusServiceUnavailable)
		return
	}

	var req vscodeRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	prompt, ok := h.promptFor(action, req)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown action %q", action), http.StatusNotFound)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	sessionID := fmt.Sprintf("vscode-%d", time.Now().UnixNano())
	output, err := h.Runner.Run(ctx, sessionID, prompt, "vscode")
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": %q}`, err.Error()), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"output": output})
}

// promptFor turns an editor action + payload into an agent prompt.
func (h *VSCodeHandler) promptFor(action string, req vscodeRequest) (string, bool) {
	lang := req.Language
	if lang == "" {
		lang = "the language shown"
	}
	switch action {
	case "ask":
		if req.Message == "" {
			return "", false
		}
		return req.Message, true
	case "explain":
		if req.Code == "" {
			return "", false
		}
		return fmt.Sprintf("Explain what this %s code does, concisely. Point out anything unusual:\n\n```%s\n%s\n```", lang, req.Language, req.Code), true
	case "fix":
		if req.Code == "" {
			return "", false
		}
		return fmt.Sprintf("Find and fix the bug(s) in this %s code. Return the corrected code in a fenced block, then list the changes briefly:\n\n```%s\n%s\n```", lang, req.Language, req.Code), true
	case "tests":
		if req.Code == "" {
			return "", false
		}
		pathHint := ""
		if req.Path != "" {
			pathHint = fmt.Sprintf(" The code is from %s.", req.Path)
		}
		return fmt.Sprintf("Write unit tests for this %s code.%s Return the test file in a fenced block:\n\n```%s\n%s\n```", lang, pathHint, req.Language, req.Code), true
	}
	return "", false
}