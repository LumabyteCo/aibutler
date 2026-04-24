package manifest

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// validPluginName matches safe plugin names: alphanumeric start, then alphanumeric/hyphens/underscores/dots, max 64 chars.
var validPluginName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// Manifest is the parsed plugin.toml file.
type Manifest struct {
	Name         string `toml:"name"`
	Version      string `toml:"version"`
	Description  string `toml:"description"`
	Author       string `toml:"author"`
	License      string `toml:"license"`
	MinButlerVer string `toml:"min_butler_version"`

	Capabilities []string `toml:"capabilities"` // e.g., ["tool.call", "credential.read:openai_api_key"]

	Tools        []ToolDef        `toml:"tools"`
	ModelAdapter *ModelAdapterDef `toml:"model_adapter"`

	Settings       map[string]ParamDef `toml:"settings"`
	Configurations map[string]ParamDef `toml:"configurations"`
	Options        map[string]ParamDef `toml:"options"`

	// WASMPath is the path to the .wasm file, relative to the manifest directory.
	WASMPath string `toml:"wasm_path"`
}

// ToolDef describes an exported tool function.
type ToolDef struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Schema      string `toml:"schema"`   // JSON Schema string
	Function    string `toml:"function"` // Exported WASM function name (default: tool name)
}

// ModelAdapterDef declares the plugin exports a complete() function.
type ModelAdapterDef struct {
	Function string   `toml:"function"` // Exported WASM function name (default: "complete")
	Models   []string `toml:"models"`   // Model name patterns this adapter handles
}

// ParamDef describes a configuration parameter.
type ParamDef struct {
	Type        string      `toml:"type"`        // string, int, bool, float
	Default     interface{} `toml:"default"`
	Description string      `toml:"description"`
	Required    bool        `toml:"required"`
}

// Parse parses a TOML manifest from bytes.
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest: parse: %w", err)
	}
	return &m, nil
}

// ParseFile reads and parses a TOML manifest from a file.
func ParseFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("manifest: read %s: %w", filepath.Base(path), err)
	}
	return Parse(data)
}

// Validate checks that the manifest has all required fields.
func (m *Manifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("manifest: name is required")
	}
	if !validPluginName.MatchString(m.Name) {
		return fmt.Errorf("manifest: invalid plugin name %q (must match %s)", m.Name, validPluginName.String())
	}
	if m.Version == "" {
		return fmt.Errorf("manifest: version is required")
	}
	if m.WASMPath == "" {
		return fmt.Errorf("manifest: wasm_path is required")
	}
	for i, t := range m.Tools {
		if t.Name == "" {
			return fmt.Errorf("manifest: tools[%d]: name is required", i)
		}
		// Default function name to tool name.
		if t.Function == "" {
			m.Tools[i].Function = t.Name
		}
	}
	if m.ModelAdapter != nil && m.ModelAdapter.Function == "" {
		m.ModelAdapter.Function = "complete"
	}

	// Validate capabilities: reject wildcards and empty strings.
	for i, cap := range m.Capabilities {
		if cap == "" {
			return fmt.Errorf("manifest: capabilities[%d]: empty capability", i)
		}
		if strings.Contains(cap, "*") {
			return fmt.Errorf("manifest: capabilities[%d]: wildcards not allowed in capabilities (%q); use specific keys (e.g., credential.read:my_key)", i, cap)
		}
	}

	return nil
}

// Hash returns the SHA-256 hex digest of the manifest content.
func Hash(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}
