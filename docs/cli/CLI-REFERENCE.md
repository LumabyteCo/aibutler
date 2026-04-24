# CLI Reference

## Quick Start

```bash
aibutler              # Start interactive mode (channels + scheduler)
aibutler help         # Show all commands
aibutler version      # Print version
```

## Commands

### version

```bash
$ aibutler version
aibutler v0.1.0
```

### setup

Show current configuration and config file path.

```bash
$ aibutler setup
=== AI Butler Setup ===

Current configuration:
  Persona:          Butler
  Language:         en
  Timezone:         UTC
  Model:            claude-sonnet-4-6
  Agent Mode:       auto
  Active Channels:  terminal
  Cost Strategy:    balanced
  Monthly Budget:   $10.00

Config file: /home/user/.aibutler/config.yaml

To configure AI Butler, edit the config file directly:
  $EDITOR /home/user/.aibutler/config.yaml
```

### config show

```bash
$ aibutler config show
=== Settings ===
  Language:         en
  Timezone:         UTC
  Model:            claude-sonnet-4-6
  Persona:          Butler
  Agent Mode:       auto
  Cost Strategy:    balanced
  Monthly Budget:   $10.00
  Channels:         [terminal]

=== Configurations ===
  Schedule:         enabled
  IoT Adapter:      stub
  Voice STT:        whisper
  Voice TTS:        stub

=== Options ===
  Schedule Interval: 1m0s
  Max Concurrent:    3
  Voice Max Audio:   25MB
```

### skill list | enable | disable

```bash
$ aibutler skill list
NAME                 STATUS     TRIGGERS
----                 ------     --------
coding               enabled    code, debug, fix, implement
research             enabled    search, find, lookup

$ aibutler skill enable finance
Enabled skill: finance

$ aibutler skill disable research
Disabled skill: research
```

### cost status | history | breakdown | strategy | budget

```bash
$ aibutler cost status
=== Cost Status ===
  This Month:    $1.2340
  Budget:        $10.00
  Remaining:     $8.7660
  Strategy:      balanced

$ aibutler cost history
=== Cost History ===
  Current Month: $1.2340

$ aibutler cost breakdown
MODEL                               CALLS        INPUT       OUTPUT       COST
-----                               -----        -----       ------       ----
claude-sonnet-4-6                      42        51200        12800 $1.1200
haiku                                  15         8400         3200 $0.1140

$ aibutler cost strategy
Current strategy: balanced

$ aibutler cost strategy frugal
Cost strategy set to: frugal

$ aibutler cost budget
Monthly budget: $10.00

$ aibutler cost budget 25
Monthly budget set to: $25.00
```

Valid strategies: `frugal`, `balanced`, `quality`.

### agent list | status | history

```bash
$ aibutler agent list
ID                                   TYPE         STATE      TASK                           MODEL                CREATED
--                                   ----         -----      ----                           -----                -------
a1b2c3d4-...                         primary      running    Summarize weekly report         claude-sonnet-4-6   2026-03-08T10:00:00Z

$ aibutler agent status a1b2c3d4-...
=== Agent Status ===
  ID:           a1b2c3d4-...
  Type:         primary
  State:        running
  Task:         Summarize weekly report
  Model:        claude-sonnet-4-6
  Tokens:       4200
  Cost:         $0.0126
  Tool Calls:   3
  Duration:     1520ms
  Created:      2026-03-08T10:00:00Z

$ aibutler agent history
ID                                   TYPE         STATE      TASK                      MODEL            COST CREATED
--                                   ----         -----      ----                      -----            ---- -------
a1b2c3d4-...                         primary      done       Summarize weekly report   claude-sonnet... $0.0126 2026-03-08T10:00:00Z
```

### mode [name]

```bash
$ aibutler mode
Current agent mode: auto
  (behaves as single in v0.1)

$ aibutler mode single
Agent mode switched to: single
```

Valid modes in v0.1: `auto`, `single`. Modes `multi`, `swarm`, `custom` downgrade to `single`.

### auth list | status | revoke

```bash
$ aibutler auth list
Stored credentials:
  - anthropic
  - openai

$ aibutler auth status
Vault status: healthy

$ aibutler auth revoke openai
Revoked credential: openai
```

### voice status | providers

```bash
$ aibutler voice status
=== Voice Status ===
  STT Provider:     whisper
  TTS Provider:     stub
  Voice Response:   text
  Max Audio Size:   25MB
  STT Timeout:      30s

$ aibutler voice providers
=== STT Providers ===
  - whisper (default)
  - stub

=== TTS Providers ===
  - stub (default)
```

### backup now | list | verify | export | import

```bash
$ aibutler backup now
Backup created: /home/user/.aibutler/backups/aibutler-20260308-100500.db

$ aibutler backup list
Backups:
  aibutler-20260308-100500.db              524288 bytes  2026-03-08T10:05:00Z

$ aibutler backup verify
  OK    aibutler-20260308-100500.db (524288 bytes)

Verified: 1 ok, 0 failed

$ aibutler backup export /tmp/butler-export.db
Database exported to: /tmp/butler-export.db

$ aibutler backup import /tmp/butler-export.db
File /tmp/butler-export.db verified (524288 bytes).
To import, stop AI Butler and copy this file to your database path.
A restart is required after importing.
```

### integrity

```bash
$ aibutler integrity
Database integrity... OK
Vault health...      OK
```
