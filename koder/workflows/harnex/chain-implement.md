---
title: Chain-Implement Workflow
description: Serial implement → review → fix cycle for multi-plan work
updated: 2026-03-22
---

# Chain-Implement Workflow

When multiple plans need to land in sequence (or parallel-then-merge), each
plan goes through a three-phase cycle before the next one starts.

## The Cycle

```
For each plan:
  1. IMPLEMENT  — harnex(codex)  cx-impl-NN
  2. REVIEW     — harnex(claude) cl-rev-NN
  3. FIX        — harnex(codex)  cx-fix-NN
  4. VERIFY     — go build/vet/test from orchestrator
  5. COMMIT     — orchestrator confirms clean commit
```

### Phase 1: Implement

```bash
harnex run codex --id cx-impl-NN --tmux cx-impl-NN \
  --context "Implement koder/plans/plan_NN_slug.md. Follow the work plan steps in order. Run go build ./... and go vet ./... when done. Commit when complete."
```

Poll with `harnex pane` every 3-4 min. Wait for commit.

### Phase 2: Review

```bash
harnex run claude --id cl-rev-NN --tmux cl-rev-NN \
  --context "Review the implementation of koder/plans/plan_NN_slug.md. Read the plan, then read every changed file. Write a review to koder/reviews/NNN_slug/01_claude.md following the Implementation Review format (Completeness, Acceptance Criteria with evidence, Security, Code Quality, Verdict). Run go test ./... to verify tests pass. Commit the review file."
```

Poll every 4-5 min. Wait for review verdict.

### Phase 3: Fix

Only if review has P1 or P2 findings:

```bash
harnex run codex --id cx-fix-NN --tmux cx-fix-NN \
  --context "Fix the findings in koder/reviews/NNN_slug/01_claude.md. Address all P1 and P2 items. Run go build ./... go vet ./... go test ./... when done. Commit fixes."
```

If review is PASS with no P1/P2, skip this phase.

### Phase 4: Verify

Orchestrator (Claude Code main session) runs:

```bash
go build ./...
go vet ./...
go test ./...
git log --oneline -5
```

Confirms clean state before moving to the next plan.

## Multi-Plan Ordering

### Independent plans (parallel-safe)

When plans don't depend on each other, run their implement phases in
parallel, then review/fix sequentially:

```
Plan A: IMPLEMENT ─────────────────┐
Plan B: IMPLEMENT ─────────────────┤
                                   ▼
                            Merge to main
                                   │
Plan A: REVIEW → FIX → VERIFY ────┤
Plan B: REVIEW → FIX → VERIFY ────┤
                                   ▼
                              Next batch
```

### Dependent plans (serial)

When plan C depends on A and B, wait for both to land:

```
Plan A: IMPLEMENT → REVIEW → FIX → VERIFY → ✓
Plan B: IMPLEMENT → REVIEW → FIX → VERIFY → ✓
Plan C: IMPLEMENT → REVIEW → FIX → VERIFY → ✓
```

## Naming

| Phase | ID | tmux window |
|-------|-----|-------------|
| Implement | `cx-impl-NN` | `cx-impl-NN` |
| Review | `cl-rev-NN` | `cl-rev-NN` |
| Fix | `cx-fix-NN` | `cx-fix-NN` |

## Orchestrator Checklist

Before starting the chain:
- [ ] All plans committed to main
- [ ] `git status` is clean
- [ ] Plans are ordered by dependency

Per cycle:
- [ ] Implement phase committed
- [ ] Review written with verdict
- [ ] Fixes applied (if needed)
- [ ] `go build/vet/test` all pass
- [ ] Main is clean before next plan

## Stop Conditions

- **Review FAIL with P1**: fix phase is mandatory
- **Review PASS (0 P1, 0 P2)**: skip fix, move to next plan
- **Review PASS_WITH_NOTES**: orchestrator decides — skip fix for P3-only
- **Build/test failure after fix**: re-run fix phase once, then escalate to
  orchestrator for manual intervention
