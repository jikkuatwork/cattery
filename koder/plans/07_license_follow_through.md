# Plan 07 — License Follow-Through

## Why

- Distribution still needs compliance follow-through.
- `THIRD_PARTY_NOTICES.md` exists.
- Repo-local licensing is in place; artefacts-repo and release work remain.

## Current State

- This repo now has a top-level Apache-2.0 `LICENSE`.
- This repo now has a top-level `NOTICE`.
- The current audit lives in `THIRD_PARTY_NOTICES.md`.
- `espeak-ng` stays external via `os/exec`.
- ONNX Runtime is downloaded from Microsoft, not mirrored here.
- Model and voice files come from `cattery-artefacts`.
- Part of the follow-through lives in that artefacts repo, not this one.

## Decisions For This Pass

- Repo license: Apache-2.0.
- Split repo work from external-repo work.
- Land repo-local compliance first.
- Do not bundle `espeak-ng`.
- Do not mirror ORT in this repo.

## Scope

- Tighten `THIRD_PARTY_NOTICES.md` if wording is stale or vague.
- Add a small release note or checklist for shipping `LICENSE`, `NOTICE`, and
  `THIRD_PARTY_NOTICES.md` with binary bundles.
- Record the external tasks for `cattery-artefacts`.

## Out Of Scope

- Legal advice.
- Bundling ORT.
- Bundling `espeak-ng`.
- Editing `cattery-artefacts` from this repo.

## Work Plan

1. Re-read `THIRD_PARTY_NOTICES.md`.
   - Make sure the repo license and third-party notes line up.
2. Add a release-packaging note.
   - It must say packaged binaries ship `LICENSE` and notices.
3. Add an explicit external follow-up note for `cattery-artefacts`.
   - Apache-2.0 text.
   - Upstream attribution.
4. Update tracking.
   - Close `#07` only after repo-local work lands and the artefacts-repo
     work is at least written down.

## Files Likely Touched

- `LICENSE`
- `NOTICE`
- `THIRD_PARTY_NOTICES.md`
- `koder/issues/07_license_audit.md`
- `koder/STATE.md`
- one small release note doc, if needed

## Risks

- Repo-local and artefacts-repo duties get mixed together.
- Packaged releases may omit required notice files.
- Future bundling changes the obligations again.

## Done When

- Root `LICENSE` exists.
- Root `NOTICE` exists.
- `THIRD_PARTY_NOTICES.md` matches reality.
- Release packaging docs say which files must ship.
- External artefacts tasks are written down clearly.
