# Plan 27 — Bounded-Memory Streaming

## Why

- `koder/STATE.md` calls `#27` the next deployment blocker for Pi4 / low-RAM
  hosts.
- `#24` and `#26` bounded inference chunk size, but not pipeline memory.
- The current TTS and STT paths still scale peak RSS with full output/input
  length, which defeats the chunking work on long clips.

## Current State

- `kokoro.Engine.Speak()` normalizes text, phonemizes it, chunks phonemes at a
  fixed `chunkTokenLimit = 480`, synthesizes all chunks, then writes one WAV
  at the end (`speak/kokoro/kokoro.go:109-166`,
  `speak/kokoro/chunk.go:8-35`).
- `synthesizeChunks()` is chunked only at inference time. It appends every
  chunk's `[]float32` into one `combined` slice, also appending a 75 ms gap,
  and returns the full clip in memory (`speak/kokoro/kokoro.go:224-253`).
- `audio.WriteWAV()` requires the full `[]float32` up front so it can compute
  `dataSize` before writing the RIFF header, then writes samples one by one
  (`audio/wav.go:10-75`). There is no streaming writer or header patch path.
- CLI speak wraps the destination in `countingWriter` before calling
  `eng.Speak(...)` (`cmd/cattery/main.go:257-283`, `cmd/cattery/io.go:23-31`).
  That wrapper only exposes `Write`, so it currently hides `io.Seeker` from
  lower layers even when the real destination is an `*os.File`.
- CLI speak writes to `stdout` when piped or when `-o -` is used, and to an
  `*os.File` otherwise (`cmd/cattery/io.go:74-86`). Any WAV streaming design
  must therefore handle both seekable files and non-seekable stdout/pipes.
- Server speak buffers the full WAV in `bytes.Buffer` inside `s.synthesize()`
  before writing the HTTP response (`server/server.go:426-459`,
  `server/server.go:633-669`). Even if Kokoro stops accumulating chunk audio,
  this endpoint still retains the full response body in memory.
- STT still loads the full request body eagerly. `audio.ReadPCM()` does
  `io.ReadAll(r)` first, then decodes WAV or raw float32 into one `[]float32`
  (`audio/read.go:15-38`).
- `moonshine.Engine.Transcribe()` keeps that full slice, computes duration from
  it, resamples the whole clip if needed with `audio.Resample(...)`, then
  passes the entire 16 kHz sample slice into `transcribeChunkedPCM(...)`
  (`listen/moonshine/moonshine.go:102-145`).
- The Moonshine chunk planner is still whole-clip oriented.
  `planAudioChunksWithThreshold()` plans every chunk for the full sample slice,
  and `squaredPrefix()` allocates a `[]float64` prefix array over the whole
  clip for silence math (`listen/moonshine/chunk.go:30-83`,
  `listen/moonshine/chunk.go:219-324`). This fixes quality, not peak memory.
- Current memory probing already lives in `preflight`. `Check()` and
  `CheckAvailableMemory()` call `availableMemoryMB()`, which on Linux reads
  `MemAvailable` from `/proc/meminfo`, and on other OSes returns `-1`
  (`preflight/check.go:35-125`). There is no chunk-size resolver yet.
- There is no chunk-size config surface today. `speak.Options` and
  `listen.Options` have no such field (`speak/speak.go:17-23`,
  `listen/listen.go:17-20`); CLI parsing in `cmdSpeak`, `cmdListen`, and
  `cmdServe` does not accept `--chunk-size`
  (`cmd/cattery/main.go:82-149`, `cmd/cattery/main.go:162-283`,
  `cmd/cattery/listen.go:17-110`); no env lookup exists; and `server.Config`
  has no chunk-size field (`server/server.go:40-51`).

## Decisions

- Keep the TTS memory fix inside `kokoro.Engine.Speak()` and `audio/wav.go`.
  `speak.Engine` stays `Speak(w io.Writer, text string, opts Options) error`.
- Add a streaming WAV writer in `audio/wav.go`, with one public API and two
  implementations under it:
  - Seekable path: write a placeholder 44-byte header, stream PCM16 chunks
    immediately, then `Seek` back and patch RIFF/data sizes on `Close()`.
  - Non-seekable path: stream PCM16 to a temp file, track byte count, then on
    `Close()` write the final header to the destination and copy the temp
    payload through. This is the stdout/pipe/`bytes.Buffer` fallback.
- Keep `audio.WriteWAV()` as a thin convenience wrapper over the new streaming
  writer so existing callers keep the same signature.
- Refactor Kokoro to flush per synthesized chunk. After phonemization and voice
  resolution, open the WAV stream once, synthesize one chunk at a time with the
  existing `synthesizeChunk()`, write the 75 ms gap immediately before chunks
  after the first, write the chunk samples immediately, then drop the chunk
  slice before moving on. The long-lived TTS buffers become one chunk, one
  tiny gap slice, and the WAV writer's scratch space.
- Fix the CLI seekability trap as part of the same pass. The current
  `countingWriter` must either forward `Seek` when its wrapped writer can seek,
  or byte counting must move into the WAV stream. Prefer forwarding `Seek` so
  `cmdSpeak` keeps its duration / RTF reporting without hiding `*os.File`.
- Keep Kokoro's `480`-token hard cap from `#24`. Do not make `#27` invent a
  duration-based TTS chunk heuristic. The observed TTS memory growth comes from
  `combined []float32`, not from an unbounded inference chunk size, and
  `480` tokens is a correctness ceiling tied to the model context window
  (`speak/kokoro/chunk.go:8-12`, `koder/issues/24_tts_sentence_chunking.md`).
- Add a streaming PCM reader for STT instead of mutating `audio.ReadPCM()` into
  a half-streaming helper. The new reader should sniff the first bytes once,
  parse WAV headers incrementally when present, decode PCM16/float32 frames to
  mono `[]float32` in bounded blocks, and keep raw float32 fallback behavior
  for non-WAV streams.
- Add a streaming linear resampler instead of whole-clip `audio.Resample(...)`.
  The current resampler always allocates the entire output slice
  (`audio/resample.go:5-38`), which would reintroduce linear memory. A
  streaming resampler with one-sample lookbehind and phase state keeps the same
  interpolation model while staying bounded.
- Rework Moonshine around a sliding resampled window. `Transcribe()` should:
  1. open the streaming PCM reader,
  2. decode and resample into a bounded 16 kHz window,
  3. wait until the window has at least `target + search` audio or EOF,
  4. ask the planner for the next cut,
  5. transcribe only that chunk,
  6. retain only overlap + unread tail,
  7. repeat until EOF.
  Duration should become an accumulated counter of decoded source samples, not
  `len(fullSlice) / sampleRate`.
- Change the chunk planner API from "plan every range in the full clip" to
  "plan the next cut for the current window". Keep the existing `30s` target,
  `±3s` silence search, `0.5s` overlap, pure-silence skip, and text dedup
  rules from `#26`, but add `needMore` / `final` behavior:
  - if not at EOF and the current window is shorter than `target + search`,
    return "need more audio" rather than forcing a cut;
  - at EOF, allow the final chunk to run to the end even when shorter.
- Use per-window silence threshold selection in streaming mode. The current
  `silenceFloorFor(samples)` assumes full-clip access
  (`listen/moonshine/chunk.go:185-190`). In the new design, compute the
  threshold from the current planning window before each boundary search. The
  planner only needs local loudness to choose the next cut.
- Keep RAM detection and auto chunk-size selection in `preflight`, not a new
  `membudget` package. `preflight` already owns `MemAvailable` probing, and the
  server already imports it for memory gating.
- Compute the auto chunk size with a conservative lookup table, not a fake
  MB-per-second formula:
  - `<= 1 GB available` => `15s`
  - `<= 2 GB available` => `20s`
  - `<= 4 GB available` => `30s`
  - `<= 8 GB available` => `45s`
  - `> 8 GB available` => `60s`
  - `unknown availability` => keep the current `30s` default
  Clamp all resolved values to `10s..60s`.
- Parse `--chunk-size` and `CATTERY_CHUNK_SIZE` with one helper. Accept Go
  durations like `15s` / `1m`, and also bare integers as seconds to match the
  issue examples. Validation should fail fast outside `10s..60s`. Precedence:
  CLI flag, then env var, then auto.
- Thread the resolved `time.Duration` through `listen.Options`,
  `server.Config`, and the CLI `serve` config path. Also add it to
  `speak.Options` for interface symmetry and future engines, but document that
  Kokoro ignores the knob in `#27` because its chunk ceiling is already fixed
  by model context, not RAM.
- Keep HTTP TTS response buffering out of scope for this issue. `server` may
  keep using `bytes.Buffer` so it can send one-shot responses with
  `Content-Length`. `#27` still helps the server on STT input memory and on
  Kokoro's internal chunk accumulation, but fully bounded HTTP TTS needs a
  separate streaming-response pass.

## Scope

- Streaming WAV output for Kokoro with per-chunk flush and final header patch.
- Temp-file fallback for non-seekable TTS writers like stdout, pipes, and
  `bytes.Buffer`.
- Sliding-window PCM decode + resample for Moonshine, with no full-clip
  `io.ReadAll`, no full-clip `[]float32`, and no whole-clip prefix array.
- Chunk planner changes needed to work on bounded windows while preserving the
  current silence-search / overlap / dedup behavior from `#26`.
- Shared RAM probe + auto chunk-size resolution in `preflight`.
- `--chunk-size` / `CATTERY_CHUNK_SIZE` parsing, validation, and plumbing
  through CLI, server config, and engine options.
- Unit coverage for the new WAV writer paths, chunk-size resolver, and
  Moonshine window planner / reader behavior.

## Out of Scope

- HTTP chunked transfer or true streaming server responses.
- Streaming audio playback while synthesis is still running.
- Replacing Kokoro's token-based chunking with a duration-based chunker.
- New codecs, container formats, or audio transport protocols.
- GPU memory tuning.
- Parallel chunk synthesis / transcription.
- Reworking server queueing, char budgets, or engine pool policy.

## Work Plan

1. Add a streaming WAV writer to `audio/wav.go` with `WriteSamples(...)` /
   `Close()` semantics, a seekable header-patch path, and a temp-file fallback
   for non-seekable writers. Keep `WriteWAV(...)` as a wrapper.
2. Update the CLI output wrapper in `cmd/cattery/io.go` so byte counting no
   longer hides `io.Seeker` from the WAV layer.
3. Refactor `speak/kokoro/kokoro.go` so `Speak()` writes chunks directly to the
   streaming WAV sink instead of building one `combined []float32`. Remove or
   rewrite `synthesizeChunks()` accordingly.
4. Add a streaming PCM decoder in `audio/` that can incrementally decode WAV
   PCM16, WAV float32, and raw float32 to mono `[]float32` blocks.
5. Add a streaming linear resampler in `audio/` so Moonshine can produce
   bounded 16 kHz windows without calling whole-clip `audio.Resample(...)`.
6. Refactor `listen/moonshine/chunk.go` from whole-clip planning to "next cut
   from current window" planning, with `needMore` and EOF-aware behavior while
   preserving the existing silence search, overlap, skip-silence, and text
   dedup rules from `#26`.
7. Rework `listen/moonshine/moonshine.go` to accumulate duration while reading,
   fill a bounded resampled window, transcribe one chunk at a time, retain only
   overlap + tail, and never hold the full clip in memory.
8. Move shared memory helpers out of `preflight/check.go` into a reusable
   memory helper and add auto chunk-size resolution plus override parsing.
9. Add `ChunkSize time.Duration` plumbing to `listen.Options`, `speak.Options`,
   `server.Config`, CLI `speak` / `listen` / `serve` parsing, and
   `CATTERY_CHUNK_SIZE` lookup. Make Moonshine consume it; document Kokoro's
   no-op behavior in this pass.
10. Add tests for seekable WAV patching, non-seekable temp fallback,
    counting-writer seek forwarding, chunk-size parsing / auto selection,
    streaming PCM decode / resample continuity, and Moonshine window planning
    at `needMore`, silence, hard-cut, overlap, and EOF edges.
11. Update help text and any affected comments so the new chunk-size behavior
    and stdout fallback are discoverable.
12. Run `go test ./...`, `go build ./...`, and `go vet ./...`.

## Files Likely Touched

- Edit: `audio/wav.go`
- Edit: `audio/read.go`
- Edit: `audio/resample.go`
- Edit: `speak/kokoro/kokoro.go`
- Edit: `listen/moonshine/moonshine.go`
- Edit: `listen/moonshine/chunk.go`
- Edit: `preflight/check.go`
- Edit: `speak/speak.go`
- Edit: `listen/listen.go`
- Edit: `cmd/cattery/main.go`
- Edit: `cmd/cattery/listen.go`
- Edit: `cmd/cattery/io.go`
- Edit: `server/server.go`
- Edit: `server/listen.go`
- Edit: `koder/issues/27_bounded_memory_streaming.md`
- Edit: `koder/STATE.md`
- Create: `audio/stream.go`
- Create: `audio/stream_test.go`
- Create: `audio/wav_test.go`
- Create: `audio/resample_stream.go`
- Create: `audio/resample_stream_test.go`
- Create: `preflight/memory.go`
- Create: `preflight/memory_test.go`
- Edit: `listen/moonshine/chunk_test.go`
- Create: `listen/moonshine/stream_test.go`
- Delete: none expected

## Acceptance Criteria

- CLI/file-output TTS no longer builds one full `[]float32` for multi-chunk
  synthesis; peak RSS is bounded by one Kokoro chunk plus small write buffers.
- A 3-minute TTS synthesis to a file peaks at `<= 350 MB RSS` on the current
  benchmark VM, down from the issue's `880 MB`.
- `cattery speak` still works to `stdout` / pipes; the non-seekable path uses
  temp-disk fallback instead of in-memory accumulation.
- Moonshine no longer calls `io.ReadAll`, whole-clip `audio.ReadPCM(...)`, or
  whole-clip `audio.Resample(...)` on the main transcription path.
- A 3-minute STT transcription peaks at `<= 250 MB RSS` on the current
  benchmark VM, down from the issue's `424 MB`.
- Pi4 4 GB: both TTS and STT complete a 3-minute clip without OOM or swap.
- 1 GB VPS: STT completes a 3-minute clip; TTS completes at least a 1-minute
  file-output clip.
- `--chunk-size 15s` and `CATTERY_CHUNK_SIZE=15` are accepted, validated, and
  reduce Moonshine peak RSS further.
- Auto mode picks `15s`, `20s`, `30s`, `45s`, and `60s` on representative
  `1 GB`, `2 GB`, `4 GB`, `8 GB`, and `16 GB+` systems, and falls back to the
  current `30s` when memory availability is unknown.
- Short inputs keep the current fast path semantics: one Kokoro chunk for short
  speak text, one Moonshine chunk for short audio, no API regression.
- `go test ./...`, `go build ./...`, and `go vet ./...` pass.
