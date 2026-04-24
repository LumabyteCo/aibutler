# systemd Deployment

## Quick Install

```bash
cd deploy/systemd
sudo bash install.sh ./aibutler
```

This script:
1. Creates a dedicated `aibutler` system user
2. Copies the binary to `/usr/local/bin/`
3. Creates `/var/lib/aibutler` for data
4. Installs and enables the systemd service

## Manual Install

Copy the service file:

```bash
sudo cp deploy/systemd/aibutler.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable aibutler
sudo systemctl start aibutler
```

## Management

```bash
sudo systemctl start aibutler    # Start
sudo systemctl stop aibutler     # Stop
sudo systemctl restart aibutler  # Restart
sudo systemctl status aibutler   # Status
journalctl -u aibutler -f        # Follow logs
```

## Security

The service file includes systemd security hardening:

- `NoNewPrivileges=true` -- prevents privilege escalation
- `ProtectSystem=strict` -- mounts filesystem read-only except allowed paths
- `ProtectHome=true` -- hides home directories
- `PrivateTmp=true` -- isolates temp directory
