# 16 — Extract shared ORT runtime from engine/

## Status: open
## Priority: P0
## Depends on: nothing (foundation)
## Blocks: #17, #20, #21

## Problem

`engine/` currently mixes two concerns:

1. **ORT lifecycle** — `Init()`, `Shutdown()`, `IsInitialized()`, stderr suppression, `malloc_trim`
2. **Kokoro TTS inference** — `Engine` struct, `Synthesize()`, `Tokenize()`, `LoadVoice()`, vocab map

STT (and future modalities) need the ORT lifecycle but not the Kokoro-specific code. The STT spike already imports `engine` solely for `engine.Init()` — a sign that the abstraction boundary is wrong.

## Goal

Extract ORT lifecycle into a new `ort/` package. All modalities import `ort/` for runtime management. The `engine/` package (soon to be restructured by #17) only handles Kokoro inference.

## Current state (`engine/engine.go`)

```go
// ORT lifecycle (should move to ort/)
func Init(libPath string) error    // init ORT env, suppress stderr
func Shutdown()                     // destroy env, malloc_trim, FreeOSMemory
func IsInitialized() bool

// Kokoro-specific (stays in engine/ until #17 moves it)
type Engine struct { session *ort.DynamicAdvancedSession }
func New(modelPath string) (*Engine, error)
func (e *Engine) Close()
func (e *Engine) Synthesize(tokens []int64, style []float32, speed float32) ([]float32, error)
func Tokenize(phonemes string) []int64
func LoadVoice(path string, numTokens int) ([]float32, error)
var Vocab map[rune]int64
```

## Design

### New package: `ort/`

```go
package ort

func Init(libPath string) error     // move from engine.Init
func Shutdown()                      // move from engine.Shutdown
func IsInitialized() bool            // move from engine.IsInitialized
```

- Keeps the cgo `malloc.h` import and `malloc_trim` call
- Keeps the stderr suppression during ORT init
- Keeps the `debug.FreeOSMemory()` call
- Keeps the `unix.Dup`/`Dup2` fd redirect logic
- The `ort` package name shadows `yalue/onnxruntime_go` — callers that need both will alias one. Consider naming it `ortrt` or `runtime` if this causes friction. But `ort` is cleanest for the 90% case where callers only need lifecycle.

### Changes to `engine/`

- Remove `Init()`, `Shutdown()`, `IsInitialized()`
- Remove `import "C"` and the `malloc.h` include
- Remove `golang.org/x/sys/unix` import
- Remove `runtime/debug` import
- Keep all Kokoro-specific code unchanged
- `engine/` no longer manages ORT lifecycle

### Callers to update

| File | Current | After |
|---|---|---|
| `cmd/cattery/main.go` | `engine.Init(...)` / `engine.Shutdown()` | `ort.Init(...)` / `ort.Shutdown()` |
| `cmd/stt-spike/main.go` | `engine.Init(...)` / `engine.Shutdown()` | `ort.Init(...)` / `ort.Shutdown()` |
| `cmd/spike/main.go` | `engine.Init(...)` / `engine.Shutdown()` | `ort.Init(...)` / `ort.Shutdown()` |
| `server/server.go` | `engine.Init(...)` / `engine.Shutdown()` | `ort.Init(...)` / `ort.Shutdown()` |

### File changes

- **Create**: `ort/ort.go`
- **Edit**: `engine/engine.go` — remove lifecycle functions + cgo imports
- **Edit**: `cmd/cattery/main.go` — import `ort/` instead of `engine` for Init/Shutdown
- **Edit**: `cmd/stt-spike/main.go` — same
- **Edit**: `cmd/spike/main.go` — same
- **Edit**: `server/server.go` — same

## Acceptance criteria

- [ ] `ort.Init()` / `ort.Shutdown()` / `ort.IsInitialized()` work identically to current `engine.*` versions
- [ ] `engine/` has zero cgo, zero `unix` imports, zero lifecycle functions
- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes
- [ ] STT spike still works: `go run ./cmd/stt-spike`
- [ ] TTS CLI still works: `cattery "Hello world"`
- [ ] Server still works: `cattery serve` then `curl -X POST ...`

## Notes

- This is a pure refactor — zero behavior change, zero new features
- The `ort/` package name is intentional: it's cattery's ORT abstraction, not a re-export of `yalue/onnxruntime_go`
- If the `ort` name causes import alias friction, `ortrt` is the fallback — but try `ort` first
