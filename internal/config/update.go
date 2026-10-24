package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// UpdateAgentMode rewrites only settings.agent_mode in the YAML file at
// path. It preserves comments, key ordering, and every other field —
// including keys this binary doesn't know about.
//
// The mode command used to rewrite the whole file from the resolved
// Config struct, which silently dropped comments and unknown/legacy
// keys. A one-field change should be a one-field edit.
func UpdateAgentMode(path, mode string) error {
	root := &yaml.Node{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(bytes.TrimSpace(data)) > 0 {
			if err := yaml.Unmarshal(data, root); err != nil {
				return fmt.Errorf("config: parse %s: %w", filepath.Base(path), err)
			}
		}
	case os.IsNotExist(err):
		// Start a fresh document; the rest of the config loads as defaults.
	default:
		return fmt.Errorf("config: read %s: %w", filepath.Base(path), err)
	}

	var doc *yaml.Node
	if root.Kind == 0 {
		doc = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Kind = yaml.DocumentNode
		root.Content = []*yaml.Node{doc}
	} else if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		doc = root.Content[0]
	} else if root.Kind == yaml.MappingNode {
		doc = root
	} else {
		return fmt.Errorf("config: %s is not a YAML mapping", filepath.Base(path))
	}

	settings := mappingValue(doc, "settings")
	if settings == nil {
		settings = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.Content = append(doc.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "settings"},
			settings)
	}
	if settings.Kind != yaml.MappingNode {
		return fmt.Errorf("config: settings in %s is not a mapping", filepath.Base(path))
	}

	if v := mappingValue(settings, "agent_mode"); v != nil {
		// Mutate in place so comments attached to the key/value survive.
		v.Kind = yaml.ScalarNode
		v.Tag = "!!str"
		v.Value = mode
		v.Style = 0
	} else {
		settings.Content = append(settings.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "agent_mode"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: mode})
	}

	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("config: marshal %s: %w", filepath.Base(path), err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("config: create dir for %s: %w", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		return fmt.Errorf("config: write %s: %w", filepath.Base(path), err)
	}
	return nil
}

// mappingValue returns the value node for key in a mapping node, or nil.
func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}
