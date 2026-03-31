# #35 — Mirror ORT into cattery-artefacts

**Status: done**

## Goal

ORT runtime was the last artefact fetched from an external source (Microsoft
GitHub Releases). Mirror the platform `.tgz` files into `cattery-artefacts`,
register them in `mirror.json`, and update `downloadORT()` to use mirrors with
Microsoft as fallback. This gives cattery 100% artefact independence.

## What Was Done

### cattery-artefacts repo

- Downloaded ORT v1.24.4 `.tgz` for linux-x64, linux-aarch64, macOS-arm64
- Stored under `ort/v1.24.4/` (Git LFS)
- Added entries to `mirror.json` with size, SHA256, and GitHub raw URL

### cattery repo

- Rewrote `downloadORT()` in `download/download.go` to use `LookupRaw()` +
  `downloadWithBar()` — same pattern as espeak
- Mirror path: SHA256-verified download from cattery-artefacts
- Fallback: Microsoft GitHub Release URL (for new ORT versions before
  mirror.json is updated)
- Tightened: single `downloadWithBar` call for both paths, removed duplicated
  `http.Get` + manual progress bar code

## Acceptance — all met

- `cattery download` fetches ORT from cattery-artefacts mirror first
- If mirror is down, falls back to Microsoft
- `cattery status` shows ORT as present after download
- No change to binary size or CLI interface
