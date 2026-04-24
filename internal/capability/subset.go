package capability

import "fmt"

// Subset creates a new CapabilitySet containing only capabilities that are
// present in the parent set. This enforces the principle that subagents cannot
// have more capabilities than their parent.
//
// If allowed is non-empty, only capabilities matching those resources are included.
// If allowed is empty, all parent capabilities are inherited.
func Subset(parent *CapabilitySet, allowed []string) *CapabilitySet {
	if parent == nil {
		return NewCapabilitySet(nil)
	}

	parentCaps := parent.Capabilities()

	if len(allowed) == 0 {
		// Inherit all parent capabilities.
		inherited := make([]Capability, len(parentCaps))
		copy(inherited, parentCaps)
		return NewCapabilitySet(inherited)
	}

	// Build lookup set.
	allowSet := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allowSet[a] = true
	}

	var filtered []Capability
	for _, cap := range parentCaps {
		if allowSet[cap.Resource] {
			filtered = append(filtered, cap)
		}
	}
	return NewCapabilitySet(filtered)
}

// ValidateSubset returns an error if any resource in childResources is not
// present in the parent set (exact or wildcard match).
func ValidateSubset(parent *CapabilitySet, childResources []string) error {
	if parent == nil {
		if len(childResources) > 0 {
			return fmt.Errorf("capability: parent has no capabilities, cannot grant %v", childResources)
		}
		return nil
	}

	for _, res := range childResources {
		if _, found := parent.findGrant(res); !found {
			return fmt.Errorf("capability: parent does not have %q, cannot delegate", res)
		}
	}
	return nil
}

// Resources returns the list of resource strings in the capability set.
func (cs *CapabilitySet) Resources() []string {
	if cs == nil {
		return nil
	}
	resources := make([]string, len(cs.capabilities))
	for i, cap := range cs.capabilities {
		resources[i] = cap.Resource
	}
	return resources
}
