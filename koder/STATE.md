# Cattery — Pure Go TTS Library

## Overview

Text-to-speech in pure Go. No cgo, no Python, no system deps. Single binary, ~50MB total.

**Target**: `cattery --text "hello" --voice Leo --output out.wav`

## Stack

- **Language**: Go (pure, no cgo)
- **Inference**: ONNX Runtime via `yalue/onnxruntime_go` (dlopen)
- **Model**: KittenTTS nano int8 (23MB, Apache-2.0)
- **Phonemization**: TBD (see issue 02)
- **Audio**: WAV (pure Go writer)

## Architecture

```
cattery/
├── cmd/cattery/        # CLI
├── engine/             # ONNX inference wrapper
├── phonemize/          # Text -> phoneme conversion
├── models/             # Model interface + implementations
│   ├── model.go        # interface: Load, Generate, Voices
│   └── kitten/         # KittenTTS tokenizer/config
├── audio/              # WAV writer
└── models-data/        # Downloaded model files (gitignored)
```

## Issues

| # | Title | Status | Pri |
|---|---|---|---|
| [01](issues/01_onnx_inference_spike.md) | ONNX inference spike | open | P0 |
| [02](issues/02_phonemizer_strategy.md) | Phonemizer strategy | open | P0 |
| [03](issues/03_wav_writer.md) | WAV writer | open | P1 |
| [04](issues/04_cli.md) | CLI + model download | open | P1 |
| [05](issues/05_cross_platform_build.md) | Cross-platform build | open | P2 |

## Open Questions

- Can `onnxruntime_go` handle KittenTTS ONNX2 model ops?
- Is wazero + espeak-WASM viable? (latency?)
- Minimum text preprocessing needed? (num2words etc.)
- Ship onnxruntime embedded or require download?

## Research

- [01_initial.md](research/01_initial.md) — KittenTTS benchmarks, size analysis, library survey, phonemizer options
