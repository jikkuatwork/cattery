# 15 — Artefact Mirror Registry (mirror.json)

## Status: open (idea)
## Priority: P2

## Summary

Add a `mirror.json` to `cattery-artefacts` that lists download URLs for every
artefact. Each model/voice/runtime entry carries an ordered array of mirrors so
the downloader can fall back automatically when one source is unreachable.

Initially every entry points only at the GitHub LFS URL. Adding a second mirror
later is a one-line JSON change — no code release needed.

## Proposed Format

```jsonc
{
  "version": 1,
  "artefacts": {
    "models/kokoro-82m-int8.onnx": {
      "size": 92000000,
      "sha256": "<hash>",
      "mirrors": [
        { "url": "https://github.com/jikkuatwork/cattery-artefacts/raw/main/models/kokoro-82m-int8.onnx", "label": "github" }
      ]
    },
    "voices/af_bella.bin": {
      "size": 510000,
      "sha256": "<hash>",
      "mirrors": [
        { "url": "https://github.com/jikkuatwork/cattery-artefacts/raw/main/voices/af_bella.bin", "label": "github" }
      ]
    }
  }
}
```

Key points:

- **Keyed by relative path** inside the artefacts repo.
- **`mirrors` array is ordered** — downloader tries first entry, falls through
  on failure.
- **`label`** is human-readable (e.g. `github`, `r2`, `b2`). Not used for
  routing, just for logs/status output.
- **`sha256`** per artefact — downloader already verifies where hashes are
  recorded; this centralises them.
- **`size`** in bytes — enables progress bars before the first byte arrives.
- **`version`** field for future schema changes.

## Work Items

1. Design and commit `mirror.json` in `cattery-artefacts` with GitHub-only
   mirrors for all current artefacts.
2. Update `download/` package to fetch and parse `mirror.json` before
   downloading individual files.
3. Implement ordered fallback: try each mirror in sequence, log which one
   succeeded.
4. Migrate existing hardcoded URLs in `download/` to use the registry.

## Future

- Add alternative mirrors (R2, B2, self-hosted) by appending to the `mirrors`
  arrays — no cattery code change required.
- Could version-gate artefacts (e.g. model v2) by adding new keys; old keys
  stay for backward compat.
