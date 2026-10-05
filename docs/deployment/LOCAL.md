# Local Deployment

Run AI Butler as a persistent service on your own machine.

## Build from Source

```bash
# Prerequisites: Go 1.26+
git clone https://github.com/LumabyteCo/aibutler.git
cd aibutler
CGO_ENABLED=0 go build -o aibutler .
sudo mv aibutler /usr/local/bin/
```

Or use the Makefile:

```bash
make build          # Single binary for your platform
make build-all      # Cross-platform matrix
```

## Run

```bash
aibutler run
```

Default data directory: `~/.aibutler` (config, SQLite database, vault, plugins).
Override it with the `AIBUTLER_DATA` environment variable — the same variable
the Docker image and systemd unit use.

## Run as a macOS Service (launchd)

Create `~/Library/LaunchAgents/dev.aibutler.agent.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>dev.aibutler.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/aibutler</string>
        <string>run</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/aibutler.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/aibutler.err.log</string>
</dict>
</plist>
```

Then:

```bash
launchctl load ~/Library/LaunchAgents/dev.aibutler.agent.plist
```

Stop with `launchctl unload` on the same path. Logs land in `/tmp/aibutler.log`.

## Run as a Linux Service (systemd)

The hardened unit ships in the repo:

```bash
sudo cp deploy/systemd/aibutler.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now aibutler
```

The unit runs as a dedicated user, sets `AIBUTLER_DATA=/var/lib/aibutler`, and
grants write access only to that path (`ProtectSystem=strict`,
`ProtectHome=true`, `ReadWritePaths=/var/lib/aibutler`). See
[systemd docs](../deploy/systemd.md) for details.

## What's next

- [Docker](../deploy/docker.md)
- [Configuration reference](../configuration/CONFIG-REFERENCE.md)