# Backup and Integrity

## Quick Example

```bash
# Create a backup right now
$ aibutler backup now
Backup created: /home/user/.aibutler/backups/aibutler-20260308-143022.db

# List all backups
$ aibutler backup list
Backups:
  aibutler-20260308-143022.db   524288 bytes  2026-03-08T14:30:22Z

# Verify backup files aren't corrupted
$ aibutler backup verify
  OK    aibutler-20260308-143022.db (524288 bytes)
Verified: 1 ok, 0 failed

# Check database + vault health
$ aibutler integrity
Database integrity... OK
Vault health...      OK
```

## How Backup Works

Uses the **SQLite Online Backup API** (not file copy). This means:
- Safe to run while the database is open
- Consistent snapshot (WAL mode compatible)
- Direct call to `sqlite3.BackupInit("main", dstPath)`

## Backup Location

All backups go to `~/.aibutler/backups/` (created with `0700` permissions).

Naming: `aibutler-YYYYMMDD-HHMMSS.db` (UTC timestamp).

## CLI Commands

| Command                       | What it does                                     |
|-------------------------------|--------------------------------------------------|
| `aibutler backup now`         | Create a backup to the default backups directory  |
| `aibutler backup list`        | List all backups with size and timestamp          |
| `aibutler backup verify`      | Check each backup file is non-empty and readable  |
| `aibutler backup export FILE` | Backup to a custom file path                      |
| `aibutler backup import FILE` | Verify an import file (manual copy + restart required) |
| `aibutler integrity`          | Run PRAGMA integrity_check + vault HealthCheck    |

## Integrity Check

`aibutler integrity` runs two checks:
1. **Database**: `PRAGMA integrity_check` -- validates B-tree structure, page consistency
2. **Vault**: `vault.HealthCheck()` -- verifies the credential store is accessible and decryptable

## Export and Import

- **Export** uses the same SQLite Backup API to write to any path
- **Import** is manual: verify the file, then stop AI Butler, copy it to the DB path, restart

## Source Files

- `internal/cli/cmd_backup.go` -- All CLI commands (backup now/list/verify/export/import, integrity)
- `internal/db/backup.go` -- `DB.Backup()` using SQLite Online Backup API
- `internal/db/db.go` -- `DB.IntegrityCheck()` via PRAGMA integrity_check
