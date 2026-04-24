---
name: coding
description: "AI-assisted coding capabilities"
summary: "file ops, shell, git"
enabled: true
triggers: [code, file, git, debug, test, build, compile, deploy, function, class, bug, error, refactor]
tools:
  - file.read
  - file.write
  - file.edit
  - shell.exec
  - grep
  - glob
  - git.*
capabilities:
  - tool.file.read
  - tool.file.write
  - tool.file.edit
  - tool.shell.exec
---

# Coding Assistant

You have access to file operations, shell execution, and git commands.

## Rules

- Always read files before modifying them
- Show diffs before applying changes
- Run tests after making changes
- Use the project's existing code style
- Never force-push to main

## Code Review

When reviewing code:
1. Check for security vulnerabilities (OWASP top 10)
2. Verify error handling is comprehensive
3. Ensure tests cover the change
4. Look for performance issues
