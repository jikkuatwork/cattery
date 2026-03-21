# 13 — Local 4B LLM via ONNX

## Status: open (research)
## Priority: P3

Explore running a small LLM (4B params) locally via ONNX Runtime, same as we do for TTS:
- Phi-3.5-mini (3.8B) — ONNX exports available, int4 quantized ~2GB
- Qwen2.5-3B — good multilingual, ONNX available
- Gemma-2B — compact, well-supported

Questions:
- RAM: int4 4B model needs ~2-4 GB. Too much for $6 VPS, fine for Pi4 4GB+
- Can we share ORT instance with TTS model? (yes, different sessions)
- Token generation speed on ARM? Probably 5-15 tok/s on Pi4
- Is the quality good enough to be useful? These small models are getting surprisingly capable

This would make cattery fully self-contained — no external API needed.
