# Plan 28 — TTS Streaming Output

## Why

- `#27` is split so the TTS memory fix can land before STT streaming and
  chunk-size infra.
- Kokoro already chunks inference, but `Speak()` still keeps the whole clip in
  one `[]float32`, so long syntheses scale RSS with output length.
- This pass must stand alone: file output, stdout / pipes, and server
  `bytes.Buffer` writes all keep working, and the tree still builds / vets
  clean.

## Current State

- `speak.Engine` exposes `Speak(w io.Writer, text string, opts Options) error`.
- `kokoro.Engine.Speak()` phonemizes text, splits phonemes with
  `chunkPhonemes(..., chunkTokenLimit)`, calls `synthesizeChunks(...)`, then
  passes the full sample slice to `audio.WriteWAV(...)`.
- `synthesizeChunks()` appends every chunk plus the 75 ms gap into one
  `combined []float32` before returning.
- `audio.WriteWAV()` needs the full sample slice up front so it can compute
  RIFF and `data` sizes before writing the WAV header.
- `cmdSpeak()` wraps the output in `countingWriter`, but `countingWriter` only
  implements `Write`, so a wrapped `*os.File` stops looking seekable.
- `openSpeakOutput()` returns `os.Stdout` for `-` or piped output, and an
  `*os.File` for normal file output.
- `server.synthesize()` calls `eng.Speak(&bytes.Buffer, ...)`, so the new WAV
  path must also work on non-seekable in-memory writers even though HTTP TTS
  response streaming stays out of scope.

## Decisions

- Keep the public TTS surface unchanged:
  `speak.Engine` stays `Speak(w io.Writer, text string, opts Options) error`.
- Add one public streaming WAV writer in `audio/wav.go` with constructor +
  `WriteSamples([]float32)` / `Close()` semantics. Keep `WriteWAV()` as a thin
  wrapper over it.
- Support two internal write paths under that API:
  - Seekable: write a placeholder 44-byte header, stream PCM16 immediately,
    then `Seek` back and patch RIFF / `data` sizes on `Close()`.
  - Non-seekable: stream PCM16 into a temp file, track byte count, then on
    `Close()` write the final header to the destination and copy the temp
    payload through.
- Fix `countingWriter` by forwarding `Seek` when the wrapped writer supports
  `io.Seeker`. Keep byte counting in `Write` so CLI duration / RTF reporting
  still works.
- Refactor `kokoro.Engine.Speak()` to open the WAV stream once, reuse the
  existing `synthesizeChunk(...)` helper, write each chunk as soon as it is
  produced, insert the 75 ms gap only between written chunks, and drop each
  chunk slice before moving on.
- Remove or rewrite `synthesizeChunks()` so there is no long-lived
  `combined []float32`.
- Do not add `ChunkSize` or new TTS chunk heuristics here. That belongs to
  plan 30, and Kokoro's current `480`-token ceiling stays as-is in this pass.
- Leave server HTTP buffering alone in this pass. `bytes.Buffer` support only
  preserves the current `eng.Speak(...)` contract.

## Scope

- Streaming WAV output in `audio/wav.go`.
- Seekable header patching and temp-file fallback for non-seekable writers.
- `countingWriter` seek forwarding.
- Kokoro flush-per-chunk synthesis.
- Unit tests for WAV writer paths and `countingWriter` seek forwarding.

## Out of Scope

- HTTP chunked transfer or streaming server responses.
- New TTS chunk heuristics or a `ChunkSize` knob.
- Audio playback while synthesis is still running.
- STT decode / resample streaming.

## Work Plan

1. Add the streaming WAV writer in `audio/wav.go`, including shared header
   encoding, PCM16 sample writes, a seekable patch path, and a temp-file
   fallback. Keep `WriteWAV(...)` as the convenience wrapper.
2. Add `audio/wav_test.go` coverage for seekable header patching,
   non-seekable temp fallback, and `WriteWAV(...)` wrapper parity.
3. Update `cmd/cattery/io.go` so `countingWriter` forwards `Seek(...)` when
   possible, and add a focused unit test for that behavior.
4. Refactor `speak/kokoro/kokoro.go` so `Speak()` streams chunk audio directly
   into the WAV sink instead of building one combined sample slice.
5. Run `go test ./...`, `go build ./...`, and `go vet ./...` with only this
   plan's changes applied.

## Files Likely Touched

- `audio/wav.go`
- new `audio/wav_test.go`
- `cmd/cattery/io.go`
- new `cmd/cattery/io_test.go`
- `speak/kokoro/kokoro.go`
- `koder/issues/27_bounded_memory_streaming.md`
- `koder/STATE.md`

## Acceptance Criteria

- `kokoro.Engine.Speak()` no longer builds one full `combined []float32` for
  multi-chunk synthesis.
- File output uses the seekable header-patch path when the destination can
  seek.
- Stdout, pipes, and `bytes.Buffer` still produce valid WAV output via the
  non-seekable fallback path.
- `countingWriter` no longer hides `io.Seeker` from lower layers.
- `audio.WriteWAV(...)` still works for existing callers.
- `go test ./...`, `go build ./...`, and `go vet ./...` pass with this plan
  alone.
