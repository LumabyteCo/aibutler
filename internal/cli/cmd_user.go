package cli

import (
	"context"
	"fmt"
	"io"

	rbacpkg "github.com/LumabyteCo/aibutler/internal/rbac"
)

// CmdUser manages RBAC users.
// Usage: aibutler user create|list|roles|assign
func CmdUser(app *App, args []string, w io.Writer) error {
	if app.RBAC == nil {
		return fmt.Errorf("RBAC not initialized. Ensure database is available.")
	}

	if len(args) == 0 {
		fmt.Fprintln(w, "Usage: aibutler user <subcommand>")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Subcommands:")
		fmt.Fprintln(w, "  create <username> <email> [role]  Create a new user (default role: user)")
		fmt.Fprintln(w, "  list                              List all users")
		fmt.Fprintln(w, "  assign <username> <role>          Assign a role to a user")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Roles: admin, user, viewer, agent")
		return nil
	}

	ctx := context.Background()

	switch args[0] {
	case "create":
		if len(args) < 3 {
			return fmt.Errorf("usage: aibutler user create <username> <email> [role]")
		}
		username := args[1]
		email := args[2]
		role := "user"
		if len(args) >= 4 {
			role = args[3]
		}
		id, err := app.RBAC.CreateUser(ctx, username, email, rbacpkg.Role(role))
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		fmt.Fprintf(w, "Created user %s (ID: %d, role: %s)\n", username, id, role)

	case "list":
		users, err := app.RBAC.ListUsers(ctx)
		if err != nil {
			return fmt.Errorf("list users: %w", err)
		}
		if len(users) == 0 {
			fmt.Fprintln(w, "No users found.")
			return nil
		}
		fmt.Fprintf(w, "%-20s %-30s %-10s %-6s\n", "USERNAME", "EMAIL", "ROLE", "ACTIVE")
		for _, u := range users {
			active := "yes"
			if !u.Active {
				active = "no"
			}
			fmt.Fprintf(w, "%-20s %-30s %-10s %-6s\n", u.Username, u.Email, u.Role, active)
		}

	case "assign":
		if len(args) < 3 {
			return fmt.Errorf("usage: aibutler user assign <username> <role>")
		}
		username := args[1]
		role := args[2]
		if err := app.RBAC.AssignRole(ctx, username, rbacpkg.Role(role)); err != nil {
			return fmt.Errorf("assign role: %w", err)
		}
		fmt.Fprintf(w, "Assigned role %s to user %s\n", role, username)

	default:
		return fmt.Errorf("unknown subcommand: %s. Use: create, list, assign", args[0])
	}

	return nil
}
