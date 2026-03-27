# Plan 37 — Production `llm/` Package for Qwen3.5 Local Inference (#13)

## Status: DRAFT

## Goal

Turn the completed LLM spikes into a production `llm/` modality that matches
the existing cattery shape:

- local-only, zero-config-by-default UX
- registry + downloader managed artefacts
- ONNX Runtime via the existing dlopen lifecycle
- lazy engine pools with explicit eviction on constrained devices
- CLI and server entry points that mirror current TTS/STT conventions

The target is `Qwen3.5-4B` with a production package boundary that can start as
a local CLI engine and then extend cleanly into the server.

## Overview

This plan adds a production `llm/` package for Qwen3.5 local inference. The
spikes already proved the three hard parts:

- the `qwen3.5-4b` artefacts fit the current registry/download model
- a pure Go tokenizer is viable from `tokenizer.json`
- autoregressive ONNX decode with explicit state replacement works in Go

The implementation work now is productization: extract the spike code into a
stable engine package, wire it into the CLI, then add server pooling and
OpenAI-compatible chat completions with explicit memory-aware engine swapping.

## Package structure

```text
llm/
├── engine.go        # llm.Engine interface (Generate, Close)
└── qwen/
    ├── qwen.go      # Engine constructor, model loading, session setup
    ├── decode.go    # Autoregressive decode loop with state management
    └── tokenizer.go # Pure Go BPE tokenizer (extracted from spike)
```

## Current anchors

- [`registry/registry.go`](/home/glasscube/Projects/cattery/registry/registry.go)
  already contains a `KindLLM` registry entry for `qwen3.5-4b-v1.0` with the
  text-only q4 artefacts and metadata discovered in plan 36.
- [`tts/kokoro/kokoro.go`](/home/glasscube/Projects/cattery/tts/kokoro/kokoro.go)
  shows the preferred constructor shape: resolve model files, ensure ORT is
  initialized, create sessions, expose a narrow engine interface, and keep
  `Close()` explicit.
- [`stt/moonshine/moonshine.go`](/home/glasscube/Projects/cattery/stt/moonshine/moonshine.go)
  and [`stt/moonshine/decoder.go`](/home/glasscube/Projects/cattery/stt/moonshine/decoder.go)
  show the current pattern for model metadata parsing, dynamic sessions, cache
  tensors, and strict `Destroy()` discipline.
- [`server/pool.go`](/home/glasscube/Projects/cattery/server/pool.go) already
  provides the lazy-creation / idle-eviction pool needed for LLM lifecycle.
- [`server/server.go`](/home/glasscube/Projects/cattery/server/server.go)
  already contains the integration seam for a third modality plus the shared
  ORT lifetime boundary.
- [`cmd/cattery/main.go`](/home/glasscube/Projects/cattery/cmd/cattery/main.go)
  already has the command dispatch and `--advanced` help split that issue `#13`
  should follow.

## Phase 1 — Core engine

### Goal

Extract the proven spike code into a production `llm/` package with a stable
engine surface and enough configurability for both CLI and server callers.

### Scope

- Create `llm/engine.go` with:

```go
type Engine interface {
    Generate(ctx context.Context, prompt string, opts Options) (*Result, error)
    Close() error
}
```

- Add `llm.Options` and `llm.Result` for the minimum shared surface:
  `System`, `MaxTokens`, `Temperature`, `Stop`, token counts, finish reason.
- Create `llm/qwen/qwen.go` following the `kokoro.New()` / `moonshine.New()`
  pattern:
  - validate model dir
  - parse registry metadata
  - load tokenizer
  - create embed session
  - inspect/create decoder session
  - hold model config needed by decode
- Extract the pure Go tokenizer from `cmd/spike-tokenizer/main.go` into
  `llm/qwen/tokenizer.go`:
  - NFC normalization
  - `regexp2` pretokenizer
  - byte-level byte<->unicode mapping
  - BPE merge ranking from `tokenizer.json`
  - full added-token decode support, including `<think>` / `</think>`
- Extract the decode loop from `cmd/spike-llm/main.go` into `llm/qwen/decode.go`:
  - embed session first, decoder second
  - per-step state replacement
  - explicit tensor destruction on every path
  - greedy decode first; temperature wiring can exist in `Options` even if
    phase 1 initially clamps to greedy when `temperature <= 0`
- Implement minimal ChatML prompt formatting in Go instead of evaluating
  `chat_template.jinja` at runtime:
  - optional system message
  - required user message
  - assistant prefix
  - stop on EOS and `<|im_end|>`
- Add a small constructor helper in `llm/qwen/qwen.go` so callers can build the
  engine from downloaded registry results instead of reaching into spike code.

### Files to change

- **Create**: `llm/engine.go`
- **Create**: `llm/qwen/qwen.go`
- **Create**: `llm/qwen/decode.go`
- **Create**: `llm/qwen/tokenizer.go`
- **Edit**: `go.mod` / `go.sum` — add `github.com/dlclark/regexp2` if not
  already present from the spike
- **Edit**: `cmd/spike-tokenizer/main.go` — switch to the extracted tokenizer or
  leave as a thin smoke tool
- **Edit**: `cmd/spike-llm/main.go` — switch to the extracted engine or leave as
  a thin benchmark harness

### Acceptance criteria

- `llm/qwen.New(modelDir, meta)` constructs a working engine from the
  registered `qwen3.5-4b-v1.0` artefacts.
- A focused Go test or smoke binary can generate text from a user prompt using
  the extracted package without copying spike-only code.
- Tokenizer encode/decode still round-trips the representative prompts already
  recorded in plan 36.
- The runtime path is still pure Go plus the existing ORT cgo boundary.
- `Close()` releases both sessions and all cached state without leaks or double
  destroys.

### Effort

`1.5-2 days`

### Notes / risk

- The 4B graph contract must be rediscovered at implementation time rather than
  assumed from the 0.8B spike.
- If sampling is more work than expected, phase 1 should still ship with greedy
  decode and a stable `Options` surface.

## Phase 2 — CLI integration

### Goal

Expose local LLM inference as a first-class CLI command with the same
zero-config default as TTS/STT and the same `--advanced` escape hatch from
issue `#31`.

### Scope

- Add `cattery llm "prompt"` as a new top-level command.
- Support:
  - `--system`
  - `--stdin`
  - `--max-tokens`
  - `--temperature`
- Keep simple mode narrow:
  - `cattery llm "What is the capital of France?"`
  - `cattery llm --stdin < prompt.txt`
  - `cattery llm --system "You are helpful" "Hi"`
- Put advanced knobs behind `cattery help --advanced`, matching the existing
  help split:
  - `--model`
  - `--max-tokens`
  - `--temperature`
  - any future low-level decode flags
- Reuse the current command patterns:
  - model lookup through `registry`
  - download through `download.Ensure`
  - ORT init/shutdown in the command path
  - memory normalization through `preflight.GuardMemoryError`
- Print plain text to stdout and keep stderr summaries short, matching the
  simplified CLI style from plan 35.

### Files to change

- **Edit**: `cmd/cattery/main.go` — register `llm`, parse flags, update help
- **Create**: `cmd/cattery/llm.go` — command implementation
- **Edit**: `cmd/cattery/main_test.go` — command/help coverage
- **Create or edit**: focused CLI tests for prompt resolution and flag parsing

### Acceptance criteria

- `cattery llm "hello"` downloads the default local LLM model if needed and
  prints generated text to stdout.
- `cattery llm --stdin` reads from stdin without forcing an extra positional
  argument.
- `cattery help` mentions `llm` in the default command list.
- `cattery help --advanced` shows `--model`, `--temperature`, and
  `--max-tokens` for the LLM command without cluttering default help.
- Invalid prompts, unknown model refs, and OOM-like failures return one-line
  CLI errors consistent with current command behavior.

### Effort

`0.5-1 day`

### Notes / risk

- Keep phase 2 CLI-only. Do not block it on server JSON schema design or
  streaming support.

## Phase 3 — Server integration

### Goal

Add a production LLM server path that follows the current pool-based server
architecture and exposes an OpenAI-compatible chat completions endpoint.

### Scope

- Extend `server.Config` and `server.Server` with:
  - default LLM model selection
  - `llmModel *registry.Model`
  - `llmPool *Pool[llm.Engine]`
- Mirror the existing TTS/STT initialization pattern in `server.New()`:
  - resolve default LLM model
  - ensure downloads
  - add lazy pool creation
  - prewarm only when `KeepAlive` is enabled
- Add `POST /v1/chat/completions`:
  - OpenAI-style request/response envelope
  - at minimum support a single prompt assembled from `messages`
  - start with one local model choice, not multi-provider routing
- Add streaming response support via SSE:
  - `stream: true` returns incremental deltas
  - non-streaming returns the final assistant message
- Reuse the existing auth middleware, queueing, and 503 behavior.
- Add explicit pre-borrow eviction for Pi4-class memory:
  - before borrowing from `llmPool`, evict idle TTS/STT engines
  - if `llmPool` is in use, keep current request behavior deterministic rather
    than trying to interleave modalities

### Files to change

- **Edit**: `server/server.go` — config, model resolution, pool setup, routes,
  status payload, shutdown
- **Create**: `server/llm.go` — request/response types and handlers
- **Edit**: `server/pool.go` — only if a small helper is needed for explicit
  pre-borrow eviction without abusing `Shutdown()`
- **Edit**: `cmd/cattery/main.go` — serve help text if new LLM server flags are
  surfaced
- **Create or edit**: `server/*_test.go` — route, auth, and JSON behavior tests

### Acceptance criteria

- `POST /v1/chat/completions` returns an OpenAI-style completion for a basic
  `messages` request.
- `stream: true` returns SSE chunks and a terminating done event.
- LLM requests reuse the same auth, queueing, and error conventions as the
  current server endpoints.
- Idle TTS/STT engines are explicitly evicted before an LLM borrow on the
  server path.
- Server shutdown closes the LLM pool cleanly and still tears down ORT exactly
  once.

### Effort

`1-1.5 days`

### Notes / risk

- Keep the first server shape narrow: OpenAI-compatible enough for basic clients
  without trying to replicate the full upstream API surface.

## Phase 4 — Engine swapping and memory validation

### Goal

Harden the LLM path for Pi4 4GB-class devices by making memory reclamation and
budget checks explicit instead of relying only on idle eviction timing.

### Scope

- Add an explicit eviction helper in the server before LLM borrow:
  - evict idle TTS pool
  - evict idle STT pool
  - then borrow/create the LLM engine
- Ensure LLM teardown paths call `ort.Drain()` / `malloc_trim` after session
  destruction so C-heap pages return to the OS promptly.
- Extend preflight or memory helpers with an LLM-aware budget check:
  - warn or fail fast if available RAM is clearly below the minimum for the
    configured model
  - keep this separate from runtime OOM normalization
- Validate the real sequential swap path:
  `STT -> evict -> LLM -> evict -> TTS`
- Record measured swap overhead and steady-state RSS behavior on at least one
  constrained setup.

### Files to change

- **Edit**: `server/server.go` and/or `server/pool.go` — explicit eviction path
- **Edit**: `ort/ort.go` only if an extra public helper beyond `Drain()` is
  needed
- **Edit**: `preflight/memory.go` — model-aware memory budget validation
- **Create or edit**: focused tests for eviction and memory helper behavior
- **Optional edit**: `memtest/` if the LLM path is folded into the existing RSS
  harness later

### Acceptance criteria

- LLM borrow on the server path performs explicit pre-borrow eviction of idle
  TTS/STT engines.
- After LLM teardown, RSS drops materially once `malloc_trim` runs rather than
  remaining pinned until process exit.
- Low-memory environments get a deterministic warning or error before a likely
  failed 4B load.
- The documented Pi4-style sequential pipeline is operationally credible, not
  just architecturally plausible.

### Effort

`0.5-1 day`

### Notes / risk

- This phase should stay focused on memory correctness and operational proof,
  not on widening API surface area.

## Resolved — 4B graph architecture

**4B is hybrid, same as 0.8B.** Verified March 28, 2026 (see plan 36, "4B
Graph Validation" section). The 4B export has 32 layers: 24 conv/recurrent
layers and 8 full-attention KV layers, with `position_ids[3,-1,-1]` for mRoPE.

`decode.go` must use the three-family state container pattern from the spike:
convolution, recurrent, and sparse KV state tensors. This is not optional —
both the 0.8B and 4B models require it.

## Recommended execution order

1. Phase 1 first. Everything else depends on a stable `llm.Engine`.
2. Phase 2 second. CLI gives the fastest product feedback with the smallest API
   surface.
3. Phase 3 third. Server work should build on the CLI-proven engine.
4. Phase 4 last. Memory hardening should validate the integrated system, not
   block the first end-to-end path.

## Definition of done for issue #13

- A production `llm/` package exists under `llm/qwen`.
- `cattery llm` works end to end with the default local Qwen model.
- The server exposes `POST /v1/chat/completions` with optional SSE streaming.
- Engine swapping is explicit enough that Pi4 4GB remains a credible target
  floor for sequential STT -> LLM -> TTS use.
