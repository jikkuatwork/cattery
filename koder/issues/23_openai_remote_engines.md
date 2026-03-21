# 23 — OpenAI-compatible remote engines

## Status: open
## Priority: P2
## Depends on: #17 (speak.Engine interface), #20 (listen.Engine interface), #18 (registry)
## Blocks: nothing

## Problem

Cattery currently only works with local ONNX models. Users who:
- Don't want to download 150MB of artefacts
- Want higher quality than Kokoro/Moonshine
- Are on resource-constrained machines
- Want to use LLM capabilities (future `cattery think`)

...have no option. Meanwhile, OpenAI's API provides TTS, STT, and LLM behind a single API key.

## Goal

OpenAI models are just models in the registry. `--model openai-tts-1` works the
same as `--model kokoro-82m-v1.0`. No `--remote` flag. No special treatment in the
CLI. The only difference: remote models appear in `cattery list` only when
`OPENAI_API_KEY` is set.

## Design

### Models as models — no `--remote` flag

Remote engines are registered in the same registry as local ones. The `--model`
flag selects them like any other model. The registry knows which models are
local (need downloads) and which are remote (need API key).

```bash
export OPENAI_API_KEY=sk-...

cattery speak --model openai-tts-1 "Hello"       # remote TTS
cattery speak --model kokoro-82m-v1.0 "Hello"     # local TTS (default)
cattery listen --model openai-whisper-1 audio.wav  # remote STT
cattery listen --model moonshine-tiny "Hello"      # local STT (default)
```

Default model is always local. User must explicitly choose a remote model.
This prevents accidental API spend.

### `cattery list` — local vs remote

Remote models only appear when `OPENAI_API_KEY` is set:

```
$ cattery list

TTS Models
  ✓ Kokoro 82M              88.0 MB   local
  ☁ OpenAI TTS-1                       remote
  ☁ OpenAI TTS-1 HD                    remote

STT Models
  ✓ Moonshine Tiny           27.0 MB   local
  ☁ OpenAI Whisper-1                    remote
```

Without the key:

```
$ cattery list

TTS Models
  ✓ Kokoro 82M              88.0 MB   local

STT Models
  ✓ Moonshine Tiny           27.0 MB   local
```

No hint that remote models exist unless the key is set. Clean, no clutter.

### `OPENAI_BASE_URL` for OpenAI-compatible providers

```bash
export OPENAI_API_KEY=sk-...
export OPENAI_BASE_URL=https://openrouter.ai/api/v1  # or Ollama, etc.
```

This covers:
- **OpenAI** (default)
- **OpenRouter** (hundreds of models)
- **Ollama** (local LLM, OpenAI-compatible API)
- **Azure OpenAI** (with base URL override)
- **Any OpenAI-compatible provider**

### Registry integration

Remote models are registered entries with `Location: "remote"` (or similar):

```go
// In registry
{
    ID:       "openai-tts-1",
    Kind:     KindTTS,
    Name:     "OpenAI TTS-1",
    Location: Remote,  // vs Local
    Meta: map[string]string{
        "api_model": "tts-1",
        "voices":    "alloy,echo,fable,onyx,nova,shimmer",
    },
}
```

The registry's `GetByKind()` and listing functions filter by `OPENAI_API_KEY`
presence — remote entries are invisible without it.

### Engine construction

The CLI / server resolves model ID → engine:

```go
model := registry.Get(modelID)
switch {
case model.Location == registry.Remote:
    eng = openai.NewSpeakEngine(apiKey, baseURL, model)
case model.Kind == registry.KindTTS:
    eng = kokoro.New(modelDir, model.Meta)
}
```

This is a simple switch at the top level — no factory pattern, no plugin system.
Each remote model ID maps to a known engine constructor.

### Implementation: `speak/openai/`

```go
package openai

type Engine struct {
    apiKey  string
    baseURL string
    model   string // "tts-1" or "tts-1-hd"
}

func New(apiKey, baseURL, apiModel string) *Engine
func (e *Engine) Speak(w io.Writer, text string, opts speak.Options) error
func (e *Engine) Voices() []speak.Voice
func (e *Engine) Close() error
```

OpenAI TTS API:
```
POST /v1/audio/speech
{ "model": "tts-1", "input": "Hello", "voice": "alloy" }
→ audio/mpeg body
```

### Implementation: `listen/openai/`

```go
package openai

type Engine struct {
    apiKey  string
    baseURL string
    model   string // "whisper-1"
}

func New(apiKey, baseURL, apiModel string) *Engine
func (e *Engine) Transcribe(r io.Reader, opts listen.Options) (*listen.Result, error)
func (e *Engine) SampleRate() int  // not meaningful for remote, return 0
func (e *Engine) Close() error
```

OpenAI Whisper API:
```
POST /v1/audio/transcriptions
Content-Type: multipart/form-data
file=@audio.wav, model=whisper-1
→ { "text": "..." }
```

### Voices

Each remote model has its own voice set. OpenAI TTS has 6: alloy, echo, fable,
onyx, nova, shimmer. These are returned by `Voices()`. Voice selection via
`--voice` works the same as local — by name.

No cross-mapping between local and remote voices. `--voice bella` on an OpenAI
model is an error, not a silent fallback. PoLS: if you asked for bella, you
meant bella.

### No new dependencies

Use `net/http` + `encoding/json` + `mime/multipart` for API calls. No SDK.

### Server integration

The server resolves models the same way as CLI:

```
cattery serve --speak-model openai-tts-1 --listen-model moonshine-tiny
```

Or defaults to local for everything.

### File changes

- **Create**: `speak/openai/openai.go` — OpenAI TTS engine
- **Create**: `listen/openai/openai.go` — OpenAI Whisper engine
- **Edit**: `registry/registry.go` — add Location field, remote model entries, API key gating on list
- **Edit**: `cmd/cattery/main.go` — engine resolution from model ID
- **Edit**: `server/server.go` — support remote engines in pool config

## Acceptance criteria

- [ ] `cattery speak --model openai-tts-1 "Hello"` produces audio (with API key set)
- [ ] `cattery listen --model openai-whisper-1 audio.wav` produces text
- [ ] `cattery list` shows remote models only when `OPENAI_API_KEY` is set
- [ ] `cattery list` hides remote models when no key
- [ ] `--model openai-tts-1` without API key → clear error message
- [ ] `OPENAI_BASE_URL` overrides the API endpoint
- [ ] Works with OpenRouter, Ollama (OpenAI-compatible providers)
- [ ] `--voice` with invalid voice for remote model → error (not silent fallback)
- [ ] No new Go dependencies (pure `net/http`)
- [ ] API key not logged or exposed in status/errors
- [ ] Default model is always local — no accidental API spend

## Notes

- Keep this minimal: just TTS + STT remote engines. LLM (`think`) is a separate issue.
- The OpenAI TTS API returns MP3 by default. Request `response_format=wav` or
  convert. WAV is simpler for consistency with local engines.
- Rate limiting / retry: basic retry with exponential backoff for 429 responses.
- Remote engines don't need pooling — they're stateless HTTP clients. The pool
  pattern only applies to local ONNX engines that hold sessions in memory.
- OpenAI Whisper accepts: flac, mp3, mp4, mpeg, mpga, m4a, ogg, wav, webm.
