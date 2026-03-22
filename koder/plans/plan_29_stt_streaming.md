# Plan 29 — STT Streaming Decode

## Why

- `#27` needs STT peak RSS bounded by the active window, not by full input
  length.
- `moonshine.Engine.Transcribe()` still reads the whole clip, resamples the
  whole clip, and plans chunks against the whole clip.
- This pass must land without depending on the TTS streaming work or the
  chunk-size config work.

## Current State

- `listen.Engine` still exposes `Transcribe(io.Reader, listen.Options)`.
- The CLI and the server already call `eng.Transcribe(...)` directly with one
  reader and one `listen.Options` value.
- `audio.ReadPCM()` does `io.ReadAll(r)`, sniffs WAV vs raw float32, then
  decodes the full clip into one mono `[]float32`.
- `decodeWAV()` supports WAV PCM16 and WAV float32. Non-WAV input falls back
  to raw little-endian float32 PCM at the provided default rate.
- `audio.Resample()` allocates the full output slice before returning.
- `moonshine.Engine.Transcribe()` computes duration from the fully decoded
  clip, resamples the whole slice if needed, then calls
  `transcribeChunkedPCM(...)`.
- `transcribeChunkedPCM()`, `planAudioChunksWithThreshold()`,
  `silenceFloorFor()`, and `squaredPrefix()` all assume whole-clip access.
- `stitchChunkTexts()` already does the boundary-word dedup pass and should
  remain the final text join step.

## Decisions

- Keep the public STT surface unchanged:
  `listen.Engine` stays `Transcribe(io.Reader, listen.Options)`.
- Add a new streaming PCM reader in `audio/` rather than mutating
  `audio.ReadPCM()` into a pseudo-streaming helper.
- The new reader should sniff once, then incrementally decode WAV PCM16,
  WAV float32, or raw float32 input into bounded mono `[]float32` blocks.
- Add a streaming linear resampler in `audio/` with one-sample lookbehind and
  phase state. Keep the same interpolation model as `audio.Resample(...)`.
- Keep the current 30 s target, `+/- 3 s` silence search, 0.5 s overlap,
  silence skip, and text dedup rules in this pass. Configurable chunk size is
  deferred to plan 30.
- Rework the planner API from "plan the whole clip" to "plan the next cut from
  the current window". Add explicit `needMore` behavior for non-EOF windows
  shorter than `target + search`, and allow a short final chunk at EOF.
- Compute the silence floor from the current planning window before each
  boundary search. Do not keep whole-clip prefix arrays in the hot path.
- Rework `moonshine.Engine.Transcribe()` into a sliding-window loop around the
  existing one-chunk inference helper `transcribePCM(samples)`.
- Track duration from the count of decoded source samples, not from
  `len(fullSlice) / sampleRate`.

## Scope

- Streaming PCM decode for WAV PCM16, WAV float32, and raw float32 input.
- Streaming linear resampling into bounded 16 kHz windows.
- Window-based chunk planning with `needMore` / EOF behavior.
- Sliding-window Moonshine transcription with overlap retention.
- Tests for streaming decode, resample continuity, and planner edge cases.

## Out of Scope

- `listen.Engine` API changes.
- New audio formats, codecs, or transport protocols.
- CLI / env / server chunk-size configuration.
- HTTP streaming responses.
- Parallel transcription or decoder-state reuse across chunks.

## Work Plan

1. Add a streaming PCM reader in `audio/` that can incrementally decode WAV
   PCM16, WAV float32, and raw float32 input into bounded mono blocks.
2. Add a streaming linear resampler in `audio/` with chunk-boundary continuity
   tests. Keep `audio.Resample(...)` available for any whole-buffer callers.
3. Refactor `listen/moonshine/chunk.go` so the planner works on the current
   window only, preserves the existing cut rules, and reports `needMore` /
   final-window cases explicitly.
4. Rework `moonshine.Engine.Transcribe()` into a loop that decodes, resamples,
   fills a bounded 16 kHz window, transcribes one chunk, then retains only the
   overlap + unread tail before continuing.
5. Add tests in `audio/stream_test.go`, `audio/resample_stream_test.go`,
   `listen/moonshine/chunk_test.go`, and `listen/moonshine/stream_test.go`
   for decode continuity, resample continuity, silence cuts, hard cuts,
   overlap handling, `needMore`, and EOF edges.
6. Run `go test ./...`, `go build ./...`, and `go vet ./...` with only this
   plan's changes applied.

## Files Likely Touched

- `audio/read.go`
- new `audio/stream.go`
- new `audio/stream_test.go`
- `audio/resample.go`
- new `audio/resample_stream.go`
- new `audio/resample_stream_test.go`
- `listen/moonshine/moonshine.go`
- `listen/moonshine/chunk.go`
- `listen/moonshine/chunk_test.go`
- new `listen/moonshine/stream_test.go`
- `koder/issues/27_bounded_memory_streaming.md`
- `koder/STATE.md`

## Acceptance Criteria

- `moonshine.Engine.Transcribe()` no longer reads the full clip with
  `io.ReadAll` or resamples the full clip into one slice.
- WAV PCM16, WAV float32, and raw float32 input are decoded incrementally into
  mono `[]float32` blocks.
- The streaming resampler produces continuous 16 kHz output across block
  boundaries while keeping only bounded state.
- The planner can return `needMore` for a non-EOF short window and a short
  final chunk at EOF.
- Short audio still results in one transcription call and the correct
  `listen.Result.Duration`.
- Pure-silence windows are skipped, and boundary-word dedup still removes
  repeated overlap words.
- `go test ./...`, `go build ./...`, and `go vet ./...` pass with this plan
  alone.
