# 21 — Server API redesign for multi-modality

## Status: done
## Priority: P2
## Depends on: #17 (TTS interface), #20 (STT package)
## Blocks: nothing

## Problem

`server/server.go` is TTS-only: it pools `*engine.Engine`, handles `/v1/tts`, and knows about voices/phonemization. With STT arriving, the server needs to handle multiple modality endpoints with potentially different pool strategies (TTS engines are heavy ~180MB, STT engines are light ~20MB).

## Goal

Redesign the server to host TTS + STT (and future modalities) behind a unified REST API. Each modality has its own endpoint and resource management, but they share ORT runtime and server infrastructure. All models and voices addressed by numeric index in requests, with slugs always present in JSON responses.

## Core design principle: index in, slug+index out

API requests accept numeric indices (fast, easy). API responses always include both the numeric index and the string slug/ID (machine-friendly, self-documenting).

```
Request:  { "voice": 4 }
Response: { "voice": 4, "voice_id": "af_bella", "voice_name": "Bella" }
```

This matches the CLI convention — numbers are the primary interface, slugs are metadata.

## Design

### Endpoints

```
POST /v1/speak          # TTS: text → audio (renamed from /v1/tts)
POST /v1/listen         # STT: audio → text (new)
GET  /v1/voices         # list TTS voices (indexed)
GET  /v1/models         # list all models (indexed, TTS + STT)
GET  /v1/status         # server health, pool stats, queue depth
```

### Endpoint naming

Rename `/v1/tts` → `/v1/speak` and add `/v1/listen` to match CLI verbs. Consistent mental model: the CLI verb and the API path are the same word.

**Backwards compat**: keep `/v1/tts` as an alias for one release cycle, then drop it.

### `POST /v1/speak`

```json
// Request — voice and model by index
{
    "text": "Hello, world.",
    "voice": 4,
    "model": 1,
    "speed": 1.0,
    "lang": "en-us"
}

// voice accepts: number (index) or string (name/slug)
// model accepts: number (index) or string (slug)
// both optional — defaults to model 1, random voice
```

Response: `audio/wav` body with metadata headers:

```
Content-Type: audio/wav
X-Model: 1
X-Model-ID: kokoro-82m-v1.0
X-Model-Name: Kokoro 82M
X-Voice: 4
X-Voice-ID: af_bella
X-Voice-Name: Bella
X-Audio-Duration: 1.82s
X-Processing-Time: 1.24s
X-RTF: 0.68
```

Headers always include both index and slug — callers can use whichever they need.

### `POST /v1/listen`

```
POST /v1/listen
Content-Type: audio/wav
Body: raw audio bytes

// Optional query params:
?lang=en       # language hint
?model=1       # model by index (or slug)
```

Response:

```json
{
    "text": "The quick brown fox jumps over the lazy dog.",
    "duration": 3.28,
    "processing_time": 0.12,
    "rtf": 0.037,
    "model": 1,
    "model_id": "moonshine-tiny-v1.0",
    "model_name": "Moonshine Tiny"
}
```

Raw audio body (not JSON-wrapped). Content-Type header tells the format.

**Supported input formats (initial):**
- `audio/wav` — decode WAV, extract PCM, resample if needed
- `application/octet-stream` — assume raw PCM float32 at model sample rate
- Future: `audio/ogg`, `audio/webm` for Telegram voice notes

### `GET /v1/models`

```json
[
    {
        "index": 1,
        "id": "kokoro-82m-v1.0",
        "kind": "tts",
        "name": "Kokoro 82M",
        "location": "local",
        "downloaded": true,
        "size_bytes": 92361116,
        "voices": 27
    },
    {
        "index": 2,
        "id": "openai-tts-1",
        "kind": "tts",
        "name": "OpenAI TTS-1",
        "location": "remote",
        "downloaded": null,
        "voices": 6
    },
    {
        "index": 1,
        "id": "moonshine-tiny-v1.0",
        "kind": "stt",
        "name": "Moonshine Tiny",
        "location": "local",
        "downloaded": true,
        "size_bytes": 27000000
    }
]
```

Note: indices are per-kind. TTS model 1 and STT model 1 are different models. The `kind` field disambiguates. Remote models only included when `OPENAI_API_KEY` is set.

### `GET /v1/voices`

```json
[
    { "index": 1, "id": "af_heart", "name": "Heart", "gender": "female", "accent": "American", "description": "Warm, expressive", "model": 1, "model_id": "kokoro-82m-v1.0" },
    { "index": 2, "id": "af_alloy", "name": "Alloy", "gender": "female", "accent": "American", "description": "Balanced, versatile", "model": 1, "model_id": "kokoro-82m-v1.0" },
    ...
]
```

Optional filter: `?model=1` to list voices for a specific model.

### `GET /v1/status`

```json
{
    "status": "ok",
    "speak": {
        "model": 1,
        "model_id": "kokoro-82m-v1.0",
        "model_name": "Kokoro 82M",
        "workers": 1,
        "engines_ready": 1,
        "max_chars": 500,
        "chars_used": 0
    },
    "listen": {
        "model": 1,
        "model_id": "moonshine-tiny-v1.0",
        "model_name": "Moonshine Tiny",
        "workers": 1,
        "engines_ready": 0
    },
    "queued": 0,
    "processed": 42,
    "failed": 0,
    "uptime": "2h30m"
}
```

### Pool architecture

Per-modality pools:

```go
type Server struct {
    cfg        Config
    mux        *http.ServeMux

    speakPool  *Pool[speak.Engine]
    listenPool *Pool[listen.Engine]
}
```

Pool sizing — TTS and STT have different footprints:
- TTS: ~180MB peak, heavy inference → default 1 worker
- STT: ~20MB, fast inference → default 1 worker

```
cattery serve --speak-workers 2 --listen-workers 1
```

### Shared ORT lifecycle

ORT init/shutdown shared (from #16). Server calls `ort.Init()` once. Idle eviction only calls `ort.Shutdown()` when ALL pools are empty.

### Character budget

Keep `MaxChars` for TTS only. STT memory is bounded by audio length (~30s max natively). No budget needed for STT initially.

### Config

```go
type Config struct {
    Port           int
    SpeakWorkers   int           // TTS pool size (default 1)
    ListenWorkers  int           // STT pool size (default 1)
    QueueMax       int           // shared queue depth
    MaxChars       int           // TTS char budget
    IdleTimeout    time.Duration // engine eviction timeout
    KeepAlive      bool          // pre-warm, never evict
    SpeakModel     int           // TTS model index (default 1)
    ListenModel    int           // STT model index (default 1)
}
```

### File changes

- **Edit**: `server/server.go` — restructure for multi-modality, index-based request handling
- **Create**: `server/pool.go` — generic or per-type engine pool
- **Create**: `server/listen.go` — `/v1/listen` handler
- **Edit**: `cmd/cattery/main.go` — update serve flags

## Acceptance criteria

- [x] `POST /v1/speak` accepts `"voice": 4` (numeric index)
- [x] `POST /v1/speak` accepts `"voice": "bella"` (name fallback)
- [x] Response headers include both index and slug for model + voice
- [x] `POST /v1/listen` transcribes WAV audio, response includes index + slug
- [x] `GET /v1/models` returns indexed model list with kind, location, download status
- [x] `GET /v1/voices` returns indexed voice list with model reference
- [x] `GET /v1/status` shows per-modality pool stats with index + slug
- [x] Remote models only appear when `OPENAI_API_KEY` is set
- [x] `POST /v1/tts` still works as alias (backwards compat)
- [x] TTS and STT pools are independently sized
- [x] Idle eviction works correctly with both pools
- [x] `go build ./...` and `go vet ./...` pass
- [x] Server handles concurrent TTS + STT requests

## Notes

- Index-in, slug+index-out: the API never requires slugs but always returns them
- Indices are per-kind, not global. TTS #1 and STT #1 coexist.
- The `/v1/listen` endpoint accepts raw audio body — no base64, no JSON wrapping
- Remote engines (OpenAI) don't need pooling — they're stateless HTTP clients
- pprof endpoints stay
- Future: `POST /v1/speak` with `Accept: application/json` could return base64 audio + metadata for web clients. Not in initial scope.
