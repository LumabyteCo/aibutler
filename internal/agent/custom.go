package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
)

// CustomRole defines a user-configured specialist agent.
type CustomRole struct {
	Name        string   // Unique role name
	Description string   // What this role does
	Model       string   // Model override (empty = use primary)
	Tools       []string // Allowed tool names (empty = all)
	Prompt      string   // Additional system instructions
}

// RoleRouter routes tasks to custom roles based on the configured strategy.
type RoleRouter struct {
	roles    []CustomRole
	strategy RoutingStrategy
	rrIndex  atomic.Int64 // for round-robin
	db       *sql.DB
}

// RoutingStrategy determines how tasks are assigned to roles.
type RoutingStrategy string

const (
	RouteClassify   RoutingStrategy = "classify"    // LLM classifies which role to use
	RouteExplicit   RoutingStrategy = "explicit"    // User specifies role in task
	RouteRoundRobin RoutingStrategy = "round-robin" // Cycle through roles
)

// NewRoleRouter creates a router with the given roles and strategy.
func NewRoleRouter(roles []CustomRole, strategy string, db *sql.DB) *RoleRouter {
	s := RouteClassify
	switch strategy {
	case "explicit":
		s = RouteExplicit
	case "round-robin":
		s = RouteRoundRobin
	}
	return &RoleRouter{
		roles:    roles,
		strategy: s,
		db:       db,
	}
}

// Route selects a role for the given task.
func (r *RoleRouter) Route(ctx context.Context, task string, model ModelAdapter) (*CustomRole, error) {
	if len(r.roles) == 0 {
		return nil, fmt.Errorf("no custom roles configured")
	}

	switch r.strategy {
	case RouteExplicit:
		return r.routeExplicit(task)
	case RouteRoundRobin:
		return r.routeRoundRobin()
	default:
		return r.routeClassify(ctx, task, model)
	}
}

// routeExplicit looks for "@role_name" prefix in the task.
func (r *RoleRouter) routeExplicit(task string) (*CustomRole, error) {
	lower := strings.ToLower(strings.TrimSpace(task))
	for i := range r.roles {
		prefix := "@" + strings.ToLower(r.roles[i].Name)
		if strings.HasPrefix(lower, prefix) {
			return &r.roles[i], nil
		}
	}
	// Fall back to first role if no explicit match.
	return &r.roles[0], nil
}

// routeRoundRobin cycles through roles.
func (r *RoleRouter) routeRoundRobin() (*CustomRole, error) {
	idx := r.rrIndex.Add(1) - 1
	return &r.roles[idx%int64(len(r.roles))], nil
}

// routeClassify uses the LLM to classify which role should handle the task.
func (r *RoleRouter) routeClassify(ctx context.Context, task string, model ModelAdapter) (*CustomRole, error) {
	if model == nil {
		return &r.roles[0], nil
	}

	// Build classification prompt.
	var roleList strings.Builder
	for _, role := range r.roles {
		fmt.Fprintf(&roleList, "- %s: %s\n", role.Name, role.Description)
	}

	classifyPrompt := fmt.Sprintf(
		"You are a task router. Given the task below, respond with ONLY the role name (one word) that should handle it.\n\nAvailable roles:\n%s\nTask: %s\n\nRole:",
		roleList.String(), task)

	resp, err := model.Complete(ctx, []Message{
		{Role: "user", Content: classifyPrompt},
	})
	if err != nil {
		// Fall back to first role on error.
		return &r.roles[0], nil
	}

	chosen := strings.TrimSpace(strings.ToLower(resp.Content))
	for i := range r.roles {
		if strings.ToLower(r.roles[i].Name) == chosen {
			return &r.roles[i], nil
		}
	}

	// No match — use first role.
	return &r.roles[0], nil
}

// Roles returns the configured roles.
func (r *RoleRouter) Roles() []CustomRole {
	return r.roles
}

// LoadRolesFromDB loads custom roles from the database.
func LoadRolesFromDB(ctx context.Context, db *sql.DB) ([]CustomRole, error) {
	if db == nil {
		return nil, nil
	}

	rows, err := db.QueryContext(ctx,
		`SELECT name, description, model_override, tools, system_prompt FROM custom_agent_roles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("load custom roles: %w", err)
	}
	defer rows.Close()

	var roles []CustomRole
	for rows.Next() {
		var name, desc string
		var modelOverride, toolsJSON, prompt sql.NullString
		if err := rows.Scan(&name, &desc, &modelOverride, &toolsJSON, &prompt); err != nil {
			continue
		}

		role := CustomRole{
			Name:        name,
			Description: desc,
			Model:       modelOverride.String,
			Prompt:      prompt.String,
		}

		if toolsJSON.Valid && toolsJSON.String != "" {
			json.Unmarshal([]byte(toolsJSON.String), &role.Tools)
		}

		roles = append(roles, role)
	}
	return roles, nil
}

// SaveRoleToDB persists a custom role to the database.
func SaveRoleToDB(ctx context.Context, db *sql.DB, role CustomRole) error {
	if db == nil {
		return fmt.Errorf("no database")
	}

	toolsJSON, _ := json.Marshal(role.Tools)
	_, err := db.ExecContext(ctx,
		`INSERT INTO custom_agent_roles (name, description, model_override, tools, system_prompt)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		  description = excluded.description, model_override = excluded.model_override,
		  tools = excluded.tools, system_prompt = excluded.system_prompt,
		  updated_at = datetime('now')`,
		role.Name, role.Description, role.Model, string(toolsJSON), role.Prompt)
	return err
}
