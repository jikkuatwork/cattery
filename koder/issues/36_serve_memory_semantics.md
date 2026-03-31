# 36 — Serve --memory semantics

## Status: open
## Priority: P3

## Context

The `--memory` flag was added to `cattery serve` to control cross-pool engine
eviction. Currently it acts as a co-residency hint: if the sum of loaded
engines fits within the budget, skip eviction when switching modalities.

When `--memory` is omitted, `MemoryBudget` is 0 and eviction always happens
(safe default for Pi4). No upper bound is enforced — a single engine can
consume all available RAM.

## Open questions

- Should `--memory` also act as a hard ceiling (refuse to load if budget
  exceeded), or is co-residency control sufficient?
- In practice the server can't refuse a request — if `/v1/chat/completions`
  arrives it must load the LLM. So a hard ceiling would mean returning 503,
  which may not be desirable.
- Should the default (no flag) infer available system RAM via `preflight`
  and auto-set a budget, rather than defaulting to "always evict"?
- Should the flag name be `--memory` or something more descriptive like
  `--memory-budget` or `--max-memory`?

## Decision needed

Think through the UX and decide whether the current "co-residency hint"
behaviour is the right abstraction, or whether it should evolve.
