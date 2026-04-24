# Local Deployment

## Build from Source

```bash
# Prerequisites: Go 1.22+
git clone https://github.com/LumabyteCo/aibutler.git
cd aibutler
CGO_ENABLED=0 go build -o aibutler .
```

Or use the Makefile:

```bash
make build          # Single binary for your platform
make build-all      # Cross-platform: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
```

`make build-all` produces 4 binaries in `dist/`:

```
dist/aibutler-linux-amd64
dist/aibutler-linux-arm64
dist/aibutler-darwin-amd64
dist/aibutler-darwin-arm64
```

## Run

```bash
./aibutler              # Start interactive mode (channels + scheduler)
./aibutler version      # Print version
./aibutler help         # Show all commands
```

Running `./aibutler start` starts the default mode: WebChat on `localhost:3377` plus the scheduler.

## Data Directory

All state lives in `~/.aibutler/`:

```
~/.aibutler/
  config.yaml       # Configuration (Three Enriches: Settings / Configurations / Options)
  aibutler.db        # SQLite database (WAL mode, busy_timeout=5000ms)
  vault/             # Encrypted credential storage (0700 permissions)
  prompts/
    persona.yaml     # Persona definition
    skills/          # Skill prompt files
```

Override the config path with `AIBUTLER_CONFIG=/path/to/config.yaml`.

## Run as a macOS Service (launchd)

Copy the plist to `~/Library/LaunchAgents/`:

```bash
cp dist/launchd/dev.aibutler.agent.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/dev.aibutler.agent.plist
```

The plist runs `/usr/local/bin/aibutler` at login with auto-restart (`KeepAlive: true`).
Logs go to `/tmp/aibutler.out.log` and `/tmp/aibutler.err.log`.

To stop:

```bash
launchctl unload ~/Library/LaunchAgents/dev.aibutler.agent.plist
```

## Run as a Linux Service (systemd)

```bash
sudo cp dist/systemd/aibutler.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now aibutler
```

The unit runs as a dedicated `aibutler` user with security hardening (NoNewPrivileges, ProtectSystem=strict, PrivateTmp). See [VPS.md](VPS.md) for full server setup.
