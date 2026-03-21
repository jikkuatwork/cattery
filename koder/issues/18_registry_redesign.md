# 18 — Registry redesign for multi-modal artefacts

## Status: open
## Priority: P1
## Depends on: #16 (ORT extraction)
## Blocks: #20 (STT package)

## Problem

The registry (`registry/registry.go`) only knows about TTS models and voices:

```go
type Model struct {
    ID, Name, Description string
    SizeBytes             int64
    Filename, SHA256      string
    SampleRate, StyleDim, MaxTokens int
    DefaultVoice          string
    Voices                []Voice
}
```

This is entirely Kokoro/TTS-specific. STT models have different metadata (encoder + decoder files, tokenizer, no voices). Future modalities (LLM, vision) will have yet other shapes. The download system (`download/`) is also TTS-coupled — it expects model + voice + ORT.

## Goal

Redesign registry to handle multiple artefact types while keeping the download system auth-free and simple. Every artefact cattery needs — TTS models, STT models, voices, tokenizers, ORT runtime — should be discoverable and downloadable through one registry.

## Design

### Artefact types

```go
type Kind string

const (
    KindTTS       Kind = "tts"       // text-to-speech model
    KindSTT       Kind = "stt"       // speech-to-text model
    KindVoice     Kind = "voice"     // voice style file (TTS-specific)
    KindTokenizer Kind = "tokenizer" // tokenizer file (STT-specific)
    KindRuntime   Kind = "runtime"   // ORT shared library
)
```

### Core types

```go
// Artefact is a downloadable file with a known hash.
type Artefact struct {
    Filename  string // e.g. "model_quantized.onnx"
    SizeBytes int64
    SHA256    string // empty = no verification (some HF files)
    URL       string // download URL (empty = derived from model source)
}

// Location indicates where a model runs.
type Location string

const (
    Local  Location = "local"  // runs on device via ONNX
    Remote Location = "remote" // calls external API (e.g. OpenAI)
)

// Model describes a model available for download or remote access.
type Model struct {
    Index       int               // stable numeric index, per-kind (1-based)
    ID          string            // slug, e.g. "kokoro-82m-v1.0"
    Kind        Kind              // "tts" or "stt"
    Location    Location          // "local" or "remote"
    Name        string            // e.g. "Kokoro 82M"
    Description string
    Lang        []string          // supported languages, e.g. ["en"]
    Files       []Artefact        // model files (local only)
    Voices      []Voice           // TTS only — empty for STT
    Meta        map[string]string // engine-specific metadata
}
```

### Metadata via `Meta` map

Rather than having `SampleRate`, `StyleDim`, `MaxTokens` as top-level fields (which are Kokoro-specific), use a `Meta` map. Each engine knows which keys it needs:

```go
// Kokoro TTS reads:
Meta: map[string]string{
    "sample_rate": "24000",
    "style_dim":   "256",
    "max_tokens":  "510",
    "default_voice": "af_heart",
}

// Moonshine STT reads:
Meta: map[string]string{
    "sample_rate":  "16000",
    "encoder_file": "encoder_model_quantized.onnx",
    "decoder_file": "decoder_model_merged_quantized.onnx",
    "num_layers":   "6",
    "num_heads":    "8",
    "head_dim":     "36",
    "max_steps":    "448",
    "bos_token":    "1",
    "eos_token":    "2",
}
```

This keeps the registry generic while letting engines extract typed config.

### Voice stays as a struct

Voices are TTS-specific but important enough to keep as a first-class type (not buried in Meta). An STT model simply has `Voices: nil`.

```go
type Voice struct {
    ID          string
    Name        string
    Gender      string
    Accent      string
    Description string
    File        Artefact // the downloadable voice file
}
```

### Lookup API

```go
func Get(id string) *Model                    // by exact slug
func GetByIndex(kind Kind, index int) *Model  // by numeric index within kind
func GetByKind(kind Kind) []*Model            // all TTS models, all STT models, etc.
func Default(kind Kind) *Model                // default model for a kind (index 1)
func Resolve(kind Kind, ref string) *Model    // ref is index (e.g. "3") or slug — try index first, then slug
```

### Index assignment

Indices are **stable per-kind, 1-based**, assigned in registry order:

- TTS: Kokoro = 1, OpenAI TTS-1 = 2, OpenAI TTS-1 HD = 3
- STT: Moonshine Tiny = 1, OpenAI Whisper-1 = 2

Once assigned, an index never changes. New models get the next index.
Removed models leave a gap (like issue numbers).

Voice indices are also stable per-model, 1-based, matching the order in the
`Voices` slice. Voice 1 for Kokoro is `af_heart`, voice 4 is `af_bella`, etc.

### Visibility gating

Remote models (`Location: Remote`) are only returned by listing/lookup functions
when `OPENAI_API_KEY` is set in the environment. This is checked at call time,
not at registry init — so the list updates if the env var changes.

```go
func GetByKind(kind Kind) []*Model {
    // filters out Remote models if no OPENAI_API_KEY
}
```

### Download integration

The `download/` package currently has `Ensure(dataDir, model, voice)` which is TTS-specific. Generalize to:

```go
// Ensure downloads all files for a model (and optionally specific voices).
func Ensure(dataDir string, model *registry.Model, voices ...*registry.Voice) (*Result, error)

// Result contains resolved local paths.
type Result struct {
    ORTLib    string            // path to ORT shared library
    Files     map[string]string // artefact filename → local path
}
```

Callers look up specific files by name:
```go
res.Files["model_quantized.onnx"]        // TTS model
res.Files["encoder_model_quantized.onnx"] // STT encoder
```

### Artefact sources

Currently artefacts come from two places:
1. `jikkuatwork/cattery-artefacts` (Git LFS) — TTS model + voices
2. Microsoft GitHub Releases — ORT runtime

STT adds a third:
3. HuggingFace `onnx-community/moonshine-tiny-ONNX` — STT model + tokenizer

The `Artefact.URL` field makes this explicit. If empty, the download system uses the default source (cattery-artefacts). If set, it downloads from that URL directly. No auth for any source.

**Future consideration**: issue #15 (mirror.json) could override these URLs. But that's separate from this issue.

### Registry data

The initial registry should contain:

1. **kokoro-82m-v1.0** (TTS) — existing, restructured into new types
2. **moonshine-tiny-v1.0** (STT) — new, from spike data
3. **ort-1.24.1** (runtime) — existing, possibly extracted as its own entry

### File changes

- **Edit**: `registry/registry.go` — new types, restructured data
- **Edit**: `download/download.go` — generalized Ensure + Result
- **Edit**: `cmd/cattery/main.go` — adapt to new registry/download API
- **Edit**: `server/server.go` — adapt to new registry/download API
- **Edit**: `paths/paths.go` — may need new path helpers for STT model dir

## Acceptance criteria

- [ ] `registry.Get("kokoro-82m-v1.0")` returns a `Kind: "tts"` model
- [ ] `registry.Get("moonshine-tiny-v1.0")` returns a `Kind: "stt"` model
- [ ] `registry.GetByKind("tts")` and `registry.GetByKind("stt")` work
- [ ] `download.Ensure()` works for both TTS and STT models
- [ ] All artefact URLs are auth-free
- [ ] `cattery download` fetches everything (or by kind)
- [ ] `cattery list` shows TTS and STT models
- [ ] `cattery status` shows all model types
- [ ] `go build ./...` and `go vet ./...` pass
- [ ] Existing TTS functionality unbroken

## Notes

- Keep the Voice struct — it's a real concept, not premature abstraction
- The Meta map is a pragmatic escape hatch. Don't over-type it.
- The Moonshine tokenizer file (`tokenizer.json`) is an artefact like any other — list it in `Model.Files`
- SHA256 verification: the spike downloads from HF without checksums. Record them once we stabilize the artefact versions.
