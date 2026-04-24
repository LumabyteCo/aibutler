package manifest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/plugin/manifest"
)

const minimalTOML = `
name = "test-plugin"
version = "1.0.0"
capabilities = ["tool.call"]
wasm_path = "plugin.wasm"
`

const fullTOML = `
name = "full-plugin"
version = "2.1.0"
description = "A full test plugin"
author = "Test Author"
license = "MIT"
min_butler_version = "0.5.0"
wasm_path = "full.wasm"
capabilities = ["tool.call", "credential.read:openai_api_key", "kv.read", "kv.write"]

[[tools]]
name = "analyze"
description = "Analyze data"
schema = '{"type":"object","properties":{"data":{"type":"string"}}}'
function = "run_analyze"

[[tools]]
name = "summarize"
description = "Summarize text"
schema = '{"type":"object","properties":{"text":{"type":"string"}}}'

[model_adapter]
function = "model_complete"
models = ["custom-model-v1", "custom-model-v2"]

[settings]
api_key = { type = "string", description = "API key for service", required = true }

[configurations]
base_url = { type = "string", default = "https://api.example.com", description = "Base URL" }

[options]
timeout_ms = { type = "int", default = 5000, description = "Timeout in milliseconds" }
`

func TestParseMinimal(t *testing.T) {
	m, err := manifest.Parse([]byte(minimalTOML))
	if err != nil {
		t.Fatalf("parse minimal: %v", err)
	}
	if m.Name != "test-plugin" {
		t.Errorf("name = %q, want test-plugin", m.Name)
	}
	if m.Version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", m.Version)
	}
	if len(m.Capabilities) != 1 || m.Capabilities[0] != "tool.call" {
		t.Errorf("capabilities = %v, want [tool.call]", m.Capabilities)
	}
}

func TestParseFull(t *testing.T) {
	m, err := manifest.Parse([]byte(fullTOML))
	if err != nil {
		t.Fatalf("parse full: %v", err)
	}
	if m.Name != "full-plugin" {
		t.Errorf("name = %q", m.Name)
	}
	if m.Author != "Test Author" {
		t.Errorf("author = %q", m.Author)
	}
	if len(m.Tools) != 2 {
		t.Fatalf("tools count = %d, want 2", len(m.Tools))
	}
	if m.Tools[0].Name != "analyze" {
		t.Errorf("tool[0].name = %q", m.Tools[0].Name)
	}
	if m.Tools[0].Function != "run_analyze" {
		t.Errorf("tool[0].function = %q, want run_analyze", m.Tools[0].Function)
	}
	if m.ModelAdapter == nil {
		t.Fatal("model_adapter is nil")
	}
	if m.ModelAdapter.Function != "model_complete" {
		t.Errorf("model_adapter.function = %q", m.ModelAdapter.Function)
	}
	if len(m.ModelAdapter.Models) != 2 {
		t.Errorf("model_adapter.models count = %d", len(m.ModelAdapter.Models))
	}
}

func TestParseTools(t *testing.T) {
	m, err := manifest.Parse([]byte(fullTOML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Tools[1].Name != "summarize" {
		t.Errorf("tool[1].name = %q, want summarize", m.Tools[1].Name)
	}
	if m.Tools[1].Schema == "" {
		t.Error("tool[1].schema should not be empty")
	}
}

func TestParseThreeEnriches(t *testing.T) {
	m, err := manifest.Parse([]byte(fullTOML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := m.Settings["api_key"]; !ok {
		t.Error("settings should have api_key")
	}
	if m.Settings["api_key"].Required != true {
		t.Error("api_key should be required")
	}
	if _, ok := m.Configurations["base_url"]; !ok {
		t.Error("configurations should have base_url")
	}
	if _, ok := m.Options["timeout_ms"]; !ok {
		t.Error("options should have timeout_ms")
	}
}

func TestValidateRejectsEmptyName(t *testing.T) {
	m := &manifest.Manifest{Version: "1.0.0"}
	if err := m.Validate(); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestValidateRejectsEmptyVersion(t *testing.T) {
	m := &manifest.Manifest{Name: "test"}
	if err := m.Validate(); err == nil {
		t.Error("expected error for empty version")
	}
}

func TestValidateRejectsToolWithNoName(t *testing.T) {
	m := &manifest.Manifest{
		Name:     "test",
		Version:  "1.0.0",
		WASMPath: "plugin.wasm",
		Tools:    []manifest.ToolDef{{Description: "no name"}},
	}
	if err := m.Validate(); err == nil {
		t.Error("expected error for tool with no name")
	}
}

func TestValidateDefaultsToolFunction(t *testing.T) {
	m := &manifest.Manifest{
		Name:     "test",
		Version:  "1.0.0",
		WASMPath: "plugin.wasm",
		Tools:    []manifest.ToolDef{{Name: "my_tool"}},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if m.Tools[0].Function != "my_tool" {
		t.Errorf("function = %q, want my_tool", m.Tools[0].Function)
	}
}

func TestValidateDefaultsModelAdapterFunction(t *testing.T) {
	m := &manifest.Manifest{
		Name:         "test",
		Version:      "1.0.0",
		WASMPath:     "plugin.wasm",
		ModelAdapter: &manifest.ModelAdapterDef{},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if m.ModelAdapter.Function != "complete" {
		t.Errorf("model_adapter.function = %q, want complete", m.ModelAdapter.Function)
	}
}

func TestHashDeterministic(t *testing.T) {
	data := []byte(minimalTOML)
	h1 := manifest.Hash(data)
	h2 := manifest.Hash(data)
	if h1 != h2 {
		t.Errorf("hash not deterministic: %s != %s", h1, h2)
	}
	if len(h1) != 64 { // SHA-256 hex = 64 chars
		t.Errorf("hash length = %d, want 64", len(h1))
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.toml")
	if err := os.WriteFile(path, []byte(minimalTOML), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	m, err := manifest.ParseFile(path)
	if err != nil {
		t.Fatalf("parse file: %v", err)
	}
	if m.Name != "test-plugin" {
		t.Errorf("name = %q", m.Name)
	}
}

func TestValidateAcceptsWellFormed(t *testing.T) {
	m, err := manifest.Parse([]byte(fullTOML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("validate well-formed: %v", err)
	}
}

func TestValidateRejectsCredentialWildcard(t *testing.T) {
	m := &manifest.Manifest{
		Name:         "bad-plugin",
		Version:      "1.0.0",
		WASMPath:     "plugin.wasm",
		Capabilities: []string{"credential.read:*"},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for wildcard capability")
	}
	if !contains(err.Error(), "wildcard") {
		t.Errorf("error = %v, should mention wildcard", err)
	}
}

func TestValidateRejectsEmptyCapability(t *testing.T) {
	m := &manifest.Manifest{
		Name:         "bad",
		Version:      "1.0.0",
		WASMPath:     "plugin.wasm",
		Capabilities: []string{"tool.call", ""},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for empty capability")
	}
}

func TestValidateAcceptsSpecificCredential(t *testing.T) {
	m := &manifest.Manifest{
		Name:         "ok-plugin",
		Version:      "1.0.0",
		WASMPath:     "plugin.wasm",
		Capabilities: []string{"credential.read:openai_api_key", "tool.call"},
	}
	// This would fail defense audit (critical combo), but Validate() only checks format.
	err := m.Validate()
	if err != nil {
		t.Errorf("unexpected validate error for specific credential: %v", err)
	}
}

func TestValidateRejectsPathTraversalName(t *testing.T) {
	cases := []struct {
		name string
		val  string
	}{
		{"dot-dot-slash", "../etc/passwd"},
		{"slash", "foo/bar"},
		{"backslash", "foo\\bar"},
		{"starts-with-dot", ".hidden"},
		{"starts-with-hyphen", "-bad"},
		{"empty-after-validation", ""},
		{"too-long", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, // 66 chars > 64
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &manifest.Manifest{Name: tc.val, Version: "1.0.0"}
			if err := m.Validate(); err == nil {
				t.Errorf("expected error for name %q", tc.val)
			}
		})
	}
}

func TestValidateAcceptsSafeName(t *testing.T) {
	cases := []string{"my-plugin", "plugin_v2", "Plugin.v1", "a", "test123"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			m := &manifest.Manifest{Name: name, Version: "1.0.0", WASMPath: "plugin.wasm"}
			if err := m.Validate(); err != nil {
				t.Errorf("unexpected error for name %q: %v", name, err)
			}
		})
	}
}

func TestValidateRejectsEmptyWASMPath(t *testing.T) {
	m := &manifest.Manifest{Name: "test", Version: "1.0.0", WASMPath: ""}
	err := m.Validate()
	if err == nil {
		t.Error("expected error for empty wasm_path")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}
func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
