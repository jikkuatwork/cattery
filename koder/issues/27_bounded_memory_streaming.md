---
id: 27
title: "Bounded-memory streaming for TTS and STT pipelines"
status: open
priority: P1
depends_on: "#24 (TTS chunking), #26 (STT chunking)"
blocks: "Pi4 / low-RAM VPS deployment"
created: 2026-03-22
---

# 27 — Bounded-memory streaming for TTS and STT pipelines

## Status: open
## Priority: P1
## Depends on: #24 (TTS chunking), #26 (STT chunking)
## Blocks: Pi4 / low-RAM VPS deployment

## Problem

Chunking (#24, #26) solved the inference problem — each chunk runs in bounded
time and memory. But the pipeline still accumulates all chunk outputs in memory
before writing:

- **TTS**: all chunk PCM is concatenated into one `[]float32`, then written as
  a single WAV. A 3-min synthesis peaks at 880MB despite each chunk needing
  only ~310MB.
- **STT**: the full audio is read into memory via `audio.ReadPCM()` before
  chunking. A 3-min WAV at 16kHz float32 is ~19MB of PCM — manageable, but
  grows linearly and adds to the ~190MB model footprint.

On a Pi4 (4GB) or $6 VPS (1GB), accumulating outputs makes long-form
synthesis/transcription OOM or swap-thrash, even though per-chunk inference
fits comfortably.

### Measured (6-core ARM64 VM, 16GB)

| Pipeline | 1 chunk RSS | 3-min full RSS | Gap |
|---|---|---|---|
| TTS | 310 MB | 880 MB | 570 MB wasted |
| STT | 190 MB | 424 MB | 234 MB wasted |

## Goal

Peak RSS should be proportional to **one chunk**, not the full clip. A 3-minute
synthesis should use the same memory as a 10-second one.

## Progress

- 2026-03-22: plan 28 landed. TTS now streams chunk audio through
  `audio.WAVWriter`; seekable outputs patch the WAV header in place, while
  stdout, pipes, and `bytes.Buffer` use a temp-file fallback.
- 2026-03-22: `cmd/cattery`'s `countingWriter` now forwards `Seek(...)` when
  possible and tracks logical output size correctly, so CLI duration / RTF
  reporting stays accurate on the seekable path.
- 2026-03-22: plan 29 landed. Moonshine now streams PCM decode and linear
  resample into a bounded 16 kHz window, the planner works on the current
  window with explicit `needMore` / EOF behavior, and transcription retains
  only overlap + unread tail while tracking duration from decoded source
  samples.
- 2026-03-22: plan 30 landed. `preflight` now resolves chunk size from
  `--chunk-size`, `CATTERY_CHUNK_SIZE`, or RAM-based auto mode; Moonshine
  consumes the resolved duration; Kokoro accepts it as a no-op for interface
  symmetry; low-memory speak no longer hard-rejects; and TTS/STT entrypoints
  normalize OOM-style failures into clean single-line errors.
- Remaining work is empirical RSS / Pi4 / 1 GB validation before closing the
  issue.

## Design considerations

### TTS streaming output

The WAV format requires the total data length in the header (bytes 4-8 and
40-44). Two approaches:

1. **Two-pass**: write chunks to a temp file as raw PCM, then prepend the WAV
   header with the known length. Simple, correct, needs temp disk space.
2. **Seekable write**: write a placeholder WAV header, stream chunks directly
   to the output file, then seek back and patch the length fields. No temp
   file, but requires a seekable writer (not stdout pipes).
3. **Chunked flush with final patch**: write header with placeholder, flush
   each chunk's PCM immediately, patch header at close. Same as (2) but
   makes the flush-per-chunk explicit.

Recommended: option 3 for file output, option 1 as fallback for pipes/stdout.

### STT streaming input

`audio.ReadPCM()` currently reads everything into `[]float32`. For bounded
memory:

1. Read and resample in fixed-size windows (e.g. 35s worth at a time —
   chunk target + search window)
2. Feed each window to the chunk planner
3. Discard consumed audio after each chunk is transcribed

This is more invasive — the chunk planner currently expects the full sample
slice. May need a sliding-window reader that keeps only the current chunk +
lookahead in memory.

### Dynamic chunk sizing

Rather than a single hardcoded chunk size, select based on available memory:

- **Default (auto)**: detect available RAM at startup. Pick the largest chunk
  size that keeps peak RSS under 50% of available memory. Floor at 10s
  (model minimum), ceiling at 60s (quality ceiling for Moonshine).
- **Explicit override**: `--chunk-size 30s` or `CATTERY_CHUNK_SIZE=30` for
  users who know their constraints.
- **Pi4 profile**: if auto-detect sees ≤2GB available, default to 15-20s
  chunks. This keeps TTS peak under ~350MB and STT under ~200MB, leaving
  room for OS + other processes.
- **Big systems**: auto-detect picks larger chunks (45-60s), fewer cuts,
  slightly better quality at boundary stitching.

The auto-detect should be conservative — 50% of available RAM leaves headroom
for OS page cache, espeak-ng subprocess, and the WAV buffer.

### Configurable via

- CLI flag: `--chunk-size DURATION` (applies to both speak and listen)
- Env var: `CATTERY_CHUNK_SIZE`
- Server config: chunk size in server.Config
- Default: auto-detect

### What "planning for Pi4" means for bigger systems

The Pi4 constraint (4GB, 4-core A72) sets the floor, not the ceiling:

- Bounded memory is strictly better on all platforms — big systems just get
  larger auto-selected chunks with fewer boundary artifacts
- No capability is removed — a 64GB server still processes the full clip,
  just with larger chunks and fewer disk flushes
- The only tradeoff is slightly more disk I/O on the TTS write path, which
  is negligible compared to inference time

## Scope

- Stream TTS chunk output to disk (flush per chunk, patch WAV header at end)
- Stream STT input in windows (don't load full clip into memory)
- Auto-detect available RAM and select chunk size
- Optional `--chunk-size` override for CLI and server
- Keep per-chunk RSS as the memory ceiling

## Out of scope

- Streaming audio playback (play while synthesizing)
- Network streaming (HTTP chunked transfer for server API)
- Changing the WAV format or adding codec support
- GPU memory management
- Per-chunk parallelism (synthesize chunk N+1 while writing chunk N)

## Acceptance criteria

- [ ] 3-min TTS synthesis peaks at ≤350MB RSS on this VM (vs 880MB today)
- [ ] 3-min STT transcription peaks at ≤250MB RSS (vs 424MB today)
- [ ] Pi4 4GB: both TTS and STT complete a 3-min clip without OOM or swap
- [ ] 1GB VPS: STT completes a 3-min clip; TTS completes at least 1-min
- [x] 512MB: warns on stderr but proceeds with 10s chunks; no panic/trace on OOM
- [x] `--chunk-size 15s` works and reduces peak RSS further
- [x] Auto-detect picks reasonable defaults on 512MB, 1GB, 4GB, 16GB systems
- [x] All memory failures produce clean single-line errors, never stack traces
- [ ] No regression on short audio (< 30s) — same path, same memory
- [x] `go build ./...` and `go vet ./...` pass

## File changes (likely)

- **Edit**: `speak/kokoro/chunk.go` — flush chunks to writer instead of accumulating
- **Edit**: `audio/wav.go` — seekable WAV writer with header patching
- **Edit**: `listen/moonshine/moonshine.go` — windowed PCM reading
- **Edit**: `listen/moonshine/chunk.go` — sliding-window chunk planner
- **Create**: `membudget/` or `preflight/memory.go` — RAM detection + chunk size selection
- **Edit**: `cmd/cattery/main.go` — `--chunk-size` flag
- **Edit**: `server/server.go` — chunk size in config

## Notes

- The WAV header patch is a well-known pattern — ffmpeg, sox, and most audio
  tools do this. It's not fragile.
- Pi4 benchmarks should be done on actual hardware or QEMU with memory
  capping (`-m 4G`) to validate the estimates.
- Dynamic chunk sizing adds complexity but prevents a "works on my machine"
  class of bugs. The auto-detect should be dead simple: read /proc/meminfo
  or sysctl, pick a chunk duration, done.
