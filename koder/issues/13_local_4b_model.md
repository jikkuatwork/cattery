# 13 — Local LLM via ONNX

## Status: open (research complete, ready for planning)
## Priority: P1

Run a local LLM via ONNX Runtime, same stack as TTS/STT. No external APIs needed.

## Target Model

**Qwen3.5-4B** (int4 quantized, ~2GB on disk, ~2.5-3GB RAM)

- Released March 2026. Dense architecture, Apache 2.0 license
- 262K context window, natively multimodal
- Best-in-class quality for sub-4B models
- ONNX export via HuggingFace Optimum (needs verification/conversion)
- Tokenizer: tiktoken-based (need pure Go impl or bundled vocab)

## UX

Same philosophy as TTS/STT — zero config by default:

```
cattery llm "What is the capital of France?"    # just works
cattery llm --stdin < prompt.txt                # pipe input
cattery llm --system "You are helpful" "Hi"     # system prompt
```

Model name, quantization, parameter count — none of this surfaces in simple mode.
`--advanced` reveals `--model`, `--temperature`, `--max-tokens`, etc.

## Memory Strategy: Engine Swapping

Pi4 4GB is the target floor. TTS+STT use ~180MB hot, LLM needs ~2.5-3GB.
Solution: **sequential pipeline with engine eviction** (already built in `server/pool.go`).

```
STT (listen) → evict → LLM (think) → evict → TTS (speak)
  ~180MB                 ~2.5-3GB                ~180MB
```

Only one heavy engine is loaded at a time. The existing lazy pool + idle eviction
+ `malloc_trim` reclaims C heap between phases. Extra latency per swap: ~1.4s.

Estimated round-trip on Pi4: ~8-12s (STT ~2s + swap ~1.4s + LLM ~3-5s + swap ~1.4s + TTS ~2s).

## Architecture

```
llm/
├── engine.go        # llm.Engine interface (Generate, Close)
└── qwen/
    └── qwen.go      # ORT session, KV-cache, autoregressive decode loop
```

- Shares ORT instance with TTS/STT (separate session)
- Auto-downloads int4 ONNX model to ~/.cattery/ (same as other artefacts)
- Registered in model registry as `qwen3.5-4b`
- Server exposes `/v1/chat/completions` (OpenAI-compatible)

## Open Questions

1. **ONNX export** — verify Qwen3.5-4B community ONNX exists on HuggingFace, or convert via Optimum
2. **Tokenizer in Go** — need tiktoken-compatible tokenizer; evaluate go-tiktoken or bundle vocab
3. **KV-cache in onnxruntime_go** — no published examples; needs spike to validate dynamic tensor I/O for autoregressive generation
4. **Eager eviction** — server needs to evict other modality pools before LLM borrow, not just on idle timeout

## Research (March 2026)

Models evaluated: Qwen3.5 (0.8B/2B/4B), Qwen3 (0.6B/1.7B/4B), Phi-4-mini (3.8B),
Llama 3.2 (1B/3B), Gemma 3 (1B/4B), SmolLM2-1.7B.

Qwen3.5-4B selected for: newest architecture, Apache 2.0 license, best quality/size ratio,
same ORT stack, dense (not MoE) for predictable memory.

Runtime decision: ONNX Runtime over llama.cpp — avoids second inference engine,
reuses existing dlopen/pool infrastructure, lower memory overhead on constrained devices.
