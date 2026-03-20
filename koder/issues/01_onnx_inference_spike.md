# 01 — Spike: ONNX Runtime Inference with KittenTTS

**Status**: open
**Priority**: P0

## Goal

Prove that `yalue/onnxruntime_go` can load and run the KittenTTS nano int8 ONNX model, producing raw audio floats from hardcoded token IDs.

## Why first

Everything else (phonemizer, CLI, WAV output) is pointless if Go can't run the model. This is the single biggest risk.

## Acceptance criteria

- [ ] Go program loads KittenTTS nano int8 ONNX model via onnxruntime_go
- [ ] Feeds hardcoded phoneme token IDs + voice embedding
- [ ] Gets back float32 audio samples
- [ ] Writes raw PCM to a file that plays correctly (even if manually converted)

## Notes

- Hardcode everything — no phonemizer, no WAV header, just prove inference works
- Check what ONNX opset version the model needs
- Document any onnxruntime_go quirks or limitations found
