# Third-Party Notices

Audit date: 2026-03-21

This file records the current licensing position of the code, runtime downloads,
and model assets used by `cattery`. It is an engineering audit, not legal advice.

## Current status

- This repository does not currently contain a top-level `LICENSE` file for
  `cattery` itself. Until the owner adds one, the project code is not
  open-source licensed by default.
- `cattery` shells out to a system-installed `espeak-ng` binary from
  `phonemize/espeak.go`; it does not vendor or bundle `espeak-ng`.
- `cattery` downloads ONNX Runtime from Microsoft's official releases and
  downloads model and voice assets from `jikkuatwork/cattery-artefacts`.

## Audited components

| Component | Version / source | License | How `cattery` uses it | Distribution notes |
| --- | --- | --- | --- | --- |
| `github.com/yalue/onnxruntime_go` | `v1.27.0` | MIT | Go wrapper linked into the binary | Keep MIT notice with source or binary distributions. |
| `golang.org/x/sys` | `v0.42.0` | BSD-3-Clause | Low-level OS helpers linked into the binary | Keep BSD notice/disclaimer with source or binary distributions. |
| ONNX Runtime | `1.24.1` from `microsoft/onnxruntime` releases | MIT | Native shared library downloaded on first run | Current design avoids mirroring it. If you ever bundle or mirror it, include the MIT license and upstream third-party notices from the release archive. |
| Kokoro model and voice files | `hexgrad/Kokoro-82M` and `onnx-community/Kokoro-82M-v1.0-ONNX` | Apache-2.0 | Quantized ONNX model and voice bins fetched from `cattery-artefacts` | Apache-2.0 allows redistribution, but the mirror should ship a copy of the license and preserve upstream attribution/notices. Attribute both the original model and the ONNX conversion source. |
| `espeak-ng` | System package | GPL-3.0-or-later | Invoked as an external executable over `os/exec` | Keep it external. Re-review licensing before bundling or redistributing `espeak-ng` with `cattery`. |

## Findings

1. The direct Go module dependency graph is permissive.
   `yalue/onnxruntime_go` is MIT and `golang.org/x/sys` is BSD-3-Clause.

2. Hosting the Kokoro model and voice files in `cattery-artefacts` appears
   allowed.
   The upstream Kokoro model and the ONNX Community conversion are published as
   Apache-2.0, which permits redistribution. The missing piece is compliance:
   the mirror should include a copy of the Apache-2.0 license and upstream
   attribution, not just a README that names the source.

3. ONNX Runtime is low risk in the current architecture.
   `cattery` points users at Microsoft's official release artifacts instead of
   mirroring the runtime. That keeps the redistribution burden lower, but if
   future packaging starts bundling the runtime then the MIT notice and the
   release's `ThirdPartyNotices.txt` should travel with it.

4. `espeak-ng` is the main copyleft boundary to preserve.
   The current design executes `espeak-ng` as a separate program. That is a much
   safer posture than linking or bundling it, but it should stay external unless
   the project is ready for a separate GPL review.

## Blocking follow-ups

- Add a root `LICENSE` file for `cattery` itself.
- Add Apache-2.0 license text and upstream attribution to the
  `cattery-artefacts` repository.
- Ship this notice file, or an equivalent third-party notices file, with any
  future packaged binary releases.
- Re-run the audit if `cattery` ever bundles ONNX Runtime, vendors model files
  directly in this repo, or redistributes `espeak-ng`.

## Files reviewed

- `go.mod`
- `download/download.go`
- `phonemize/espeak.go`
- `scripts/upload-artefacts.sh`
- `koder/issues/07_license_audit.md`
