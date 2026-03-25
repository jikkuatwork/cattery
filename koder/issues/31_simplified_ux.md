# 31 — Simplified default UX with --advanced escape hatch

## Status: open
## Priority: P2
## Depends on: nothing

## Problem

Cattery has one TTS model and one STT model. Exposing model selection,
registry details, and `cattery list` in the default UX adds complexity that
>90% of users don't need. The experience should feel like "one tool, one
model" by default.

## Design

### CLI

- `cattery list` hidden from default help; shown with `--advanced` or `cattery help --advanced`
- Default `cattery tts "Hello"` — no model flag needed (already the case)
- `--model` flag still works but not shown in default help
- `--advanced` reveals: model selection, `list` subcommand, registry details

### Server API

- `/v1/models` and `/v1/voices` still exist (no breaking change) but not
  prominent in docs
- Status response keeps model info (useful for debugging)

### Principle

Default user sees: `cattery tts "text"`, `cattery stt audio.wav`, `cattery serve`.
Advanced user adds `--advanced` to see everything else.
Single model per modality is the mental model. Multi-model plumbing stays
internal for when a second model ships.
