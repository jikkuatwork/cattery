# 07 — License Audit

## Status: open
## Priority: P1

Audit completed on 2026-03-21. The issue remains open because the compliance
follow-through is not finished yet.

## Findings

- `github.com/yalue/onnxruntime_go` is MIT.
- `golang.org/x/sys` is BSD-3-Clause.
- ONNX Runtime is MIT and is currently downloaded from Microsoft's official
  releases rather than mirrored by `cattery`.
- `hexgrad/Kokoro-82M` and `onnx-community/Kokoro-82M-v1.0-ONNX` are
  Apache-2.0. That makes model and voice mirroring plausible, but the mirror
  still needs Apache compliance material.
- `espeak-ng` is GPL-3.0-or-later. Current usage is via `os/exec`, which is a
  safer boundary than bundling or linking, so keep it external.
- The `cattery` repo itself still has no top-level `LICENSE` file. That blocks
  clean open-source distribution of this project's own code regardless of the
  third-party components.

## Answer To The Key Question

Yes, hosting the Kokoro model and voice files in `cattery-artefacts` looks
compatible with the upstream Apache-2.0 licensing. The remaining risk is not
"permission to mirror"; it is "mirror needs to carry the license and
attribution obligations correctly."

## Blocking Tasks

- [ ] Add a root `LICENSE` file for `cattery` itself.
- [ ] Add Apache-2.0 license text to `cattery-artefacts`.
- [ ] Add upstream attribution in `cattery-artefacts` for both the original
      Kokoro model and the ONNX Community conversion source.
- [ ] Ship third-party notices with packaged binary releases.
- [ ] Re-review licensing before bundling ONNX Runtime or `espeak-ng`.

## Notes

- `cattery-artefacts` currently has a README that names the model source and
  license, but that should not be treated as the final compliance step.
- The detailed audit summary lives in `THIRD_PARTY_NOTICES.md`.
