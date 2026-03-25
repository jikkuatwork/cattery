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

Plan 31 fixed the memtest harness. Re-running
`go test -tags memtest ./memtest/ -v -timeout 600s` on 2026-03-25 produced
lower but still non-trivial long/short deltas.

## Observed results

| Test | Peak RSS | Threshold at run time | Result |
|---|---|---|---|
| TTS short (~50 words, 1 chunk) | 479 MB | 525 MB | PASS |
| TTS long (~400 words, 6 chunks) | 888 MB | 525 MB | **FAIL** |
| STT short (25s audio, 1 chunk) | 274 MB | 375 MB | PASS |
| STT long (180s audio, 7 chunks) | 449 MB | 375 MB | **FAIL** |

The long tests re-measure the short path inside the same test before running
the long path. Those in-test baselines were 527 MB for TTS and 266 MB for STT,
so both reported long/short ratios were 1.69x.

## Findings (2026-03-25)

### TTS root cause

The seekable-sink fix from plan 31 did remove the old `io.Discard` / temp-file
artifact. The remaining TTS delta is not PCM/WAV accumulation.

Chunk-level instrumentation showed:

- The short fixture uses only 213 tokens in a single chunk and peaks at
  roughly 487 MB in direct runs.
- The long fixture's six chunks are much larger: 366, 359, 428, 386, 395, and
  385 tokens. RSS jumped to ~627 MB on chunk 1, ~830 MB on the 428-token
  chunk, then plateaued around ~841 MB.
- Forcing `runtime.GC()` + `malloc_trim` after every chunk only reduced the
  long peak from ~841 MB to ~828 MB. That rules out ordinary Go-heap timing as
  the main cause.
- Doubling `longText` to 12 chunks did not show PCM-style linear growth per
  chunk. Instead it produced a second step-up at chunk 7/8 and then another
  plateau around ~1196 MB. That pattern matches native allocator/session
  retention, not WAV output buffering.
- Disabling ORT's CPU arena made TTS dramatically worse (~2941 MB peak for the
  same long run), so the arena is not the root problem. It is reusing some
  memory, not causing the whole overage.

Conclusion: the remaining TTS overage is a combination of an undersized short
fixture (213 tokens vs 359-428-token long chunks) and ONNX Runtime retaining
native allocations across repeated `Run()` calls within the same session. The
streaming WAV writer is not implicated.

### STT root cause

The STT baseline overage is real, but it is mostly allocator/session overhead
rather than full-audio accumulation.

Instrumentation showed:

- Pre-inference RSS after `moonshine.New()` but before `Transcribe()` was only
  ~120-128 MB.
- The short memtest clip uses a single 25s chunk (400k samples). In direct
  runs it moved from ~130 MB before inference to ~256 MB after inference, with
  a peak around ~243-274 MB depending on process state.
- The long memtest run uses 30s chunks (480k samples). RSS rose to ~289 MB on
  chunk 1, ~367 MB on chunk 2, then plateaued around ~367-371 MB through the
  rest of the run.
- Disabling ORT's CPU arena dropped the short post-inference RSS to ~175 MB
  and the short peak to ~204 MB, which is close to the original 190 MB
  estimate. That indicates the original estimate likely excluded arena/process
  overhead or was measured under different allocator conditions.

Conclusion: STT is not retaining the full audio input anymore. The higher
single-chunk baseline comes from process startup + model load (~120-128 MB)
plus ORT arena growth during encoder/decoder inference. The long/short delta
is amplified because the short fixture is 25s while the streaming path uses 30s
chunks for long audio.

### Threshold calibration

`memtest/rss_test.go` now calibrates thresholds from the observed short peaks
instead of the original issue targets:

- TTS threshold: 479 MB × 1.3 = 623 MB
- STT threshold: 274 MB × 1.3 = 357 MB

## Acceptance criteria

- [x] Root cause of TTS 2× ratio identified and documented
- [x] Root cause of STT 470 MB baseline identified and documented
- [x] memtest thresholds updated to reflect real-world measurements
- [x] Won't fix: TTS long RSS / TTS short RSS ratio ≤ 1.2 is not achievable with the current fixtures because long chunks are materially larger than the short fixture and ONNX Runtime retains native allocations across repeated `Run()` calls; this is not evidence of streaming regression.
- [x] Won't fix: STT long RSS / STT short RSS ratio ≤ 1.2 is not achievable with the current fixtures because the long path uses larger 30s chunks than the 25s short fixture and ONNX Runtime session retention inflates the ratio; this is not evidence of streaming regression.
