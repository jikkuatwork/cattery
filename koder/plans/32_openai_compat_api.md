# Plan 32 — OpenAI-Compatible Server API (#32)

## Status: REVIEWED

## Goal

Replace cattery's remaining custom server endpoints with OpenAI-compatible
HTTP shapes so the official OpenAI SDKs can use the local server directly:

- keep `POST /v1/chat/completions` as-is
- reshape TTS to `POST /v1/audio/speech`
- reshape STT to `POST /v1/audio/transcriptions`
- reshape `GET /v1/models` to the OpenAI list envelope
- remove old routes and obsolete custom API structs

This is a server-surface change only. Engine pooling, model resolution,
download behavior, auth, `/v1/status`, and CLI commands stay intact.

## Current anchors

- [`server/server.go`](/home/glasscube/Projects/cattery/server/server.go)
  currently registers `POST /v1/tts`, `POST /v1/stt`, `GET /v1/models`, and
  `GET /v1/voices`, and it holds the current `ttsRequest`, `modelResponse`,
  and `voiceResponse` API structs.
- [`server/stt.go`](/home/glasscube/Projects/cattery/server/stt.go)
  currently accepts raw request bodies plus `Content-Type` validation and
  returns the custom `sttResponse`.
- [`server/llm.go`](/home/glasscube/Projects/cattery/server/llm.go)
  already matches the intended OpenAI-compatible server style and should not
  be behaviorally changed.
- [`server/auth_test.go`](/home/glasscube/Projects/cattery/server/auth_test.go)
  references protected paths and will need route updates.
- [`server/llm_test.go`](/home/glasscube/Projects/cattery/server/llm_test.go)
  already validates the retained chat completions endpoint and should remain
  green after the route cleanup.

## API target

### 1. TTS: `POST /v1/audio/speech`

Request JSON:

```json
{
  "model": "kokoro-82m-v1.0",
  "input": "Hello world",
  "voice": "bella",
  "speed": 1.0,
  "response_format": "wav"
}
```

Rules:

- `input` is required and replaces `text`
- `voice` remains optional; string or numeric-index decoding should continue
  through the existing `requestRef` helper
- `model` remains optional; omitted means configured default TTS model
- `speed` remains optional; default `1.0`, allowed range stays `0.5..2.0`
- `response_format` is optional; accept empty or `"wav"` only
- `gender` and `lang` are removed from the public API

Response:

- status `200 OK`
- raw WAV bytes
- `Content-Type: audio/wav`
- keep `Content-Length`
- drop `Content-Disposition` for closer OpenAI compatibility
- remove custom headers such as `X-Model`, `X-Voice`, `X-Audio-Duration`,
  `X-Processing-Time`, and `X-RTF`

### 2. STT: `POST /v1/audio/transcriptions`

Multipart form-data fields:

- `file` required: uploaded audio file
- `model` optional: model ID or configured default
- `language` optional: language hint passed into `stt.Options.Lang`
- `response_format` optional: `json` default, `text` alternate

Behavior:

- use `r.FormFile("file")` rather than reading the raw body directly
- stop validating the outer request `Content-Type` as audio; instead require
  `multipart/form-data`
- pass the uploaded file stream into `eng.Transcribe(...)`
- continue using the existing queue, pool, and memory-guard path

Responses:

Default JSON:

```json
{
  "text": "transcribed text"
}
```

Text mode:

- status `200 OK`
- `Content-Type: text/plain; charset=utf-8`
- body is the plain transcript text

Only OpenAI-simple output is in scope here. The current duration/RTF/model
fields are removed from the API response.

### 3. Models: `GET /v1/models`

Response shape:

```json
{
  "object": "list",
  "data": [
    {
      "id": "kokoro-82m-v1.0",
      "object": "model",
      "created": 0,
      "owned_by": "cattery"
    }
  ]
}
```

Rules:

- include TTS, STT, and LLM models in one `data` array, same source as today
- `id` is the registry model ID
- `object` is always `"model"`
- `created` can be a fixed placeholder `0` for now unless registry metadata is
  later extended
- `owned_by` should be a stable string such as `"cattery"`
- drop custom fields: `index`, `kind`, `name`, `location`, `downloaded`,
  `size_bytes`, `voices`

### 4. Removed endpoints

These routes should be deleted, not aliased:

- `POST /v1/tts`
- `POST /v1/stt`
- `GET /v1/voices`

The issue explicitly wants old endpoints to return `404`, so compatibility
means replacement, not a dual API surface.

## Proposed code changes

### `server/server.go`

Update the package comment header to list:

- `POST /v1/audio/speech`
- `POST /v1/audio/transcriptions`
- `POST /v1/chat/completions`
- `GET /v1/models`
- `GET /v1/status`

Replace the route registration block in `New(cfg Config)` with:

```go
s.mux.Handle("POST /v1/audio/speech", protected(http.HandlerFunc(s.handleAudioSpeech)))
s.mux.Handle("POST /v1/audio/transcriptions", protected(http.HandlerFunc(s.handleAudioTranscriptions)))
s.mux.Handle("POST /v1/chat/completions", protected(http.HandlerFunc(s.handleChatCompletions)))
s.mux.Handle("GET /v1/models", protected(http.HandlerFunc(s.handleModels)))
s.mux.HandleFunc("GET /v1/status", s.handleStatus)
```

Delete the old route registrations for `/v1/tts`, `/v1/stt`, and `/v1/voices`.

Remove obsolete API structs from this file:

- `type ttsRequest struct`
- `type modelResponse struct`
- `type voiceResponse struct`

Add replacement structs in this file or a nearby server API file:

```go
type audioSpeechRequest struct {
    Input          string     `json:"input"`
    Voice          requestRef `json:"voice,omitempty"`
    Model          requestRef `json:"model,omitempty"`
    Speed          float64    `json:"speed,omitempty"`
    ResponseFormat string     `json:"response_format,omitempty"`
}

type openAIModelListResponse struct {
    Object string              `json:"object"`
    Data   []openAIModelObject `json:"data"`
}

type openAIModelObject struct {
    ID      string `json:"id"`
    Object  string `json:"object"`
    Created int64  `json:"created"`
    OwnedBy string `json:"owned_by"`
}
```

Change the TTS helper signature to consume the renamed request type:

```go
func (s *Server) synthesize(
    ctx context.Context,
    req audioSpeechRequest,
    model *registry.Model,
    voice tts.Voice,
) (*bytes.Buffer, synthMeta, error)
```

Keep `synthMeta` as a value return, not a pointer. The plan should treat this
as deliberate: metadata is always produced alongside a successful synthesis
result, so a value avoids nil-handling without changing the main buffer/error
contract.

`handleModels` should be rewritten to return the OpenAI list envelope but can
still source models from `registry.GetByKind(...)` exactly as today.

Delete `handleVoices`.

### `server/stt.go`

Remove the custom response type:

```go
type sttResponse struct { ... }
```

Add request/response helpers:

```go
type audioTranscriptionJSONResponse struct {
    Text string `json:"text"`
}

type openAIErrorEnvelope struct {
    Error openAIError `json:"error"`
}

type openAIError struct {
    Message string `json:"message"`
    Type    string `json:"type"`
    Code    any    `json:"code"`
}

func (s *Server) handleAudioTranscriptions(w http.ResponseWriter, r *http.Request)
func parseAudioTranscriptionForm(r *http.Request) (audioTranscriptionForm, error)
func writeAudioTranscriptionResponse(w http.ResponseWriter, text string, format string)
func writeOpenAIError(w http.ResponseWriter, status int, message string)
```

Suggested parsed form shape:

```go
type audioTranscriptionForm struct {
    File           multipart.File
    Filename       string
    Model          string
    Language       string
    ResponseFormat string
}
```

Handler flow:

1. `r.ParseMultipartForm(...)` or direct `r.FormFile("file")`
2. require `file`
3. resolve model from multipart form field `model` only, replacing the current query-param lookup
4. borrow STT engine
5. call `eng.Transcribe(file, stt.Options{Lang: ..., ChunkSize: s.cfg.ChunkSize})`
6. emit JSON or text response based on `response_format`

Validation rules:

- missing `file` => `400`
- bad model => `400`
- unsupported `response_format` => `400`
- non-multipart request => `400`
- queue full / memory pressure behavior stays identical to current handler

`validateSTTContentType` becomes obsolete and should be deleted unless reused
for multipart parsing in a smaller form.

All request-validation failures for TTS/STT/models should use the shared
OpenAI-style error envelope:

```json
{
  "error": {
    "message": "unsupported response_format",
    "type": "invalid_request_error",
    "code": null
  }
}
```

Use the helper for new or touched handlers so official SDKs see
`error.message` in the expected place.

### `server/llm.go`

No functional endpoint changes. Keep:

```go
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request)
```

Only touch this file if a shared OpenAI error/helper type is factored and it
meaningfully reduces duplication. Otherwise leave it alone.

## Tests

### Existing test updates

`server/auth_test.go`

- update the rate-limit test to hit `POST /v1/audio/speech` instead of
  `POST /v1/tts`
- keep `GET /v1/models` assertions as-is because that route remains protected
- do not add `/v1/status` auth requirements; it remains public

`server/llm_test.go`

- keep current `POST /v1/chat/completions` coverage unchanged
- ensure fake server construction still compiles after handler renames and
  deleted structs in `server/server.go`

### New server HTTP tests

Add dedicated endpoint coverage because the repo currently has no tests for the
new TTS/STT/models shapes.

Suggested files:

- `server/tts_test.go`
- `server/stt_test.go`
- `server/models_test.go`

Minimum cases:

`server/tts_test.go`

- `POST /v1/audio/speech` accepts `input`
- rejects missing `input`
- rejects unsupported `response_format`
- returns `audio/wav`
- does not emit removed `X-*` headers
- does not emit `Content-Disposition`

`server/stt_test.go`

- `POST /v1/audio/transcriptions` accepts multipart upload with `file`
- default response is `{"text": "..."}`
- `response_format=text` returns plain text
- rejects non-multipart requests
- rejects missing `file`

`server/models_test.go`

- `GET /v1/models` returns `{"object":"list","data":[...]}`
- every entry uses `object:"model"`
- old custom fields are absent

Shared error coverage

- touched endpoints return nested OpenAI-style `{"error":{"message":...}}`
  bodies on `400`

`server/auth_test.go`

- optionally add one explicit assertion that a protected new endpoint returns
  `401` without a key

### End-state verification

- `go test ./server/...`
- `go build ./...`
- manual smoke:
  - `curl -X POST /v1/audio/speech ...`
  - `curl -F file=@sample.wav /v1/audio/transcriptions`
  - OpenAI SDK samples from the issue
- confirm `POST /v1/tts`, `POST /v1/stt`, and `GET /v1/voices` now return `404`

## Phase order

### Phase 1 — TTS route + handler migration

- add `audioSpeechRequest`
- rename `handleTTS` to `handleAudioSpeech`
- register `POST /v1/audio/speech`
- remove `POST /v1/tts`
- switch JSON decoding from `text` to `input`
- validate `response_format`
- remove `gender` / `lang` from the public API
- pass `""` for `tts.Options.Gender`
- hardcode `tts.Options.Lang` to `"en-us"` to preserve current default behavior
- update `resolveTTSVoice(...)` call sites to pass empty gender (`""`) unless
  the helper is simplified to remove that parameter
- stop writing compatibility-breaking `X-*` headers
- drop `Content-Disposition`
- keep `DisallowUnknownFields` for now and note it as a forward-compatibility
  tradeoff rather than broadening the accepted request shape in this change

This phase is independently functional: the new route and handler behavior land
together rather than exposing a compile-only surface.

### Phase 2 — STT multipart migration

- replace raw-body STT handling with multipart parsing
- resolve `model` from the multipart form field instead of `r.URL.Query()`
- add `response_format` support for `json` and `text`
- remove `sttResponse`
- remove `validateSTTContentType`
- use the shared OpenAI-style error helper for validation failures

### Phase 3 — Models cleanup

- rewrite `handleModels` to emit the OpenAI list envelope
- remove `modelResponse` and `voiceResponse`
- delete `handleVoices`
- use the shared OpenAI-style error helper if new validation paths are added

### Phase 4 — Test migration and regression lock

- update `auth_test.go`
- keep `llm_test.go` green
- add new endpoint tests for TTS, STT, and models
- verify old routes 404

This ordering keeps the work incremental while ensuring each public route change
lands with working request/response behavior rather than a compile-only shim.

## Out of scope

- changing CLI flags or CLI request shapes
- adding `/v1/audio/translations`
- exposing voice discovery through a new OpenAI-style metadata endpoint
- changing pool behavior, worker counts, or auth semantics
- broad LLM API changes beyond preserving current chat completions behavior
