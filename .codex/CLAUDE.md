# Cattery — Project Instructions

## Project Tracking

- `koder/STATE.md` — source of truth for status, stack, open issues
- `koder/issues/NN_slug.md` — individual issue specs
- `koder/plans/` — technical plans
- `koder/reviews/` — turn-based review conversations

Always read `koder/STATE.md` at session start.

## Non-Negotiable: Zero External Dependencies

The entire system lives in two repos: `cattery` and `cattery-artefacts`. No download, fetch, or network call may target any host outside these two repos. CDN mirrors are allowed only if they serve copies of assets already in `cattery-artefacts`, and the GitHub raw URL must always remain a working fallback. Never introduce a third-party download dependency.

## Multi-Agent Dispatch (Harnex)

Use the global `harnex` skill for dispatch. No local docs — the skill is the single source of truth.

## Scratch State

- `tmp/TICK.md` — orchestration scratch file (current issue, phase, notes)
- `tmp/` is gitignored; cleared during `/close`
