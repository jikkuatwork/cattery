# 07 — License Audit

## Status: open
## Priority: P1

Audit licenses for all dependencies and assets:
- Kokoro-82M model license (Apache 2.0?) — verify redistribution terms
- ONNX Runtime license (MIT) — confirm bundling/download is fine
- espeak-ng license (GPL) — we shell out, not link, but verify implications
- yalue/onnxruntime_go binding license
- Voice files — what license are the Kokoro voice packs under?

Key question: are we okay hosting/distributing model files via cattery-artefacts? Need to check if the original model license permits mirroring.
