# 33 — Enable GitHub Actions PR creation permission

## Status: open
## Priority: P3

## Problem

The `build-espeak.yml` workflow can't auto-create PRs because the repo setting "Allow GitHub Actions to create and approve pull requests" is disabled. The commit job pushes the branch successfully but `gh pr create` fails with:

```
GitHub Actions is not permitted to create or approve pull requests
```

## Fix

Settings → Actions → General → Workflow permissions → check "Allow GitHub Actions to create and approve pull requests". Save.

## Notes

- One-time manual setting change, no code needed.
- Affects any future workflow that needs to open PRs (e.g., dependency updates, bundle rebuilds).
