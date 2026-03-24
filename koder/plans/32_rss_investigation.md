# Plan 32 — RSS Investigation (#28)

## Why

With the memtest harness fixed (plan 31), we can trust the numbers. #28 asks
two questions: why is TTS long ~2× short, and why is STT baseline higher than
the original 190 MB estimate? This plan resolves those questions and calibrates
thresholds to match real-world measurements.

## Depends on

Plan 31 (fix memtest harness). The harness must produce accurate, isolated RSS
measurements before this investigation makes sense.

## Steps

### Step 1 — Collect post-fix baseline

Run the memtest suite with plan 31's fixes applied. Record:

| Test | Peak RSS | Ratio |
|---|---|---|
| TTS short | ? MB | — |
| TTS long | ? MB | long/short = ? |
| STT short | ? MB | — |
| STT long | ? MB | long/short = ? |

If TTS long/short ratio is already ≤1.2 after the seekable-sink fix, Finding 1
from #28 was entirely caused by the `io.Discard` fallback and needs no further
engine-level investigation. Skip to Step 3.

### Step 2 — Investigate remaining accumulation (if ratio > 1.2)

If the ratio is still high after the harness fix:

**2a. Add per-chunk RSS instrumentation**

Add temporary `t.Logf` calls inside `speak/kokoro/kokoro.go`'s chunk loop to
log RSS before and after each chunk's inference + WAV write. This shows whether
RSS grows monotonically (accumulation) or stays flat (high-but-bounded
baseline).

Don't commit this instrumentation — it's for investigation only.

**2b. Test with extended text**

Double `longText` to ~800 words and run again. If RSS grows linearly with
chunk count, there's real accumulation. If it plateaus, it's a GC timing issue.

**2c. Check GC + malloc_trim between chunks**

If accumulation is confirmed, add `runtime.GC()` + `ort.Drain()` after each
chunk in the synthesis loop. If this collapses the ratio, the fix is to add
explicit memory return between chunks (acceptable production cost: ~5ms per
chunk).

**2d. Check ORT tensor lifetime**

If GC + drain doesn't help, profile ORT's internal allocator. The ONNX Runtime
arena allocator may retain peak allocation across `Run()` calls within the same
session. If so, the only fix is `session.Destroy()` + recreate between chunks
(expensive, ~200ms) — document this as a known ORT behavior and adjust
thresholds instead.

### Step 3 — Investigate STT baseline

The original 190 MB estimate may have been taken in different conditions. To
understand the real baseline:

**3a. Measure pre-inference RSS**

Add a one-shot RSS read after `moonshine.New()` but before `Transcribe()`.
This separates "model load" from "inference peak".

**3b. Compare with and without ORT arena**

If ORT retains encoder activation buffers, `ONNX_DISABLE_ARENA=1` can be set
(if supported) to check whether the arena is inflating RSS. Document the
finding.

**3c. Accept or explain the baseline**

If the baseline is genuinely higher than estimated, update the issue #28
documentation with the corrected number and explain why (ORT arena, Go runtime
overhead, etc.).

### Step 4 — Calibrate thresholds

Based on the real measurements:

1. Set `ttsPeakRSSThresholdMB` = observed TTS short peak × 1.3 (30% headroom
   for platform/GC variance)
2. Set `sttPeakRSSThresholdMB` = observed STT short peak × 1.3
3. The threshold must be high enough that the test doesn't flake on CI but low
   enough to catch a genuine 2× regression.

Update `memtest/rss_test.go` constants.

### Step 5 — Update #27 and #28 acceptance criteria

- Update issue #27's remaining checkboxes with the observed numbers
- Update issue #28's acceptance criteria as resolved
- Document root causes in #28's issue file

## Files changed

- **Edit**: `memtest/rss_test.go` — update threshold constants
- **Edit**: `koder/issues/28_rss_validation.md` — document root causes
- **Edit**: `koder/issues/27_bounded_memory_streaming.md` — update acceptance
  checkboxes with real numbers
- **Possibly edit**: `speak/kokoro/kokoro.go` — if per-chunk GC is needed
  (only if Step 2c confirms accumulation)

## Acceptance criteria (from #28)

- [ ] Root cause of TTS long/short ratio identified and documented
- [ ] Root cause of STT baseline identified and documented
- [ ] memtest thresholds updated to reflect real-world measurements
- [ ] TTS long/short RSS ratio ≤ 1.2
- [ ] STT long/short RSS ratio ≤ 1.2
