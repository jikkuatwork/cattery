# Plan 33 — Empirical RSS Validation (#27 close-out)

## Why

Issue #27's code work is done (plans 28-30 landed TTS streaming, STT streaming,
and chunk-size infra). The remaining acceptance criteria require empirical
validation on constrained systems: Pi4 (4GB) and 1GB VPS. This plan closes #27.

## Depends on

Plan 32 (RSS investigation). Thresholds must be calibrated and ratios confirmed
≤1.2 before running the final validation matrix.

## Validation matrix

| Environment | RAM | Method | TTS 3-min | STT 3-min | Notes |
|---|---|---|---|---|---|
| Dev VM (ARM64) | 16 GB | native | baseline | baseline | Already have numbers from plan 32 |
| Simulated 4 GB | 4 GB | cgroup memory limit | must complete | must complete | Pi4 proxy |
| Simulated 1 GB | 1 GB | cgroup memory limit | ≥1 min | must complete | $6 VPS proxy |
| Simulated 512 MB | 512 MB | cgroup memory limit | warn + proceed | warn + proceed | Edge case |

### Why cgroups, not QEMU

`systemd-run --scope -p MemoryMax=4G` (or raw cgroup v2 `memory.max`) caps
RSS at the process level on the same kernel. This is faster and more
reproducible than QEMU with `-m 4G`, which also limits page cache and kernel
buffers. For user-space RSS validation, cgroup limits are the right tool.

## Steps

### Step 1 — Set up constrained runner script

Create `scripts/memtest-constrained.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

MEM=${1:-4G}
echo "=== memtest under ${MEM} memory limit ==="
systemd-run --scope -p MemoryMax="$MEM" \
    go test -tags memtest ./memtest/ -v -timeout 600s 2>&1 | tee "tmp/memtest-${MEM}.log"
```

This captures full output for analysis. The script is a convenience — the
validation can also be run manually.

### Step 2 — Run validation matrix

Run the script at each memory level:

```bash
scripts/memtest-constrained.sh 4G
scripts/memtest-constrained.sh 1G
scripts/memtest-constrained.sh 512M
```

For each run, record:
- Pass/fail per test
- Peak RSS per test
- Whether OOM-killer fired (check `dmesg`)
- Whether the low-memory warning appeared on stderr (512M case)
- Auto-selected chunk size at each level

### Step 3 — Evaluate results

**4 GB (Pi4 proxy):**
- All four tests must pass.
- Peak RSS for TTS and STT long tests must stay under the calibrated thresholds.
- Auto chunk size should be in the 15-20s range.

**1 GB (VPS proxy):**
- STT short and long must complete.
- TTS short must complete; TTS long (3 min) may OOM — this is acceptable per
  #27's criteria ("TTS completes at least 1-min").
- If TTS long OOMs, re-run with shorter text to confirm 1-min works.
- Auto chunk size should be 10-15s.

**512 MB:**
- Low-memory warning must appear on stderr.
- Tests should proceed with 10s chunks (the floor).
- OOM is acceptable but must produce a clean error, not a stack trace.

### Step 4 — Update issues and close

If all criteria pass:

1. Check off remaining #27 acceptance criteria with observed numbers
2. Update `koder/STATE.md`:
   - Move #27 to **done** with a note on validated environments
   - Update "What's Next" to remove #27 references
3. Close #27

If any criterion fails, document the failure and create a follow-up issue
rather than leaving #27 open indefinitely.

## Files changed

- **Create**: `scripts/memtest-constrained.sh` — convenience runner
- **Edit**: `koder/issues/27_bounded_memory_streaming.md` — check acceptance boxes
- **Edit**: `koder/STATE.md` — mark #27 done, update What's Next

## Acceptance criteria (remaining from #27)

- [ ] 3-min TTS synthesis peaks at ≤350 MB RSS (or calibrated threshold)
- [ ] 3-min STT transcription peaks at ≤250 MB RSS (or calibrated threshold)
- [ ] Pi4 4GB: both TTS and STT complete 3-min clip without OOM
- [ ] 1GB VPS: STT completes 3-min clip; TTS completes at least 1-min
- [ ] No regression on short audio (<30s) — same path, same memory
