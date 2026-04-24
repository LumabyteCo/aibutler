package specialist

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSpecialists(t *testing.T) {
	specs := DefaultSpecialists()
	if len(specs) != 5 {
		t.Fatalf("DefaultSpecialists() returned %d, want 5", len(specs))
	}

	names := map[string]bool{}
	for _, s := range specs {
		names[s.Name] = true
	}

	for _, want := range []string{"coding", "home", "creative", "research", "general"} {
		if !names[want] {
			t.Errorf("missing specialist %q", want)
		}
	}
}

func TestBuildRoutes(t *testing.T) {
	specs := DefaultSpecialists()
	routes := BuildRoutes(specs)

	if len(routes) != len(specs) {
		t.Fatalf("BuildRoutes returned %d routes, want %d", len(routes), len(specs))
	}

	for i, r := range routes {
		if r.AgentName != specs[i].Name {
			t.Errorf("route[%d].AgentName = %q, want %q", i, r.AgentName, specs[i].Name)
		}
		if r.Description != specs[i].Description {
			t.Errorf("route[%d].Description = %q, want %q", i, r.Description, specs[i].Description)
		}
	}
}

func TestLoadTemplates(t *testing.T) {
	// Create temp directory with a YAML template.
	dir := t.TempDir()

	template := `name: devops
description: DevOps and infrastructure
capabilities:
  - tool.shell.*
  - tool.git.*
  - tool.code.*
skills:
  - coding
model: claude-sonnet-4-6
max_tool_calls: 100
timeout: 10m
`
	if err := os.WriteFile(filepath.Join(dir, "devops.yaml"), []byte(template), 0644); err != nil {
		t.Fatal(err)
	}

	// Also create a non-YAML file that should be skipped.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("skip me"), 0644); err != nil {
		t.Fatal(err)
	}

	// And an invalid YAML file.
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(":::invalid"), 0644); err != nil {
		t.Fatal(err)
	}

	configs, err := LoadTemplates(dir)
	if err != nil {
		t.Fatalf("LoadTemplates error: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("LoadTemplates returned %d configs, want 1", len(configs))
	}

	if configs[0].Name != "devops" {
		t.Errorf("config name = %q, want 'devops'", configs[0].Name)
	}
	if len(configs[0].Capabilities) != 3 {
		t.Errorf("capabilities count = %d, want 3", len(configs[0].Capabilities))
	}
}
