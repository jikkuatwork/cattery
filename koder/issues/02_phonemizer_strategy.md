# 02 — Phonemizer Strategy Decision

**Status**: open
**Priority**: P0

## Goal

Choose and prove a phonemization approach that works in pure Go (no cgo).

## Options

1. **wazero + espeak WASM** -- most promising for zero-dep goal
2. **os/exec espeak-ng** -- simplest, requires system install
3. **Lexicon lookup + fallback** -- fast but incomplete
4. **Pure Go implementation** -- huge effort, unlikely

## Acceptance criteria

- [ ] Decision documented with rationale
- [ ] Spike proves chosen approach works end-to-end
- [ ] Latency measured (must not dominate overall RTF)
- [ ] Text -> IPA phonemes -> KittenTTS token IDs verified correct

## Dependencies

- Depends on issue 01 (need to know what token format the model expects)
