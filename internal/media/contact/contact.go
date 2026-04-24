// Package contact provides vCard 3.0/4.0 parsing using stdlib only.
package contact

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// VCard holds parsed contact information from a vCard record.
type VCard struct {
	Name         string
	Email        string
	Phone        string
	Organization string
}

// ParseVCard parses a single vCard 3.0/4.0 text block.
// It extracts FN (full name), EMAIL, TEL, and ORG fields.
func ParseVCard(data string) (*VCard, error) {
	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")
	lines := strings.Split(data, "\n")

	// Unfold continuation lines (lines starting with whitespace).
	unfolded := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if len(unfolded) > 0 {
				unfolded[len(unfolded)-1] += strings.TrimLeft(line, " \t")
			}
		} else {
			unfolded = append(unfolded, line)
		}
	}

	card := &VCard{}
	inCard := false

	for _, line := range unfolded {
		upper := strings.ToUpper(line)
		if upper == "BEGIN:VCARD" {
			inCard = true
			continue
		}
		if upper == "END:VCARD" {
			break
		}
		if !inCard {
			continue
		}

		// Split property name from value.
		// Properties may have parameters: PROP;PARAM=val:value
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		propFull := line[:colon]
		value := strings.TrimSpace(line[colon+1:])

		// Strip parameters from property name.
		prop := strings.ToUpper(strings.SplitN(propFull, ";", 2)[0])

		switch prop {
		case "FN":
			card.Name = unescapeVCard(value)
		case "N":
			// Only set Name from N if FN is not set.
			if card.Name == "" {
				parts := strings.Split(unescapeVCard(value), ";")
				if len(parts) >= 2 {
					card.Name = strings.TrimSpace(parts[1] + " " + parts[0])
				}
			}
		case "EMAIL":
			if card.Email == "" {
				card.Email = value
			}
		case "TEL":
			if card.Phone == "" {
				card.Phone = value
			}
		case "ORG":
			if card.Organization == "" {
				card.Organization = unescapeVCard(strings.SplitN(value, ";", 2)[0])
			}
		}
	}

	if !inCard {
		return nil, fmt.Errorf("contact: no BEGIN:VCARD found")
	}
	return card, nil
}

// unescapeVCard unescapes vCard escaped characters.
func unescapeVCard(s string) string {
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\N`, "\n")
	s = strings.ReplaceAll(s, `\,`, ",")
	s = strings.ReplaceAll(s, `\;`, ";")
	s = strings.ReplaceAll(s, `\\`, `\`)
	return strings.TrimSpace(s)
}

// ParseVCardFile parses a .vcf file that may contain multiple vCard records.
func ParseVCardFile(_ context.Context, path string) ([]*VCard, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("contact: open %q: %w", path, err)
	}
	defer f.Close()

	var cards []*VCard
	var current strings.Builder
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := scanner.Text()
		current.WriteString(line + "\n")
		if strings.ToUpper(strings.TrimSpace(line)) == "END:VCARD" {
			card, err := ParseVCard(current.String())
			if err == nil {
				cards = append(cards, card)
			}
			current.Reset()
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("contact: scan %q: %w", path, err)
	}
	return cards, nil
}

// RegisterContactTools registers media.contact.parse and media.contact.parse_file.
func RegisterContactTools(registry toolRegistry) {
	registry.Register(
		"media.contact.parse",
		"Parse a vCard 3.0/4.0 text block and return structured contact data.",
		`{"type":"object","properties":{"vcard":{"type":"string","description":"vCard text data"}},"required":["vcard"]}`,
		"tool.file.read",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				VCard string `json:"vcard"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("media.contact.parse: invalid input: %w", err)
			}
			if args.VCard == "" {
				return "", fmt.Errorf("media.contact.parse: vcard is required")
			}
			card, err := ParseVCard(args.VCard)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(card)
			return string(out), nil
		},
	)

	registry.Register(
		"media.contact.parse_file",
		"Parse a .vcf file containing one or more vCard records.",
		`{"type":"object","properties":{"path":{"type":"string","description":"Path to the .vcf file"}},"required":["path"]}`,
		"tool.file.read",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("media.contact.parse_file: invalid input: %w", err)
			}
			if args.Path == "" {
				return "", fmt.Errorf("media.contact.parse_file: path is required")
			}
			cards, err := ParseVCardFile(ctx, args.Path)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]interface{}{
				"contacts": cards,
				"count":    len(cards),
			})
			return string(out), nil
		},
	)
}
