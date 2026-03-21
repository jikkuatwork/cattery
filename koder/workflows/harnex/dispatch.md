---
title: Dispatching with Harnex
description: Launch and monitor coding agents via harnex Fire & Watch
updated: 2026-03-22
---

# Dispatching with Harnex

Harnex is the **only** way to launch another coding agent from Claude Code.
Never use raw `tmux send-keys`, `c-zai-dangerous`, or direct CLI spawning.

## Why Harnex Only

- Session registry tracks what's running, where, and for which repo
- `harnex pane` gives clean screen snapshots without PTY escape noise
- `harnex stop` sends the correct exit sequence per CLI type
- `harnex status` shows all live sessions across repos
- No orphaned processes, no lost tmux windows

## The Pattern: Fire & Watch

Every dispatch follows the same three phases: **spawn**, **watch**, **stop**.

### 1. Spawn

All work happens directly on **main**. No worktrees, no feature branches.
This is a personal repo — commit directly to main after each issue.

```bash
# Ensure main is clean before launching
git status

# Launch from repo root
cd /home/kodeman/Projects/cattery
harnex run codex --id cx-impl-${ISSUE_NUM} --tmux cx-impl-${ISSUE_NUM} \
  --context "Implement koder/issues/${ISSUE_NUM}_*.md. Run go build ./... and go vet ./... when done. Commit when complete."
```

### 2. Watch

Poll the agent's screen via `harnex pane`. Run this in a background task
with a sleep interval appropriate for the work:

- **Implementation**: poll every 3-4 minutes
- **Review**: poll every 4-5 minutes
- **Quick fix**: poll every 1-2 minutes

```bash
# Single check
harnex pane --id cx-impl-16 --lines 50

# Background poll (from Claude Code)
sleep 180 && harnex pane --id cx-impl-16 --lines 50
```

When polling results arrive, check for:
- **Still working**: agent is reading files, running tests, editing code
- **At prompt**: agent finished — read the last output to see results
- **Error/stuck**: agent hit a blocker — may need manual intervention

### 3. Stop

When the agent is done (at prompt, work committed):

```bash
harnex stop --id cx-impl-16
```

## Naming Conventions

| Step | ID pattern | tmux window | Example |
|------|-----------|-------------|---------|
| Implement | `cx-impl-NN` | `cx-impl-NN` | `cx-impl-16` |
| Review | `cl-rev-NN` | `cl-rev-NN` | `cl-rev-16` |
| Fix | `cx-fix-NN` | `cx-fix-NN` | `cx-fix-16` |

**Rule**: Always use `--tmux <same-as-id>` so the tmux window name matches
the session ID. Never use a different tmux name — it breaks `harnex pane`.

## Full Dispatch Lifecycle (Cattery Issue)

```
1. Ensure main is clean
2. harnex run codex --id cx-impl-NN --tmux cx-impl-NN (from repo root)
3. Poll with harnex pane every 3-4 min
4. When done: harnex stop, verify go build/vet pass
5. Next issue
```

No worktrees, no feature branches, no review cycles. Commit directly to main.

### Cattery issue chain (#16-#21)

The restructure chain has dependencies. Execute in order, each committing
directly to main before starting the next:

```bash
cd /home/kodeman/Projects/cattery

# Issue #16: Extract ORT runtime (foundation, no deps)
harnex run codex --id cx-impl-16 --tmux cx-impl-16 \
  --context "Implement koder/issues/16_extract_ort_runtime.md. Run go build ./... and go vet ./... when done. Commit."
# Watch → stop → next

# Issue #17: TTS interface (depends on #16 committed)
# Issue #18: Registry redesign
# Issue #20: STT package
# Issue #19: CLI redesign
# Issue #21: Server API redesign
```

Issues #22 (bundle espeak) and #23 (OpenAI remote engines) can run in
parallel — they don't depend on the #16-#21 chain.

## What NOT to Do

- **Never** launch agents with raw `tmux send-keys` or `tmux new-window`
- **Never** use `c-zai-dangerous` or `claude` directly in tmux
- **Never** use `--tmux NAME` where NAME differs from `--id`
- **Never** poll with raw `tmux capture-pane` — use `harnex pane`
- **Never** rely on `--wait-for-idle` alone — always use Fire & Watch
- **Never** create worktrees or feature branches — work directly on main

## Checking Status

```bash
harnex status           # current repo sessions
harnex status --all     # all repos
```
