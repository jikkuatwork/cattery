# Plan 26 — STT Audio Chunking

## Why

- Moonshine-tiny degrades badly on audio past roughly 30s.
- The fix should stay transparent to callers.
- `#26` is the next quality pass called out in `koder/STATE.md`.

## Current State

- `listen.Engine` exposes `Transcribe(io.Reader, listen.Options)`.
- Both the CLI and the server call `eng.Transcribe(...)` directly.
- There is no package-level `Listen()` hook today.
- `listen/moonshine/moonshine.go` reads the full input with
  `audio.ReadPCM(...)`.
- `audio.ReadPCM(...)` returns mono `[]float32` PCM samples.
- WAV PCM16 and raw float32 PCM are supported on input.
- `Transcribe()` computes duration, resamples to the model rate, then runs one
  encoder pass and one decoder loop over the whole clip.
- There is no chunking, silence skip, or overlap dedup yet.
- `listen/moonshine/` has no tests today.

## Decisions

- Keep the feature inside `moonshine.Engine.Transcribe()`.
- Do not change `listen.Engine`, CLI flags, server handlers, or API shape.
- Add a new helper file, `listen/moonshine/chunk.go`, for chunk planning and
  transcript stitching.
- Chunk after `audio.ReadPCM(...)` and after resampling to `e.sampleRate`,
  before `ortgo.NewTensor(...)`. All chunk sample counts and boundaries are
  in `e.sampleRate` (16000) space, not the original input sample rate.
- Use fixed 30s target chunks.
- For each target boundary, search for silence inside a `27s..33s` window
  relative to the chunk start.
- If silence exists, cut at the silence point closest to the 30s target.
- If no silence exists in that window, hard-cut at 30s.
- Add 0.5s overlap by starting the next chunk 0.5s before the prior cut.
- Dedup overlap at the text layer using suffix-prefix word matching: find the
  longest suffix of chunk[i] text that equals a prefix of chunk[i+1] text,
  capped at 8 words. This is not a general LCS — only boundary overlap matters.
  At 0.5s overlap and normal speech rate (~2.5 words/sec), expect 1-2 words.
- Silence detection uses sliding-window RMS on mono 16 kHz PCM.
- The current code stores PCM as normalized `[]float32`, not `[]int16`, so the
  RMS math should be expressed in dBFS, equivalent to int16 full-scale.
- First cut for silence: RMS below `-40 dBFS` for at least `200 ms`.
- If the clip is globally very quiet, relax the silence floor to `-50 dBFS`.
- Audio at or under 30s keeps the current single-pass path.
- Pure-silence chunks are skipped.
- If all chunks are silent, return an empty transcript with the real duration
  instead of running inference on silence.
- Keep all chunking knobs as unexported package constants for now.
- Do not make them configurable in `listen.Options` or registry metadata yet.

## Scope

- Chunk planning on resampled Moonshine PCM.
- Sliding-window RMS silence detection.
- Hard-cut fallback when silence is not found.
- 0.5s overlap and word-level dedup between adjacent transcripts.
- Refactor `Transcribe()` so the inference path can run per chunk.
- Unit tests for chunk selection and transcript stitching.

## Out Of Scope

- Streaming STT.
- Word timestamps or confidence scores.
- VAD or a separate speech/noise model.
- Changing accepted audio formats.
- Making chunking heuristics user-configurable.
- Reworking `audio.ReadPCM(...)` to keep raw int16 buffers around.
- Reducing peak memory by streaming decode; this pass still reads the full
  clip into memory first.

## Work Plan

1. Extract the current single-chunk inference path from `Transcribe()` into a
   helper that accepts already-decoded PCM samples.
2. Add chunk constants and sample-count helpers in
   `listen/moonshine/chunk.go`.
3. Implement sliding-window RMS over resampled mono samples and detect silence
   runs that stay under the threshold for at least `200 ms`.
4. Build a chunk planner with a 30s target, a `±3s` silence search, a
   nearest-silence cut rule, a hard-cut-at-30s fallback, and a 0.5s overlap
   on the next chunk start.
5. Skip pure-silence chunks before inference.
6. Stitch chunk transcripts using suffix-prefix word matching (cap 8 words)
   to remove duplicated boundary words before concatenation. Each chunk gets
   a fresh KV cache — no cross-chunk decoder state.
7. Keep `listen.Result.Duration` tied to the original clip duration and time
   the full `Transcribe()` call once, not per chunk.
8. Add tests for short audio passthrough, preferred silence cuts near a 30s
   boundary, hard-cut fallback with no silence, very quiet audio threshold
   relaxation, pure silence skip / empty final transcript, and overlap dedup
   at chunk joins.
9. Run `go test ./...`, `go build ./...`, and `go vet ./...`.

## Files Likely Touched

- `listen/moonshine/moonshine.go`
- new `listen/moonshine/chunk.go`
- new `listen/moonshine/chunk_test.go`
- `koder/issues/26_stt_audio_chunking.md`
- `koder/STATE.md`

## Acceptance Criteria

- `eng.Transcribe(...)` handles 60s+ audio by chunking internally.
- Callers still pass one reader and get one `listen.Result`.
- Audio at or under 30s keeps current behavior.
- Chunk boundaries prefer nearby silence inside the `27s..33s` search window.
- If no silence is found, the planner hard-cuts at 30s.
- The 0.5s overlap plus dedup removes repeated boundary words.
- Pure silence does not produce hallucinated transcript text.
- Very quiet audio still finds silence cuts via the relaxed threshold.
- Long round-trip audio no longer falls into repetition loops.
- `go test ./...`, `go build ./...`, and `go vet ./...` pass.
