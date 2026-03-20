# 03 — WAV Writer (Pure Go)

**Status**: open
**Priority**: P1

## Goal

Write a minimal WAV file writer. Pure Go, no dependencies.

## Acceptance criteria

- [ ] Writes valid WAV from float32 PCM samples
- [ ] Configurable sample rate (default 24kHz for KittenTTS)
- [ ] Mono, 16-bit output
- [ ] Plays correctly in standard audio players
- [ ] Zero external dependencies
