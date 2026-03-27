# Plan 36 — Local LLM ONNX Spike (Issue #13)

## Status: DRAFT

## Goal

De-risk a local `llm/` modality that reuses the existing ONNX Runtime stack,
registry, downloader, and lazy pool infrastructure already used by
[`tts/tts.go`](/home/glasscube/Projects/cattery/tts/tts.go) and
[`stt/stt.go`](/home/glasscube/Projects/cattery/stt/stt.go).

The target is a text-first Qwen3.5 local engine that can fit the current
project shape:

- `llm/engine.go` should mirror the existing modality interfaces
- model files should register through [`registry/registry.go`](/home/glasscube/Projects/cattery/registry/registry.go)
- downloads should flow through [`download/download.go`](/home/glasscube/Projects/cattery/download/download.go)
- server-side lifecycle should reuse [`server/pool.go`](/home/glasscube/Projects/cattery/server/pool.go) and the existing modality swap pattern in [`server/server.go`](/home/glasscube/Projects/cattery/server/server.go)

This plan is intentionally a spike plan, not an implementation plan. The work
should stop after we have hard evidence that the model artefacts, tokenizer,
and autoregressive decode loop are viable on the current stack.

## Current Repo Anchors

- [`stt/moonshine/decoder.go`](/home/glasscube/Projects/cattery/stt/moonshine/decoder.go) already proves `onnxruntime_go` can run a decoder loop with dynamic sessions, zero-length cache tensors, and per-step KV-cache updates.
- [`stt/moonshine/moonshine.go`](/home/glasscube/Projects/cattery/stt/moonshine/moonshine.go) shows the current pattern for loading multiple ONNX files plus a tokenizer from registry metadata.
- [`tts/kokoro/kokoro.go`](/home/glasscube/Projects/cattery/tts/kokoro/kokoro.go) shows the expected engine constructor and download/bootstrap flow for a local modality.
- [`server/server.go`](/home/glasscube/Projects/cattery/server/server.go) already has independent TTS/STT pools and a shared ORT lifetime boundary; issue `#13` will need the same pattern for LLM plus an explicit pre-borrow eviction path.

## Upstream Facts To Treat As Inputs

As of March 27, 2026:

- `onnx-community/Qwen3.5-4B-ONNX` exists on Hugging Face and was last updated on March 9, 2026.
- `onnx-community/Qwen3.5-0.8B-ONNX` exists on Hugging Face and was last updated on March 4, 2026.
- `Qwen/Qwen3.5-4B` exists on Hugging Face and was last updated on March 2, 2026.
- The 4B ONNX repo already contains text-path q4 artefacts:
  `onnx/decoder_model_merged_q4.onnx`,
  `onnx/decoder_model_merged_q4.onnx_data`,
  `onnx/decoder_model_merged_q4.onnx_data_1`,
  `onnx/embed_tokens_q4.onnx`,
  `onnx/embed_tokens_q4.onnx_data`,
  plus `tokenizer.json`, `config.json`, `generation_config.json`,
  `tokenizer_config.json`, and `chat_template.jinja`.
- The text-only q4 file set above is about `3,128,365,749` bytes total
  (`~2.91 GiB`) before any optional vision files.
- `tokenizer_config.json` reports `tokenizer_class: "Qwen2Tokenizer"`.
- `tokenizer.json` is a BPE tokenizer with `248,044` vocab entries and
  `247,587` merges, which means the tokenizer spike should verify whether a
  GPT-2-style BPE loader is sufficient before assuming a custom tiktoken port.

## Proposed `llm/` Shape

```text
llm/
├── engine.go        # llm.Engine interface
└── qwen/
    ├── qwen.go      # model bootstrap, session setup, Generate loop
    ├── decode.go    # autoregressive loop + KV-cache handling
    └── tokenizer.go # pure Go BPE/tokenizer adapter
```

Proposed minimal engine surface:

```go
type Engine interface {
    Generate(ctx context.Context, prompt string, opts Options) (*Result, error)
    Close() error
}
```

The spike should validate that this surface is enough before any server or CLI
integration is attempted.

## Spike 1 — ONNX Model Availability

### Goal

Prove that Qwen3.5-4B can be treated as a normal cattery local artefact:
downloadable, registrable, mirrorable, and loadable with the same metadata
pattern already used for Kokoro and Moonshine.

### Concrete steps

1. Confirm the exact minimum text-only file set from `onnx-community/Qwen3.5-4B-ONNX`.
   Exclude vision artefacts unless the decode prototype proves they are required.
2. Record exact filenames, upstream URLs, and content lengths for:
   `decoder_model_merged_q4*`, `embed_tokens_q4*`, `tokenizer.json`,
   `config.json`, `generation_config.json`, `tokenizer_config.json`,
   and `chat_template.jinja`.
3. Verify whether `embed_tokens_q4.onnx` is mandatory at runtime or whether the
   merged decoder can accept `input_ids` directly for text-only generation.
   This matters for registry size, download time, and engine complexity.
4. Prototype a temporary registry entry in the same shape as
   [`registry/registry.go`](/home/glasscube/Projects/cattery/registry/registry.go):
   one `KindLLM` model with explicit file list and metadata keys for
   `decoder_file`, `decoder_data_files`, `embed_file`, `tokenizer_file`,
   `context_window`, `hidden_size`, `num_layers`, `num_heads`,
   `num_kv_heads`, and `eos_token`.
5. Verify downloader behavior against split `.onnx_data` files in
   [`download/download.go`](/home/glasscube/Projects/cattery/download/download.go),
   including progress bars, SHA handling, and disk-space checks.
6. Decide hosting strategy:
   direct Hugging Face URLs first, or mirror the exact text-only bundle into
   `cattery-artefacts` Git LFS once licensing and size are acceptable.
7. Download the confirmed text-only q4 file set from HuggingFace and upload to
   `cattery-artefacts` Git LFS, following the same layout as TTS/STT models.
   - Strip vision artefacts and any files not needed for text-only generation.
   - Verify SHA checksums match upstream after upload.
   - Update the registry entry URLs to point at the `cattery-artefacts` mirror.
   - If the bundle is too large for LFS (>4 GiB limit per file), split or keep
     HuggingFace as primary source and document the decision.
8. If the upstream 4B ONNX artefacts are missing, broken, or too large to host,
   document the fallback conversion pipeline:
   PyTorch `Qwen/Qwen3.5-4B` ->
   Optimum ONNX export ->
   ORT preprocessing ->
   ONNX Runtime quantization to q4/int4-compatible weights ->
   repack into the cattery registry file layout.

### Success criteria

- We have a checked-in note inside the plan or issue with the exact upstream
  file inventory and byte sizes.
- We know whether the text-only runtime needs one ONNX file or a
  decoder-plus-embed pair.
- We know whether the artefact bundle fits the project’s current distribution
  model:
  direct-download only, mirrored to `cattery-artefacts`, or too large for LFS.
- A future implementation can add a registry entry without needing more model
  discovery work.

### Estimated effort

`0.5-1 day`

### Risk / fallback

- Risk: the multimodal export may require extra non-text artefacts or unusual
  runtime wiring.
- Risk: `~2.91 GiB` text-only q4 is materially larger than the current TTS/STT
  artefacts and may be a poor fit for Git LFS mirroring.
- Fallback: treat Hugging Face as the primary download source and mirror only
  after the runtime path is proven.
- Fallback: if the 4B export is unstable, use `onnx-community/Qwen3.5-0.8B-ONNX`
  as the implementation spike target while keeping 4B as the product target.

## Spike 2 — Go Tokenizer

### Goal

Prove that prompt assembly and token encode/decode can be done in pure Go with
acceptable correctness and negligible latency, without adding Python or CGo.

### Concrete steps

1. Inspect `Qwen/Qwen3.5-4B` tokenizer assets:
   `tokenizer.json`, `tokenizer_config.json`, `vocab.json`, `merges.txt`,
   and `chat_template.jinja`.
2. Evaluate whether an existing pure Go library can load Qwen’s BPE assets
   directly.
   Candidates:
   `go-tiktoken` if it can ingest custom vocab/merges,
   or a simpler GPT-2-style BPE loader if the Qwen tokenizer does not require
   OpenAI-specific rank tables at runtime.
3. Build a tiny tokenizer spike package under `cmd/` or `internal/` that:
   - loads tokenizer assets from disk
   - renders a minimal chat prompt from `chat_template.jinja` or an equivalent
     hard-coded text template
   - encodes a short prompt
   - decodes sampled token IDs back to text
4. Cross-check output against Hugging Face Python tokenization for a fixed set
   of prompts:
   plain prompt, system+user prompt, code snippet, Unicode prompt, and a long
   prompt that exercises whitespace and special tokens.
5. Benchmark encode/decode throughput on amd64 and arm64 using the same style as
   existing lightweight spikes.
   The threshold is pragmatic: tokenizer time must be lost in the noise
   relative to model inference.
6. Decide what to store in the registry:
   `tokenizer.json` only, or `tokenizer.json` plus `vocab.json`/`merges.txt`
   if the chosen Go implementation needs the split files.
7. Define the minimal `llm.Options` fields that affect tokenization:
   system prompt, max output tokens, temperature, and stop tokens.

### Success criteria

- A pure Go tokenizer path exists and round-trips representative prompts.
- The chosen implementation can reproduce special token handling needed for
  chat completion prompts.
- Encode/decode cost is negligible compared with one decode loop run.
- We know the exact tokenizer files that must be registered and downloaded.

### Estimated effort

`1 day`

### Risk / fallback

- Risk: `go-tiktoken` may not support loading arbitrary Qwen vocab/merge data.
- Risk: `chat_template.jinja` may force templating logic that is more complex
  than the first-pass CLI/server UX needs.
- Fallback: ignore Jinja for the spike and hard-code the minimal message format
  needed to match Qwen chat prompts.
- Fallback: if no existing library is good enough, write a narrow BPE loader
  for Qwen assets instead of adopting a large general tokenizer dependency.

## Spike 3 — KV-Cache Autoregressive Decode in `onnxruntime_go`

### Goal

Prove that a local LLM decode loop is viable with `onnxruntime_go` and the
current dlopen-based ORT strategy, including dynamic tensors for cache input and
per-token stepping.

### Concrete steps

1. Start from the working cache pattern in
   [`stt/moonshine/decoder.go`](/home/glasscube/Projects/cattery/stt/moonshine/decoder.go),
   not from a blank implementation.
   Reuse the same ideas:
   dynamic advanced session,
   explicit input/output name lists,
   zero-length first-step cache tensors,
   cache replacement on each step,
   and manual `Destroy()` discipline.
2. Inspect the ONNX graph input/output names for
   `decoder_model_merged_q4.onnx` and confirm the exact text-generation API
   shape:
   `input_ids` or `inputs_embeds`,
   `attention_mask`,
   `position_ids`,
   `past_key_values.*`,
   logits,
   and `present_key_values.*`.
3. Build a standalone decode spike against
   `onnx-community/Qwen3.5-0.8B-ONNX` first.
   The smaller model reduces download size and memory pressure while we prove
   the loop mechanics.
4. Once the 0.8B spike can generate deterministic tokens, repeat the same loop
   against the 4B q4 artefacts and record:
   first-token latency,
   steady-state tok/s,
   peak RSS,
   and whether engine teardown returns memory cleanly enough for modality swaps.
5. Validate the two main input strategies:
   - direct `input_ids` into merged decoder
   - external `embed_tokens_q4` session feeding decoder inputs if required
6. Verify memory behavior with the existing pool model in
   [`server/pool.go`](/home/glasscube/Projects/cattery/server/pool.go):
   ensure an LLM borrow can coexist with current lazy creation semantics, then
   specify the extra explicit-eviction hook needed before LLM work begins.
7. Draft the first-pass `llm.Engine` and server integration seam:
   `server` will eventually need an `llmPool`, a `think` borrow path, and an
   OpenAI-style `POST /v1/chat/completions` route, but the spike should stop at
   a local benchmark binary and interface sketch.
8. Compare the decode loop difficulty with a thin CGo wrapper around
   `onnxruntime-genai` only after the pure Go ORT path has been attempted and
   measured.

### Success criteria

- A local benchmark binary can generate text token-by-token using
  `onnxruntime_go`.
- The spike documents the exact tensor names and shapes required by the Qwen
  decoder graph.
- Cache handling is stable across multiple tokens without shape or lifetime
  errors.
- We have a measured throughput baseline on at least one amd64 machine and one
  arm64 machine, even if the arm64 run uses the 0.8B model.
- We know whether the existing pool + ORT teardown strategy is good enough for
  STT -> LLM -> TTS swapping on 4 GB-class devices.

### Estimated effort

`1.5-2 days`

### Risk / fallback

- Risk: Qwen’s decoder graph may require more inputs than Moonshine’s decoder
  path, especially around attention masks, positions, and embeddings.
- Risk: `onnxruntime_go` may support the tensor plumbing but still be too clumsy
  for a maintainable production decode loop.
- Risk: 4B q4 throughput may be acceptable on x86 but too slow on Pi-class ARM.
- Fallback: ship the spike with `0.8B` as the proven implementation target and
  keep `4B` as an optional higher-memory model.
- Fallback: if cache wiring or throughput is impractical, evaluate a very thin
  CGo bridge to `onnxruntime-genai` while still keeping the registry, download,
  and pool layers in Go.

## Recommended Execution Order

1. Spike 1 first, because it fixes the exact file inventory and hosting story.
2. Spike 2 second, because tokenizer certainty is required before prompt and
   decode validation means anything.
3. Spike 3 last, starting with `0.8B` for mechanics and only then moving to
   `4B` q4 for real memory/perf measurements.

## Exit Criteria For Planning

After these spikes, we should be able to make a hard go/no-go call on issue
`#13` without more open-ended research:

- `Go`: pure Go tokenizer works, ORT decode loop works, and the artefact bundle
  is operationally manageable.
- `Conditional go`: `0.8B` works but `4B` needs better hosting or a different
  hardware target.
- `No-go on pure Go ORT`: tokenizer is fine, but decode complexity or
  performance forces a thin `onnxruntime-genai` wrapper.
