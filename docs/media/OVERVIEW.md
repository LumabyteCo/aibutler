# Media Pipeline

## Quick Example

```
User sends "report.pdf" (45KB) via Telegram
  -> Pipeline.Process(data, "report.pdf")
  -> DetectMIME: extension ".pdf" -> "application/pdf"
  -> Find processor: PDFProcessor.CanProcess("application/pdf") -> true
  -> PDFProcessor.Process() -> Result{Type: "text", Content: "...extracted text..."}

User sends "main.go" (2KB)
  -> DetectMIME: extension ".go" -> "text/x-go"
  -> CodeProcessor handles it -> Result{Type: "code", Language: "go", Content: "..."}
```

## How It Works

The `media.Pipeline` routes incoming files through registered processors:

1. **Size check** -- Reject files over `maxSize` (default 20MB from `options.media.max_upload_size_mb`)
2. **MIME detection** -- Extension-based first, then `http.DetectContentType` fallback
3. **Route to processor** -- First processor where `CanProcess(mime)` returns true
4. **Fallback** -- If no processor matches, return a generic `file` result with metadata

## MIME Detection

Extension overrides for code files (where content sniffing gives unhelpful results):

| Extensions                     | MIME Type          |
|-------------------------------|--------------------|
| `.go`                         | `text/x-go`        |
| `.py`                         | `text/x-python`    |
| `.js`                         | `text/javascript`  |
| `.ts`                         | `text/typescript`  |
| `.rs`, `.rb`, `.java`, `.c`, `.cpp` | `text/x-{lang}` |
| `.md`, `.yaml`, `.json`, `.csv` | Standard types   |
| `.pdf`                        | `application/pdf`  |
| `.ogg`, `.mp3`, `.wav`, `.webm` | `audio/*`        |

Full list: 22 extension overrides + 20 language mappings in `detect.go`.

## Result

`media.Result` fields: `Type` (text/image/code/audio_transcript/file), `Content`, `MimeType`, `Language`, `ImageData` (for vision models), `Metadata`.

## Processor Interface

Implement `CanProcess(mimeType string) bool` and `Process(ctx, data, mimeType, filename)`. Register with `pipeline.Register(proc)`. First match wins.

## Current scope & roadmap

- **Today (v0.1):** MIME detection, routing framework, code/text processing
- **Planned:** OCR, vision model integration, PDF text extraction

## Source Files

- `internal/media/media.go` -- Pipeline, Processor interface, Result
- `internal/media/detect.go` -- DetectMIME, LanguageForExtension, extension maps
