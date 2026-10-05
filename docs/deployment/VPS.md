# VPS Deployment

Run AI Butler on a small cloud server — a 1 vCPU / 1 GB instance is enough
for the binary; add RAM if you run local models alongside it.

## 1. Install

```bash
# Go 1.26+
git clone https://github.com/LumabyteCo/aibutler.git
cd aibutler
CGO_ENABLED=0 go build -o aibutler .
sudo useradd --system --home /var/lib/aibutler --shell /usr/sbin/nologin aibutler || true
sudo mkdir -p /var/lib/aibutler
sudo chown aibutler:aibutler /var/lib/aibutler
sudo cp aibutler /usr/local/bin/
```

## 2. Configure

All state lives under `AIBUTLER_DATA=/var/lib/aibutler`:

```bash
sudo -u aibutler aibutler vault set anthropic_api_key sk-ant-...
sudo -u aibutler aibutler setup
```

## 3. systemd

```bash
sudo cp deploy/systemd/aibutler.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now aibutler
```

The unit sets `AIBUTLER_DATA=/var/lib/aibutler` — all config, database, and
vault files live there. It grants write access only to that path via
`ReadWritePaths`, with `ProtectSystem=strict` and `ProtectHome=true`.

## 4. Expose the web chat

Put the web chat behind TLS with a reverse proxy. The service listens on
`localhost:3377` by default:

```nginx
server {
    listen 443 ssl http2;
    server_name butler.example.com;
    # ssl_certificate ...;
    location / {
        proxy_pass http://127.0.0.1:3377;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
    }
}
```

WebSocket support (the `Upgrade` headers above) is required for streaming.

For public exposure, enable Internet mode — see
[Web Chat docs](../channels/WEBCHAT.md) for the built-in password auth,
TOTP, and TLS options.

## Hardening checklist

| Setting | Value |
|---|---|
| `ProtectSystem` | strict |
| `ProtectHome` | true |
| `NoNewPrivileges` | true |
| `ReadWritePaths` | /var/lib/aibutler only |
| Bind address | `127.0.0.1` behind a proxy |

## What's next

- [systemd reference](../deploy/systemd.md)
- [Kubernetes / Helm](../deploy/kubernetes.md)