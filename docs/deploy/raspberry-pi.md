# Raspberry Pi Deployment

## Supported Hardware

- Raspberry Pi 4 (2GB+ RAM recommended)
- Raspberry Pi 5

## Install from Binary

Download the ARM build:

```bash
curl -LO https://github.com/LumabyteCo/aibutler/releases/latest/download/aibutler_linux_arm64.tar.gz
tar xzf aibutler_linux_arm64.tar.gz
sudo mv aibutler /usr/local/bin/
```

For 32-bit Raspberry Pi OS:

```bash
curl -LO https://github.com/LumabyteCo/aibutler/releases/latest/download/aibutler_linux_armv7.tar.gz
tar xzf aibutler_linux_armv7.tar.gz
sudo mv aibutler /usr/local/bin/
```

## Run as systemd Service

```bash
cd deploy/systemd
sudo bash install.sh /usr/local/bin/aibutler
sudo systemctl start aibutler
```

## Docker on Pi

```bash
docker compose up -d
```

The Dockerfile uses `CGO_ENABLED=0` so the binary is statically linked and works on all ARM variants.

## Performance Tips

- Use an SSD or fast SD card for the data directory
- Allocate swap if using 2GB RAM model
- Consider running Ollama on a separate, more powerful machine and pointing AI Butler to it via configuration
