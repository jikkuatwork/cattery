# 22 — Bundle espeak-ng (zero system dependencies)

## Status: done
## Priority: P1
## Depends on: nothing
## Blocks: nothing (but critical for single-binary install story)

## Problem

espeak-ng is the only system dependency. Users must `apt install espeak-ng` before cattery works. This breaks the "single command install" promise:

```
go install github.com/jikkuatwork/cattery/cmd/cattery@latest
cattery "Hello"  # fails: espeak-ng not found
```

On Pi4 / embedded / air-gapped systems, requiring apt access adds friction, may need sudo, and breaks offline operation.

## Goal

Eliminate the espeak-ng system dependency. After `go install`, `cattery` auto-downloads espeak-ng on first run (same as ORT and models) — no `apt install` required.

## Decision: download-on-first-run (revised 2026-03-30)

Original plan was `go:embed`. Revised because:
- cattery already requires first-run downloads (ORT, models, voices) — espeak is no different
- +11MB binary bloat for a purity that doesn't exist (network is already required)
- 11MB of tarballs checked into git history is unnecessary repo bloat
- `cattery-artefacts` is the canonical home for all runtime dependencies

Artefacts now live at `cattery-artefacts/espeak-ng-v1.51/` (Git LFS), registered in `mirror.json`. `phonemize/bundle/` purged from cattery git history.

### Previous rationale (archived)

- GitHub LFS has bandwidth quotas — can get throttled
- Repos get renamed, deleted, or transferred
- CDN/mirror URLs change over time
- Any breakage = cattery stops working on fresh installs
- Air-gapped setups break if `~/.cattery/` is wiped (new SD card, new user)

### Why `go:embed`?

- Binary is self-contained — works on first run, no network required
- No URL rot — the version you compile is the version that works forever
- Size cost is ~3MB (compressed espeak-ng binary + data) — total binary goes from ~8MB to ~11MB
- Same pre-build work as the download approach (build espeak-ng for 4 platforms)
- Simpler code — no download logic, no mirror fallback, no checksum verification for espeak-ng

### Legal basis

**espeak-ng binary** (LGPL-2.1+): distributing unmodified binary is permitted. Requires attribution and source pointer in `THIRD_PARTY_NOTICES.md`.

**espeak-ng-data** (GPL-3.0): embedded as an opaque compressed archive, extracted to disk, consumed by a separate process (`os/exec`). This is **mere aggregation** under GPL Section 5 — no relicensing required. Same legal basis as Linux distros shipping GPL and non-GPL software on the same image.

To keep the aggregation argument airtight:
- espeak-ng binary + data live in a compressed archive blob, not as loose Go source
- Extracted to `~/.cattery/espeak-ng/`, executed via `os/exec` (separate process)
- cattery code never imports or links espeak-ng C code
- `THIRD_PARTY_NOTICES.md` includes full LGPL/GPL attribution + source pointer to espeak-ng repo

## Design

### Pre-build espeak-ng static binaries

Build static espeak-ng binaries for supported platforms:
- `linux/amd64`
- `linux/arm64` (Pi4)
- `darwin/amd64`
- `darwin/arm64`

Built via `cmake -DBUILD_SHARED_LIBS=OFF` + musl on Linux for truly static binaries. macOS uses system toolchain.

### Embed layout

```
phonemize/
├── embed_linux_amd64.go    # //go:build linux && amd64
├── embed_linux_arm64.go    # //go:build linux && arm64
├── embed_darwin_amd64.go   # //go:build darwin && amd64
├── embed_darwin_arm64.go   # //go:build darwin && arm64
└── bundle/
    ├── linux_amd64.tar.gz  # espeak-ng binary (static, ~300KB compressed)
    ├── linux_arm64.tar.gz
    ├── darwin_amd64.tar.gz
    ├── darwin_arm64.tar.gz
    └── data.tar.gz         # espeak-ng-data (platform-independent, ~2MB compressed)
```

Each `embed_<platform>.go` file:
```go
//go:build linux && amd64

package phonemize

import _ "embed"

//go:embed bundle/linux_amd64.tar.gz
var espeakBinArchive []byte

//go:embed bundle/data.tar.gz
var espeakDataArchive []byte
```

### Runtime extraction

On first use, extract to `~/.cattery/espeak-ng/`:

```
~/.cattery/espeak-ng/
├── bin/
│   └── espeak-ng           # platform-specific binary (chmod +x)
└── data/
    └── ...                 # espeak-ng-data (phoneme rules, languages)
```

Extraction is cached — skip if `~/.cattery/espeak-ng/bin/espeak-ng` already exists and matches expected version. A version marker file (`~/.cattery/espeak-ng/.version`) tracks which cattery version extracted the files, so upgrades can re-extract if the bundled espeak-ng changes.

### Changes to `phonemize/`

Currently calls system espeak-ng via PATH:

```go
cmd := exec.Command("espeak-ng", ...)
```

After this change:

```go
func (p *EspeakPhonemizer) espeakBin() (string, error) {
    bundled := filepath.Join(paths.DataDir(), "espeak-ng", "bin", "espeak-ng")
    if _, err := os.Stat(bundled); err == nil {
        return bundled, nil
    }
    // Extract from embedded archive
    if err := extractEspeak(); err != nil {
        return "", fmt.Errorf("extract bundled espeak-ng: %w", err)
    }
    return bundled, nil
}
```

When using the bundled binary, set `ESPEAK_DATA_PATH`:

```go
cmd := exec.Command(bin, "-q", "--ipa=3", "--sep= ", "-v", lang, text)
cmd.Env = append(os.Environ(),
    "ESPEAK_DATA_PATH="+filepath.Join(paths.DataDir(), "espeak-ng", "data"),
)
```

System espeak-ng is **not** used as fallback — the bundled version is always preferred to ensure consistent IPA output across all installations.

### File changes

- **Add**: `phonemize/embed_<platform>.go` (4 files) — build-tagged embed directives
- **Add**: `phonemize/bundle/` — pre-built compressed archives
- **Add**: `phonemize/extract.go` — extraction + caching logic
- **Edit**: `phonemize/phonemize.go` — use bundled binary, set ESPEAK_DATA_PATH
- **Edit**: `preflight/preflight.go` — remove "espeak-ng not found" system check (it's always available now)
- **Edit**: `THIRD_PARTY_NOTICES.md` — add LGPL/GPL attribution + source pointer

### Build process

A `scripts/build-espeak.sh` script:
1. Clones espeak-ng at a pinned tag (e.g., `1.51`)
2. Cross-compiles static binaries for 4 platforms
3. Packages each as `<platform>.tar.gz`
4. Packages `espeak-ng-data/` as `data.tar.gz`
5. Drops archives into `phonemize/bundle/`

This runs **once** when updating the bundled espeak-ng version, not on every build. The archives are checked into the repo (they're small — ~3MB total).

## Acceptance criteria

- [ ] `go install ... && cattery "Hello"` works without `apt install` on all 4 platforms
- [ ] No network required — works on air-gapped systems from first run
- [ ] espeak-ng binary + data extracted to `~/.cattery/espeak-ng/` on first use, cached thereafter
- [ ] Version marker enables clean re-extraction on cattery upgrades
- [ ] `ESPEAK_DATA_PATH` correctly set for bundled binary
- [ ] `cattery status` shows bundled espeak-ng version
- [ ] `THIRD_PARTY_NOTICES.md` updated with LGPL-2.1+/GPL-3.0 attribution and source pointer
- [ ] `scripts/build-espeak.sh` reproducibly builds static binaries for 4 platforms
- [ ] Binary size increase is under 4MB

## Notes

- espeak-ng-data is ~2MB uncompressed, compresses well. Binary is ~300KB per platform.
- Total embedded payload: ~3MB compressed across all platforms (only the matching platform's binary is included per build via build tags; data is shared).
- The `go:embed` approach means `go install` from source includes espeak-ng — no separate download step, no artefacts repo dependency for this component.
- Future: if espeak-ng is ever replaced (e.g., WASM+wazero for cleaner embedding), the extraction cache in `~/.cattery/espeak-ng/` can be cleaned up by the new version.
