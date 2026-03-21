# Cattery — Pure Go TTS Library

## Overview

Text-to-speech in pure Go. No cgo, no Python. Tiny binary (~3MB), all runtime deps auto-downloaded on first run.

```
go install github.com/jikkuatwork/cattery/cmd/cattery@latest
cattery "Hello, world."    # downloads model/voice/ORT on first run
```

## Stack

- **Language**: Go (pure, no cgo)
- **Inference**: ONNX Runtime 1.24.1+ via `yalue/onnxruntime_go` v1.27.0 (dlopen)
- **Model**: Kokoro-82M int8 quantized (92MB ONNX)
- **Phonemization**: espeak-ng via `os/exec` (only remaining system dep)
- **Audio**: WAV (pure Go writer)

## Architecture

```
cattery/
├── cmd/
│   ├── cattery/       # CLI (flags, auto-download, full pipeline)
│   └── spike/         # Original spike (kept for reference, has timing)
├── engine/            # ONNX inference, tokenizer, voice loading
├── phonemize/         # espeak-ng IPA phonemizer (os/exec)
├── audio/             # WAV writer (pure Go)
├── download/          # Auto-download model/voice/ORT with SHA256 verification
├── scripts/           # Helper scripts
└── koder/             # Project tracking
```

## Auto-Download (no auth anywhere)

| Asset | Source | URL pattern |
|---|---|---|
| Model + voices | `jikkuatwork/cattery-artefacts` (Git LFS) | `github.com/.../raw/main/models/kokoro-82m-v1.0/...` |
| ORT runtime | Microsoft official releases | `github.com/microsoft/onnxruntime/releases/...` |

Files cached in `~/.cattery/` after first download.

## Performance (aarch64 VM, 70 tokens → 4.6s audio)

| Phase | Time | % |
|---|---|---|
| ORT init | 8ms | 0.2% |
| Phonemize | 13ms | 0.4% |
| Session load | 352ms | 9.7% |
| **Inference** | **3,217ms** | **88.8%** |
| WAV write | 32ms | 0.9% |

- **RTF**: 0.70x (faster than real-time)
- **Peak RSS**: 239MB (Go heap: 1MB, rest is ORT/model)
- **Zero GC cycles** during inference

## Size

| Component | Size | Shipped with |
|---|---|---|
| Go binary | 2.7MB | `go install` |
| libonnxruntime.so | 18MB | auto-download |
| Model (82M q8) | 89MB | auto-download |
| Voice (1 file) | 510KB | auto-download |

## Model Details (Kokoro-82M ONNX)

- **Inputs**: `input_ids` int64 [1,seq], `style` float32 [1,256], `speed` float32 [1]
- **Output**: `waveform` float32 [1,N] at 24kHz
- **Tokens**: 178 IPA phoneme vocab, padded with 0 at start/end
- **Voices**: raw float32 .bin files, shape [510,256], indexed by token count

## Issues

| # | Title | Status | Pri |
|---|---|---|---|
| [01](issues/01_onnx_inference_spike.md) | ONNX inference spike | **done** | P0 |
| [02](issues/02_phonemizer_strategy.md) | Phonemizer strategy | **v1 done** (espeak os/exec) | P0 |
| [03](issues/03_wav_writer.md) | WAV writer | **done** | P1 |
| [04](issues/04_cli.md) | CLI + model download | **in progress** | P1 |
| [05](issues/05_cross_platform_build.md) | Cross-platform build | open | P2 |

## What's Done

- Full end-to-end pipeline: text → phonemes → tokens → ONNX → WAV
- Engine package (`engine/`): tokenizer, voice loader, ONNX session wrapper
- Download package (`download/`): auto-fetch model/voice/ORT with SHA256 verification
- CLI (`cmd/cattery/`): flags for voice, speed, output, lang — builds & runs
- Artefacts repo (`jikkuatwork/cattery-artefacts`): model + voice hosted via Git LFS

## What's Next

- **Test CLI end-to-end** with fresh `~/.cattery/` (auto-download flow)
- **espeak-ng dependency**: only system dep left — explore WASM embed (wazero) or bundled binary
- **More voices**: upload additional voices to cattery-artefacts
- **Cross-platform**: test on macOS, verify ORT download for darwin/amd64/arm64

## Key Decisions Made

- **No pure Go ONNX**: GoMLX/gonnx lack FFT/ISTFT ops needed by Kokoro. ORT via dlopen is the only viable path.
- **Download on first run**: binary stays ~3MB, runtime deps fetched as needed. No embedding.
- **Separate artefacts repo**: `cattery-artefacts` holds binaries via Git LFS. Keeps source repo small. Stable URLs, no auth.
- **ORT from Microsoft**: not mirrored — their GitHub Release URLs are permanent.
- **espeak-ng via os/exec**: simplest phonemizer. Works now. WASM embed deferred.

## Research

- [01_initial.md](research/01_initial.md) — KittenTTS benchmarks, size analysis, library survey, phonemizer options
