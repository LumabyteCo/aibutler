package router

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// Route maps a message to a specialist agent.
type Route struct {
	AgentName    string
	Description  string   // used for keyword matching
	Keywords     []string // explicit routing keywords
	Capabilities []string
	Model        string // model override (optional)
}

// Router routes incoming messages to specialist agents.
type Router struct {
	routes []Route
	model  agent.ModelAdapter // for LLM fallback classification (nil = rule-based only)
	db     *sql.DB
}

// New creates a Router with the given routes.
func New(routes []Route, model agent.ModelAdapter, db *sql.DB) *Router {
	return &Router{
		routes: routes,
		model:  model,
		db:     db,
	}
}

// handoffPatterns matches explicit directives like "ask the coding agent to...",
// "have the home agent...", "send to research agent".
var handoffPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bask\s+(?:the\s+)?(\w+)\s+agent\b`),
	regexp.MustCompile(`(?i)\bhave\s+(?:the\s+)?(\w+)\s+agent\b`),
	regexp.MustCompile(`(?i)\bsend\s+to\s+(?:the\s+)?(\w+)\s+agent\b`),
	regexp.MustCompile(`(?i)\broute\s+to\s+(?:the\s+)?(\w+)\s+agent\b`),
	regexp.MustCompile(`(?i)\buse\s+(?:the\s+)?(\w+)\s+agent\b`),
}

// Route determines which agent should handle the given message.
// Returns the agent name.
func (r *Router) Route(ctx context.Context, message string) (string, error) {
	if len(r.routes) == 0 {
		return "general", nil
	}

	// Step 1: Check for explicit handoff directive.
	if name := r.matchHandoff(message); name != "" {
		return name, nil
	}

	// Step 2: Keyword matching against routes.
	if name := r.matchKeywords(message); name != "" {
		return name, nil
	}

	// Step 3: LLM fallback classification.
	if r.model != nil {
		if name, err := r.classifyWithLLM(ctx, message); err == nil && name != "" {
			return name, nil
		}
	}

	// Step 4: Default to "general".
	return "general", nil
}

// matchHandoff checks for explicit directives naming an agent.
func (r *Router) matchHandoff(message string) string {
	for _, pat := range handoffPatterns {
		if matches := pat.FindStringSubmatch(message); len(matches) > 1 {
			candidate := strings.ToLower(matches[1])
			for _, route := range r.routes {
				if strings.ToLower(route.AgentName) == candidate {
					return route.AgentName
				}
			}
		}
	}
	return ""
}

// matchKeywords checks message against route keywords and description words.
func (r *Router) matchKeywords(message string) string {
	lower := strings.ToLower(message)

	bestRoute := ""
	bestScore := 0

	for _, route := range r.routes {
		score := 0
		// Check explicit keywords.
		for _, kw := range route.Keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				score += 2
			}
		}
		// Check description words.
		descWords := strings.Fields(strings.ToLower(route.Description))
		for _, word := range descWords {
			if len(word) > 3 && strings.Contains(lower, word) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestRoute = route.AgentName
		}
	}

	if bestScore > 0 {
		return bestRoute
	}
	return ""
}

// classifyWithLLM sends a classification prompt to the model.
func (r *Router) classifyWithLLM(ctx context.Context, message string) (string, error) {
	var routeList strings.Builder
	for _, route := range r.routes {
		fmt.Fprintf(&routeList, "- %s: %s\n", route.AgentName, route.Description)
	}

	prompt := fmt.Sprintf(
		"Classify this message to one of these agents:\n%sMessage: %s\nRespond with just the agent name.",
		routeList.String(), message)

	resp, err := r.model.Complete(ctx, []agent.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return "", err
	}

	chosen := strings.TrimSpace(strings.ToLower(resp.Content))
	for _, route := range r.routes {
		if strings.ToLower(route.AgentName) == chosen {
			return route.AgentName, nil
		}
	}
	return "", nil
}

// Routes returns the configured routes.
func (r *Router) Routes() []Route {
	return r.routes
}
