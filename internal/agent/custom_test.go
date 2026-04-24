package agent_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestRoleRouterExplicit(t *testing.T) {
	roles := []agent.CustomRole{
		{Name: "researcher", Description: "Research tasks"},
		{Name: "coder", Description: "Code tasks"},
	}
	router := agent.NewRoleRouter(roles, "explicit", nil)

	// "@coder fix the bug" should route to coder.
	role, err := router.Route(context.Background(), "@coder fix the bug", nil)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if role.Name != "coder" {
		t.Errorf("got role %q, want coder", role.Name)
	}

	// "@researcher find papers" should route to researcher.
	role, err = router.Route(context.Background(), "@researcher find papers", nil)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if role.Name != "researcher" {
		t.Errorf("got role %q, want researcher", role.Name)
	}

	// No prefix — falls back to first role.
	role, err = router.Route(context.Background(), "do something", nil)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if role.Name != "researcher" {
		t.Errorf("got role %q, want researcher (fallback)", role.Name)
	}
}

func TestRoleRouterRoundRobin(t *testing.T) {
	roles := []agent.CustomRole{
		{Name: "a", Description: "Role A"},
		{Name: "b", Description: "Role B"},
		{Name: "c", Description: "Role C"},
	}
	router := agent.NewRoleRouter(roles, "round-robin", nil)

	got := make([]string, 6)
	for i := 0; i < 6; i++ {
		role, err := router.Route(context.Background(), "task", nil)
		if err != nil {
			t.Fatalf("route %d: %v", i, err)
		}
		got[i] = role.Name
	}

	// Should cycle: a, b, c, a, b, c
	expected := []string{"a", "b", "c", "a", "b", "c"}
	for i, name := range got {
		if name != expected[i] {
			t.Errorf("route[%d] = %q, want %q", i, name, expected[i])
		}
	}
}

func TestRoleRouterClassifyFallback(t *testing.T) {
	roles := []agent.CustomRole{
		{Name: "default", Description: "Default role"},
	}
	router := agent.NewRoleRouter(roles, "classify", nil)

	// With nil model, should fall back to first role.
	role, err := router.Route(context.Background(), "any task", nil)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if role.Name != "default" {
		t.Errorf("got role %q, want default", role.Name)
	}
}

func TestRoleRouterClassifyWithModel(t *testing.T) {
	roles := []agent.CustomRole{
		{Name: "researcher", Description: "Research tasks"},
		{Name: "coder", Description: "Code tasks"},
	}
	router := agent.NewRoleRouter(roles, "classify", nil)

	// Model that responds "coder"
	model := testutil.NewFakeModel(agent.Response{Content: "coder"})

	role, err := router.Route(context.Background(), "fix the bug", model)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if role.Name != "coder" {
		t.Errorf("got role %q, want coder", role.Name)
	}
}

func TestRoleRouterNoRoles(t *testing.T) {
	router := agent.NewRoleRouter(nil, "explicit", nil)
	_, err := router.Route(context.Background(), "task", nil)
	if err == nil {
		t.Error("expected error for empty roles")
	}
}

func TestLoadSaveRolesDB(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	// Save a role.
	role := agent.CustomRole{
		Name:        "analyst",
		Description: "Data analysis specialist",
		Model:       "claude-opus",
		Tools:       []string{"memory.search", "web.search"},
		Prompt:      "You are a data analyst.",
	}
	if err := agent.SaveRoleToDB(ctx, conn, role); err != nil {
		t.Fatalf("save role: %v", err)
	}

	// Load roles.
	roles, err := agent.LoadRolesFromDB(ctx, conn)
	if err != nil {
		t.Fatalf("load roles: %v", err)
	}
	if len(roles) != 1 {
		t.Fatalf("got %d roles, want 1", len(roles))
	}
	if roles[0].Name != "analyst" {
		t.Errorf("name = %q, want analyst", roles[0].Name)
	}
	if roles[0].Description != "Data analysis specialist" {
		t.Errorf("description = %q", roles[0].Description)
	}
	if roles[0].Model != "claude-opus" {
		t.Errorf("model = %q, want claude-opus", roles[0].Model)
	}
	if len(roles[0].Tools) != 2 {
		t.Errorf("tools = %v, want 2 items", roles[0].Tools)
	}
	if roles[0].Prompt != "You are a data analyst." {
		t.Errorf("prompt = %q", roles[0].Prompt)
	}

	// Upsert (update existing).
	role.Description = "Updated description"
	if err := agent.SaveRoleToDB(ctx, conn, role); err != nil {
		t.Fatalf("upsert role: %v", err)
	}
	roles, _ = agent.LoadRolesFromDB(ctx, conn)
	if roles[0].Description != "Updated description" {
		t.Errorf("description after upsert = %q", roles[0].Description)
	}
}
