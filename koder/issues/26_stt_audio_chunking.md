# 26 — STT audio chunking to prevent hallucination on long input

## Status: open
## Priority: P1
## Depends on: #20 (listen package)
## Blocks: nothing

## Problem

Moonshine-tiny hallucinates on audio longer than ~30 seconds. STT output
degrades into repetitive loops ("the field of the field of the field...")
or fixates on a single token ("15-15-15-15-15..."). This was confirmed
during normalize round-trip testing: individual chunks (2-30s) transcribe
near-perfectly, but combined audio (76s+) produces garbage.

This is a fundamental limitation of small STT models with limited context
windows, not a bug in the audio or TTS output.

## Goal

Make `Transcribe()` transparently handle audio of any length by splitting into
~30-second segments, transcribing each independently, and concatenating
the text output. No API change — callers still pass full audio.

## Design

### Chunking strategy

1. **Fixed window with silence detection**: split at ~30s boundaries, but
   prefer to cut at silence gaps (< -40dB for > 200ms) within a ±3s
   search window around each 30s mark
2. **Overlap**: include 0.5s overlap between segments to avoid cutting
   words mid-utterance. Trim duplicate words from overlap region
3. **Fallback**: if no silence found in the search window, hard-cut at 30s

### Where to chunk

Inside `moonshine.Engine.Transcribe()`, after `audio.ReadPCM` and resampling, before inference:

```
audio → detect length → if > threshold: chunk audio → for each chunk: transcribe → concatenate text
```

### Parameters

- `chunkDuration`: 30s (default) — Moonshine's sweet spot
- `searchWindow`: ±3s around each cut point
- `silenceThreshold`: -40dB RMS
- `silenceMinDuration`: 200ms
- `overlapDuration`: 0.5s

### Edge cases

- Audio shorter than 30s: no chunking, pass through directly
- Pure silence segments: skip, don't transcribe
- Very quiet audio: lower silence threshold to -50dB
- Overlap dedup: suffix-prefix word match (not general LCS) — find the
  longest suffix of chunk[i] text that equals a prefix of chunk[i+1]
  text, capped at 8 words. At 0.5s overlap expect ~1-2 words.

## File changes

- **Create**: `listen/moonshine/chunk.go` — audio chunking + silence detection
- **Edit**: `listen/moonshine/moonshine.go` — wire chunking into `Transcribe()`
- **Maybe create**: `listen/moonshine/chunk_test.go` — unit tests

## Acceptance criteria

- [ ] `cattery listen long_audio.wav` works for 60s+ audio without hallucination
- [ ] Short audio (< 30s) is unchanged — no regression
- [ ] Chunk boundaries don't split words
- [ ] `go build ./...` and `go vet ./...` pass
- [ ] Round-trip test: long TTS → STT produces coherent text (no repetition loops)

## Notes

- 30s is conservative; Moonshine may handle up to 45-60s on some inputs.
  Start conservative, can increase later based on testing.
- Silence detection operates on normalized `[]float32` PCM (16kHz after
  resampling), no FFT needed — just sliding-window RMS in dBFS.
  Chunk sample counts always use `e.sampleRate` (16000), not the
  original input rate.
- Future: VAD (voice activity detection) would be more robust than
  amplitude-based silence detection, but adds complexity. Simple
  approach first.
- The TTS chunking in speak/kokoro/chunk.go is the mirror of this —
  same pattern, different domain.
