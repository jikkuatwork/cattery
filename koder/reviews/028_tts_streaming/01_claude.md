# Implementation Review — Plan 28: TTS Streaming WAV Output

**Commit**: `21ab754` (Implement TTS streaming WAV output)
**Plan**: `koder/plans/plan_28_tts_streaming.md`
**Reviewer**: Claude (implementation review)

---

## Completeness

All five work-plan items are implemented:

1. **Streaming WAV writer** (`audio/wav.go`) — `WAVWriter` struct with
   `NewWAVWriter`, `WriteSamples`, `Close`. Two internal paths: seekable
   (header placeholder → patch on close) and non-seekable (temp file → flush
   on close). `WriteWAV` preserved as thin wrapper.
2. **WAV writer tests** (`audio/wav_test.go`) — seekable header patching,
   non-seekable temp fallback, `WriteWAV` parity with streaming writer.
3. **`countingWriter` seek forwarding** (`cmd/cattery/io.go`) — `Seek` method
   added, delegates to inner writer when it implements `io.Seeker`. Logical
   offset tracked so `count` stays correct after seek-back-and-rewrite.
4. **`countingWriter` tests** (`cmd/cattery/io_test.go`) — seek forwarding,
   logical size tracking after overwrite and extend, non-seekable error path.
5. **Kokoro flush-per-chunk** (`speak/kokoro/kokoro.go`) — `synthesizeChunks`
   removed; `Speak` now opens a `WAVWriter`, writes each chunk immediately,
   nils the samples slice after write, inserts gap only between chunks.

**Files touched** match plan exactly (7 files, all expected).

## Acceptance Criteria

- [x] **`kokoro.Engine.Speak()` no longer builds one full `combined []float32`**
  — `synthesizeChunks` deleted. Each chunk's `samples` is written then set to
  `nil` (kokoro.go:200). No accumulation across chunks.

- [x] **File output uses seekable header-patch path when destination can seek**
  — `probeWriteSeeker` (wav.go:150-158) tests for `io.Seeker` with a zero-offset
  seek probe. `closeSeekable` (wav.go:161-174) seeks back to byte 0 and patches
  the RIFF/data sizes.

- [x] **Stdout, pipes, and `bytes.Buffer` still produce valid WAV via non-seekable fallback**
  — `NewWAVWriter` falls through to temp file when `probeWriteSeeker` returns
  false. `closeBuffered` (wav.go:177-191) writes header then copies temp payload.
  Test `TestWAVWriterNonSeekableBuffersUntilClose` confirms with `bytes.Buffer`.

- [x] **`countingWriter` no longer hides `io.Seeker` from lower layers**
  — `countingWriter.Seek` (io.go:39-51) forwards to the inner writer's `Seek`.
  `probeWriteSeeker` will now see `countingWriter` as a `writeSeeker` when the
  underlying `*os.File` supports it.

- [x] **`audio.WriteWAV(...)` still works for existing callers**
  — Wrapper preserved (wav.go:138-148), delegates to `NewWAVWriter` +
  `WriteSamples` + `Close`. Test `TestWriteWAVMatchesStreamingWriter` confirms
  output parity.

- [x] **`go test ./...`, `go build ./...`, `go vet ./...` pass**
  — All three verified clean at review time.

## Security

No concerns. Temp files use `os.CreateTemp` with a fixed prefix (no user input
in path). Temp cleanup is in `cleanupTemp` called from `Close`, and `WriteWAV`
also cleans up on mid-write errors (wav.go:144). No network, no user-controlled
paths in the WAV writer.

## Code Quality

### Positive

- **Clean dual-path architecture**: the seekable vs buffered split in
  `WAVWriter` is well-separated. `probeWriteSeeker` uses a real seek probe
  rather than a type assertion alone, which handles wrapped writers correctly.

- **Header encoding consolidated**: `encodeWAVHeader` replaces the old inline
  `binary.Write` calls with a single function that both paths share.

- **`fitsWAVHeader` overflow guard**: prevents 32-bit overflow in RIFF header
  fields. Checked at construction and at each `WriteSamples` call.

- **Named return + defer for WAV close**: `Speak` uses `(err error)` named
  return so the deferred `wav.Close()` can surface header-patching errors
  without a separate error variable.

- **`countingWriter` tracks high-water mark**: `count` is max(offset) not
  cumulative, so seek-back-and-overwrite doesn't inflate the byte count. Test
  exercises this explicitly.

### Findings

**P2-1: Temp file leaked on `WriteSamples` error in streaming path**

If `WriteSamples` is called directly (not via `WriteWAV`) and returns an error
mid-stream, the caller may not call `Close()`, leaving the temp file on disk.
`WriteWAV` handles this (wav.go:144 calls `cleanupTemp`), but direct
`WAVWriter` users must remember to always `Close()` — even on error.

This is a documentation / API-surface concern, not a bug. The `Close` godoc
says it "finalizes the WAV stream" but doesn't mention cleanup responsibility.
A one-line note in the `NewWAVWriter` or `WAVWriter` doc ("Close must be called
even if WriteSamples returns an error") would prevent misuse.

**P3-1: `seekBuffer` duplicated across test files**

`audio/wav_test.go` has `seekBuffer` and `cmd/cattery/io_test.go` has
`seekRecorder` — both are in-memory write-seekers with nearly identical logic.
Not blocking, but a shared `internal/testutil` helper would reduce duplication
if more packages need one.

**P3-2: `pcm16BytesPerPCM` naming**

The constant name reads as "PCM16 bytes per PCM" which is slightly circular.
Something like `bytesPerSample` would be clearer. Minor.

## Verdict

**PASS (0 P1, 1 P2, 2 P3)**

The implementation faithfully covers every plan decision and acceptance
criterion. The streaming WAV writer, seekable header patching, temp-file
fallback, `countingWriter` seek forwarding, and per-chunk synthesis flushing
all work as specified. Tests cover both paths. Build, vet, and tests clean.
The single P2 is a documentation suggestion for direct `WAVWriter` callers —
no code change required for current usage since `kokoro.Speak` always closes
via defer.
