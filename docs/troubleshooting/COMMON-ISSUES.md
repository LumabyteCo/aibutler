# Common Issues

## Build Fails

**Symptom:** CGo errors, missing C compiler.

**Fix:** AI Butler requires `CGO_ENABLED=0`. The SQLite driver (`ncruces/go-sqlite3`) uses Wasm, not CGo.

```bash
CGO_ENABLED=0 go build -o aibutler .
```

Also verify Go 1.22+ is installed:

```bash
go version
```

## "database is locked"

**Symptom:** Operations fail with `database is locked` or `database table is locked`.

**Why:** Another process or a long-running query is holding the lock.

**Fix:** AI Butler sets `busy_timeout=5000` (5 seconds) and uses WAL mode by default. If you still see this:

1. Ensure only one AI Butler process is running.
2. Increase the timeout in `config.yaml`:
   ```yaml
   options:
     database:
       busy_timeout: 10000  # 10 seconds
   ```
3. Check for zombie processes: `ps aux | grep aibutler`.

## Channel Not Responding

**Symptom:** Messages sent to Telegram/Slack/Discord get no reply.

**Checklist:**

1. Is the channel listed in `active_channels`?
   ```yaml
   settings:
     active_channels: ["terminal", "telegram"]
   ```
2. Is the channel enabled in configurations?
   ```yaml
   configurations:
     channels:
       telegram:
         enabled: true
   ```
3. Are credentials stored in the vault?
   ```bash
   aibutler auth list    # Check stored credentials
   aibutler auth status  # Verify credential health
   ```

## "unknown command: ..."

**Symptom:** `unknown command: foo` on the CLI.

**Valid commands:**

```
aibutler setup | config | skill | cost | agent | mode | auth | voice | backup | integrity | version | help
```

Run `aibutler help` for full usage.

## Config Not Loading

**Symptom:** Changes to `config.yaml` have no effect, or defaults are used.

**Checklist:**

1. Default path: `~/.aibutler/config.yaml`
2. Override with: `AIBUTLER_CONFIG=/path/to/config.yaml`
3. Check YAML syntax: `python3 -c "import yaml; yaml.safe_load(open('config.yaml'))"`
4. File must be readable by the aibutler process user.

## Backup Fails on In-Memory Database

**Symptom:** `backup: cannot backup in-memory database`

**Why:** The SQLite Online Backup API cannot back up `:memory:` databases. This only happens in test configurations.

**Fix:** Ensure your production config uses a file-based database (the default `~/.aibutler/aibutler.db`).

## High Memory Usage

**Symptom:** AI Butler process using unexpectedly large amounts of memory.

**Tune these settings:**

```yaml
configurations:
  agents:
    max_concurrent: 5      # Lower this to reduce parallel agent memory
settings:
  cost:
    strategy: "frugal"     # Sliding window: 30 messages (vs 100 balanced, 200 quality)
options:
  prompts:
    max_tier1_tokens: 700  # Ceiling for Tier 1 prompt pointers
```

## Permission Denied on Vault

**Symptom:** `init vault: ...` or `permission denied` errors on startup.

**Fix:** The vault directory must have `0700` permissions:

```bash
chmod 700 ~/.aibutler/vault
ls -la ~/.aibutler/ | grep vault
# drwx------  vault/
```

On Linux with systemd, verify `ReadWritePaths` includes the data directory.
