# Normalize Round-Trip Test

Manual/semi-automated TTS→STT round-trip test for the `normalize/` package.

## Why

TTS quality after normalization can't be tested deterministically — the STT
output varies slightly between runs, and "similarity" is subjective. This
workflow provides test sentences, expected spoken forms, and a script to run
the round-trip and score results.

## How it works

1. `cases.txt` defines test cases: input text + expected spoken form
2. `run.sh` loops through cases, runs `cattery speak` → `cattery listen`
3. Each case produces: raw STT output, similarity score (word overlap %)
4. Results are appended to `results/` with timestamps
5. Run multiple times to check consistency

## Usage

```bash
# Single run (all cases)
./koder/workflows/normalize_roundtrip/run.sh

# Multiple runs for consistency
./koder/workflows/normalize_roundtrip/run.sh --runs 3

# Without normalization (baseline comparison)
SKIP_NORMALIZE=1 ./koder/workflows/normalize_roundtrip/run.sh
```

## Reading results

Each run produces a timestamped file in `results/`:

```
=== Run 1/3 at 2026-03-22T10:30:00 ===
CASE: Dr. Smith paid $3.50 for IEEE membership
EXPECTED: Doctor Smith paid three dollars and fifty cents for I E E E membership
GOT:      doctor smith paid three dollars and fifty cents for i triple e membership
SCORE:    82% word overlap
```

Scores above 70% generally indicate the normalizer is helping. Compare
with and without `SKIP_NORMALIZE=1` to measure the delta.

## Adding cases

Edit `cases.txt` — one case per block, separated by blank lines:

```
INPUT: your test text here
EXPECT: the expected spoken form
```

Focus on cases where raw text fails badly: acronyms, numbers, currency,
dates, mixed-case scientific terms.
