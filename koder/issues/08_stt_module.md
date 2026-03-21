# 08 — Speech-to-Text Module

## Status: open (spike proven)
## Priority: P2

Explore adding a small STT module to cattery, same philosophy: pure Go, ONNX Runtime, no cgo beyond ORT dlopen.

## Spike Results (2026-03-22)

**Moonshine-tiny confirmed working** with ORT 1.24.1 from Go. See `cmd/stt-spike/main.go`.

| Metric | Value |
|---|---|
| Model | Moonshine-tiny (onnx-community quantized) |
| Size | 27MB (encoder 7.6MB + decoder 19.3MB) |
| Input | Raw 16kHz PCM (no mel spectrograms) |
| Encoder inference | ~20ms for 3.3s audio |
| Decoder (12 steps) | ~100ms |
| RTF | ~0.035x (28x faster than real-time) |
| Memory | ~20MB total |
| Accuracy | Exact transcription of TTS round-trip test |

Key findings:
- Shares ORT instance with TTS — single `engine.Init()` call
- Encoder-decoder architecture: 2 ONNX files, autoregressive decoding
- KV cache pattern: keep encoder cache from step 0, update decoder cache each step
- Tokenizer: 32k sentencepiece vocab from `tokenizer.json`
- Resampling 24kHz→16kHz: simple linear interpolation sufficient for spike

## Research

Deep research reports in `koder/research/reports/02_small_stt_models/`:
- `qwen.md` — broad survey (Whisper, Vosk, Moonshine, Silero)
- `notebooklm.md` — source-grounded analysis
- `qwen_moonshine_followup.md` — ONNX compat deep dive

### Model comparison

| Model | ONNX size | WER (LS clean) | License | Preprocessing |
|---|---|---|---|---|
| **Moonshine tiny** | 27MB | ~4.5% | MIT (code), weights unclear | Raw 16kHz PCM |
| Moonshine base | 60MB | ~3.2% | MIT (code), weights unclear | Raw 16kHz PCM |
| Whisper tiny.en | ~75MB | ~5.7% | MIT | Mel spectrogram (needs C) |
| Vosk small | ~40MB | ~8-10% | Apache-2.0 | MFCC (Go feasible) |

## Open Questions

- Moonshine weight license — code is MIT, weights have no specified license. May need maintainer clarification for distribution.
- ONNX files are community-converted (onnx-community), not official from Useful Sensors.
- Production resampling — spike uses linear interpolation; may want a proper filter.

## Next Steps

1. Wrap spike into `stt/` package mirroring `engine/` package
2. Add Moonshine model to `registry/` and `download/`
3. Integrate with server API (new `/transcribe` endpoint)
4. Clarify weight license before bundling in artefacts repo
