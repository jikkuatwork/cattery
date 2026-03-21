# 17 — TTS engine interface + package restructure

## Status: open
## Priority: P0
## Depends on: #16 (ORT extraction)
## Blocks: #19 (CLI redesign), #20 (STT package), #21 (server redesign)

## Problem

`engine/` is a flat package that hardcodes Kokoro-82M specifics: vocab map, style dimensions, token padding, voice file format. When a second TTS model arrives (or voice cloning), there's no interface to swap implementations. The package name "engine" is also generic — it says nothing about TTS.

## Goal

1. Define a `speak.Engine` interface for TTS
2. Move Kokoro-specific code to `speak/kokoro/`
3. Keep the interface minimal — don't over-abstract
4. Future TTS engines (different models, voice cloning) implement the same interface

## Design

### New package structure

```
speak/
├── speak.go           # Interface + types
└── kokoro/
    └── kokoro.go      # Current engine/ code, adapted
```

### `speak/speak.go` — interface + shared types

```go
package speak

import "io"

// Engine synthesizes speech from text.
type Engine interface {
    // Speak synthesizes the given text and writes WAV audio to w.
    // voice is a voice identifier (name, ID, or empty for default).
    // lang is a BCP-47 language tag (e.g. "en-us").
    Speak(w io.Writer, text string, opts Options) error

    // Voices returns the available voices for this engine.
    Voices() []Voice

    // Close releases engine resources (ONNX sessions, etc).
    Close() error
}

// Options controls synthesis parameters.
type Options struct {
    Voice  string  // voice name/ID (empty = engine default)
    Gender string  // "male"/"female" filter (empty = any)
    Lang   string  // language (empty = engine default, typically "en-us")
    Speed  float64 // 0.5–2.0 (0 = default 1.0)
}

// Voice describes an available voice.
type Voice struct {
    ID          string
    Name        string
    Gender      string // "female" or "male"
    Accent      string // e.g. "American", "British"
    Description string
}
```

### `speak/kokoro/kokoro.go` — implementation

Moves the current code from `engine/engine.go`:
- `Engine` struct wraps the ONNX session (renamed to avoid stutter: `kokoro.Engine`)
- `New(modelPath string, dataDir string) (*Engine, error)` — loads model
- Implements `speak.Engine` interface
- `Speak()` handles the full pipeline: phonemize → tokenize → load voice → synthesize → write WAV
- Vocab map, `Tokenize()`, `LoadVoice()`, `StyleDim`, `SampleRate` stay here as Kokoro internals
- Phonemization (espeak-ng) is called internally — it's part of the Kokoro pipeline

### Key design decisions

**Speak() does the full pipeline.** The current code requires callers to manually chain: phonemize → tokenize → load voice → synthesize → encode WAV. This is internal plumbing that shouldn't leak. The `Speak()` method takes text in, writes audio out.

**Voice resolution moves inside the engine.** Each engine knows its own voices. The `speak.Engine.Voices()` method returns what's available. Voice resolution (by name, number, gender filter) is engine-internal.

**Registry voices merge with engine voices.** Currently `registry.Voice` and the engine are separate concepts. After this change, `speak.Voice` is the canonical type. The registry provides download metadata; the engine provides runtime voice info. These are bridged at construction time.

**Phonemization is engine-internal.** Kokoro uses espeak-ng. Future engines might use their own phonemizer or none at all. Don't force a shared phonemization step.

### What happens to `engine/`

Delete it. All references update to `speak/kokoro`. The `engine` package was always "Kokoro TTS" — now the name says so.

### Callers to update

| File | Current | After |
|---|---|---|
| `cmd/cattery/main.go` | Manual pipeline: engine.Init, engine.New, phonemize, Tokenize, LoadVoice, Synthesize | `kokoro.New(...)` then `eng.Speak(w, text, opts)` |
| `server/server.go` | Same manual pipeline + engine pool | Engine pool of `speak.Engine` + `eng.Speak(w, text, opts)` |

### File changes

- **Create**: `speak/speak.go` — interface + types
- **Create**: `speak/kokoro/kokoro.go` — Kokoro implementation (from `engine/engine.go`)
- **Delete**: `engine/engine.go`
- **Edit**: `cmd/cattery/main.go` — use `speak/kokoro` + `speak.Options`
- **Edit**: `server/server.go` — pool `speak.Engine` instead of `*engine.Engine`
- **Edit**: `registry/registry.go` — may simplify Voice type (download metadata only)

## Acceptance criteria

- [ ] `speak.Engine` interface exists with `Speak()`, `Voices()`, `Close()`
- [ ] `speak/kokoro` implements `speak.Engine`
- [ ] `engine/` package is deleted
- [ ] `cattery "Hello world"` works via new code path
- [ ] `cattery serve` + POST /v1/tts works via new code path
- [ ] `cattery list` shows voices from engine, not just registry
- [ ] `go build ./...` and `go vet ./...` pass
- [ ] No behavior change from user perspective

## Notes

- The `Speak(w io.Writer, ...)` signature lets callers write to files, HTTP responses, or buffers without intermediate copies
- Voice cloning would be a future engine that implements `speak.Engine` with a different voice model (e.g. a reference audio clip instead of a style vector)
- The engine pool in `server/` should pool `speak.Engine` — the borrow/return/evict pattern stays, just with the interface type
- Performance metadata (RTF, duration) can be returned via a `SpeakResult` struct or response headers — decide during implementation
