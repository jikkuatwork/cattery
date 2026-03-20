# Initial Research — KittenTTS & Go TTS Feasibility

## KittenTTS Nano INT8 Benchmarks (aarch64 VM)

| Metric | Value |
|---|---|
| Model size | 23MB ONNX |
| Voices | 8 (3MB voices.npz) |
| Sample rate | 24kHz |
| RTF (avg) | 5.9x realtime |
| RTF (short) | ~5.4x |
| RTF (long) | ~6.3x |
| Peak RAM (Python) | ~481MB |

## KittenTTS Mini FP32 (80M params, 75MB)

| Metric | Value |
|---|---|
| RTF (avg) | 3.1x realtime |
| Noticeably slower, marginal quality gain over nano int8 |

## PyInstaller Binary Analysis (why Python bundle was 246MB)

| Component | Size | Needed? |
|---|---|---|
| torch (via spacy->thinc) | 400MB | No |
| babel, pandas, sympy | 72MB | No |
| ONNX model | 23MB | Yes |
| onnxruntime .so | 19MB | Yes |
| numpy + OpenBLAS | 32MB | Yes |
| libpython3.12 | 22MB | Yes |
| espeak-ng data | 19MB | Yes |
| misaki + spacy | 28MB | Yes |

## Size Budget for Go Version

| Part | Est. Size |
|---|---|
| Go binary | ~5MB |
| libonnxruntime.so (shipped) | ~15-20MB |
| ONNX model (nano int8) | 23MB |
| voices.npz | 3MB |
| Phoneme data | ~4MB |
| **Total** | **~50MB** |

## Key Libraries Identified

| Library | Purpose | cgo? | License |
|---|---|---|---|
| `yalue/onnxruntime_go` | ONNX inference (dlopen) | No | MIT |
| `BenLubar/espeak` | espeak-ng binding | Yes (cgo) | -- |
| sherpa-onnx Go | Full TTS pipeline | Yes (cgo) | Apache-2.0 |

## The Phonemizer Problem

KittenTTS pipeline: `text -> preprocess -> espeak-ng phonemes -> token IDs -> ONNX model -> audio`

Options for Go (no cgo):
1. **Pre-built phoneme lexicon** -- lookup table, fast but incomplete on unknown words
2. **Call espeak-ng binary** -- `os/exec`, simple but requires system install
3. **Embed espeak WASM** -- run espeak-ng compiled to WASM via a Go WASM runtime (wazero)
4. **Pure Go phonemizer** -- doesn't exist yet, would need to write one
5. **Ship espeak-ng binary alongside** -- practical, minimal dep

Option 3 (wazero + espeak WASM) is the most interesting for "zero dependency" goal. espeak-ng has been compiled to WASM (used in browser TTS demos). wazero is a pure Go WASM runtime (no cgo).

## WASM Precedent

- KittenTTS has been run in browsers via ONNX RT Web + phonemizer.js (espeak->WASM)
- sherpa-onnx compiles to WASM with embedded espeak-ng
- phonemizer.js wraps espeak WASM specifically for phonemization

## Models to Consider

| Model | Params | Size | License | Notes |
|---|---|---|---|---|
| KittenTTS nano int8 | 15M | 23MB | Apache-2.0 | Proven, 5.9x RT |
| Piper (en-US-lessac) | ~15M | ~15MB | MIT | Well-supported in sherpa |
| Kokoro (tiny) | -- | ~40MB | Apache-2.0 | Higher quality |
