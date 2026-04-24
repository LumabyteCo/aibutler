#!/usr/bin/env bash
set -euo pipefail

BINARY="${1:-./aibutler}"
SERVICE_FILE="$(dirname "$0")/aibutler.service"

echo "Installing AI Butler as a systemd service..."

# Create system user if it doesn't exist.
if ! id -u aibutler &>/dev/null; then
    sudo useradd --system --no-create-home --shell /usr/sbin/nologin aibutler
    echo "Created system user: aibutler"
fi

# Create data directory.
sudo mkdir -p /var/lib/aibutler
sudo chown aibutler:aibutler /var/lib/aibutler

# Copy binary.
sudo cp "$BINARY" /usr/local/bin/aibutler
sudo chmod 755 /usr/local/bin/aibutler
echo "Installed binary to /usr/local/bin/aibutler"

# Install service file.
sudo cp "$SERVICE_FILE" /etc/systemd/system/aibutler.service
sudo systemctl daemon-reload
sudo systemctl enable aibutler
echo "Service installed and enabled."

echo ""
echo "Start with:   sudo systemctl start aibutler"
echo "Status:       sudo systemctl status aibutler"
echo "Logs:         journalctl -u aibutler -f"
