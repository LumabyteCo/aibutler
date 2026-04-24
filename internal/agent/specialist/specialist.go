package specialist

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent/router"

	"gopkg.in/yaml.v3"
)

// SpecialistConfig defines a specialist agent configuration.
type SpecialistConfig struct {
	Name         string        `yaml:"name"`
	Description  string        `yaml:"description"`
	Capabilities []string      `yaml:"capabilities"`
	Skills       []string      `yaml:"skills"`
	Model        string        `yaml:"model"`
	MaxToolCalls int           `yaml:"max_tool_calls"`
	Timeout      time.Duration `yaml:"timeout"`
}

// DefaultSpecialists returns the built-in specialist configurations.
func DefaultSpecialists() []SpecialistConfig {
	return []SpecialistConfig{
		{Name: "coding", Description: "Code editing, shell, git operations", Capabilities: []string{"tool.file.*", "tool.shell.*", "tool.git.*"}, Skills: []string{"coding"}},
		{Name: "home", Description: "Smart home, IoT, scheduling", Capabilities: []string{"tool.iot.*", "tool.schedule.*"}, Skills: []string{"scheduler"}},
		{Name: "creative", Description: "AI image, video, music generation", Capabilities: []string{"tool.ai.*"}, Skills: []string{"creative"}},
		{Name: "research", Description: "Web search, memory, knowledge", Capabilities: []string{"tool.web.*", "tool.memory.*"}, Skills: []string{"research"}},
		{Name: "general", Description: "General assistant, tasks, contacts, data", Capabilities: []string{"*"}, Skills: []string{}},
	}
}

// BuildRoutes converts specialist configs into router routes.
func BuildRoutes(specs []SpecialistConfig) []router.Route {
	routes := make([]router.Route, len(specs))
	for i, s := range specs {
		routes[i] = router.Route{
			AgentName:    s.Name,
			Description:  s.Description,
			Capabilities: s.Capabilities,
			Model:        s.Model,
		}
	}
	return routes
}

// LoadTemplates reads YAML template files from the given directory
// and returns SpecialistConfig for each file found.
func LoadTemplates(dir string) ([]SpecialistConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("specialist: read templates dir: %w", err)
	}

	var configs []SpecialistConfig
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		var cfg SpecialistConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			continue
		}
		if cfg.Name == "" {
			continue
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}
