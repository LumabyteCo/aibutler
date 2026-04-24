package prompt

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed defaults/*.md
var defaultSkillsFS embed.FS

// Skill represents a parsed skill file (markdown + YAML frontmatter).
type Skill struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Summary      string   `yaml:"summary"` // ~5 tokens for Tier 1 index
	Triggers     []string `yaml:"triggers"`
	Tools        []string `yaml:"tools"`
	Capabilities []string `yaml:"capabilities"`
	Enabled      bool     `yaml:"enabled"`
	Body         string   `yaml:"-"` // Markdown content after frontmatter
}

// LoadSkill parses a skill file with YAML frontmatter + markdown body.
func LoadSkill(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("skill: read %s: %w", path, err)
	}
	return parseSkill(string(data))
}

// LoadSkillsDir loads all .md skill files from a directory.
func LoadSkillsDir(dir string) ([]*Skill, error) {
	if dir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("skill: read dir %s: %w", dir, err)
	}

	var skills []*Skill
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		skill, err := LoadSkill(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue // Skip invalid skill files.
		}
		skills = append(skills, skill)
	}
	return skills, nil
}

// LoadDefaultSkills loads the embedded default skill files.
func LoadDefaultSkills() ([]*Skill, error) {
	entries, err := defaultSkillsFS.ReadDir("defaults")
	if err != nil {
		return nil, fmt.Errorf("skill: read embedded defaults: %w", err)
	}

	var skills []*Skill
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := defaultSkillsFS.ReadFile("defaults/" + entry.Name())
		if err != nil {
			continue
		}
		skill, err := parseSkill(string(data))
		if err != nil {
			continue
		}
		skills = append(skills, skill)
	}
	return skills, nil
}

// MatchSkills returns skills whose triggers match the user message.
// Returns at most maxSkills matches, ordered by number of trigger hits.
func MatchSkills(skills []*Skill, message string, maxSkills int) []*Skill {
	if maxSkills <= 0 {
		maxSkills = 3
	}

	msg := strings.ToLower(message)
	type scored struct {
		skill *Skill
		hits  int
	}

	var matches []scored
	for _, s := range skills {
		if !s.Enabled {
			continue
		}
		hits := 0
		for _, trigger := range s.Triggers {
			if strings.Contains(msg, strings.ToLower(trigger)) {
				hits++
			}
		}
		if hits > 0 {
			matches = append(matches, scored{skill: s, hits: hits})
		}
	}

	// Sort by hits descending (simple insertion sort for small N).
	for i := 1; i < len(matches); i++ {
		for j := i; j > 0 && matches[j].hits > matches[j-1].hits; j-- {
			matches[j], matches[j-1] = matches[j-1], matches[j]
		}
	}

	var result []*Skill
	for i := 0; i < len(matches) && i < maxSkills; i++ {
		result = append(result, matches[i].skill)
	}
	return result
}

// parseSkill splits YAML frontmatter from markdown body.
func parseSkill(content string) (*Skill, error) {
	content = strings.TrimSpace(content)

	// Frontmatter is between --- delimiters.
	if !strings.HasPrefix(content, "---") {
		return nil, fmt.Errorf("skill: missing frontmatter delimiter")
	}

	// Find the closing ---.
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return nil, fmt.Errorf("skill: missing closing frontmatter delimiter")
	}

	frontmatter := strings.TrimSpace(rest[:idx])
	body := strings.TrimSpace(rest[idx+4:]) // Skip \n---

	var skill Skill
	if err := yaml.Unmarshal([]byte(frontmatter), &skill); err != nil {
		return nil, fmt.Errorf("skill: parse frontmatter: %w", err)
	}
	skill.Body = body

	return &skill, nil
}
