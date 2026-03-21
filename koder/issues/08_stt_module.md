# 08 — Speech-to-Text Module

## Status: open
## Priority: P2

Explore adding a small STT module to cattery, same philosophy: pure Go, ONNX Runtime, no cgo beyond ORT dlopen.

Candidates:
- Whisper tiny/base (39M/74M params) — ONNX exports exist
- Moonshine tiny (27M) — purpose-built for edge, ONNX available
- Silero STT — small, MIT licensed

Questions:
- Can we share the ORT runtime instance with TTS?
- What's the RAM overhead of a second model?
- Latency for short utterances (Telegram voice messages are typically 5-30s)?

This would complete the audio pipeline: STT → LLM → TTS.
