# 20 — STT package: Moonshine-tiny

## Status: open
## Priority: P1
## Depends on: #16 (ORT extraction), #18 (registry redesign)
## Blocks: #19 (CLI listen command), #21 (server STT endpoint)

## Problem

The STT spike (`cmd/stt-spike/main.go`) proves Moonshine-tiny works from Go via ORT. But it's 500 lines of inline code with no reusable package, no interface, and hardcoded everything. It needs to become a proper `listen/` package that mirrors the `speak/` structure.

## Goal

Build `listen/` package with a `listen.Engine` interface and Moonshine-tiny as the first implementation. Designed for multiple engines and languages from day one.

## Spike learnings (from `cmd/stt-spike/main.go`)

- Moonshine-tiny: encoder-decoder architecture, 2 ONNX files
- Encoder: single pass, raw 16kHz PCM input → hidden states
- Decoder: autoregressive with KV cache (6 layers × 4 tensors)
- Tokenizer: sentencepiece 32k vocab from `tokenizer.json`
- Cache pattern: keep encoder cache from step 0, update decoder cache each step
- `use_cache_branch` bool tensor switches between initial and cached paths
- Greedy decoding (argmax) sufficient for this model size
- Constants: `numLayers=6, numHeads=8, headDim=36, bosToken=1, eosToken=2, maxSteps=448`

## Design

### Package structure

```
listen/
├── listen.go            # Interface + types
└── moonshine/
    ├── moonshine.go     # Engine implementation
    ├── decoder.go       # Autoregressive decoder with KV cache
    └── tokenizer.go     # Sentencepiece tokenizer loader
```

### `listen/listen.go` — interface

```go
package listen

import "io"

// Engine transcribes audio to text.
type Engine interface {
    // Transcribe reads audio from r and returns the transcription.
    // The audio format is WAV or raw PCM (engine-specific).
    Transcribe(r io.Reader, opts Options) (*Result, error)

    // SampleRate returns the expected input sample rate.
    SampleRate() int

    // Close releases engine resources.
    Close() error
}

// Options controls transcription parameters.
type Options struct {
    Lang string // language hint (empty = engine default, typically "en")
}

// Result holds transcription output.
type Result struct {
    Text     string  // transcribed text
    Duration float64 // audio duration in seconds
    Elapsed  float64 // processing time in seconds
    RTF      float64 // real-time factor (elapsed / duration)
}
```

### `listen/moonshine/moonshine.go` — implementation

```go
package moonshine

type Engine struct {
    encoder   *ort.DynamicAdvancedSession
    decoder   *ort.DynamicAdvancedSession
    tokenizer map[int]string
    // ... config from registry Meta
}

func New(modelDir string) (*Engine, error)
func (e *Engine) Transcribe(r io.Reader, opts listen.Options) (*listen.Result, error)
func (e *Engine) SampleRate() int  // returns 16000
func (e *Engine) Close() error
```

### Key implementation details

**Audio input handling.** `Transcribe()` accepts `io.Reader`. The implementation should:
1. Read the full audio into memory (Moonshine needs the complete waveform for the encoder)
2. Detect format: WAV header → decode; raw → assume PCM float32 at engine sample rate
3. Resample if needed (e.g., 24kHz TTS output → 16kHz for Moonshine)

**Resampling.** The spike uses linear interpolation. This is adequate for the Moonshine input quality bar. Keep it simple — no external resampling library. Put the resampler in `audio/resample.go` so it's shared.

**KV cache management.** The spike's `kvCache` struct and the `transcribe()` loop are the core complexity. Extract into `decoder.go`:

```go
type decoder struct {
    session   *ort.DynamicAdvancedSession
    numLayers int
    numHeads  int
    headDim   int
    maxSteps  int
    bosToken  int64
    eosToken  int64
}

func (d *decoder) decode(encoderHidden ort.Value, encSeqLen int64) ([]int64, error)
```

**Tokenizer.** Extract the `loadTokenizer()` function into `tokenizer.go`. Reads HuggingFace `tokenizer.json` format, builds `map[int]string` for decoding.

**Model constants from registry.** The spike hardcodes `numLayers=6, numHeads=8, headDim=36`. These should come from `registry.Model.Meta` so different Moonshine variants (tiny vs base) work without code changes.

### Construction from registry

```go
// New creates a Moonshine engine from downloaded model files.
func New(modelDir string, meta map[string]string) (*Engine, error) {
    // meta provides: encoder_file, decoder_file, num_layers, num_heads, head_dim, etc.
    // All files expected in modelDir.
}
```

The CLI/server looks up the registry model, calls `download.Ensure()`, then constructs:

```go
model := registry.Get("moonshine-tiny-v1.0")
res, _ := download.Ensure(dataDir, model)
eng, _ := moonshine.New(modelDir, model.Meta)
```

### What to extract from spike into shared packages

| Code | From | To |
|---|---|---|
| `resample()` | spike inline | `audio/resample.go` |
| WAV reading (for listen input) | doesn't exist yet | `audio/read.go` |
| ORT lifecycle | already moved by #16 | `ort/` |

### Audio reading

The `audio/` package currently only writes WAV. Add reading:

```go
// ReadPCM reads audio from r and returns float32 PCM samples + sample rate.
// Supports WAV format. Raw PCM assumed at defaultRate if no header found.
func ReadPCM(r io.Reader, defaultRate int) ([]float32, int, error)
```

This keeps audio format handling out of the STT engine.

### File changes

- **Create**: `listen/listen.go` — interface + types
- **Create**: `listen/moonshine/moonshine.go` — engine
- **Create**: `listen/moonshine/decoder.go` — autoregressive decoder
- **Create**: `listen/moonshine/tokenizer.go` — tokenizer loader
- **Create**: `audio/resample.go` — `Resample(samples, fromRate, toRate)`
- **Create**: `audio/read.go` — `ReadPCM(r, defaultRate)`
- **Keep**: `cmd/stt-spike/main.go` — leave as reference, don't modify

## Acceptance criteria

- [ ] `listen.Engine` interface exists with `Transcribe()`, `SampleRate()`, `Close()`
- [ ] `moonshine.New()` loads encoder + decoder + tokenizer from downloaded files
- [ ] `eng.Transcribe(wavReader, opts)` returns correct text for English audio
- [ ] Handles WAV input (16-bit PCM, various sample rates via resampling)
- [ ] Handles raw PCM float32 input at 16kHz
- [ ] KV cache is properly managed (no memory leaks across calls)
- [ ] Multiple sequential transcriptions work (engine is reusable)
- [ ] `eng.Close()` destroys ONNX sessions cleanly
- [ ] `audio.Resample()` works for 24kHz→16kHz and other common rates
- [ ] `audio.ReadPCM()` decodes WAV headers
- [ ] `go build ./...` and `go vet ./...` pass
- [ ] Round-trip test: TTS output → resample → STT → matches input text

## Performance targets (from spike, aarch64 VM)

| Metric | Target |
|---|---|
| Encoder | <50ms for 5s audio |
| Decoder | <200ms for typical sentence |
| RTF | <0.05x (20x+ faster than real-time) |
| Memory | <30MB marginal (above ORT base) |

## Notes

- The spike proves the happy path. The package needs to handle edge cases: empty audio, very long audio (>30s), silence, non-English audio with English model
- Moonshine-tiny is English-only. The `Lang` option is a hint for future multi-lingual models. For Moonshine-tiny, ignore it or warn if non-English.
- Don't implement beam search or temperature sampling — greedy argmax is fine for this model size
- The `listen.Engine` interface is intentionally minimal. Don't add streaming, word timestamps, or confidence scores until there's a real need.
- ORT sessions for encoder and decoder are separate — they can't share weights. Both must be loaded.
