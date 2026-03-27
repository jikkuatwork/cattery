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

### Status

Complete on March 28, 2026.

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

### Findings

- Verified upstream repo: `https://huggingface.co/onnx-community/Qwen3.5-4B-ONNX`
  with text-only q4 artefacts fetched from `resolve/main/...`.
- Exact text-only q4 inventory and byte sizes:
  - `onnx/decoder_model_merged_q4.onnx` — `1,206,737`
  - `onnx/decoder_model_merged_q4.onnx_data` — `2,093,368,320`
  - `onnx/decoder_model_merged_q4.onnx_data_1` — `607,298,560`
  - `onnx/embed_tokens_q4.onnx` — `857`
  - `onnx/embed_tokens_q4.onnx_data` — `407,244,800`
  - `tokenizer.json` — `19,226,111`
  - `config.json` — `3,198`
  - `generation_config.json` — `248`
  - `tokenizer_config.json` — `9,162`
  - `chat_template.jinja` — `7,756`
  - Total — `3,128,365,749` bytes (`~2.91 GiB`)
- Downloader check: [`download/download.go`](/home/glasscube/Projects/cattery/download/download.go)
  already downloads each `registry.Artefact` filename independently, so split
  `.onnx_data` shards work without downloader changes as long as every shard is
  listed as its own file in the registry.
- `embed_tokens_q4` is still likely required for text-only runtime. This is an
  inference, not a proven runtime fact yet: the export names the decoder file
  `decoder_model_merged_q4`, but the repo still ships a separate
  `embed_tokens_q4` graph and `config.json` advertises external data for both
  `embed_tokens` and `decoder_model_merged_q4`. Until Spike 3 proves the graph
  accepts raw `input_ids` end to end, the safer assumption is a two-session
  runtime: embed session plus merged decoder session.
- Hosting decision: start with direct Hugging Face URLs in the registry and
  defer mirroring to `cattery-artefacts` until the runtime path is proven.
  `~2.91 GiB` is plausible but borderline for Git LFS-based mirroring, and
  there is no value in duplicating the bundle before decode viability is
  confirmed.
- Verified config metadata from upstream files:
  - `config.json` `text_config.hidden_size` — `2560`
  - `config.json` `text_config.num_hidden_layers` — `32`
  - `config.json` `text_config.num_attention_heads` — `16`
  - `config.json` `text_config.num_key_value_heads` — `4`
  - `config.json` `text_config.max_position_embeddings` — `262144`
  - `config.json` `text_config.eos_token_id` — `248044`
  - `generation_config.json` `eos_token_id` — `[248046, 248044]`

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

### Status

Complete on March 28, 2026.

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

### Findings

- Added spike binary: [`cmd/spike-tokenizer/main.go`](/home/glasscube/Projects/cattery/cmd/spike-tokenizer/main.go).
  It downloads `tokenizer.json` and `tokenizer_config.json` from
  `onnx-community/Qwen3.5-4B-ONNX`, loads the tokenizer, renders a hard-coded
  ChatML prompt, and prints prompt -> token IDs -> decoded text -> round-trip.
- Library evaluation:
  - `github.com/daulet/tokenizers` can load Hugging Face `tokenizer.json`, but
    it requires a separately built/downloaded Rust `libtokenizers` static
    library plus CGo. That adds a new native dependency beyond the existing ORT
    boundary, so it is not the preferred production path here.
  - `github.com/pkoukk/tiktoken-go` is a tiktoken loader. It can work with
    OpenAI-style BPE rank tables, but it is not a direct fit for Hugging Face
    `tokenizer.json` and would still require custom conversion logic.
  - `github.com/sugarme/tokenizer` is pure Go and can parse Hugging Face-style
    tokenizer configs, but it failed on the real Qwen tokenizer: the
    pretokenizer regex in `tokenizer.json` contains negative lookahead
    (`\\s+(?!\\S)`), which caused a panic because the library routes that regex
    through Go's `regexp`, which does not support Perl lookarounds.
- Chosen approach for the spike: a narrow pure Go loader in the spike binary
  itself, not a reusable package yet. It implements only the pieces required by
  Qwen's current tokenizer JSON:
  - NFC normalization
  - the Hugging Face split regex via `github.com/dlclark/regexp2`
  - GPT-2/Qwen byte-level byte<->unicode mapping
  - BPE merge ranking loaded directly from `tokenizer.json`
  - exact special-token extraction for added tokens such as
    `<|im_start|>`, `<|im_end|>`, and `<|endoftext|>`
- Special-token handling is correct in the spike:
  - `<|endoftext|>` -> `248044`
  - `<|im_start|>` -> `248045`
  - `<|im_end|>` -> `248046`
  - The chat prompt
    `<|im_start|>system\n...\n<|im_start|>assistant\n`
    encodes with explicit special-token IDs and decodes back to the original
    prompt exactly.
- Encode/decode correctness from the spike:
  - `"Hello, world!"` -> `[9419 11 1814 0]` -> round-trip `true`
  - `"What is the capital of France?"` -> `[3710 369 279 6511 314 9338 30]` -> round-trip `true`
  - `"func main() { fmt.Println(\"hello\") }"` -> `[2739 1822 363 313 8611 12063 437 14556 871 333]` -> round-trip `true`
  - `"日本語のテスト"` -> `[247359 15303 181801]` -> round-trip `true`
  - ChatML prompt with `<|im_start|>` / `<|im_end|>` -> round-trip `true`
- Registry/download decision stays the same as Spike 1: `tokenizer.json` is
  sufficient. The spike loads vocab, merges, byte-level config, and added
  tokens directly from that single file; no separate `vocab.json` or
  `merges.txt` is required.
- Gotcha: the downloaded ONNX-community `tokenizer_config.json` currently
  reports `tokenizer_class: "TokenizersBackend"` in the spike environment, not
  `Qwen2Tokenizer`. The tokenizer behavior still matches the Qwen byte-level
  BPE layout, so the JSON contents matter more than the class string.
- Recommendation for implementation after the spike:
  keep the runtime path pure Go and extract a small internal tokenizer adapter
  from this spike rather than adopting either a new CGo/Rust dependency or a
  large generic tokenizer library that does not fully support Qwen's regex
  pretokenizer.

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

### Findings

- Added spike binary: [`cmd/spike-llm/main.go`](/home/glasscube/Projects/cattery/cmd/spike-llm/main.go).
  It downloads the text-only `Qwen3.5-0.8B-ONNX` q4 artefacts into
  `models-data/qwen3.5-0.8b/`, initializes ORT from `~/.cattery/ort/`,
  prints the exact embed/decoder graph signatures, tokenizes a ChatML prompt,
  runs `embed_tokens_q4.onnx`, then performs greedy autoregressive decode with
  per-step cache replacement until EOS or `max_tokens`.
- Runtime proved that the merged decoder does **not** accept `input_ids`
  directly. The text path is a required two-session pipeline for this model:
  `input_ids -> embed_tokens_q4 -> inputs_embeds -> decoder_model_merged_q4`.
- Exact 0.8B graph contract observed from `onnxruntime_go.GetInputOutputInfo`:
  - Embed input:
    `input_ids` `int64[-1,-1]`
  - Embed output:
    `inputs_embeds` `float32[-1,-1,1024]`
  - Decoder core inputs:
    `inputs_embeds` `float32[-1,-1,1024]`
    `attention_mask` `int64[-1,-1]`
    `position_ids` `int64[3,-1,-1]`
  - Decoder state inputs, repeated per layer:
    `past_conv.N` `float32[-1,6144,4]` for layers
    `0,1,2,4,5,6,8,9,10,12,13,14,16,17,18,20,21,22`
    `past_recurrent.N` `float32[-1,16,128,128]` for the same layers
    `past_key_values.N.key` / `past_key_values.N.value`
    `float32[-1,2,-1,256]` for layers `3,7,11,15,19,23`
  - Decoder outputs:
    `logits` `float32[-1,-1,248320]`
    plus matching `present_conv.N`, `present_recurrent.N`, and
    `present.N.key` / `present.N.value` tensors with the same shapes.
- Important decode detail: `Qwen3.5-0.8B` is a **hybrid** export, not a plain
  transformer-only KV-cache model. The first-pass Moonshine-style logic was not
  enough; the spike had to preserve three state families:
  convolution state, recurrent state, and sparse full-attention KV state.
  Initial state tensors that worked:
  - `past_conv.*` -> zero-filled `[1,6144,4]`
  - `past_recurrent.*` -> zero-filled `[1,16,128,128]`
  - `past_key_values.*` -> empty `[1,2,0,256]`
  - `position_ids` -> text-only `[3,1,seq]` with the same scalar positions
    repeated across the leading `3` plane required by the exported mRoPE layout
- The autoregressive loop now works end to end on the 0.8B model in pure Go.
  Sample prompt:
  `<|im_start|>user\nWrite one short sentence about cats.<|im_end|>\n<|im_start|>assistant\n`
  generated:
  `<think>\n\n</think>\n\nCats are known for their playful antics and silent communication through their purrs.<|im_end|>`
- Measured on this amd64 Linux workstation on March 28, 2026:
  - First-token latency: `643.455038ms`
  - Steady-state throughput: `7.58 tok/s`
  - Peak RSS (`/proc/self/status` `VmHWM`): `775.62 MiB`
  - Total generation time for 21 output tokens: `3.280845169s`
- Tokenizer follow-up discovered during Spike 3:
  decode must map **all** `added_tokens`, not only `special` ones, because the
  model can emit non-special added tokens such as `<think>` / `</think>`.
- Decode loop complexity assessment:
  - `onnxruntime_go` itself is sufficient. Dynamic sessions, dynamic output
    allocation, and explicit value destruction are enough for local generation.
  - The difficult part is model-specific state plumbing, not ORT bindings.
  - The `0.8B` export is materially more complex than the expected `4B`
    transformer-style cache pattern because of its hybrid linear-attention /
    recurrent blocks.
- Recommendation:
  - **Go** for a pure-Go ORT local LLM path in principle.
  - **Conditional** on target model choice: this exact 0.8B contract is viable,
    but it is more complex than the original 4B assumptions.
  - For the product target, inspect the 4B graph before implementation rather
    than assuming the 0.8B state layout carries over. The same high-level
    architecture should still work:
    separate embed session,
    dynamic decoder session,
    per-step state replacement,
    explicit `Destroy()` discipline.
    But the exact state tensor set must be rediscovered from the 4B ONNX graph
    first.

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
