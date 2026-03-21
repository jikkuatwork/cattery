---
name: open
description: Load project context from this repo's `koder/STATE.md` and related `koder/issues/*.md` files. Use when starting work here, when the user asks to "open", "read state", "read koder/STATE", "get context", "understand the repo first", "what's next", or wants a quick project-status summary before implementation.
---

# Open

Run this before normal repo discovery when `koder/STATE.md` exists.
Treat `koder/STATE.md` as the source of truth for status and priority.
Treat it as the restart anchor after context loss or a fresh session.

## Quick Start

1. Read `koder/STATE.md` first.
2. Read only the issue files that matter for the active request.
3. Open only the code paths needed for the active request.

## Workflow

1. Locate the repo root by searching upward for `koder/STATE.md`.
2. Read `koder/STATE.md` directly.
3. Summarize:
   - project goal and stack
   - current priorities from `What's Next`
   - open issues from the `Issues` table
   - status drift between `STATE.md` and `koder/issues/*.md`
   - any repo workflow notes needed to resume cleanly
   - constraints from `Key Decisions Made`
4. If the user asks about a specific area, then read the matching issue file in
   `koder/issues/` and only the relevant code.
5. If `STATE.md` conflicts with older issue notes, prefer `STATE.md` and call
   out the mismatch briefly.
6. If `koder/STATE.md` is missing, say so and fall back to normal repo
   discovery.

## Output Shape

Keep the first reply short. Cover:

- what the project is
- what is already done
- highest-value open work
- stale or conflicting tracking data
- what files you will read next for the user's actual task

## First Pass

For the first pass, read:

- `koder/STATE.md`
- the relevant files in `koder/issues/`

Do not add helper scripts for this. Keep the skill file-only and lightweight.
