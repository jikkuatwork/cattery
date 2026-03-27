# Review: Issue #13 — Spike 3 (KV-Cache Autoregressive Decode)

**Verdict: PASS**

Reviewed files:
- `cmd/spike-llm/main.go` (1225 lines)
- `koder/plans/36_llm_spike.md` (Spike 3 Findings section, lines 397–469)

---

## Summary

The spike proves end-to-end autoregressive decode in pure Go with `onnxruntime_go`.
The code correctly handles the hybrid three-family state architecture (conv, recurrent,
sparse KV), follows the Moonshine decoder pattern of dynamic sessions with explicit
`Destroy()` discipline, and demonstrates clean memory lifecycle across all tensor
types. The plan findings are accurate and the conditional go recommendation is
well-reasoned. Two P2 items and three P3 items noted below — none block merging.

---

## Findings

### P2 — Should fix before implementation extraction

**P2-1: `stepEmbeds` leaks on mid-loop error returns.**
If `buildAuxTensors`, `buildDecoderInputs`, `decoderSession.Run`, `nextToken`, or
`replaceCache` fails on step > 0, the function returns early. At that point
`stepEmbeds` is a `nextEmbeds` value from the previous iteration that differs from
`inputEmbeds` — but the cleanup at lines 413–415 never runs because it's after the
loop, not in a defer. The `defer inputEmbeds.Destroy()` (line 336) and
`defer destroyValues(cache)` (line 345) do fire, but `stepEmbeds` is orphaned.

Harmless in a spike binary (exits on error), but worth fixing when extracting to
`llm/qwen/decode.go`: either defer a cleanup closure that checks
`stepEmbeds != inputEmbeds`, or nil-out `stepEmbeds` after Destroy in the loop body
so the post-loop guard catches it.

**P2-2: Dead function `indexOfString`.**
`indexOfString` at line 1070 is defined but never called. Minor dead code — remove
when extracting.

### P3 — Minor / informational

**P3-1: Tokenizer cache is unbounded.**
`qwenTokenizer.cache` (line 109) grows without bound. Fine for a spike with short
prompts, but production `llm/qwen/tokenizer.go` should cap or evict.

**P3-2: No context.Context in generate loop.**
The decode loop has no cancellation path. A stuck or slow model run would block
indefinitely. The implementation should thread `ctx` through and check
`ctx.Err()` between steps, mirroring how the Moonshine decoder could be extended.

**P3-3: HTTP downloads use default client with no timeout.**
`http.Get` at line 1191 has no deadline. Acceptable for a spike downloading from
HuggingFace, but production code already uses `download/download.go` which handles
this.

---

## Checklist Responses

| # | Question | Result |
|---|----------|--------|
| 1 | Follows Moonshine decoder.go patterns? | **Yes.** Uses `DynamicAdvancedSession`, explicit `Destroy()` on every ORT value, separate cache slice with per-step replacement (lines 514–539 mirror Moonshine's `kvCache` pattern). Zero-length initial cache for KV-cache layers, zero-filled for conv/recurrent — matches the graph's expectations. |
| 2 | Cache/state handling correct for all three families? | **Yes.** `past_conv.*` → zero-filled `[1,6144,4]`, `past_recurrent.*` → zero-filled `[1,16,128,128]`, `past_key_values.*` → empty `[1,2,0,256]`. `zeroStateTensor` (lines 595–615) dispatches correctly based on prefix and dimension index. `stateOutputName` (lines 573–584) maps all three input prefixes to their output counterparts. `replaceCache` (lines 514–539) destroys old values before replacing, sets output slots to nil to prevent double-destroy, and explicitly destroys logits. |
| 3 | position_ids correctly shaped as [3,1,seq] for mRoPE? | **Yes.** `buildAuxTensors` line 504 creates shape `[3, 1, stepSeq]`. All three planes get identical scalar positions (lines 499–503), which is correct for text-only generation (no spatial/temporal mRoPE dimensions). Prefill uses full prompt length; subsequent steps use seq=1. |
| 4 | attention_mask properly constructed and updated? | **Yes.** Mask is `[1, totalSeq]` all-ones (lines 486–495). `totalSeq` starts at prompt length and increments by 1 each step (line 407), so the mask grows to include all cached positions. This is the correct pattern for causal LM with KV-cache — the mask tells attention which cached positions are valid. |
| 5 | EOS tokens (248044, 248046) both checked? | **Yes.** Both are hard-coded constants (lines 35–36). `eosTokens` (lines 1117–1128) additionally parses both `config.json` and `generation_config.json` EOS fields via `asInt64Slice`, which handles both scalar and array JSON values. The set is checked on every step (line 395). |
| 6 | Memory cleanup correct? | **Yes, with P2-1 caveat.** Happy-path cleanup is thorough: `inputEmbeds` (defer line 336), `cache` (defer line 345), per-step `temp` zero tensors (line 371), `mask`/`positions` (lines 372–373), `stepEmbeds` post-loop (lines 413–415), and `replaceCache` nil-ing output slots after move (line 529). The embed session correctly destroys `idsTensor` via defer (line 546). No double-destroy risk — cache values used as inputs are only destroyed in `replaceCache` after `Run` completes. |
| 7 | Plan findings accurate, complete, and actionable? | **Yes.** Findings document: two-session pipeline requirement, exact graph contract with all three state families, initial state shapes, working sample output, measured performance, tokenizer discovery (`<think>` as non-special added token), and complexity assessment. All six success criteria from the plan are addressed. |
| 8 | Go/no-go recommendation well-reasoned? | **Yes.** "Go" for pure-Go ORT path in principle, "conditional" on model choice — the 0.8B hybrid architecture is more complex than expected, and the 4B graph contract must be rediscovered before assuming the same layout. This is pragmatic: the spike proved the mechanism works, but correctly flags that the state tensor set is model-specific. The recommendation to inspect the 4B graph before implementation is sound. |
| 9 | `go build ./...` pass? | **Yes.** Clean build, no warnings. |

---

## Tokenizer Correctness Note

The findings note (line 447–449) that "decode must map all added_tokens, not only
special ones" because the model emits `<think>` / `</think>`. The code already
handles this: line 687 adds ALL added tokens to `idToToken`, while only `Special:
true` tokens go to `idToSpecial`. The `Decode` function (lines 739–753) checks
`idToSpecial` first (direct passthrough), then falls back to `idToToken` (through
`decodeBytes`). Since `<think>` characters are printable ASCII, the GPT-2 byte table
maps them to themselves, so decode is correct. The findings document a real discovery
that the code already addresses.

---

## Spike Proves Go/No-Go

The spike demonstrates that:
1. `onnxruntime_go` can drive a hybrid LLM decode loop with three state families
2. Dynamic sessions + explicit value destruction is sufficient (no genai wrapper needed)
3. Performance is reasonable: 7.58 tok/s, 776 MB peak RSS on 0.8B
4. The hard part is model-specific state plumbing, not ORT bindings

The conditional recommendation is the right call. The mechanism is proven; the 4B
graph contract is the remaining unknown.
