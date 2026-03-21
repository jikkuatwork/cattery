# Plan 07 — License Follow-Through

## Why

- Distribution is blocked.
- `THIRD_PARTY_NOTICES.md` exists.
- Root `LICENSE` does not.

## Current State

- This repo has no top-level `LICENSE`.
- The current audit lives in `THIRD_PARTY_NOTICES.md`.
- `espeak-ng` stays external via `os/exec`.
- ONNX Runtime is downloaded from Microsoft, not mirrored here.
- Model and voice files come from `cattery-artefacts`.
- Part of the follow-through lives in that artefacts repo, not this one.

## Decisions For This Pass

- Split repo work from external-repo work.
- Land repo-local compliance first.
- Do not bundle `espeak-ng`.
- Do not mirror ORT in this repo.
- Treat the project license choice as the only owner decision.

## Scope

- Add a root `LICENSE` for `cattery`.
- Tighten `THIRD_PARTY_NOTICES.md` if wording is stale or vague.
- Add a small release note or checklist for shipping `LICENSE` and
  `THIRD_PARTY_NOTICES.md` with binary bundles.
- Record the external tasks for `cattery-artefacts`.

## Out Of Scope

- Legal advice.
- Bundling ORT.
- Bundling `espeak-ng`.
- Editing `cattery-artefacts` from this repo.

## Work Plan

1. Confirm which license the repo owner wants for `cattery`.
   - If that is unknown, stop before adding `LICENSE`.
2. Add the root `LICENSE`.
3. Re-read `THIRD_PARTY_NOTICES.md`.
   - Make sure the repo license and third-party notes line up.
4. Add a release-packaging note.
   - It must say packaged binaries ship `LICENSE` and notices.
5. Add an explicit external follow-up note for `cattery-artefacts`.
   - Apache-2.0 text.
   - Upstream attribution.
6. Update tracking.
   - Close `#07` only after repo-local work lands and the artefacts-repo
     work is at least written down.

## Files Likely Touched

- `LICENSE`
- `THIRD_PARTY_NOTICES.md`
- `koder/issues/07_license_audit.md`
- `koder/STATE.md`
- one small release note doc, if needed

## Risks

- No clear repo license choice yet.
- Repo-local and artefacts-repo duties get mixed together.
- Future bundling changes the obligations again.

## Done When

- Root `LICENSE` exists.
- `THIRD_PARTY_NOTICES.md` matches reality.
- Release packaging docs say which files must ship.
- External artefacts tasks are written down clearly.
