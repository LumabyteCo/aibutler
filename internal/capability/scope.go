package capability

import (
	"path/filepath"
	"strings"
)

// matchPath checks if a request path matches any of the allowed path patterns.
// Uses filepath.Match for glob matching. Rejects paths containing "..".
func matchPath(patterns []string, reqPath string) bool {
	if len(patterns) == 0 {
		return true // no path restriction
	}

	// Reject path traversal attempts.
	if strings.Contains(reqPath, "..") {
		return false
	}

	// Clean the path.
	clean := filepath.Clean(reqPath)

	for _, pattern := range patterns {
		// Check if the path is under an allowed directory.
		if strings.HasSuffix(pattern, "/") || strings.HasSuffix(pattern, "/*") {
			dir := strings.TrimSuffix(strings.TrimSuffix(pattern, "*"), "/")
			dir = filepath.Clean(dir)
			// "." means current directory — any relative path matches.
			if dir == "." {
				return true
			}
			if strings.HasPrefix(clean, dir+"/") || clean == dir {
				return true
			}
			continue
		}
		matched, err := filepath.Match(pattern, clean)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// matchCommand checks if a request command matches the allowlist.
// Commands must match exactly (prefix matching disabled for security).
func matchCommand(allowed []string, reqCommand string) bool {
	if len(allowed) == 0 {
		return true // no command restriction
	}
	for _, cmd := range allowed {
		if cmd == reqCommand {
			return true
		}
	}
	return false
}

// matchDomain checks if a request domain matches any allowed domain.
// Supports exact match and suffix match (e.g., ".github.com" matches "api.github.com").
func matchDomain(allowed []string, reqDomain string) bool {
	if len(allowed) == 0 {
		return true // no domain restriction
	}
	for _, d := range allowed {
		if d == reqDomain {
			return true
		}
		if d == "*" {
			return true
		}
		// Suffix match: "*.github.com" matches "api.github.com"
		if strings.HasPrefix(d, "*.") {
			suffix := d[1:] // ".github.com"
			if strings.HasSuffix(reqDomain, suffix) {
				return true
			}
		}
	}
	return false
}

// matchChannel checks if a request channel matches any allowed channel.
func matchChannel(allowed []string, reqChannel string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, ch := range allowed {
		if ch == reqChannel || ch == "*" {
			return true
		}
	}
	return false
}

// matchDevice checks if a request device matches any allowed device pattern.
// Supports glob matching (e.g., "temperature-*" matches "temperature-living-room").
func matchDevice(patterns []string, reqDevice string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, reqDevice)
		if err == nil && matched {
			return true
		}
	}
	return false
}
