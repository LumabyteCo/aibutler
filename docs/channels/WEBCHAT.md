# WebChat

Built-in web interface. No API keys, no third-party setup. Start AI Butler and open your browser.

## What You Get

- Browser-based chat at `http://localhost:3377`
- Real-time streaming via WebSocket
- File upload (up to 20 MB)
- Dark mode
- 14-language interface (follows `settings.language`)
- Typing indicators

## Quick Start

```bash
aibutler start
# Open http://localhost:3377
```

That's it. WebChat is enabled by default.

## Configuration

In `~/.aibutler/config.yaml`:

```yaml
settings:
  active_channels:
    - webchat

configurations:
  web:
    port: 3377
    bind_address: localhost
    max_upload_size_mb: 20
```

### Config Fields

| Field | Default | Description |
|-------|---------|-------------|
| `port` | 3377 | HTTP server port |
| `bind_address` | localhost | Interface to bind to |
| `max_upload_size_mb` | 20 | Max file upload size in MB |

## Access from Other Devices on LAN

By default, WebChat only listens on `localhost` (not reachable from other machines). To open it to your local network:

```yaml
configurations:
  web:
    bind_address: "0.0.0.0"
```

Then access it from any device at `http://<your-ip>:3377`.

**Security note:** `localhost` binding is intentional. Changing to `0.0.0.0` exposes the chat to your entire network. Only do this on trusted networks. There is no built-in authentication on the WebChat endpoint.

## How It Works

WebChat serves a static HTML/JS frontend and opens a WebSocket connection at `/ws`. Messages flow as JSON frames:

```json
{"type": "message", "text": "hello"}
{"type": "typing"}
{"type": "file", "file_id": "upload-..."}
```

File uploads go to `POST /upload` as multipart form data and return a `file_id` reference.

## Channel Config (Optional)

```yaml
configurations:
  channels:
    webchat:
      enabled: true
      typing_indicators: true
```
