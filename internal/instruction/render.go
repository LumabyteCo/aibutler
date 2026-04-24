package instruction

import (
	"context"
	"fmt"
	"strings"
)

// categoryOrder defines the display order for markdown rendering.
var categoryOrder = []string{
	CategoryRule,
	CategoryBehavior,
	CategoryStyle,
	CategoryKnowledge,
	CategoryPreference,
}

// categoryTitles maps categories to human-readable titles.
var categoryTitles = map[string]string{
	CategoryRule:       "Rules",
	CategoryBehavior:   "Behaviors",
	CategoryStyle:      "Style",
	CategoryKnowledge:  "Knowledge",
	CategoryPreference: "Preferences",
}

// RenderMarkdown exports all active instructions as a readable markdown document.
func (s *Store) RenderMarkdown(ctx context.Context) (string, error) {
	instructions, err := s.List(ctx, ListQuery{ActiveOnly: true})
	if err != nil {
		return "", err
	}

	if len(instructions) == 0 {
		return "# Learned Instructions\n\nNo active instructions.", nil
	}

	// Group by category.
	groups := make(map[string][]Instruction)
	for _, inst := range instructions {
		groups[inst.Category] = append(groups[inst.Category], inst)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Learned Instructions (%d active)\n\n", len(instructions)))

	for _, cat := range categoryOrder {
		items, ok := groups[cat]
		if !ok {
			continue
		}

		title := categoryTitles[cat]
		if title == "" {
			title = cat
		}
		b.WriteString(fmt.Sprintf("## %s\n\n", title))

		for _, inst := range items {
			b.WriteString(fmt.Sprintf("- [P%d] %s", inst.Priority, inst.Content))
			if inst.Scope != ScopeGlobal && inst.ScopeValue != "" {
				b.WriteString(fmt.Sprintf(" (%s: %s)", inst.Scope, inst.ScopeValue))
			}
			if inst.Source == SourceAuto {
				b.WriteString(" [auto]")
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String(), nil
}
