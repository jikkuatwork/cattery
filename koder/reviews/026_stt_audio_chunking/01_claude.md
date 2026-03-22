# Plan Review — #26 STT Audio Chunking

## Summary

The plan proposes adding transparent audio chunking inside
`moonshine.Engine.Transcribe()` to prevent hallucination on clips longer than
~30 seconds. A new `chunk.go` file handles chunk planning: fixed 30s target
windows with silence-preferred cuts (sliding-window RMS, ±3s search), 0.5s
overlap between adjacent chunks, word-level dedup at boundaries, and
pure-silence skip. The existing single-pass inference is extracted into a
helper so it can run per chunk. No changes to `listen.Engine`, CLI, server, or
API shape.

## Findings

### P2-1: Issue says "16kHz int16" — samples are actually float32

The issue spec (line 78) says "Silence detection operates on raw PCM (16kHz
int16)". The plan correctly identifies that `audio.ReadPCM` returns normalized
`[]float32` and that RMS should be expressed in dBFS against full-scale. The
issue text is misleading but the plan has the right model. No code change
needed, but the issue should be updated to avoid confusing the implementer.

**Severity: P2** — the plan itself is correct, but the issue it references is
wrong. Fix the issue text.

### P2-2: Issue says `Listen()` — there is no `Listen()` function

The issue spec describes chunking inside a package-level `Listen()` function.
The plan correctly identifies that the hook point is
`Engine.Transcribe()`. The issue's "Design" section references `Listen()`
three times. The plan overrides this correctly, but the mismatch could confuse
someone reading the issue alongside the plan.

**Severity: P2** — plan is correct; issue should be updated for consistency.

### P2-3: Duration calculation should use original sample count, not resampled

The current code (moonshine.go:116) computes `duration` from the pre-resample
sample count and rate, which is correct. The plan says "Keep
`listen.Result.Duration` tied to the original clip duration" (work plan step
7), which matches. However, the plan also says to chunk "after resampling to
`e.sampleRate`" (line 29). If chunking happens post-resample, the chunk
planner will be working in resampled-sample-space. The 30s target and sample
counts must use `e.sampleRate` (16000), not the original input rate. This is
implied but never explicitly stated in the plan — worth making explicit to
avoid a subtle bug where someone uses the original rate's sample count for
chunk boundaries.

**Severity: P2** — add a note that chunk sample counts are always in
`e.sampleRate` space.

### P2-4: Overlap dedup strategy underspecified

The plan says "removing the longest shared word sequence between the prior
suffix and next prefix, with a small capped word window." This is reasonable
but underspecified:

- What is the cap? 5 words? 10? At 0.5s overlap at normal speech rate (~2.5
  words/sec), expect 1-2 overlapping words. A cap of 5-8 is sensible.
- "Longest shared word sequence" — is this longest common substring, or
  prefix-suffix match? For dedup at chunk boundaries, you want the longest
  suffix of chunk[i] that equals a prefix of chunk[i+1]. A general LCS would
  be wrong here.

**Severity: P2** — clarify: suffix-prefix match, not general LCS; state the
word window cap.

### P3-1: Encoder/decoder state isolation between chunks

The plan implies each chunk gets its own full encoder + decoder pass (extract
inference into a helper, call per chunk). This is correct for Moonshine since
the KV cache is per-decode-call and there's no cross-chunk state. Worth noting
explicitly that the KV cache is fresh per chunk — this is a correctness
invariant, not just an implementation detail.

**Severity: P3** — add a one-line note about KV cache isolation.

### P3-2: Search window description inconsistency

The issue says "±3s search window around each 30s mark." The plan says
"search for silence inside a 27s..33s window relative to the chunk start."
These are equivalent, but using both phrasings could cause confusion. The plan
should pick one and stick with it.

**Severity: P3** — cosmetic.

### P3-3: No mention of thread safety

`Transcribe()` already holds the full session for the duration of the call.
The chunking loop calls the same encoder/decoder sessions sequentially per
chunk. This is fine — ORT sessions are not thread-safe anyway, and the server
pool already serializes access. No action needed, just noting this was
considered.

**Severity: P3** — informational, no action.

## Codebase Accuracy

| Plan claim | Actual code | Correct? |
|---|---|---|
| `listen.Engine` exposes `Transcribe(io.Reader, listen.Options)` | `listen/listen.go:8` | Yes |
| No package-level `Listen()` hook | Confirmed — only `Engine.Transcribe` | Yes |
| `audio.ReadPCM(...)` returns mono `[]float32` | `audio/read.go:18` returns `([]float32, int, error)` | Yes |
| WAV PCM16 and raw float32 supported | `audio/read.go:34-37` — WAV (PCM16 + float32) and raw float32 | Yes |
| `Transcribe()` resamples to model rate | `moonshine.go:117-119` | Yes |
| Duration from pre-resample samples | `moonshine.go:116` | Yes |
| No chunking/silence/overlap exists | Confirmed — single pass | Yes |
| `listen/moonshine/` has no tests | No `*_test.go` in that dir | Yes |
| Samples are normalized float32 | PCM16 divides by 32768; float32 passed through | Yes |

## Scope Discipline

The scope is well-calibrated. It adds exactly one capability (chunking) without
touching the public interface, CLI, server, or audio format handling. The
deferral list is appropriate — streaming STT, VAD, word timestamps, and
user-configurable knobs are all correctly deferred.

One concern: the plan does not mention memory. For a 5-minute clip at 16kHz
float32, that's ~19MB of PCM loaded fully into memory, then sliced into
chunks. This is fine for the stated use case (preventing hallucination on
60s-2min clips) but the "Out of Scope" section should explicitly note that
streaming decode (to avoid loading the full clip) is deferred — which it
already does (line 68-69). Good.

## Thin-Layer Checklist

- [x] ONE capability in a sentence — "Chunk long audio before Moonshine
  inference to prevent hallucination"
- [x] Deferral list > in-scope list — 7 deferred items vs 6 in-scope items
- [x] POC testable before dependencies — chunking logic is pure functions on
  `[]float32`, testable without ORT
- [x] Structural decisions explicit — chunk after resample, package constants,
  no public API change
- [x] Acceptance criteria concrete — 10 specific, testable criteria
- [x] File change list present — 5 files listed

## Verdict

**PASS (0 P1, 4 P2, 3 P3)**

No blocking issues. The plan is sound and well-scoped. The P2s are
clarification items that should be addressed before implementation to avoid
ambiguity:

1. Fix the issue spec's "int16" and "Listen()" references
2. Explicitly state chunk sample counts use `e.sampleRate`
3. Specify suffix-prefix matching (not LCS) and word window cap for dedup
