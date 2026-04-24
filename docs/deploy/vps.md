# VPS Deployment

## Prerequisites

- Linux server (Ubuntu 22.04+ recommended)
- 1 GB RAM minimum
- Go 1.26+ (for building from source) or pre-built binary

## Install from Binary

Download the latest release:

```bash
curl -LO https://github.com/LumabyteCo/aibutler/releases/latest/download/aibutler_linux_amd64.tar.gz
tar xzf aibutler_linux_amd64.tar.gz
sudo mv aibutler /usr/local/bin/
```

## Install as systemd Service

```bash
cd deploy/systemd
sudo bash install.sh /usr/local/bin/aibutler
sudo systemctl start aibutler
```

## Reverse Proxy (nginx)

```nginx
server {
    listen 80;
    server_name aibutler.example.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

## TLS with Let's Encrypt

```bash
sudo apt install certbot python3-certbot-nginx
sudo certbot --nginx -d aibutler.example.com
```

## Firewall

```bash
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
```
