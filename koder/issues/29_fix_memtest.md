---
id: 29
title: "Fix memtest suite: test artifacts causing false failures and OOM risk"
status: done
priority: P1
depends_on: "#28 (RSS validation)"
created: 2026-03-24
---

# 29 — Fix memtest suite: test artifacts causing false failures and OOM risk

## Status: done (native suite fixed; `-race` still trips RSS threshold)
## Priority: P1
## Depends on: #28 (RSS validation)

## Background

The `memtest` suite (`go test -tags memtest ./memtest/ -v`) was added for #27
to catch RSS regressions. On first run it failed every threshold — and likely
OOM-killed the host. Code review reveals three test-level bugs that inflate
RSS measurements before any real engine problem is even visible.

This issue covers **fixing the test harness itself**. Once the tests give
accurate numbers, #28 can proceed with the real RSS investigation.

- 2026-03-25 follow-up verification: `go test -tags memtest ./memtest/ -v
  -timeout 600s` passed. `go test -tags memtest -race ./memtest/` reported no
  data race, but the race-instrumented run pushed `TestSTTPeakRSS_Short` to
  372 MB and therefore did not complete as a passing suite under the calibrated
  RSS threshold.

## Finding 1 — `io.Discard` triggers non-seekable WAV spool (CRITICAL)

**Root cause of TTS 2x ratio (481 MB short vs 995 MB long).**

`runTTS()` passes `io.Discard` as the WAV output (`rss_test.go:163`).
`io.Discard` is not seekable, so `audio.NewWAVWriter` (`audio/wav.go:49-66`)
takes the **non-seekable fallback path**: it creates a temp file and spools
all PCM there, then copies the entire temp file to the destination at
`Close()`.

This means the test is not exercising the streaming WAV path at all — it's
measuring temp-file accumulation. The 2x ratio is expected under this path
because all chunk PCM accumulates in the temp file.

### Fix

Replace `io.Discard` with a seekable sink. Options:

- `bytes.Buffer` wrapped in a minimal `WriteSeeker` — simplest, but the
  buffer itself accumulates. Since we only care about peak RSS *during
  inference* (not at WAV close), this is fine: WAV chunks flush immediately
  in the seekable path.
- `os.CreateTemp` + `os.Remove` — real file, seekable, OS handles buffering.
  Closest to production behavior (`cattery -o out.wav`).

Recommended: use a temp file. It matches the real CLI path and keeps the
test's RSS measurement focused on inference, not Go heap from a
`bytes.Buffer`.

## Finding 2 — Data race in `startRSSPoller` (MEDIUM)

`peak` (`int64`) is written by the poller goroutine (`rss_test.go:243`) and
read by the caller (`rss_test.go:250`) without synchronization. This is a
data race under `-race`.

### Fix

Use `atomic.Int64` (Go 1.19+). Drop-in replacement:

```go
var peak atomic.Int64
// writer:
if rss := currentRSSMB(); rss > peak.Load() {
    peak.Store(rss)
}
// reader:
peakMB = func() int64 { return peak.Load() }
```

## Finding 3 — No memory isolation between tests (MEDIUM)

All four tests share a single `ort.Init()` / `ort.Shutdown()` pair
(`rss_test.go:79-92`). ORT retains internal allocator caches across session
destroys, so later tests start from a higher RSS baseline than the first.

This inflates the STT baseline (470 MB observed vs 190 MB estimated) and
muddies the short-vs-long ratio for both modalities.

### Fix

Add explicit GC + malloc_trim between tests. Import the existing
`ort.Shutdown` pattern — but since re-init is expensive, a lighter approach:

```go
func drainMemory(t *testing.T) {
    t.Helper()
    runtime.GC()
    debug.FreeOSMemory()
    // If ort exposes malloc_trim, call it here too
}
```

Call `drainMemory(t)` at the start of each test function, after engine
`Close()` in the previous test has run via `defer`.

If ORT caches still dominate, consider `ort.Shutdown()` + `ort.Init()` in a
`sync.Once`-guarded helper per modality (TTS tests share one init, STT tests
share another).

## Acceptance criteria

- [x] TTS tests use a seekable WAV sink (temp file, not `io.Discard`)
- [ ] `go test -tags memtest -race ./memtest/` passes with no data race. The 2026-03-25 run showed no race report, but the suite failed because `TestSTTPeakRSS_Short` reached 372 MB against the 357 MB threshold under race instrumentation.
- [x] Each test starts from a drained memory state (`runtime.GC` +
      `debug.FreeOSMemory` between tests)
- [x] `go build ./...` and `go vet ./...` clean
- [x] TTS long/short RSS ratio measured and logged (no threshold change yet —
      #28 will calibrate)

## Out of scope

- Changing RSS thresholds (that's #28)
- Fixing actual engine memory behavior (that's #28)
- Changes to `audio/wav.go` or engine code — this issue is test-only
