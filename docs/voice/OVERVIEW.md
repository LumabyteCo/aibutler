# Voice Pipeline

## Quick Example

```
User sends voice message (OGG/OPUS from Telegram)
  -> Normalizer: OGG -> 16kHz mono WAV (via ffmpeg)
  -> STT (Whisper API): WAV -> "What's the weather?"
  -> Agent processes text, generates reply
  -> TTS: "It's 72F and sunny" -> audio bytes
  -> Voice message sent back to user
```

## Response Modes

| Mode   | Behavior                                          |
|--------|---------------------------------------------------|
| `text` | Default. No TTS, text-only replies                |
| `voice`| Always generate audio response                    |
| `auto` | Audio for short responses (< 500 chars), else text|
| `both` | Always generate audio (same as `voice`)           |

Set per-channel: `configurations.channels.webchat.voice_response: "auto"`

## STT Providers

- **whisper** (default) -- OpenAI Whisper API (`whisper-1` model, `verbose_json` format)
- **stub** -- Returns canned text for testing

## TTS Providers

- **stub** (default) -- Returns placeholder WAV. Real TTS (Edge TTS, OpenAI) is planned for a future release.

## Audio Normalization

The `Normalizer` converts unsupported formats to 16kHz mono WAV via ffmpeg:

- **No conversion needed**: WAV, MP3, OGG (sent as-is to STT)
- **Converted via ffmpeg**: WebM, FLAC, other formats
- **Graceful degradation**: If ffmpeg is missing, sends original format to the STT provider

## Configuration

```yaml
configurations:
  voice:
    stt_provider: "whisper"   # default
    tts_provider: "stub"      # default

options:
  voice:
    max_audio_size_mb: 25     # default
    stt_timeout: 30s          # default
```

## CLI

```bash
aibutler voice status     # Shows STT/TTS provider, response mode, limits
aibutler voice providers  # Lists available STT and TTS providers
```

## Source Files

- `internal/voice/pipeline.go` -- Pipeline orchestration
- `internal/voice/stt.go` -- WhisperProvider, StubSTTProvider
- `internal/voice/tts.go` -- StubTTSProvider
- `internal/voice/normalizer.go` -- ffmpeg audio conversion
- `internal/cli/cmd_voice.go` -- CLI commands
