package shell

// MatchAllowlist checks if a command name is in the allowlist.
// Empty allowlist means no commands are allowed (secure by default).
func MatchAllowlist(allowed []string, cmdName string) bool {
	for _, a := range allowed {
		if a == cmdName {
			return true
		}
		// Support prefix wildcard: "npm*" matches "npm", "npx".
		if len(a) > 1 && a[len(a)-1] == '*' {
			prefix := a[:len(a)-1]
			if len(cmdName) >= len(prefix) && cmdName[:len(prefix)] == prefix {
				return true
			}
		}
	}
	return false
}
