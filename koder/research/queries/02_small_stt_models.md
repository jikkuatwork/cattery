# Research: Small Speech-to-Text Models for Local ONNX Inference

## Context

Cattery is a Go TTS library that packages Kokoro-82M (92MB ONNX model) for
local text-to-speech. It uses ONNX Runtime via dlopen, downloads model files on
first run, and targets a single-binary experience with ~8MB binary + ~120MB
downloaded artefacts. The only system dependency is espeak-ng.

We want to add an STT module (#08) to complete the audio pipeline toward a
local conversational system (STT → LLM → TTS). The STT model must fit the same
packaging model: ONNX format, small enough to download on first run, fast
enough for real-time CPU inference on a 4-6 core machine (no GPU), and
permissively licensed.

The key constraint is **ONNX Runtime compatibility from Go** — we call ORT via
the `yalue/onnxruntime_go` bindings. The model must either ship as ONNX or have
a well-tested ONNX export path. Models that only work via PyTorch or custom C++
runtimes are not viable.

## What We Need

### 1. Model Survey (2024-2025 landscape)

- Which small STT/ASR models exist under ~200MB ONNX size?
- Include: Whisper tiny/base/small, Whisper.cpp ONNX exports, Moonshine
  (Useful Sensors), Silero STT, Vosk models, Canary (NVIDIA NeMo), Parakeet,
  wav2vec2 tiny variants, Zipformer/icefall small models, and any other
  recent lightweight ASR models released in 2024-2025.
- For each: model size (ONNX), parameter count, architecture type
  (encoder-decoder vs CTC vs transducer).

### 2. ONNX Compatibility Assessment

- Which models have official or community ONNX exports?
- Are there known issues with specific ONNX opsets or operators?
- Do any require custom operators not in standard ORT?
- Whisper ONNX: what's the state of `optimum` exports vs `whisper.cpp` ONNX?
- Moonshine: does it ship ONNX natively or need conversion?
- Which models work with ONNX Runtime 1.24+ without modifications?

### 3. Accuracy Comparison (WER)

- Word Error Rate on standard benchmarks (LibriSpeech test-clean,
  test-other, Common Voice).
- How do tiny/base variants compare to each other?
- Any models that punch above their weight class (small size, good WER)?

### 4. Latency and Resource Usage

- Real-time factor (RTF) on CPU for each model.
- Peak memory usage during inference.
- Does the model support streaming/chunked inference, or is it batch-only?
- Cold start time (model load).

### 5. Language Support

- English-only vs multilingual.
- For our use case English-first is fine, but multilingual is a plus.
- Which models handle accented English well?

### 6. Licensing

- License for each model (weights and code separately).
- Any models with restrictive or unclear licensing?
- Which are safe for Apache-2.0 distribution (bundling or downloading)?

### 7. Preprocessing Requirements

- What audio preprocessing does each model need? (sample rate, FFT, mel
  spectrograms, VAD)
- Can preprocessing be done in pure Go, or does it need a C library?
- Are there Go libraries for the required audio processing?

## Output Format

1. **Comparison table**: model name, size (ONNX), WER (LibriSpeech clean),
   RTF (CPU), ONNX status, license, streaming support.
2. **Top 3 recommendations** ranked for our use case (small, ONNX-native,
   permissive license, good accuracy).
3. **Risk assessment** for each recommendation: what could go wrong during
   integration?
4. **Preprocessing pipeline** sketch for the top pick: what Go libraries or
   code would be needed.
