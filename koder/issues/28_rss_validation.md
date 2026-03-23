---
id: 28
title: "RSS validation: investigate TTS accumulation and STT baseline overage"
status: open
priority: P1
depends_on: "#27 (bounded-memory streaming), memtest suite"
created: 2026-03-23
---

# 28 — RSS validation: investigate TTS accumulation and STT baseline overage

## Status: open
## Priority: P1
## Depends on: #27 (bounded-memory streaming)

## Background

The `memtest` suite added in #27 (`go test -tags memtest ./memtest/ -v`) ran
for the first time on this machine (ARM64 VM, 16 GB). Results were unexpected
in two ways.

## Observed results

| Test | Peak RSS | Threshold | Result |
|---|---|---|---|
| TTS short (~50 words, 1 chunk) | 481 MB | 525 MB | PASS |
| TTS long (~400 words, 8+ chunks) | 995 MB | 525 MB | **FAIL** |
| STT short (25s audio, 1 chunk) | 470 MB | 375 MB | **FAIL** |
| STT long (180s audio, 6 chunks) | 739 MB | 375 MB | **FAIL** |

Thresholds are 1.5× the acceptance targets from #27 (350 MB TTS, 250 MB STT).

## Concern 1 — TTS multi-chunk accumulation

TTS short peaks at 481 MB; TTS long peaks at 995 MB — a 2× ratio. With correct
streaming (plan 28), peak RSS for the long run should be roughly equal to the
short run, since each chunk's PCM is written to the WAV sink and the tensor
memory freed before the next chunk begins.

A 2× ratio strongly suggests either:
- chunk PCM tensors from inference are being held in memory across chunk
  boundaries (ORT or Go GC hasn't released them), OR
- the WAV writer is accumulating PCM in a buffer rather than discarding it
  chunk by chunk, OR
- two consecutive chunks' allocations overlap in the RSS snapshot because GC
  has not run malloc_trim between chunks

Needs code-level investigation in `speak/kokoro/kokoro.go` and `audio/wav.go`
to confirm whether PCM is genuinely freed between chunks or merely marked free
but not returned to the OS.

## Concern 2 — STT baseline higher than estimated

The original #27 estimate was ≤190 MB for STT (one chunk). Actual short-clip
peak is 470 MB — 2.5× the estimate. This may mean:
- The original 190 MB was measured with `runtime.GC()` + `malloc_trim` forced
  between operations, not during live inference
- Moonshine's encoder/decoder sessions retain larger activation buffers than
  expected
- The test baseline RSS (process startup + ORT init) is much higher on this VM
  than the original measurement context

The STT long/short ratio is 1.57× (739 / 470), which is less alarming than
TTS, but still indicates some accumulation across chunks.

## Questions to resolve

1. **TTS**: Is the 2× long/short RSS ratio caused by genuine PCM accumulation,
   or by GC timing (freed memory not yet returned to OS between chunks)?
   - Does `runtime.GC()` + `malloc_trim` after each chunk collapse the ratio?
   - Does the ratio grow beyond 2× with even longer text (3×, 4×)?

2. **STT**: Is 470 MB an accurate single-chunk baseline, or is ORT retaining
   encoder KV-cache or activation tensors across decode steps?
   - What is the pre-inference RSS (after engine load, before first chunk)?

3. **Thresholds**: Should the memtest thresholds be calibrated to actual
   observed baselines rather than the original estimates?

## Suggested investigation approach

- Add pre/post RSS logging inside the chunk loop (without changing behavior)
  to see whether RSS grows monotonically or fluctuates per chunk
- Extend `longText` in the memtest suite to 2×, 3× length and plot RSS vs
  chunk count to distinguish accumulation from a high-but-flat baseline
- For TTS: check whether `io.Discard` as the WAV sink (used in tests) triggers
  the temp-file fallback path in `audio.WAVWriter`, which spools PCM to disk
  and writes all at once — this would explain the linear RSS growth

## Note on the temp-file fallback

`audio.WAVWriter` has two paths: seekable (patch header in place, flush
per-chunk) and non-seekable (spool PCM to a temp file, write WAV at close).
`io.Discard` is not seekable. If the non-seekable path still accumulates PCM
in memory (or writes it to a temp file that grows proportionally), that would
explain the test results without implicating actual TTS inference.

The fix in that case would be either:
- Make the non-seekable path truly streaming (PCM goes to temp file, not RAM)
- Or use a `bytes.Buffer` wrapping a seek-capable writer in tests instead of
  `io.Discard`

## Acceptance criteria

- [ ] Root cause of TTS 2× ratio identified and documented
- [ ] Root cause of STT 470 MB baseline identified and documented
- [ ] memtest thresholds updated to reflect real-world measurements
- [ ] At minimum, TTS long RSS / TTS short RSS ratio ≤ 1.2 (within noise)
- [ ] STT long RSS / STT short RSS ratio ≤ 1.2
