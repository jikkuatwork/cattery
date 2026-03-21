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
├── paths/             # XDG-compliant data directory resolution
├── scripts/           # Helper scripts
└── koder/             # Project tracking
```

## CLI Commands

```
cattery "Hello, world."          # speak (auto-downloads on first run)
cattery --voice bella "Hello"    # pick a voice by name or ID
cattery --speed 1.5 -o out.wav   # speed + output file
cattery list                     # show models + voices
cattery status                   # platform, deps, disk usage
cattery download                 # pre-fetch model + all voices
```

## Downloads (no auth, stable URLs)

| Asset | Source | Size |
|---|---|---|
| Model + voices | `jikkuatwork/cattery-artefacts` (Git LFS) | 88MB + 13MB |
| ORT runtime | Microsoft GitHub Releases | 18MB |

All 27 voices now uploaded. Cached in `~/.local/share/cattery/` (XDG).

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

## What's Next (CLI polish)

These are the immediate next tasks for the CLI:

1. **Clean up progress bars** — show only `23MB / 120MB [========        ]`, no percentage or speed (bar is enough)
2. **Numeric IDs for models/voices** — models get 01, 02...; voices get 01, 02...; much easier to type than slugs
3. **Simplify model naming** — "Kokoro" not "Kokoro 82M" (user doesn't care about params)
4. **Add --male / --female flag** — filter voices by gender
5. **Random voice selection** — if no voice specified, pick a random one (instead of always defaulting to Heart)
6. **Reduce noise** — strip unnecessary output, keep it minimal

## Key Decisions Made

- **No pure Go ONNX**: GoMLX/gonnx lack FFT/ISTFT ops. ORT via dlopen is the only path.
- **Download on first run**: binary stays ~8MB, runtime deps fetched as needed.
- **Separate artefacts repo**: `cattery-artefacts` holds binaries via Git LFS. No auth.
- **ORT from Microsoft**: not mirrored — their GitHub Release URLs are permanent.
- **espeak-ng via os/exec**: simplest phonemizer. WASM embed deferred.
- **Per-model default voice**: each model carries its own default, no global constant.
- **All voices downloaded by `cattery download`**: they're ~510KB each, no reason to be stingy.

## Research

- [01_initial.md](research/01_initial.md) — KittenTTS benchmarks, size analysis, library survey, phonemizer options
