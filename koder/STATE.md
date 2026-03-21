# Cattery — Pure Go TTS Library

## Overview

Text-to-speech in pure Go. No cgo, no Python. Tiny binary (~8MB), all runtime deps auto-downloaded on first run.

```
go install github.com/jikkuatwork/cattery/cmd/cattery@latest
cattery "Hello, world."
```

## Stack

- **Language**: Go (pure, no cgo)
- **Inference**: ONNX Runtime 1.24.1+ via `yalue/onnxruntime_go` v1.27.0 (dlopen)
- **Model**: Kokoro-82M int8 quantized (92MB ONNX)
- **Phonemization**: espeak-ng via `os/exec` (only system dep)
- **Audio**: WAV (pure Go writer)

## Architecture

```
cattery/
├── cmd/
│   ├── cattery/       # CLI
│   └── spike/         # Original spike (reference, has timing)
├── engine/            # ONNX inference, tokenizer, voice loading
├── phonemize/         # espeak-ng IPA phonemizer
├── audio/             # Pure Go WAV writer
├── download/          # Auto-download with progress bars, resume, SHA256
├── registry/          # Model/voice metadata registry
├── paths/             # Data directory resolution (~/.cattery/)
├── scripts/           # Helper scripts
└── koder/             # Project tracking
```

## CLI Commands

```
cattery "Hello, world."          # speak with random voice
cattery --voice 3 "Hello"       # pick voice by number
cattery --voice bella "Hello"    # pick voice by name
cattery --female "Hello"         # random female voice
cattery --male "Hello"           # random male voice
cattery --speed 1.5 -o out.wav   # speed + output file
cattery list                     # show models + voices (numbered)
cattery status                   # platform, deps, disk usage
cattery download                 # pre-fetch model + all voices
```

## Downloads (no auth, stable URLs)

| Asset | Source | Size |
|---|---|---|
| Model + voices | `jikkuatwork/cattery-artefacts` (Git LFS) | 88MB + 13MB |
| ORT runtime | Microsoft GitHub Releases | 18MB |

All 27 voices now uploaded. Cached in `~/.cattery/`.

## Performance (aarch64 VM)

| Metric | Value |
|---|---|
| RTF | 0.70x (faster than real-time) |
| Peak RSS | 239MB (Go heap: 1MB) |
| Binary | 8.3MB |
| First-run download | ~120MB total |

## Issues

| # | Title | Status | Pri |
|---|---|---|---|
| [01](issues/01_onnx_inference_spike.md) | ONNX inference spike | **done** | P0 |
| [02](issues/02_phonemizer_strategy.md) | Phonemizer strategy | **done** (espeak os/exec) | P0 |
| [03](issues/03_wav_writer.md) | WAV writer | **done** | P1 |
| [04](issues/04_cli.md) | CLI + model download | **done** | P1 |
| [05](issues/05_cross_platform_build.md) | Cross-platform build | open | P2 |

## What's Next

All 6 CLI polish tasks completed. Remaining work:

- Cross-platform build testing (issue #05)
- Explore additional models when available

## Key Decisions Made

- **No pure Go ONNX**: GoMLX/gonnx lack FFT/ISTFT ops. ORT via dlopen is the only path.
- **Download on first run**: binary stays ~8MB, runtime deps fetched as needed.
- **Separate artefacts repo**: `cattery-artefacts` holds binaries via Git LFS. No auth.
- **ORT from Microsoft**: not mirrored — their GitHub Release URLs are permanent.
- **espeak-ng via os/exec**: simplest phonemizer. WASM embed deferred.
- **Random voice by default**: no voice flag = random pick; `--male`/`--female` to filter.
- **All voices downloaded by `cattery download`**: they're ~510KB each, no reason to be stingy.
- **~/.cattery/ for data**: simple, same on all platforms.
- **ORT stderr suppressed**: redirect fd during init to hide C-level warnings.

## Research

- [01_initial.md](research/01_initial.md) — KittenTTS benchmarks, size analysis, library survey, phonemizer options
