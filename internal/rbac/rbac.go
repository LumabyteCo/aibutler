// Package rbac provides role-based access control for AI Butler enterprise deployments.
package rbac

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"
)

// Role represents a user role in the RBAC system.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleUser   Role = "user"
	RoleViewer Role = "viewer"
	RoleAgent  Role = "agent" // for service accounts
)

// Permission describes a single resource+action pair.
type Permission struct {
	Resource string // "tools", "channels", "memory", "config", "agents", "admin"
	Action   string // "read", "write", "execute", "manage"
}

// RolePermissions maps roles to their allowed permissions.
type RolePermissions map[Role][]Permission

// DefaultPermissions returns the built-in permission set for all standard roles.
func DefaultPermissions() RolePermissions {
	return RolePermissions{
		RoleAdmin: {
			{Resource: "tools", Action: "read"},
			{Resource: "tools", Action: "write"},
			{Resource: "tools", Action: "execute"},
			{Resource: "tools", Action: "manage"},
			{Resource: "channels", Action: "read"},
			{Resource: "channels", Action: "write"},
			{Resource: "channels", Action: "manage"},
			{Resource: "memory", Action: "read"},
			{Resource: "memory", Action: "write"},
			{Resource: "memory", Action: "manage"},
			{Resource: "config", Action: "read"},
			{Resource: "config", Action: "write"},
			{Resource: "config", Action: "manage"},
			{Resource: "agents", Action: "read"},
			{Resource: "agents", Action: "write"},
			{Resource: "agents", Action: "execute"},
			{Resource: "agents", Action: "manage"},
			{Resource: "admin", Action: "read"},
			{Resource: "admin", Action: "write"},
			{Resource: "admin", Action: "manage"},
		},
		RoleUser: {
			{Resource: "tools", Action: "read"},
			{Resource: "tools", Action: "write"},
			{Resource: "tools", Action: "execute"},
			{Resource: "channels", Action: "read"},
			{Resource: "channels", Action: "write"},
			{Resource: "memory", Action: "read"},
			{Resource: "memory", Action: "write"},
			{Resource: "agents", Action: "read"},
			{Resource: "agents", Action: "execute"},
		},
		RoleViewer: {
			{Resource: "tools", Action: "read"},
			{Resource: "channels", Action: "read"},
			{Resource: "memory", Action: "read"},
			{Resource: "agents", Action: "read"},
		},
		RoleAgent: {
			{Resource: "tools", Action: "execute"},
			{Resource: "channels", Action: "write"},
			{Resource: "memory", Action: "read"},
		},
	}
}

// User represents an RBAC user record.
type User struct {
	ID        int64
	Username  string
	Email     string
	Role      Role
	Active    bool
	CreatedAt time.Time
}

// Engine provides RBAC operations backed by SQLite.
type Engine struct {
	db *sql.DB
}

// New creates a new RBAC engine.
func New(db *sql.DB) *Engine {
	return &Engine{db: db}
}

// CreateUser inserts a new RBAC user and returns the user ID.
func (e *Engine) CreateUser(ctx context.Context, username, email string, role Role) (int64, error) {
	if username == "" {
		return 0, fmt.Errorf("rbac: username is required")
	}
	if !validRole(role) {
		return 0, fmt.Errorf("rbac: invalid role %q", role)
	}

	res, err := e.db.ExecContext(ctx,
		`INSERT INTO rbac_users (username, email, role) VALUES (?, ?, ?)`,
		username, email, string(role))
	if err != nil {
		return 0, fmt.Errorf("rbac: create user: %w", err)
	}
	return res.LastInsertId()
}

// GetUser retrieves a user by username.
func (e *Engine) GetUser(ctx context.Context, username string) (*User, error) {
	row := e.db.QueryRowContext(ctx,
		`SELECT id, username, email, role, active, created_at FROM rbac_users WHERE username = ?`,
		username)

	var u User
	var roleStr string
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &roleStr, &u.Active, &u.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("rbac: user %q not found", username)
		}
		return nil, fmt.Errorf("rbac: get user: %w", err)
	}
	u.Role = Role(roleStr)
	return &u, nil
}

// ListUsers returns all RBAC users.
func (e *Engine) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := e.db.QueryContext(ctx,
		`SELECT id, username, email, role, active, created_at FROM rbac_users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("rbac: list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		var roleStr string
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &roleStr, &u.Active, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("rbac: scan user: %w", err)
		}
		u.Role = Role(roleStr)
		users = append(users, u)
	}
	return users, rows.Err()
}

// AssignRole changes the role assigned to a user.
func (e *Engine) AssignRole(ctx context.Context, username string, role Role) error {
	if !validRole(role) {
		return fmt.Errorf("rbac: invalid role %q", role)
	}

	res, err := e.db.ExecContext(ctx,
		`UPDATE rbac_users SET role = ?, updated_at = CURRENT_TIMESTAMP WHERE username = ?`,
		string(role), username)
	if err != nil {
		return fmt.Errorf("rbac: assign role: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("rbac: user %q not found", username)
	}
	return nil
}

// Check returns whether the given user is allowed to perform the specified action
// on the specified resource.
func (e *Engine) Check(ctx context.Context, username, resource, action string) (bool, error) {
	user, err := e.GetUser(ctx, username)
	if err != nil {
		return false, err
	}
	if !user.Active {
		return false, nil
	}

	perms := DefaultPermissions()
	rolePerms, ok := perms[user.Role]
	if !ok {
		return false, nil
	}

	for _, p := range rolePerms {
		if p.Resource == resource && p.Action == action {
			return true, nil
		}
	}
	return false, nil
}

// UserPermissions returns all permissions for a user based on their role.
func (e *Engine) UserPermissions(ctx context.Context, username string) ([]Permission, error) {
	user, err := e.GetUser(ctx, username)
	if err != nil {
		return nil, err
	}

	perms := DefaultPermissions()
	rolePerms, ok := perms[user.Role]
	if !ok {
		return nil, nil
	}
	return rolePerms, nil
}

// Middleware wraps an http.Handler with RBAC enforcement.
// The user identity is extracted from the X-Butler-User header.
// When no user header is present (anonymous), the request is allowed
// to pass through — authentication layers upstream are responsible for
// populating the header once user sessions are available.
func (e *Engine) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("X-Butler-User")
		if user == "" {
			// No identity yet — allow through (auth layer will populate later).
			next.ServeHTTP(w, r)
			return
		}
		resource := "dashboard"
		action := "read"
		if r.Method == http.MethodPut || r.Method == http.MethodPost || r.Method == http.MethodDelete {
			action = "write"
		}
		allowed, _ := e.Check(r.Context(), user, resource, action)
		if !allowed {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validRole(r Role) bool {
	switch r {
	case RoleAdmin, RoleUser, RoleViewer, RoleAgent:
		return true
	}
	return false
}
