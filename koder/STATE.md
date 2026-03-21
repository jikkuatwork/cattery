# Cattery — Pure Go TTS Library

## Overview

Text-to-speech in pure Go. No cgo, no Python. Single binary + model files.

**Target**: `cattery --text "hello" --voice af_heart --output out.wav`

## Stack

- **Language**: Go (pure, no cgo)
- **Inference**: ONNX Runtime 1.24.1+ via `yalue/onnxruntime_go` v1.27.0 (dlopen)
- **Model**: Kokoro-82M quantized (92MB, `onnx-community/Kokoro-82M-v1.0-ONNX`)
- **Phonemization**: espeak-ng via `os/exec` (system dependency for now)
- **Audio**: WAV (pure Go writer, `audio/wav.go`)

## Current Size

| Component | Size |
|---|---|
| Go binary | 2.7MB |
| libonnxruntime.so | 18MB |
| Model (82M q8) | 89MB |
| Voice (1 file) | 510KB |
| **Total** | **~110MB** |

Original 50MB budget assumed KittenTTS nano (23MB model). Kokoro-82M quantized is 89MB. Decision needed on model choice vs quality tradeoff.

## Architecture (actual)

```
cattery/
├── cmd/spike/         # Spike / proto CLI (working end-to-end)
├── audio/             # WAV writer (pure Go, no deps)
├── phonemize/         # espeak-ng IPA phonemizer (os/exec)
├── koder/             # Project tracking
└── models-data/       # Downloaded model files (gitignored)
    ├── onnx/          # ONNX model files
    ├── voices/        # Voice .bin files (raw float32 [510,256])
    └── libonnxruntime.so.X.Y.Z
```

## Model Details (Kokoro-82M ONNX)

- **Inputs**: `input_ids` int64 [1,seq], `style` float32 [1,256], `speed` float32 [1]
- **Output**: `waveform` float32 [1,N] at 24kHz
- **Tokens**: 178 IPA phoneme vocab, padded with 0 at start/end
- **Voices**: raw float32 .bin files, shape [510,256], indexed by token count
- **Source**: `onnx-community/Kokoro-82M-v1.0-ONNX` on HuggingFace

## Issues

| # | Title | Status | Pri |
|---|---|---|---|
| [01](issues/01_onnx_inference_spike.md) | ONNX inference spike | **done** | P0 |
| [02](issues/02_phonemizer_strategy.md) | Phonemizer strategy | **v1 done** (espeak os/exec) | P0 |
| [03](issues/03_wav_writer.md) | WAV writer | **done** | P1 |
| [04](issues/04_cli.md) | CLI + model download | open | P1 |
| [05](issues/05_cross_platform_build.md) | Cross-platform build | open | P2 |

## Open Questions

- Model size: stick with 82M (89MB, better quality) or find nano alternative?
- Phonemizer: upgrade to embedded espeak-ng WASM (wazero) for zero system deps?
- Ship onnxruntime embedded or require download?
- Text preprocessing: num2words, abbreviation expansion?

## Research

- [01_initial.md](research/01_initial.md) — KittenTTS benchmarks, size analysis, library survey, phonemizer options
