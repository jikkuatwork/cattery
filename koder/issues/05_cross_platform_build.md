# 05 — Cross-Platform Build & Size Audit

**Status**: open
**Priority**: P2

## Goal

Verify cattery builds and runs on linux/mac/win x amd64/arm64. Audit final binary + model size against 50MB target.

## Acceptance criteria

- [ ] CI builds for all 6 platform combos
- [ ] onnxruntime shared lib bundled per-platform
- [ ] Total size (binary + model + runtime) documented
- [ ] Under 50MB target or documented why not
