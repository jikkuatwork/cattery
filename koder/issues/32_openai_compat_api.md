# #32 — OpenAI-Compatible Server API

## Status: done | Pri: P1

## Goal

Replace cattery's custom server endpoints with OpenAI-compatible ones so the
OpenAI Python/Node SDK works as a drop-in client. Cattery becomes a local
OpenAI — same API, no cloud, runs on Pi4.

## Motivation

- "Works with any OpenAI SDK" is instantly understood by developers
- One API surface instead of two (custom + compat) halves testing/docs burden
- Every existing SDK in every language works day one — no cattery SDK needed
- Adding new models becomes trivial — the contract is already defined

## Changes

### LLM — no change
`POST /v1/chat/completions` already matches OpenAI. Keep as-is.

### TTS — minor reshape
- **Old**: `POST /v1/tts` with `{"text", "voice", "model", "speed", "lang", "gender"}`
- **New**: `POST /v1/audio/speech` with `{"input", "voice", "model", "speed", "response_format"}`
- `text` → `input` rename
- Response stays raw audio bytes (already matches OpenAI)
- `response_format` can default to `wav` (only format we support)
- Drop `gender` and `lang` from API (CLI-only convenience)

### STT — multipart rewrite
- **Old**: `POST /v1/stt` with raw audio body + Content-Type header
- **New**: `POST /v1/audio/transcriptions` with `multipart/form-data`
  - `file` — audio file upload
  - `model` — model ID (optional, default to moonshine)
  - `language` — language hint (optional)
  - `response_format` — `json` (default) or `text`
- Response: `{"text": "..."}` (matches OpenAI simple format)
- Use Go's `r.FormFile()` for multipart parsing

### Models endpoint
- **Old**: `GET /v1/models` — custom shape
- **New**: `GET /v1/models` — OpenAI format: `{"object": "list", "data": [{"id", "object": "model", "created", "owned_by"}]}`

### Remove
- `POST /v1/tts` route
- `POST /v1/stt` route
- `GET /v1/voices` route (voices discoverable via model metadata or CLI)
- Custom response structs (`sttResponse`, `ttsRequest` current shape)
- X-Model / X-Voice / X-Audio-Duration response headers (not in OpenAI spec)

### Keep unchanged
- CLI commands (`cattery tts`, `cattery stt`, `cattery llm`) — ad-hoc/script use
- Pool system, engine swapping, auth middleware — internal plumbing untouched
- `/v1/status` — cattery-specific health endpoint (no OpenAI equivalent)
- `/debug/pprof/*` — dev tooling

## Key files
- `server/server.go` — routes, TTS handler, models/voices handlers
- `server/llm.go` — chat completions handler (keep as-is)
- `server/stt.go` — STT handler (multipart rewrite)
- `server/llm_test.go` — tests (verify still pass)
- `server/auth_test.go` — tests referencing endpoints
- `cmd/cattery/main.go` — CLI (unchanged)

## Verification
1. `go build ./...` passes
2. `go test ./server/...` passes
3. OpenAI Python SDK test:
   ```python
   from openai import OpenAI
   c = OpenAI(base_url="http://localhost:7100/v1", api_key="unused")

   # LLM
   c.chat.completions.create(model="qwen3.5-4b-v1.0", messages=[{"role":"user","content":"Hi"}])

   # TTS
   c.audio.speech.create(model="kokoro-82m-v1.0", voice="bella", input="Hello world")

   # STT
   c.audio.transcriptions.create(model="moonshine-tiny-v1.0", file=open("test.wav","rb"))
   ```
4. Old endpoints return 404
