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

---

## Offline voice (wake word + local STT + system TTS)

Butler can run a fully local voice loop as a normal channel:

```
2s mic segment -> voice-activity gate -> local STT -> fuzzy wake-word match
  -> acknowledgment tone -> command window (max 8s, silence-trimmed)
  -> local STT -> normal message router (hybrid local/cloud router)
  -> reply -> system TTS -> speaker
```

No audio leaves the machine. The wake detector **refuses to start** with a
cloud STT provider, so ambient audio can never be streamed to an API.

### Requirements

- A microphone recorder: `sox` (macOS/Linux, preferred) or `arecord` (ALSA)
- A **local** STT provider: `command` (a local binary) or `local` (an
  OpenAI-compatible endpoint such as whisper.cpp's server)
- A real TTS provider: `system` (macOS `say`, Linux `espeak-ng`, Windows SAPI)
- Opt in with `active_channels: [voice]` (or `channels.voice.enabled: true`)

### Configuration — local command (Python whisper or whisper.cpp)

```yaml
settings:
  active_channels: [webchat, voice]

configurations:
  voice:
    stt_provider: command
    stt_command:
      - whisper
      - "{audio}"
      - --model
      - base
      - --output_format
      - txt
      - --output_dir
      - "{dir}"
      - --language
      - en
      - --fp16
      - "False"
    # Optional environment (needed when Butler's HOME differs from the
    # Python install, or to pin a model cache directory).
    stt_env:
      PYTHONPATH: /Users/you/Library/Python/3.9/lib/python/site-packages
    tts_provider: system
    tts_voice: Samantha        # macOS voice; Linux e.g. "en"
    wake_enabled: true
    wake_word: hey butler
```

`{audio}` is a temporary WAV; `{dir}` is a temporary output directory. The
command must write exactly one `.txt` transcript into `{dir}`. whisper.cpp
works the same way:

```yaml
stt_command:
  - whisper-cli
  - -m
  - /models/ggml-base.en.bin
  - -f
  - "{audio}"
  - -otxt
  - -of
  - "{dir}/transcript"
```

### Configuration — local server

```yaml
configurations:
  voice:
    stt_provider: local
    stt_base_url: http://localhost:8080   # -> /v1/audio/transcriptions
    stt_model: whisper-1                  # ignored by whisper.cpp
```

Works with `whisper.cpp --server`, faster-whisper-server, LocalAI, or any
OpenAI-compatible transcription endpoint.

### Privacy and policy

- Wake-word listening requires `stt_provider: local` or `command`. A cloud
  provider is rejected with a log line; ambient audio is never uploaded.
- Audio is written to a temp directory and deleted after transcription.
- Wake matching is **fuzzy** on purpose: small models mishear names
  ("Butler" → "Buggler"), so the anchor word may differ by up to two edits.
  False wakes are recoverable — they only open an 8s command window.
- The detector is paused while a command is handled and spoken, so the
  assistant cannot wake itself.

### Latency and limitations

- Wake latency is roughly one segment (2s) plus one STT pass (tiny/base
  models: ~1–3s on a laptop CPU). There is no native wake-word library and
  therefore no always-on 100ms detection.
- Command capture is a bounded window (8s, trimmed after 1.2s of silence).
  There is no barge-in or continuous dictation.
- The voice channel is half-duplex: it listens, handles, speaks, then
  listens again.
- The first local STT run may download a model unless it is already cached;
  point the command at a pre-downloaded model for true offline operation.

### CLI

```bash
aibutler voice status                  # providers, wake word, mic/player readiness
aibutler voice providers               # STT/TTS provider list
aibutler voice test audio.wav          # transcribe a file with the configured STT
aibutler voice simulate audio.wav      # transcribe + wake-word match (no microphone)
aibutler voice speak "Hello" --out out.wav [--play]
```

`voice test` and `voice simulate` are the hardware-free way to validate the
offline pipeline: generate speech with `say`/`espeak`, then run it through
the configured local STT.

### Troubleshooting

- **`No module named 'whisper'`** — Python's user site-packages follows
  `HOME`. Set `stt_env.PYTHONPATH` to the real site-packages directory (or
  use an absolute wrapper script).
- **`no microphone recorder`** — install `sox` (macOS: `brew install sox`)
  or `arecord` (ALSA utils). Check `aibutler voice status`.
- **`no system speech engine`** — macOS ships `say`; Linux needs
  `espeak-ng` (`apt install espeak-ng`); Windows uses PowerShell SAPI.
- **Mic permission** — macOS prompts the first time `sox` records. If the
  terminal denies access, enable it in System Settings → Privacy &
  Security → Microphone.

---

## STT Providers

- **whisper** (default) — OpenAI Whisper API (`whisper-1`, `verbose_json`)
- **local** — OpenAI-compatible endpoint (whisper.cpp server, faster-whisper)
- **command** — local binary; fully offline
- **deepgram** — Deepgram API (cloud)
- **stub** — canned text for tests

## TTS Providers

- **system** — macOS `say`, Linux `espeak-ng`, Windows SAPI; fully offline
- **edge** — Microsoft Edge TTS (network)
- **stub** (default) — placeholder WAV for tests

## Audio Normalization

The `Normalizer` converts unsupported formats to 16kHz mono WAV via ffmpeg:

- **No conversion needed**: WAV, MP3, OGG (sent as-is to STT)
- **Converted via ffmpeg**: WebM, FLAC, other formats
- **Graceful degradation**: If ffmpeg is missing, sends original format to the STT provider

## Configuration (cloud path)

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

## Source Files

- `internal/voice/pipeline.go` — Pipeline orchestration
- `internal/voice/stt.go` — WhisperProvider, StubSTTProvider
- `internal/voice/stt_local.go` — LocalSTTProvider, CommandSTTProvider
- `internal/voice/tts.go` — TTS interface, StubTTSProvider
- `internal/voice/tts_system.go` — macOS/Linux/Windows system TTS
- `internal/voice/audio_io.go` — sox/arecord recorder, afplay/aplay player
- `internal/voice/wakeword.go` — STT-backed wake-word detector
- `internal/voice/wake_match.go` — fuzzy phrase matching
- `internal/voice/loop.go` — wake → capture → handle → speak loop
- `internal/voice/wav.go` — WAV parsing, RMS gate, tone generation
- `internal/channel/voice/` — the voice channel adapter
- `internal/cli/cmd_voice.go` — CLI commands
