# Plan 30 — Chunk-Size Infrastructure

## Why

- `#27` still needs one shared chunk-size policy after the TTS and STT
  streaming work lands.
- The repo already probes available RAM, but there is no reusable chunk-size
  resolver, no CLI / env override, and no option plumbing for engines or the
  server.
- This pass depends on plans 28 and 29, because it only makes sense after the
  streaming paths exist to consume the resolved duration.

## Current State

- `preflight/check.go` owns `availableMemoryMB()`, which reads Linux
  `MemAvailable` and returns `-1` on other platforms.
- `preflight.Check()` and `preflight.CheckAvailableMemory()` both call that
  helper directly, and `MinMemoryMB` is still a hard 300 MB threshold.
- `server.handleSpeak()` currently calls `preflight.CheckAvailableMemory(0)`
  and rejects low-memory requests instead of adapting chunk size.
- `speak.Options` has `Voice`, `Gender`, `Lang`, and `Speed`, but no
  `ChunkSize`.
- `listen.Options` has only `Lang`.
- `server.Config` has port / worker / queue / model fields, but no chunk-size
  field.
- `cmdSpeak()`, `cmdListen()`, and `cmdServe()` do not accept `--chunk-size`.
- `printUsage()` does not mention any chunk-size flag or
  `CATTERY_CHUNK_SIZE`.
- `server.synthesize()` and `server.handleListen()` construct `speak.Options`
  and `listen.Options` without any chunk-size value.
- `main()` already prints returned CLI errors as one line, but there is no
  shared helper that resolves low-memory policy or normalizes runtime
  out-of-memory failures.

## Decisions

- Keep RAM probing and auto chunk-size resolution in `preflight`, not a new
  package. Add a reusable `preflight/memory.go` and have `check.go` call into
  it.
- Resolve the automatic chunk size with this conservative lookup table:
  - `<= 512 MB` => `10s`
  - `<= 1 GB` => `15s`
  - `<= 2 GB` => `20s`
  - `<= 4 GB` => `30s`
  - `<= 8 GB` => `45s`
  - `> 8 GB` => `60s`
  - unknown RAM => `30s`
- Clamp auto-selected values to `10s..60s`. Explicit overrides outside that
  range fail fast instead of silently clamping.
- Parse `--chunk-size` and `CATTERY_CHUNK_SIZE` with one helper. Accept Go
  durations like `15s` / `1m`, and bare integers as seconds.
- Precedence is always: CLI flag, then env var, then auto.
- Add `ChunkSize time.Duration` to `speak.Options`, `listen.Options`, and
  `server.Config`.
- Thread the resolved chunk size through `cmdSpeak`, `cmdListen`, `cmdServe`,
  `server.New`, `server.synthesize`, and `server.handleListen`.
- Moonshine should consume the resolved chunk size once plan 29 is landed.
  Kokoro should accept the field for interface symmetry, but stay a documented
  no-op in this pass.
- Replace the current hard low-memory rejection with a warning + minimum chunk
  size policy. At `<= 512 MB`, print one stderr warning at process startup,
  then continue with `10s` chunks.
- Keep the HTTP and CLI user-facing surfaces simple: no per-request API field,
  only process-level flag / env / config behavior in this pass.
- Normalize low-memory runtime failures into clean single-line errors. Add a
  narrow recovery wrapper only where a library call can otherwise panic or dump
  an unhelpful trace.

## Scope

- Reusable RAM detection helpers in `preflight`.
- Auto chunk-size lookup and override parsing.
- `ChunkSize` plumbing through CLI, server config, and engine options.
- 512 MB warning behavior.
- Clean single-line memory error handling.
- Help text and tests for the new policy.

## Out of Scope

- Changing Kokoro to use duration-based chunking.
- New server request parameters for chunk size.
- TTS / STT streaming internals from plans 28 and 29.
- Reworking preflight's non-memory checks beyond moving shared helpers.
- Network streaming or playback features.

## Work Plan

1. Move the reusable RAM probe code out of `preflight/check.go` into a new
   `preflight/memory.go`, and add helpers for auto chunk-size resolution.
2. Add parsing helpers for explicit chunk-size overrides so the CLI, env, and
   server all share the same validation and precedence logic.
3. Extend `speak.Options`, `listen.Options`, and `server.Config` with
   `ChunkSize time.Duration`.
4. Update `cmdSpeak()`, `cmdListen()`, and `cmdServe()` to accept
   `--chunk-size`, honor `CATTERY_CHUNK_SIZE`, emit the low-memory warning
   once, and pass the resolved duration downstream.
5. Update `printUsage()` so speak, listen, and serve document the new flag,
   the env var, and the auto default behavior.
6. Update `server.New`, `server.synthesize()`, and `server.handleListen()` so
   direct server users and CLI users resolve chunk size the same way.
7. Remove the hard `CheckAvailableMemory(0)` speak-request rejection and
   replace it with the new warning + minimum chunk-size policy.
8. Wire Moonshine to read `opts.ChunkSize`. Keep Kokoro ignoring the field,
   but document that behavior where `speak.Options` is used.
9. Add focused tests for memory helpers, auto selection, override parsing,
   precedence, and any server / CLI helper behavior introduced here.
10. Audit TTS / STT entrypoints so low-memory runtime failures surface as
    clean single-line errors, not panics or traces.
11. Run `go test ./...`, `go build ./...`, and `go vet ./...` after plans 28
    and 29 are already merged.

## Files Likely Touched

- `preflight/check.go`
- new `preflight/memory.go`
- new `preflight/memory_test.go`
- `speak/speak.go`
- `listen/listen.go`
- `cmd/cattery/main.go`
- `cmd/cattery/listen.go`
- `cmd/cattery/main_test.go`
- `server/server.go`
- `server/listen.go`
- `listen/moonshine/moonshine.go`
- `speak/kokoro/kokoro.go`
- `koder/issues/27_bounded_memory_streaming.md`
- `koder/STATE.md`

## Acceptance Criteria

- `--chunk-size` and `CATTERY_CHUNK_SIZE` resolve through one shared helper
  with precedence `flag > env > auto`.
- Explicit overrides accept Go durations and bare seconds, and reject values
  outside `10s..60s`.
- Auto mode maps available RAM to `10s`, `15s`, `20s`, `30s`, `45s`, or `60s`
  exactly, with `30s` when RAM is unknown.
- `speak.Options`, `listen.Options`, and `server.Config` all carry the
  resolved `ChunkSize`.
- Moonshine uses the resolved chunk size. Kokoro accepts the field but remains
  a documented no-op.
- Systems at `<= 512 MB` print one stderr warning and continue with `10s`
  chunks.
- Server speak no longer rejects requests solely because free RAM is below the
  old `MinMemoryMB` gate.
- Low-memory failures surface as clean single-line errors, never stack traces
  or raw panics.
- Help text documents the flag, env var, and auto behavior.
- `go test ./...`, `go build ./...`, and `go vet ./...` pass once plans 28
  and 29 are already landed.
