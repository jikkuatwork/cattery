# 22 — Bundle espeak-ng (zero system dependencies)

## Status: open
## Priority: P1
## Depends on: nothing (can parallel with #16-#21)
## Blocks: nothing (but critical for single-command install story)

## Problem

espeak-ng is the only system dependency. Users must `apt install espeak-ng` before cattery works. This breaks the "single command install" promise:

```
go install github.com/jikkuatwork/cattery/cmd/cattery@latest
cattery "Hello"  # fails: espeak-ng not found
```

On Pi4 / embedded systems, requiring apt access adds friction and may need sudo.

## Goal

Eliminate the espeak-ng system dependency. After `go install`, the first `cattery` invocation should download everything it needs — including espeak-ng binary + data files — with zero system packages required.

## Options considered

| Approach | Binary size | Complexity | Deps |
|---|---|---|---|
| **A: Download binary + data** | +0 (download) | Low | None |
| B: Static link via cgo | +2MB | High | Build-time C toolchain |
| C: WASM embed | +2MB | Very high | wazero runtime |

**Option A wins.** Same download-on-first-run pattern as ORT and models. Consistent UX. Zero cgo additions.

## Design

### What to download

espeak-ng consists of:
1. **Binary**: `espeak-ng` executable (~300KB, platform-specific)
2. **Data directory**: phoneme rules, language data (~2MB, platform-independent)

Both are small enough to bundle in the artefacts repo alongside models.

### Artefact layout

```
~/.cattery/
├── ort/                    # ORT shared library (existing)
├── kokoro-82m-v1.0/        # TTS model + voices (existing)
├── moonshine-tiny-v1.0/    # STT model (new, from #20)
└── espeak-ng/              # NEW
    ├── bin/
    │   └── espeak-ng       # platform-specific binary
    └── data/
        └── ...             # espeak-ng-data (phoneme rules, languages)
```

### Build espeak-ng binaries

Pre-build espeak-ng static binaries for supported platforms:
- `linux/amd64`
- `linux/arm64` (Pi4)
- `darwin/amd64`
- `darwin/arm64`

Host them in `cattery-artefacts` repo (same as ORT/models). The download system already handles platform-specific artefacts for ORT — extend it for espeak-ng.

### espeak-ng data files

The data directory (`espeak-ng-data/`) contains:
- `phontab`, `phonindex`, `phondata` — phoneme definitions
- `en_dict`, `en_rules` — English language data
- Other language files as needed

These are architecture-independent. One download for all platforms.

### Changes to `phonemize/`

Currently `phonemize/` calls `espeak-ng` via `os/exec` and relies on the system PATH:

```go
func (p *EspeakPhonemizer) Phonemize(text string) (string, error) {
    cmd := exec.Command("espeak-ng", ...)
    // ...
}
```

After this change:
1. Check `~/.cattery/espeak-ng/bin/espeak-ng` first
2. Fall back to system `espeak-ng` if present
3. If neither found, trigger download
4. Set `ESPEAK_DATA_PATH` env var when using bundled binary

```go
func (p *EspeakPhonemizer) espeakBin() string {
    bundled := filepath.Join(paths.DataDir(), "espeak-ng", "bin", "espeak-ng")
    if _, err := os.Stat(bundled); err == nil {
        return bundled
    }
    if path, err := exec.LookPath("espeak-ng"); err == nil {
        return path
    }
    return "" // needs download
}
```

### Download integration

Add espeak-ng to the registry as a `KindRuntime` artefact (or a new `KindTool` kind). The `download.Ensure()` flow for TTS should include espeak-ng alongside ORT.

### `cattery download` behavior

```
cattery download          # downloads everything including espeak-ng
cattery "Hello"           # auto-downloads espeak-ng if missing
```

### Platform detection

Same pattern as ORT download — detect `runtime.GOOS` + `runtime.GOARCH`, select the right binary archive.

### File changes

- **Edit**: `phonemize/phonemize.go` — look for bundled binary, set ESPEAK_DATA_PATH
- **Edit**: `registry/registry.go` — add espeak-ng artefact entries
- **Edit**: `download/download.go` — ensure espeak-ng alongside ORT
- **Edit**: `preflight/preflight.go` — update espeak-ng check for bundled binary
- **Add to artefacts repo**: pre-built espeak-ng binaries + data

## Acceptance criteria

- [ ] Fresh install: `go install ... && cattery "Hello"` works without `apt install`
- [ ] espeak-ng binary + data auto-downloaded on first use
- [ ] Bundled espeak-ng used when present, system espeak-ng as fallback
- [ ] `ESPEAK_DATA_PATH` correctly set for bundled binary
- [ ] Works on linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
- [ ] `cattery status` shows espeak-ng source (bundled vs system)
- [ ] `cattery download` includes espeak-ng

## Notes

- espeak-ng is LGPL-2.1+. We're distributing the unmodified binary — this is fine under LGPL. Document in THIRD_PARTY_NOTICES.md.
- The data files are GPL-3.0 (espeak-ng-data). Need to verify distribution obligations. May need to include source pointer.
- Building static espeak-ng binaries: `cmake -DBUILD_SHARED_LIBS=OFF` + musl on Linux for truly static binaries.
- This could be done incrementally: first support bundled binary if manually placed, then add auto-download.
