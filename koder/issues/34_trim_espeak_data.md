# 34 — Trim espeak-ng-data to English-only

## Status: **dropped** (2026-03-30 — keeping all 117 languages; no longer embedding so size cost is negligible)
## Priority: P3

## Problem

The bundled `data.tar.gz` is 9.2MB (20MB uncompressed) because it includes dictionaries for all 117 languages. Cattery currently only uses `en-us` for Kokoro phonemization.

## Opportunity

English-only core files (en_dict + phondata + phonindex + phontab + intonations) total 852KB uncompressed (~500KB compressed). Trimming would reduce the per-build embed cost from ~10MB to ~1.5MB.

## Approach

Add a step to `build-espeak.yml` that produces a `data-en.tar.gz` containing only the core phoneme tables + English dictionary. Switch the `go:embed` to use it.

## Notes

- If multi-language TTS support is ever added, this would need revisiting.
- Could offer both: English-only by default, full language pack as optional download.
