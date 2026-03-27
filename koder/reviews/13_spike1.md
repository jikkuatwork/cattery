# Review: Issue #13 — Spike 1 (ONNX Model Availability)

**Verdict: PASS**

Reviewed files:
- `registry/registry.go` (lines 14–240)
- `koder/plans/36_llm_spike.md` (Spike 1 Findings section, lines 138–169)

---

## Summary

The registry entry for `qwen3.5-4b-v1.0` follows existing patterns cleanly, the plan findings are thorough and accurate against the spike's success criteria, and the build passes. Two minor items noted below — neither blocks merging.

---

## Findings

### P3 — Minor / informational

**P3-1: `generation_config.json` lists two EOS tokens; registry only stores one.**
`registry.go:238` stores `"eos_token": "248044"`, but the plan findings (line 177) note that `generation_config.json` contains `eos_token_id: [248046, 248044]`. The second EOS (`248046`) may be the `<|im_end|>` chat marker. This is fine for Spike 1 (registry entry only), but Spike 3 decode loop will need to handle both stop tokens. Suggest adding a `"eos_tokens"` meta key or documenting the intent to handle the second EOS at decode time.

**P3-2: No SHA256 checksums on artefact entries.**
Kokoro has a SHA on its model file; Moonshine has none. The Qwen entry follows the Moonshine pattern (no SHAs). Acceptable for now — direct HF URLs make checksums less critical — but worth adding once the artefacts are mirrored or the runtime is proven.

**P3-3: `embed_data_files` meta key uses comma-separated value.**
`registry.go:227` stores `"embed_data_files": "onnx/embed_tokens_q4.onnx_data"` (single file, but the key name implies plurality matching the `decoder_data_files` pattern at line 225). This is consistent and fine, just noting that downstream code will need to split on comma for both keys.

---

## Checklist Responses

| # | Question | Result |
|---|----------|--------|
| 1 | Follows Kokoro/Moonshine patterns? | **Yes.** Same struct shape: Index, ID, Kind, Location, Name, Description, Lang, Files, Meta. No Voices (correct for LLM). |
| 2 | File sizes and URLs correct? | **Yes.** 10 artefacts with explicit HF `resolve/main/` URLs. Sizes match the plan's upstream inventory exactly. |
| 3 | Meta keys complete for downstream `llm/qwen/`? | **Yes.** All keys from the plan's step 4 are present: `decoder_file`, `decoder_data_files`, `embed_file`, `embed_data_files`, `tokenizer_file`, `config_file`, `generation_config_file`, `tokenizer_config_file`, `chat_template_file`, `context_window`, `hidden_size`, `num_layers`, `num_heads`, `num_kv_heads`, `eos_token`. |
| 4 | Index and Kind correct? | **Yes.** `Index: 1`, `Kind: KindLLM`. Index 1 is correct — it's the first (and only) LLM model, same as Kokoro (Index 1 for TTS) and Moonshine (Index 1 for STT). |
| 5 | Plan findings accurate and complete? | **Yes.** Findings cover: upstream repo verification, exact file inventory with byte sizes, downloader compatibility with split shards, embed_tokens assessment, hosting decision (HF-first, defer mirroring), and config metadata extraction. All success criteria are addressed. |
| 6 | `go build ./...` passes? | **Yes.** Verified — clean build, clean vet. |
| 7 | Split `.onnx_data` files as separate Artefacts? | **Correct approach.** Each shard is its own `Artefact` entry with its own URL/size. The existing downloader iterates `model.Files` independently, so this works without downloader changes. The `decoder_data_files` meta key provides the ordered comma-separated list for runtime reassembly. |

---

## Recommendation

Merge as-is. The P3 items are tracking notes for Spike 2/3, not blockers.
