# Implementation Review — Plan 29: STT Streaming Decode

**Plan**: `koder/plans/plan_29_stt_streaming.md`
**Commit**: `48eeedd` (Implement plan 29 STT streaming)
**Files changed**: 11 files, +1118 / -189

## Completeness

All five work plan items are addressed:

1. **Streaming PCM reader** — `audio/stream.go` introduces `PCMStreamReader`
   with `NewPCMStreamReader()` and `ReadSamples()`. Sniffs WAV vs raw once in
   `init()`, then incrementally decodes WAV PCM16, WAV float32, or raw float32.
2. **Streaming resampler** — `audio/resample_stream.go` introduces
   `StreamResampler` with `Write()` / `Flush()`. Same linear interpolation
   model as `audio.Resample()`. `audio.Resample()` retained for whole-buffer
   callers.
3. **Window-based planner** — `listen/moonshine/chunk.go` adds
   `planNextChunk()` and `planNextChunkWithThreshold()` operating on the
   current window. `needMore` reported for non-EOF short windows; short final
   chunk emitted at EOF. Existing `planAudioChunks()` and
   `transcribeChunkedPCM()` preserved as internal helpers.
4. **Sliding-window transcription** — `listen/moonshine/moonshine.go` adds
   `transcribeStream()` loop: decode → resample → fill bounded window →
   plan → transcribe → retain overlap tail. Duration tracked from source
   sample count.
5. **Tests** — `audio/stream_test.go` (3 tests: raw, PCM16, float32 WAV),
   `audio/resample_stream_test.go` (3 tests: downsample, upsample, same-rate),
   `listen/moonshine/chunk_test.go` (+2 tests: needMore, short final),
   `listen/moonshine/stream_test.go` (3 tests: resample+duration, overlap+dedup,
   silence skip).

**Files expected by plan vs actual**:

| Expected | Actual | Match |
|---|---|---|
| `audio/read.go` | modified | yes |
| new `audio/stream.go` | created | yes |
| new `audio/stream_test.go` | created | yes |
| `audio/resample.go` | **unchanged** | ok (no changes needed) |
| new `audio/resample_stream.go` | created | yes |
| new `audio/resample_stream_test.go` | created | yes |
| `listen/moonshine/moonshine.go` | modified | yes |
| `listen/moonshine/chunk.go` | modified | yes |
| `listen/moonshine/chunk_test.go` | modified | yes |
| new `listen/moonshine/stream_test.go` | created | yes |
| `koder/issues/27_bounded_memory_streaming.md` | modified | yes |
| `koder/STATE.md` | modified | yes |

## Acceptance Criteria

- [x] **No full-clip `io.ReadAll` or full-clip resample** — `audio/read.go`
  now delegates to `NewPCMStreamReader` + loop of `ReadSamples(4096)`.
  `transcribeStream` reads 1-second blocks and resamples incrementally.
- [x] **WAV PCM16, WAV float32, raw float32 decoded incrementally** —
  `PCMStreamReader.readWAVSamples()` decodes bounded frames per call;
  `readRawFloat32()` reads bounded 4-byte-aligned blocks. Tests verify all
  three formats via block-based collection.
- [x] **Streaming resampler continuous across block boundaries** —
  `StreamResampler` maintains `nextPos`, `pendingStart`, and lookbehind state
  via `discardConsumed()`. Tests verify output matches `audio.Resample()` for
  varying chunk sizes (downsample, upsample, same-rate).
- [x] **Planner returns `needMore` for non-EOF short window** —
  `planNextChunkWithThreshold()` returns `needMore: true` when
  `!atEOF && len(samples) < target+search` (chunk.go:79-84).
  `TestPlanNextChunkNeedsMoreBeforeEOF` verifies.
- [x] **Short audio → one transcription call with correct duration** —
  `TestTranscribeStreamShortAudioResamplesAndTracksDuration` sends 1.25s at
  8 kHz, verifies 1 call, correct resampled length, and
  `Duration ≈ 1.25`.
- [x] **Pure-silence windows skipped, boundary-word dedup removes overlap** —
  `TestTranscribeStreamSkipsPureSilence` verifies 0 transcribe calls on 65s
  silence. `TestTranscribeStreamRetainsOverlapAndDedupsText` verifies
  "the moon" overlap is deduped across chunk boundary.
- [x] **`go test ./...`, `go build ./...`, `go vet ./...` pass** — Confirmed.

## Security

No new external input surfaces introduced. WAV header parsing in
`PCMStreamReader.initWAV()` validates chunk sizes, format tags, bit depths,
and channel counts before proceeding. Frame-aligned reads prevent
out-of-bounds access. NaN/Inf checks retained in `decodeRawFloat32Bytes` and
`decodeFloat32Frames`.

## Code Quality

### P3 — `padBytesRemaining` field is dead code

`PCMStreamReader.padBytesRemaining` (stream.go:28) is declared and referenced
in `finishWAVData()` (stream.go:268-277), but is never set to a non-zero value.
The WAV spec requires a pad byte after odd-length data chunks, but the init
code rejects data chunks with `chunkSize % frameSize != 0` (stream.go:183),
and frame sizes are always even (2×ch or 4×ch), so the pad case can never be
reached. The field and method are harmless but unused.

### P3 — `ReadPCM` capacity hint could use stream metadata

`audio/read.go:25` uses `make([]float32, 0, 4096)` for the accumulator. For
WAV input, `PCMStreamReader` knows `dataBytesRemaining` which could provide a
better capacity hint. Minor; the current approach works via amortized growth.

## Verdict

**PASS (0 P1, 0 P2, 2 P3)**

The implementation faithfully follows the plan's structural decisions and scope
boundaries. All acceptance criteria are met with corresponding test evidence.
The streaming pipeline (decode → resample → plan → transcribe → retain
overlap) is correctly wired with bounded memory. The public `listen.Engine`
surface is unchanged. No scope creep detected.
