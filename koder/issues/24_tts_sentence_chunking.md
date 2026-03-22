# 24 — Transparent sentence chunking for long text TTS

## Status: open
## Priority: P1
## Depends on: #17 (speak interface)
## Blocks: nothing

## Problem

Kokoro-82M has a hard 510-token positional encoding limit. ~200-300 characters of English text fills this window. Longer text silently fails or produces garbage. Users expect `cattery speak` to handle arbitrary-length input.

Every production TTS system (OpenAI, ElevenLabs, Google) chunks internally — the model limit is an implementation detail, not a user-facing constraint.

## Goal

Make `Speak()` transparently handle text of any length by splitting into sentence-sized chunks, synthesizing each within the 510-token window, and concatenating the audio output. No API change — callers still pass full text.

## Design

### Chunking strategy

1. Split text on sentence boundaries: `.` `!` `?` followed by space or end
2. If a single sentence exceeds the token budget, split on clause boundaries: `,` `;` `:` `—`
3. If a clause still exceeds, split on word boundaries (last resort)
4. Target: each chunk should tokenize to ≤ 480 tokens (leave 30-token margin for padding)

### Where to chunk

Inside `kokoro.Speak()`, after phonemization but before tokenization:

```
text → phonemize → chunk phonemes → for each chunk: tokenize → load style → synthesize → collect samples
```

Chunking on phonemes (not raw text) is more accurate — token count maps directly to phoneme length. But chunking on raw text before phonemization is simpler and nearly as good, since espeak-ng roughly preserves sentence boundaries.

**Recommendation**: chunk on raw text at sentence boundaries, then verify token count after phonemization. Re-split if a chunk exceeds the budget. Simple first pass.

### Audio stitching

Concatenate raw float32 samples from each chunk. Insert a small silence gap between chunks (50-100ms, ~1200-2400 samples at 24kHz) to sound natural at sentence boundaries. Write one WAV header for the combined output.

### Style vector per chunk

Each chunk gets its own style vector lookup based on its token count. This is correct — the voice file maps token position to style, and each chunk is an independent synthesis pass.

### Edge cases

- Single sentence that exceeds 510 tokens: split on commas/clauses
- Text with no sentence punctuation: split on word boundaries at ~400 tokens
- Empty chunks after splitting: skip
- Trailing whitespace/punctuation: trim per chunk

## File changes

- **Edit**: `speak/kokoro/kokoro.go` — add chunking in `Speak()`, extract `synthesizeChunk()`
- **Maybe create**: `speak/kokoro/chunk.go` — sentence splitter + token budget checker

## Acceptance criteria

- [ ] `cattery speak "very long paragraph..."` works for 1000+ character input
- [ ] Sentence boundaries produce natural pauses
- [ ] Short text (under 510 tokens) is unchanged — no regression
- [ ] Chunk boundaries don't cut words mid-phoneme
- [ ] `go build ./...` and `go vet ./...` pass
- [ ] Round-trip test: long TTS → STT produces coherent transcription

## Notes

- 510 is the voice file dimension (rows in the style matrix), matching Kokoro's positional encoding
- The margin (480 target vs 510 max) accounts for the +2 padding tokens added in `synthesize()`
- Future: streaming synthesis could yield audio chunk-by-chunk instead of buffering all samples. Out of scope here.
- The normalizer spike (`tmp/normalize.rb`) is a separate concern — it preprocesses text *before* chunking
