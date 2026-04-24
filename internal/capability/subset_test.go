package capability

import (
	"testing"
)

func TestSubsetNilParent(t *testing.T) {
	cs := Subset(nil, []string{"tool.shell.exec"})
	if cs == nil {
		t.Fatal("expected non-nil capability set")
	}
	if len(cs.Resources()) != 0 {
		t.Errorf("expected empty resources, got %v", cs.Resources())
	}
}

func TestSubsetInheritAll(t *testing.T) {
	parent := NewCapabilitySet([]Capability{
		{Resource: "tool.shell.exec"},
		{Resource: "tool.web.search"},
		{Resource: "data.memory.read"},
	})

	child := Subset(parent, nil) // empty allowed = inherit all
	resources := child.Resources()
	if len(resources) != 3 {
		t.Fatalf("expected 3 resources, got %d: %v", len(resources), resources)
	}

	want := map[string]bool{"tool.shell.exec": true, "tool.web.search": true, "data.memory.read": true}
	for _, r := range resources {
		if !want[r] {
			t.Errorf("unexpected resource %q", r)
		}
	}
}

func TestSubsetFiltered(t *testing.T) {
	parent := NewCapabilitySet([]Capability{
		{Resource: "tool.shell.exec"},
		{Resource: "tool.web.search"},
		{Resource: "data.memory.read"},
	})

	child := Subset(parent, []string{"tool.web.search", "data.memory.read"})
	resources := child.Resources()
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d: %v", len(resources), resources)
	}

	want := map[string]bool{"tool.web.search": true, "data.memory.read": true}
	for _, r := range resources {
		if !want[r] {
			t.Errorf("unexpected resource %q", r)
		}
	}
}

func TestSubsetAllowedNotInParent(t *testing.T) {
	parent := NewCapabilitySet([]Capability{
		{Resource: "tool.shell.exec"},
	})

	// Asking for a resource the parent doesn't have => not included.
	child := Subset(parent, []string{"tool.web.search"})
	if len(child.Resources()) != 0 {
		t.Errorf("expected 0 resources, got %v", child.Resources())
	}
}

func TestValidateSubsetNilParent(t *testing.T) {
	// Nil parent, empty resources => ok.
	if err := ValidateSubset(nil, nil); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}

	// Nil parent, non-empty resources => error.
	err := ValidateSubset(nil, []string{"tool.shell.exec"})
	if err == nil {
		t.Error("expected error for nil parent with resources")
	}
}

func TestValidateSubsetAllPresent(t *testing.T) {
	parent := NewCapabilitySet([]Capability{
		{Resource: "tool.shell.exec"},
		{Resource: "tool.web.search"},
	})

	err := ValidateSubset(parent, []string{"tool.shell.exec", "tool.web.search"})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestValidateSubsetMissing(t *testing.T) {
	parent := NewCapabilitySet([]Capability{
		{Resource: "tool.shell.exec"},
	})

	err := ValidateSubset(parent, []string{"tool.shell.exec", "tool.web.search"})
	if err == nil {
		t.Error("expected error for missing capability")
	}
}

func TestValidateSubsetWildcard(t *testing.T) {
	parent := NewCapabilitySet([]Capability{
		{Resource: "tool.shell.*"},
	})

	// "tool.shell.exec" should match "tool.shell.*".
	err := ValidateSubset(parent, []string{"tool.shell.exec"})
	if err != nil {
		t.Errorf("expected wildcard match, got %v", err)
	}

	// "tool.web.search" should NOT match "tool.shell.*".
	err = ValidateSubset(parent, []string{"tool.web.search"})
	if err == nil {
		t.Error("expected error for non-matching wildcard")
	}
}

func TestResourcesNilSet(t *testing.T) {
	var cs *CapabilitySet
	if cs.Resources() != nil {
		t.Errorf("expected nil, got %v", cs.Resources())
	}
}
