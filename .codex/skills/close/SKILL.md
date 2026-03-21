---
name: close
description: Close out work in this repo cleanly. Use when the user asks to wrap up, leave the workspace tidy, prepare a handoff, update status/state files, summarize progress, or explicitly invokes `$close` to update state and commit a coherent session before stopping.
---

# Close

Leave the workspace easy to resume. Prefer a short, accurate handoff over a long retrospective.

## Workflow

### 1. Rebuild the current state

- Read project instructions that affect closeout if they exist.
- Check `git status --short` before making more edits.
- Identify the files that actually changed during the session.
- Find any session trackers the repo uses, such as `koder/STATE.md`, issue notes, changelogs, or task files.

### 2. Tidy the repo

- Finish any small, obvious follow-through that is directly implied by the work already done.
- Update state files so they describe the current truth, not the plan from earlier in the session.
- Keep tracker edits concrete: what changed, what is still blocked, and what should happen next.
- Do not create extra documentation unless it materially improves handoff quality.

### 3. Verify what matters

- Run the smallest useful validation for the work that changed.
- Prefer targeted checks over broad test suites unless the change was broad.
- Record what was verified and what was not run.
- If validation fails, either fix it or say clearly that the session is not in a clean state.

### 4. Commit when asked, or when `$close` is explicitly invoked

- Treat an explicit `$close` invocation as a request to review the diff,
  update the repo tracker if needed, commit a coherent session, and leave the
  worktree clean unless the user says otherwise.
- Review the diff before committing.
- Keep unrelated user changes intact.
- Use a specific commit message that reflects the actual work.
- After committing, re-check `git status --short` and report whether the tree is clean.

### 5. Produce the handoff

End with a concise closeout that covers:

- What changed
- What was verified
- What remains open or risky
- What the next sensible step is
- Whether the worktree is clean or still dirty

## Project Tracker Guidance

- If a repo has a canonical state file such as `koder/STATE.md`, update it when priorities, issue status, blockers, or important decisions changed during the session.
- If an issue note or task file was materially advanced, sync its status with the code and docs.
- Keep state files terse. They should help the next session start fast.

## Quality Bar

- Do not say the session is wrapped if the repo state is ambiguous.
- Do not hide skipped tests, open blockers, or uncommitted changes.
- Prefer exact file names and concrete next steps over general advice.
