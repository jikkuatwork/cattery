# 25 — Pure Go text normalizer for TTS preprocessing

## Status: open
## Priority: P1
## Depends on: nothing
## Blocks: nothing (but pairs naturally with #24 chunking)

## Problem

TTS models receive raw text but need spoken-form input. Acronyms like "IEEE" get phonemized as a single word instead of "I E E E". Numbers like "7.4" become "seven four" instead of "seven point four". Titles, currency, dates, and symbols all have written vs. spoken forms that diverge.

The Ruby spike (`tmp/normalize.rb`) proved the concept — acronyms, numbers, and titles round-trip dramatically better through TTS→STT after normalization. Now we need a proper Go implementation that lives inside the binary.

## Goal

A `normalize/` package that converts written English text into speakable form. Runs before phonemization in the `Speak()` pipeline. Zero external dependencies — ships in the cattery binary.

## Design

### Package: `normalize/`

```go
package normalize

// Normalize converts written text to spoken form for TTS.
func Normalize(text string) string
```

Single function, no options for v1. English-only (matches Kokoro).

### Normalization pipeline (ordered)

Each rule is a pass over the text. Order matters — early passes create text that later passes consume.

1. **Titles/honorifics**: `Dr.` → `Doctor`, `Mr.` → `Mister`, `Prof.` → `Professor`, etc.
2. **Currency**: `$3.50` → `three dollars and fifty cents`, `€100` → `one hundred euros`
3. **Percentages**: `340%` → `three hundred and forty percent`
4. **Decimal numbers**: `7.4` → `seven point four`, `3.14159` → `three point one four one five nine`
5. **Ordinals**: `1st` → `first`, `23rd` → `twenty third`
6. **Cardinal numbers**: `340` → `three hundred and forty` (standalone integers)
7. **Dates**: `3/22/2026` → `March twenty second, twenty twenty six`
8. **Times**: `3:30pm` → `three thirty p m`
9. **Mixed-case scientific**: `mRNA` → `M R N A`, `pH` → `P H`, `DNA` → `D N A`
10. **Possessive acronyms**: `MIT's` → `M I T's`
11. **Acronyms**: `IEEE` → `I E E E` (all-caps 2+ letters, spell out). Exception list for words pronounced as words: NASA, NATO, ASAP, LASER, RADAR, SCUBA, UNESCO, AIDS, etc.
12. **Symbols**: `&` → `and`, `@` → `at`, `#` → `number`, `+` → `plus`, `=` → `equals`
13. **Units**: `km` → `kilometers`, `kg` → `kilograms`, `mph` → `miles per hour`
14. **Common abbreviations**: `etc.` → `et cetera`, `vs.` → `versus`, `approx.` → `approximately`

### Data files

- `normalize/dict.go` — static maps compiled into binary:
  - `titles` map (~20 entries)
  - `spokenAsWord` set (~30 entries: NASA, NATO, etc.)
  - `symbols` map (~15 entries)
  - `units` map (~30 entries)
  - `abbreviations` map (~50 entries)
  - `currencies` map (~10 entries: $, €, £, ¥)

No external JSON files. Everything is Go source — keeps the binary self-contained.

### Number verbalization (`normalize/numbers.go`)

Port the logic from the Ruby spike. English cardinal and ordinal numbers up to 999,999,999.

```go
func cardinal(n int64) string    // 340 → "three hundred and forty"
func ordinal(n int64) string     // 23 → "twenty third"
func decimal(s string) string    // "7.4" → "seven point four"
func year(n int) string          // 2026 → "twenty twenty six"
```

### Integration point

In `speak/kokoro/kokoro.go`, before phonemization:

```go
func (e *Engine) Speak(w io.Writer, text string, opts speak.Options) error {
    text = normalize.Normalize(text)  // ← new
    phonemes, err := p.Phonemize(text)
    // ...
}
```

Or better: in the `speak.Engine` interface contract, normalization is engine-internal (like phonemization). Each engine decides whether and how to normalize.

### What NOT to build

- Multi-language support (English-only for now)
- Context-aware disambiguation ("read" as past/present, "lead" as noun/verb)
- Phonetic pronunciation dictionaries (that's espeak-ng's job)
- Anything requiring ML inference

## File changes

- **Create**: `normalize/normalize.go` — main pipeline + `Normalize()` function
- **Create**: `normalize/numbers.go` — cardinal, ordinal, decimal, year verbalization
- **Create**: `normalize/dict.go` — static dictionaries (titles, symbols, units, abbreviations, acronyms)
- **Edit**: `speak/kokoro/kokoro.go` — call `normalize.Normalize()` before phonemization

## Acceptance criteria

- [ ] `normalize.Normalize("Dr. Smith paid $3.50")` → `"Doctor Smith paid three dollars and fifty cents"`
- [ ] `normalize.Normalize("The IEEE standard")` → `"The I E E E standard"`
- [ ] `normalize.Normalize("NASA launched")` → `"NASA launched"` (spoken-as-word exception)
- [ ] `normalize.Normalize("mRNA at MIT's lab")` → `"M R N A at M I T's lab"`
- [ ] `normalize.Normalize("7.4% of 1,000")` → `"seven point four percent of one thousand"`
- [ ] `normalize.Normalize("3/22/2026")` → `"March twenty second, twenty twenty six"`
- [ ] Round-trip test: hard text → normalize → TTS → STT shows significant improvement over raw
- [ ] `go build ./...` and `go vet ./...` pass
- [ ] Zero external dependencies
- [ ] Binary size increase < 50KB

## Notes

- The Ruby spike (`tmp/normalize.rb`) is the reference implementation. Port the logic, not the code.
- espeak-ng already normalizes some things (basic numbers, common abbreviations). Our normalizer handles the cases espeak gets wrong or doesn't attempt (acronyms, currency phrasing, mixed-case scientific terms). There may be some overlap — that's fine, double-normalizing "Doctor" is harmless.
- Future: if multilingual support is needed, NVIDIA NeMo's WFST grammars can be compiled to `.far` files and shipped as downloadable artefacts (like models). The Go normalizer covers the 80% case for English.
- Pairs with #24 (chunking): normalize first, then chunk, then synthesize.
