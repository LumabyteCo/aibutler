# AGENTS.md

AI Butler supports the A2A v2 protocol for agent-to-agent communication.

## Agent Card
Available at `/.well-known/agent.json` when A2A is enabled.

## Capabilities
- Memory search and knowledge graph queries
- Scheduled task creation and management
- Multi-channel message delivery
- Swarm orchestration for complex tasks

## Integration
AI Butler can act as:
1. **A2A peer** -- accepts delegated tasks via POST /a2a/tasks
2. **MCP server** -- exposes memory, schedule, channels as MCP resources/tools
3. **Bridge host** -- launches external agents (Aider, OpenClaw) via subprocess
