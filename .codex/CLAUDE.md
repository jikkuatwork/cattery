# Cattery — Project Instructions

## Project Tracking

- `koder/STATE.md` — source of truth for status, stack, open issues
- `koder/issues/NN_slug.md` — individual issue specs
- `koder/plans/` — technical plans
- `koder/reviews/` — turn-based review conversations

Always read `koder/STATE.md` at session start.

## Multi-Agent Dispatch (Harnex)

Full docs: `koder/workflows/harnex/README.md` and `koder/workflows/harnex/dispatch.md`

Harnex is the **only** way to launch another coding agent from Claude Code.
Never use raw `tmux send-keys` or direct CLI spawning.

### Fire & Watch pattern

1. **Spawn**: create worktree, `cd` into it, `harnex run` with `--tmux`
2. **Watch**: poll with `harnex pane --id <ID> --lines 50` every 3-5 min
3. **Stop**: `harnex stop --id <ID>` when done, verify commits

### Naming

| Step | ID | tmux window |
|---|---|---|
| Implement | `cx-impl-NN` | `cx-impl-NN` |
| Review | `cl-rev-NN` | `cl-rev-NN` |
| Fix | `cx-fix-NN` | `cx-fix-NN` |

### Key rules

- Always `--tmux <same-as-id>` (name must match)
- Always run harnex commands from the worktree directory
- Commit issue/plan files on main before creating worktree
- Merge each issue to main before starting dependent issues

## Scratch State

- `tmp/TICK.md` — orchestration scratch file (current issue, phase, notes)
- `tmp/` is gitignored; cleared during `/close`
