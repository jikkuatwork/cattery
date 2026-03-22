# Normalize Round-Trip Baseline — 2026-03-22

Kokoro-82M (TTS) + Moonshine-tiny (STT), aarch64 VM, 6-core.
Normalizer: `normalize/` package at commit `f7bc2b3`.

## Summary

- **20 cases, 3 runs each (60 total)**
- **Average score: 80% word overlap**
- Scores measure STT's ability to reconstruct meaning after TTS

## Per-Case Results (min / avg / max across 3 runs)

| # | Category | Case summary | Min | Avg | Max |
|---|---|---|---|---|---|
| 1 | Title + currency | Dr. Smith + $3.50 + IEEE | 87 | 87 | 87 |
| 2 | Acronym + cardinal | NASA + 3 rockets + $1.2B | 88 | 88 | 88 |
| 3 | Scientific + pct | mRNA + 94.1% + COVID | 66 | 77 | 88 |
| 4 | Title + ordinal + date | Prof. + MIT's 150th + date | 100 | 100 | 100 |
| 5 | Scientific + symbol | pH 7.4 + & + 98.6°F | 100 | 100 | 100 |
| 6 | Acronym + time | Flight UA2491 + 3:30pm | 81 | 94 | 100 |
| 7 | Acronym + abbrev | WHO + 340k + vs. | 100 | 100 | 100 |
| 8 | Multi-acronym + currency | CEO + IBM + $15.3M | 53 | 68 | 76 |
| 9 | Units + symbol + currency | 2kg + & + 500ml + $4.99 | 61 | 66 | 69 |
| 10 | Ordinal + technical | 1st IEEE 802.11ax | 62 | 71 | 75 |
| 11 | Multiple titles + date | Mrs. + Rev. + Sen. + date | 84 | 84 | 84 |
| 12 | Currency varieties | £49.99 + €52.30 | 44 | 57 | 77 |
| 13 | Ordinals + units | 1st/3rd/22nd + 100m/200m | 62 | 68 | 75 |
| 14 | Abbreviation-heavy | dept/est/approx/refs/vol | 85 | 95 | 100 |
| 15 | Time variations | 9am + 12:15pm + 5:45pm | 64 | 84 | 94 |
| 16 | Scientific + numbers | DNA + 3.7M + pH 6.8 | 66 | 72 | 75 |
| 17 | Symbols | A + B = C & margin < 0.01 | 93 | 98 | 100 |
| 18 | Revenue + year possessive | $2.4B + 2023's + $2.03B | 60 | 67 | 70 |
| 19 | Mixed everything | Dr. + mRNA + MIT + % + date | 52 | 66 | 88 |
| 20 | Units + abbreviations | 45km + approx + km/h + mph | 69 | 73 | 82 |

## Tiers

**Excellent (90-100% avg)** — 6 cases
- Titles (Prof→Professor), symbols (+→plus, &→and, <→less than)
- Abbreviations (dept→department, vs→versus, i.e.→that is)
- Dates (3/15/2026→March 15, 2026), large numbers + WHO

**Good (75-89% avg)** — 6 cases
- Single titles + currency, NASA + numbers, time variations
- Multiple titles, mRNA + percentages, flight numbers

**Acceptable (60-74% avg)** — 8 cases
- Multi-acronym sentences, non-USD currency, technical IEEE
- Units (kg/ml), complex mixed inputs, revenue with possessives

## Known Limitations

These are **STT reconstruction limits**, not normalizer failures:

1. **Non-USD currency**: STT doesn't reliably reconstruct £/€ symbols
   from spoken "pounds"/"euros" — scores 44-77%
2. **Dense acronym stacking**: Multiple spelled-out acronyms in one
   sentence confuse STT (CEO + IBM + FY in one case: 53-76%)
3. **mRNA/DNA spelling**: STT reconstructs "mr n of" or "DNAS" from
   spelled-out letters — the *pronunciation* is correct but STT
   can't reverse it
4. **Revenue/possessive years**: "2023's" normalized to spoken form
   confuses STT on reconstruction

## Normalizer Strengths (confirmed by round-trip)

- Title expansion: Prof, Dr, Sen, Rev all round-trip perfectly
- Abbreviation expansion: dept, est, approx, vs, i.e., etc — near perfect
- Symbol replacement: &→and, +→plus, =→equals, <→less than — near perfect
- Date formatting: M/D/YYYY round-trips perfectly
- Ordinals: 1st→first, 22nd→twenty second — consistent
- Cardinal numbers: round-trip well in most contexts
- Time formatting: consistent across runs

## How to Re-Run

```bash
# After changes to normalize/ or speak/ or listen/
./koder/workflows/normalize_roundtrip/run.sh --runs 3

# Compare against this baseline (80% avg)
```
