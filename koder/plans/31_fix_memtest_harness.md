# Plan 31 — Fix Memtest Harness (#29)

## Why

The memtest suite exists to catch RSS regressions for #27, but harness-level
bugs made the initial run fail every threshold. Commit `ddad3cc` fixed two of
three findings (seekable sink, atomic poller). The remaining work is memory
isolation between tests and a validation run to confirm the harness is
trustworthy before #28 proceeds.

## Current State (post-ddad3cc)

- **Finding 1 (io.Discard → temp file)**: DONE. `runTTS()` now uses
  `os.CreateTemp` in `t.TempDir()` — seekable, exercises the real WAV path.
- **Finding 2 (data race)**: DONE. `startRSSPoller()` uses `atomic.Int64`.
- **Finding 3 (memory isolation)**: PARTIAL. `drainMemory()` calls
  `runtime.GC()` + `debug.FreeOSMemory()` and is invoked at the start of each
  test, but it does not call `malloc_trim(0)` to return freed C-heap pages
  (ORT allocator). ORT retains internal allocator caches across session
  destroys, so later tests may start from a higher RSS baseline.
- **STT long test**: does not log short/long ratio like the TTS long test does.
  Adding this makes the output symmetrical and gives #28 the data it needs.

## Steps

### Step 1 — Add malloc_trim to drainMemory

`ort/ort.go` already exposes `C.malloc_trim(0)` inside `Shutdown()`. Extract a
lightweight `ort.Drain()` function (or export the cgo call separately) so the
memtest can call it without tearing down the ORT environment.

```go
// ort/ort.go
func Drain() {
    C.malloc_trim(0)
    debug.FreeOSMemory()
}
```

Update `drainMemory()` in `memtest/rss_test.go`:

```go
func drainMemory(t *testing.T) {
    t.Helper()
    runtime.GC()
    ort.Drain()
}
```

### Step 2 — Add short/long ratio logging to STT long test

`TestSTTPeakRSS_Long` currently only logs its own peak. Mirror the TTS long
test pattern: run a short baseline first, drain, run long, log the ratio.

```go
func TestSTTPeakRSS_Long(t *testing.T) {
    drainMemory(t)
    shortPeak := runSTT(t, 25)
    drainMemory(t)
    peak := runSTT(t, 180)
    t.Logf("STT long:  peak RSS %d MB (threshold %d MB, 180s audio)", peak, sttPeakRSSThresholdMB)
    if shortPeak > 0 {
        ratio := float64(peak) / float64(shortPeak)
        t.Logf("STT long/short RSS ratio: %.2fx (%d MB / %d MB)", ratio, peak, shortPeak)
    }
    assertRSS(t, "STT long", peak, sttPeakRSSThresholdMB)
}
```

### Step 3 — Validate

```bash
go build ./...
go vet ./...
go test -tags memtest -race ./memtest/ -v
```

Confirm:
- No data race under `-race`
- All four tests produce reasonable numbers
- Short/long ratios are logged for both TTS and STT
- No panic or OOM

### Step 4 — Record results

Log the raw numbers (peak RSS per test, ratios) in a comment on the commit or
in #28's issue file as the "post-fix harness baseline". These become the
starting point for #28's investigation.

## Decisions

- **Don't restart ORT between tests.** `ort.Shutdown()` + `ort.Init()` takes
  ~1.4s and changes the execution profile. `malloc_trim` is sufficient to
  return freed C heap to the OS without tearing down the environment.
- **Don't change thresholds.** That's #28's job. The harness should report
  accurate numbers; threshold calibration is a separate concern.
- **Keep tests in the current order.** TTS short → TTS long → STT short →
  STT long. Each test drains memory before starting.

## Files changed

- **Edit**: `ort/ort.go` — add `Drain()` export
- **Edit**: `memtest/rss_test.go` — update `drainMemory()` to call `ort.Drain()`,
  add STT ratio logging

## Acceptance criteria (from #29)

- [x] TTS tests use a seekable WAV sink (not `io.Discard`) — done in ddad3cc
- [ ] `go test -tags memtest -race ./memtest/` passes with no data race
- [ ] Each test starts from a drained memory state (GC + malloc_trim)
- [ ] `go build ./...` and `go vet ./...` clean
- [ ] TTS and STT long/short RSS ratios measured and logged
