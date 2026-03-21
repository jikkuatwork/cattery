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

Create the worktree, then launch the agent via harnex from the worktree
directory.

```bash
# IMPORTANT: Commit all untracked files the agent will need BEFORE
# creating the worktree. Worktrees branch from the current commit —
# untracked files (issues, plans, reviews) won't carry over.
git status
# If dirty: commit issues/plans on main first
git add koder/issues/16_extract_ort_runtime.md  # etc.
git commit -m "Add issue #16: extract ORT runtime"

# Create worktree and branch (from main)
ISSUE_NUM=16
ISSUE_SLUG="extract-ort-runtime"
WORKTREE="/home/kodeman/Projects/cattery-issue-${ISSUE_NUM}-${ISSUE_SLUG}"

git worktree add ${WORKTREE} -b issue/${ISSUE_NUM}_${ISSUE_SLUG} main
```

Launch the agent. **Must cd into the worktree first** — Claude Code has no
`--cd` flag, and Codex's `--cd` registers the session under the worktree repo
root (which breaks cross-repo `harnex pane` lookups).

```bash
# For Codex (implementation)
cd ${WORKTREE}
harnex run codex --id cx-impl-${ISSUE_NUM} --tmux cx-impl-${ISSUE_NUM} \
  --context "Implement koder/issues/${ISSUE_NUM}_*.md. Run go build ./... and go vet ./... when done. Commit after each phase."

# For Claude (review)
cd ${WORKTREE}
harnex run claude --id cl-rev-${ISSUE_NUM} --tmux cl-rev-${ISSUE_NUM} \
  --context "Read and execute /tmp/review-task.md"
```

For complex review tasks, write the instructions to a temp file first:

```bash
cat > /tmp/review-task.md <<'EOF'
Review the implementation of issue NN against the spec.
Read koder/issues/NN_slug.md and check all acceptance criteria.
Write your review to koder/reviews/NN_slug/01_claude.md.
EOF
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

**Cross-repo caveat**: If the session was launched from a worktree, ALL
`harnex` commands (`pane`, `stop`, `status`) must run from the same
directory, or pass `--repo <worktree-path>`. Running from main will fail
with "session not found" even though the session is running.

When polling results arrive, check for:
- **Still working**: agent is reading files, running tests, editing code
- **At prompt**: agent finished — read the last output to see results
- **Error/stuck**: agent hit a blocker — may need manual intervention

### 3. Stop

When the agent is done (at prompt, work committed). **Always run from the
worktree directory**:

```bash
cd ${WORKTREE}
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
1. Create worktree + branch from main
2. harnex run codex --id cx-impl-NN --tmux cx-impl-NN (from worktree)
3. Poll with harnex pane every 3-4 min
4. When done: harnex stop, verify commits
5. harnex run claude --id cl-rev-NN --tmux cl-rev-NN (review)
6. Poll with harnex pane every 4-5 min
7. When done: harnex stop, read review file
8. If NEEDS FIXES: harnex run codex --id cx-fix-NN (fix pass)
9. If PASS: merge to main, clean up worktree + branch
```

### Cattery issue chain (#16-#21)

The restructure chain has dependencies. Execute in order, merging each
to main before starting the next:

```bash
# Issue #16: Extract ORT runtime (foundation, no deps)
ISSUE_NUM=16; ISSUE_SLUG="extract-ort-runtime"
WORKTREE="/home/kodeman/Projects/cattery-issue-${ISSUE_NUM}-${ISSUE_SLUG}"
git worktree add ${WORKTREE} -b issue/${ISSUE_NUM}_${ISSUE_SLUG} main
cd ${WORKTREE}
harnex run codex --id cx-impl-${ISSUE_NUM} --tmux cx-impl-${ISSUE_NUM} \
  --context "Implement koder/issues/16_extract_ort_runtime.md. Run go build ./... and go vet ./... when done. Commit."
# Watch → stop → review → merge to main

# Issue #17: TTS interface (depends on #16 merged)
ISSUE_NUM=17; ISSUE_SLUG="tts-engine-interface"
# ... same pattern, branch from updated main

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
- **Never** pass `-- --cd <path>` to Claude sessions (unsupported flag)
- **Never** poll with raw `tmux capture-pane` — use `harnex pane`
- **Never** rely on `--wait-for-idle` alone — always use Fire & Watch

## Checking Status

```bash
harnex status           # current repo sessions
harnex status --all     # all repos
```
