# Small Speech-to-Text Models for Local ONNX Inference

**Source:** Web Research (NotebookLM research API unavailable)
**Date:** 2026-03-21
**Query:** queries/02_small_stt_models.md
**Notebook:** ebd3e4b9-0a22-4aa2-9160-95f463e236f2

---

## Model Comparison Table

| Model | ONNX Size | Params | WER (LS clean) | RTF (CPU) | ONNX Status | License | Streaming |
|---|---|---|---|---|---|---|---|
| Moonshine Tiny (q8) | ~27 MB | 27M | 4.52% | ~0.07x | Native | MIT | Yes |
| Moonshine Base (q8) | ~60 MB | 61M | 3.23% | ~0.17x | Native | MIT | Yes |
| Whisper tiny.en (int8) | ~117 MB | 38M | 5.66% | ~0.39x | Via optimum | MIT | No |
| Whisper tiny.en (fp32) | ~221 MB | 38M | 5.66% | ~0.52x | Via optimum | MIT | No |
| Whisper base.en (int8) | ~165 MB | 73M | 4.25% | ~0.6x | Via optimum | MIT | No |
| Zipformer-en-20M | ~20 MB | 20M | ~8-10%* | <0.3x | Native ONNX | Apache-2.0 | Yes |
| Vosk small-en | ~50 MB | N/A | ~10-12%* | <0.5x | Partial | Apache-2.0 | Yes |
| Silero STT | ~50 MB | N/A | ~8-10%* | <0.5x | ONNX avail. | AGPL-3.0 | No |

**Notes:**
- ONNX Size = encoder + decoder combined (quantized where noted)
- RTF = Real-Time Factor on x86 CPU, 4 threads. Lower = faster. Values <1.0 = faster than real-time
- LS clean = LibriSpeech test-clean WER
- *Estimated from available benchmarks; exact numbers vary by evaluation setup
- Moonshine RTF measured on Linux x86 per 1s audio chunk

## Top 3 Recommendations

### 1. Moonshine Tiny (quantized) -- RECOMMENDED

**Why:** Best accuracy-to-size ratio of any model under 30MB. Native ONNX
support with .ort flatbuffer format. MIT license. Streaming support built-in.
Does not use mel spectrograms -- uses learned convolution layers on raw 16kHz
audio, which dramatically simplifies Go preprocessing.

- **ONNX size:** ~27 MB (encoder 7.6 MB + decoder 19.3 MB, int8 quantized)
- **WER:** 4.52% on LibriSpeech test-clean (better than Whisper tiny.en at 5.66%)
- **Latency:** 69ms per 1s audio on Linux x86, 34ms on Apple Silicon
- **Preprocessing:** 16kHz PCM audio -> 3 conv layers (built into model). No mel spectrogram needed
- **License:** MIT (English models)
- **Streaming:** Yes, with RoPE-based variable-length encoding

### 2. Moonshine Base (quantized) -- BEST ACCURACY

**Why:** Under 200MB target with significantly better accuracy. Same
architecture and integration path as Tiny, just larger.

- **ONNX size:** ~60 MB (encoder 19.6 MB + decoder 40.5 MB, int8 quantized)
- **WER:** 3.23% on LibriSpeech test-clean
- **Latency:** 165ms per 1s audio on Linux x86
- **Preprocessing:** Same as Tiny -- raw 16kHz audio
- **License:** MIT (English models)
- **Streaming:** Yes

### 3. Zipformer-en-20M (sherpa-onnx) -- SMALLEST / STREAMING FALLBACK

**Why:** Extremely small at ~20MB. Native ONNX, Apache-2.0 license, and
sherpa-onnx has Go bindings already. Streaming transducer architecture.
Lower accuracy than Moonshine but viable for command/keyword use cases.

- **ONNX size:** ~20 MB
- **WER:** ~8-10% estimated on LibriSpeech (less well-documented)
- **Latency:** Sub-real-time on ARM (Raspberry Pi), very fast on x86
- **Preprocessing:** 80-dim log-mel filterbank features at 16kHz
- **License:** Apache-2.0
- **Streaming:** Yes (transducer-based)

## Risk Assessment

### Moonshine Tiny/Base

| Risk | Severity | Mitigation |
|---|---|---|
| ONNX opset compatibility with ORT 1.24+ | Medium | Test early; Moonshine targets ORT natively |
| Multi-model ONNX files (encoder+decoder) | Medium | Need to manage two inference sessions |
| No existing Go integration | High | Must write Go bindings from scratch using yalue/onnxruntime_go |
| Non-English models use restrictive license | Low | English models are MIT; only affects future multilingual |
| Audio resampling to 16kHz in Go | Low | Straightforward with go-audio or manual resampling |

### Zipformer-en-20M

| Risk | Severity | Mitigation |
|---|---|---|
| Lower accuracy for general transcription | High | Acceptable for voice commands, not for dictation |
| Mel spectrogram preprocessing in Go | High | Need to implement 80-dim log-mel in pure Go or use C lib |
| Sherpa-onnx Go bindings add CGo dependency | Medium | Could bypass sherpa and call ORT directly |

### Whisper tiny.en (NOT recommended but assessed)

| Risk | Severity | Mitigation |
|---|---|---|
| Large ONNX size (~117-221 MB) | High | Int8 helps but still large vs Moonshine |
| No native streaming | High | Must process full utterances; poor for real-time |
| Complex mel spectrogram preprocessing | High | 80-dim log-mel with specific windowing params |
| Decoder is autoregressive (slow) | Medium | Each token requires full decoder pass |

## Preprocessing Pipeline for Moonshine (Top Pick)

Moonshine's key advantage: it does NOT need mel spectrograms. The encoder
starts with 3 learned convolution layers that process raw audio directly.

### Required Go preprocessing:

```
1. Read audio (WAV/PCM) -> []float32 samples
2. Resample to 16kHz if needed
3. Normalize to [-1.0, 1.0] range
4. Pass directly to ONNX encoder
```

### Go libraries needed:

- **Audio I/O:** `go-audio/audio` + `go-audio/wav` for WAV reading
- **Resampling:** `zaf/go-resample` or manual sinc interpolation
- **ORT inference:** `yalue/onnxruntime_go` (already used in cattery for Kokoro)
- **No FFT/mel library needed** -- this is Moonshine's killer feature for Go

### Inference flow:

```
1. Load encoder.onnx and decoder.onnx into ORT sessions
2. Feed raw float32 audio to encoder -> get encoder hidden states
3. Feed encoder output + start token to decoder -> autoregressive decode
4. Decode token IDs to text using SentencePiece/tokenizer vocab
```

### Tokenizer consideration:

Moonshine uses a SentencePiece tokenizer. Options:
- Port the vocabulary lookup to pure Go (tokens -> text mapping only)
- Use a Go SentencePiece library like `markusressel/go-sentencepiece`

## Models NOT Recommended

| Model | Reason |
|---|---|
| Silero STT | AGPL-3.0 license incompatible with Apache-2.0 distribution |
| Vosk | Kaldi-based runtime, not pure ONNX; complex dependency chain |
| Whisper base/small | Too large for ONNX (base ~300MB, small ~950MB) |
| Canary/Parakeet | Models are 600MB-2.5B params; far too large |
| wav2vec2 | CTC-only, poor for general ASR; limited ONNX support |

## Key Findings Summary

1. **Moonshine Tiny is the clear winner** for cattery's use case: 27MB
   quantized ONNX, better WER than Whisper tiny, MIT license, native ONNX,
   streaming, and -- critically -- no mel spectrogram preprocessing needed.

2. **Moonshine Base is the upgrade path** at 60MB if accuracy matters more
   than size. Same integration effort.

3. **The preprocessing story is excellent.** Unlike every Whisper variant
   and most other ASR models, Moonshine processes raw 16kHz audio directly.
   This eliminates the hardest part of Go integration (implementing mel
   spectrograms).

4. **The main integration risk** is managing two ONNX sessions
   (encoder+decoder) and implementing the autoregressive decode loop in Go.
   This is the same pattern cattery already uses for Kokoro TTS, so the
   architecture is proven.

5. **sherpa-onnx is worth monitoring** as an alternative runtime. It has Go
   bindings and supports many models including Moonshine, but adds a CGo
   dependency that conflicts with cattery's dlopen approach.

## Sources

- [Moonshine GitHub](https://github.com/moonshine-ai/moonshine)
- [Moonshine Paper](https://arxiv.org/html/2410.15608v1)
- [Moonshine ONNX models on HuggingFace](https://huggingface.co/UsefulSensors/moonshine)
- [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx)
- [Whisper tiny.en ONNX specs (sherpa docs)](https://k2-fsa.github.io/sherpa/onnx/pretrained_models/whisper/tiny.en.html)
- [Zipformer-en-20M](https://huggingface.co/csukuangfj/sherpa-onnx-streaming-zipformer-en-20M-2023-02-17)
- [STT Benchmarks 2026 (Northflank)](https://northflank.com/blog/best-open-source-speech-to-text-stt-model-in-2026-benchmarks)
- [Whisper ONNX quantization analysis](https://arxiv.org/html/2503.09905v1)
- [ONNX-ASR library](https://github.com/istupakov/onnx-asr)
