package rbac_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/rbac"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestCreateUser(t *testing.T) {
	db := testutil.TestDB(t)
	engine := rbac.New(db.Conn())
	ctx := context.Background()

	id, err := engine.CreateUser(ctx, "alice", "alice@example.com", rbac.RoleUser)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if id < 1 {
		t.Errorf("expected positive id, got %d", id)
	}
}

func TestGetUser(t *testing.T) {
	db := testutil.TestDB(t)
	engine := rbac.New(db.Conn())
	ctx := context.Background()

	_, err := engine.CreateUser(ctx, "bob", "bob@example.com", rbac.RoleAdmin)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	user, err := engine.GetUser(ctx, "bob")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if user.Username != "bob" {
		t.Errorf("username = %q, want %q", user.Username, "bob")
	}
	if user.Email != "bob@example.com" {
		t.Errorf("email = %q, want %q", user.Email, "bob@example.com")
	}
	if user.Role != rbac.RoleAdmin {
		t.Errorf("role = %q, want %q", user.Role, rbac.RoleAdmin)
	}
	if !user.Active {
		t.Error("expected user to be active")
	}
}

func TestListUsers(t *testing.T) {
	db := testutil.TestDB(t)
	engine := rbac.New(db.Conn())
	ctx := context.Background()

	_, _ = engine.CreateUser(ctx, "u1", "u1@test.com", rbac.RoleUser)
	_, _ = engine.CreateUser(ctx, "u2", "u2@test.com", rbac.RoleViewer)
	_, _ = engine.CreateUser(ctx, "u3", "u3@test.com", rbac.RoleAgent)

	users, err := engine.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 3 {
		t.Errorf("got %d users, want 3", len(users))
	}
}

func TestAssignRole(t *testing.T) {
	db := testutil.TestDB(t)
	engine := rbac.New(db.Conn())
	ctx := context.Background()

	_, _ = engine.CreateUser(ctx, "carol", "carol@test.com", rbac.RoleUser)

	if err := engine.AssignRole(ctx, "carol", rbac.RoleAdmin); err != nil {
		t.Fatalf("assign role: %v", err)
	}

	user, err := engine.GetUser(ctx, "carol")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if user.Role != rbac.RoleAdmin {
		t.Errorf("role = %q, want %q", user.Role, rbac.RoleAdmin)
	}
}

func TestCheckPermissionAllowed(t *testing.T) {
	db := testutil.TestDB(t)
	engine := rbac.New(db.Conn())
	ctx := context.Background()

	_, _ = engine.CreateUser(ctx, "dave", "dave@test.com", rbac.RoleUser)

	allowed, err := engine.Check(ctx, "dave", "tools", "execute")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !allowed {
		t.Error("expected user to have tools.execute permission")
	}
}

func TestCheckPermissionDenied(t *testing.T) {
	db := testutil.TestDB(t)
	engine := rbac.New(db.Conn())
	ctx := context.Background()

	_, _ = engine.CreateUser(ctx, "eve", "eve@test.com", rbac.RoleViewer)

	allowed, err := engine.Check(ctx, "eve", "tools", "execute")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if allowed {
		t.Error("viewer should not have tools.execute permission")
	}
}

func TestDefaultPermissions(t *testing.T) {
	perms := rbac.DefaultPermissions()

	// Admin should have access to all resources.
	adminPerms := perms[rbac.RoleAdmin]
	if len(adminPerms) == 0 {
		t.Error("admin should have permissions")
	}

	// Verify agent has limited permissions.
	agentPerms := perms[rbac.RoleAgent]
	for _, p := range agentPerms {
		if p.Resource == "admin" {
			t.Error("agent should not have admin permissions")
		}
		if p.Action == "manage" {
			t.Error("agent should not have manage action")
		}
	}

	// Verify viewer has only read permissions.
	viewerPerms := perms[rbac.RoleViewer]
	for _, p := range viewerPerms {
		if p.Action != "read" {
			t.Errorf("viewer should only have read actions, got %q on %q", p.Action, p.Resource)
		}
	}
}
