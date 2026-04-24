# VPS Deployment

## Prerequisites

- Linux VPS (Ubuntu 22.04+, Debian 12+, etc.)
- Go 1.22+ (only if building from source)

## Install the Binary

Option A -- build on the server:

```bash
CGO_ENABLED=0 go build -o /usr/local/bin/aibutler .
```

Option B -- build locally, copy to server:

```bash
# On your machine:
make build-all
scp dist/aibutler-linux-amd64 you@server:/usr/local/bin/aibutler

# On the server:
chmod +x /usr/local/bin/aibutler
```

## Create a Service User

```bash
sudo useradd -r -m -d /home/aibutler -s /usr/sbin/nologin aibutler
```

## Install the systemd Unit

```bash
sudo cp dist/systemd/aibutler.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now aibutler
```

## Verify

```bash
sudo systemctl status aibutler
sudo journalctl -u aibutler -f       # Follow logs
```

## Data Location

The systemd unit sets `AIBUTLER_HOME=/home/aibutler/.aibutler`. All config, database, and vault files live there. The unit grants write access only to that path via `ReadWritePaths`.

## Firewall

AI Butler's WebChat listens on port 3377 by default (configurable in `config.yaml` under `configurations.web.port`). Only expose it if you want WebChat accessible externally:

```bash
# UFW example
sudo ufw allow 3377/tcp

# Or restrict to your IP
sudo ufw allow from 203.0.113.50 to any port 3377
```

If you only use Telegram/Slack/Discord channels, port 3377 does not need to be open.

## Security Hardening

The systemd unit includes these protections out of the box:

| Directive | Effect |
|-----------|--------|
| `NoNewPrivileges=yes` | Process cannot gain new privileges |
| `ProtectSystem=strict` | Filesystem is read-only except allowed paths |
| `ProtectHome=read-only` | Home directories are read-only except `/home/aibutler/.aibutler` |
| `ReadWritePaths=/home/aibutler/.aibutler` | Only writable path |
| `PrivateTmp=yes` | Isolated /tmp |
| `LimitNOFILE=65536` | File descriptor limit |
| `Restart=on-failure` | Auto-restart with 5s delay |

## Updating

```bash
# Stop, replace binary, start
sudo systemctl stop aibutler
sudo cp aibutler-linux-amd64 /usr/local/bin/aibutler
sudo systemctl start aibutler
```

The SQLite database schema auto-migrates on startup (`ApplySchema` runs at boot).
