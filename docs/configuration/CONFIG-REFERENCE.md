# Configuration Reference

Config file: `~/.aibutler/config.yaml` (override with `$AIBUTLER_CONFIG`).
All values have sensible defaults. Only set what you want to change.

## Settings (Everyone)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `settings.language` | string | `"en"` | UI language code |
| `settings.timezone` | string | `"UTC"` | Timezone for scheduling and briefings |
| `settings.notifications` | bool | `true` | Enable notifications |
| `settings.morning_briefing` | string | `"8:00 AM"` | Morning briefing time |
| `settings.active_channels` | []string | `["webchat"]` | Active channel list |
| `settings.model` | string | `"claude-sonnet-4-6"` | Primary model (overrides `configurations.models.primary`) |
| `settings.persona_name` | string | `"Butler"` | Persona display name |
| `settings.skills` | []string | `["coding", "research"]` | Enabled skills |
| `settings.agents_enabled` | bool | `true` | Enable agent system |
| `settings.agent_mode` | string | `"auto"` | Agent mode: `auto` or `single` |
| `settings.cost.strategy` | string | `"balanced"` | Cost strategy: `frugal`, `balanced`, `quality` |
| `settings.cost.monthly_budget` | float64 | `10.00` | Monthly budget in USD |

## Configurations (Power Users)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `configurations.models.primary` | string | `"claude-sonnet-4-6"` | Primary model ID |
| `configurations.models.fallback` | string | `""` | Fallback model ID |
| `configurations.channels.<name>.enabled` | bool | — | Enable a channel |
| `configurations.channels.<name>.typing_indicators` | bool | — | Show typing indicators |
| `configurations.channels.<name>.voice_response` | string | — | Voice mode: `text`, `voice`, `auto`, `both` |
| `configurations.agents.max_concurrent` | int | `5` | Max concurrent agents |
| `configurations.agents.max_depth` | int | `3` | Max nesting depth (3 hops = 4 agents) |
| `configurations.agents.default_subagent_model` | string | `"haiku"` | Default model for sub-agents |
| `configurations.security.shell.mode` | string | `"allowlist"` | Shell security: `allowlist` or `denylist` |
| `configurations.security.shell.allowed` | []string | `[]` | Allowed shell commands |
| `configurations.cost.alerts` | []int | `[50, 75, 90, 100]` | Budget alert thresholds (%) |
| `configurations.cost.alert_channel` | string | `"same"` | Channel for budget alerts |
| `configurations.cost.on_budget_reached` | string | `"warn"` | Action at budget limit: `warn` or `pause` |
| `configurations.prompts.persona_file` | string | `~/.aibutler/prompts/persona.yaml` | Path to persona file |
| `configurations.prompts.skills_dir` | string | `~/.aibutler/prompts/skills` | Path to skills directory |
| `configurations.web.port` | int | `3377` | WebChat port |
| `configurations.web.bind_address` | string | `"localhost"` | WebChat bind address |
| `configurations.web.max_upload_size_mb` | int64 | `20` | Max upload size (MB) |
| `configurations.mcp.servers` | []object | `[]` | MCP server connections (name, command, args, env) |
| `configurations.schedule.enabled` | bool | `true` | Enable scheduler |
| `configurations.iot.adapter` | string | `"stub"` | IoT adapter: `stub`, `homeassistant` |
| `configurations.voice.stt_provider` | string | `"whisper"` | STT provider |
| `configurations.voice.tts_provider` | string | `"stub"` | TTS provider |

## Options (Developers)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `options.models.max_tokens` | int | `8192` | Max tokens per response |
| `options.models.temperature` | float64 | `0.7` | Model temperature |
| `options.models.request_timeout` | duration | `120s` | Request timeout |
| `options.models.retry_count` | int | `3` | Retry count on failure |
| `options.database.busy_timeout` | int | `5000` | SQLite busy timeout (ms) |
| `options.agents.max_tool_calls` | int | `50` | Max tool calls per agent run |
| `options.agents.subagent_timeout` | duration | `5m` | Sub-agent timeout |
| `options.agents.background_timeout` | duration | `4h` | Background agent timeout |
| `options.agents.background_max` | int | `3` | Max background agents |
| `options.prompts.max_tier1_tokens` | int | `700` | Token ceiling for Tier 1 pointers |
| `options.prompts.max_skills_per_turn` | int | `3` | Max skills per turn |
| `options.prompts.skill_trigger_threshold` | float64 | `0.5` | Skill trigger confidence threshold |
| `options.prompts.truncation_strategy` | string | `"balanced"` | Truncation: `balanced` or `essential_only` |
| `options.cost.cache_ttl` | duration | `5m` | Cost cache TTL |
| `options.cost.expensive_threshold` | int | `5000` | Token count considered expensive |
| `options.typing.interval_ms` | int | `3000` | Typing indicator interval (ms) |
| `options.typing.timeout_ms` | int | `120000` | Typing indicator timeout (ms) |
| `options.media.max_upload_size_mb` | int | `20` | Media max upload size (MB) |
| `options.media.max_text_lines` | int | `500` | Max text lines for media extraction |
| `options.schedule.tick_interval` | duration | `60s` | Scheduler tick interval |
| `options.schedule.max_concurrent` | int | `3` | Max concurrent scheduled tasks |
| `options.voice.max_audio_size_mb` | int | `25` | Max audio file size (MB) |
| `options.voice.stt_timeout` | duration | `30s` | STT processing timeout |

## Complete Example

```yaml
settings:
  language: en
  timezone: America/New_York
  notifications: true
  morning_briefing: "8:00 AM"
  active_channels: [terminal, webchat]
  model: claude-sonnet-4-6
  persona_name: Butler
  skills: [coding, research]
  agents_enabled: true
  agent_mode: auto
  cost:
    strategy: balanced
    monthly_budget: 10.00

configurations:
  models:
    primary: claude-sonnet-4-6
    fallback: haiku
  agents:
    max_concurrent: 5
    max_depth: 3
    default_subagent_model: haiku
  security:
    shell:
      mode: allowlist
      allowed: [ls, cat, grep, git]
  cost:
    alerts: [50, 75, 90, 100]
    alert_channel: same
    on_budget_reached: warn
  web:
    port: 3377
    bind_address: localhost
    max_upload_size_mb: 20
  schedule:
    enabled: true
  iot:
    adapter: stub
  voice:
    stt_provider: whisper
    tts_provider: stub

options:
  models:
    max_tokens: 8192
    temperature: 0.7
    request_timeout: 120s
    retry_count: 3
  agents:
    max_tool_calls: 50
    subagent_timeout: 5m
    background_timeout: 4h
    background_max: 3
  schedule:
    tick_interval: 60s
    max_concurrent: 3
```
