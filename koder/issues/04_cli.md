# 04 — CLI with Model Download Support

**Status**: open
**Priority**: P1

## Goal

`cattery --text "hello" --voice Leo --output out.wav`

## Acceptance criteria

- [ ] CLI parses text, voice, output flags
- [ ] Downloads model files on first run (or via `cattery download`)
- [ ] Lists available voices via `cattery voices`
- [ ] Streams output for long text (not buffer-all-then-write)

## Dependencies

- Depends on issues 01, 02, 03
