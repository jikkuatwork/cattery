# Review: Issue #13 — Spike 2 (Go Tokenizer)

**Verdict: PASS**

Reviewed files:
- `cmd/spike-tokenizer/main.go` (598 lines)
- `go.mod` / `go.sum` (new deps: `dlclark/regexp2`, `golang.org/x/text`)
- `koder/plans/36_llm_spike.md` (Spike 2 Findings section, lines 258–312)

---

## Summary

The spike delivers a working pure Go BPE tokenizer that correctly loads the
Qwen `tokenizer.json`, handles special tokens, and round-trips all five test
case categories. The library evaluation is thorough and the decision to write a
narrow loader instead of adopting a CGo dependency or a broken pure Go library
is well-reasoned. Two minor performance/completeness items noted below — neither
blocks merging.

---

## Findings

### P2 — Worth fixing before extraction into `llm/qwen/tokenizer.go`

**P2-1: BPE merge loop merges only one occurrence of the best pair per iteration.**
`main.go:325–353` — The inner loop finds `bestIdx` (the position of the
lowest-rank pair) and then only merges at that single index. Standard BPE merges
**all** occurrences of the winning pair in one pass. The result is identical —
the same pair gets re-found next iteration — but the algorithm is O(n) slower
per merge step (O(n³) total vs O(n²) for standard BPE). For a spike with short
prompts this is invisible, but for production prompts at the Qwen context window
(262k tokens) it could matter. Fix when extracting to package:

```go
// Replace the single-index merge with an all-occurrences merge:
merged := make([]string, 0, len(symbols)-1)
bestPair := pairKey(symbols[bestIdx], symbols[bestIdx+1])
for i := 0; i < len(symbols); {
    if i < len(symbols)-1 && pairKey(symbols[i], symbols[i+1]) == bestPair {
        merged = append(merged, symbols[i]+symbols[i+1])
        i += 2
        continue
    }
    merged = append(merged, symbols[i])
    i++
}
```

### P3 — Minor / informational

**P3-1: `qwenTokenizer.cache` grows without bound.**
`main.go:75` — The BPE encode cache is a plain `map[string][]int` with no
eviction. Fine for a spike binary, but production extraction should either cap
the cache or document that it is bounded by vocab size (since each unique
byte-encoded chunk converges to a finite set).

**P3-2: No encode/decode benchmark results reported.**
Plan step 5 asks for throughput benchmarks on amd64/arm64 with the threshold
"tokenizer time must be lost in the noise relative to model inference". The
findings confirm correctness but don't report timing. Since BPE on short prompts
is obviously sub-millisecond and inference is seconds, this is pragmatically fine
— but a quick `time.Now()` measurement in the spike output would have closed the
success criterion explicitly.

**P3-3: Plan step 7 (`llm.Options` fields) not addressed in findings.**
The plan asks for the minimal `llm.Options` fields that affect tokenization
(system prompt, max output tokens, temperature, stop tokens). The findings
don't cover this. Acceptable — it's more of a Spike 3 / implementation concern
than a tokenizer-spike deliverable — but worth noting for completeness.

**P3-4: `models-data/` download directory is not gitignored.**
`main.go:22` downloads tokenizer files to `models-data/qwen3.5-4b-v1.0/`.
If someone runs the spike, these ~19 MB files could accidentally get staged.
The directory should be in `.gitignore` (or the spike should use `tmp/`).

---

## Checklist Responses

| # | Question | Result |
|---|----------|--------|
| 1 | Custom BPE loader correct for HF `tokenizer.json` format? | **Yes.** Loads vocab, merges (handles both string and array formats), byte-level tables, pretokenizer regex, and added tokens directly from the single JSON. NFC normalization applied. |
| 2 | Special tokens handled correctly? | **Yes.** `<|endoftext|>` → 248044, `<|im_start|>` → 248045, `<|im_end|>` → 248046. Tokens sorted longest-first for correct split precedence. Verified in both encode and decode paths. |
| 3 | ChatML template format correct for Qwen3.5? | **Yes.** `formatQwenChat` at line 531 produces standard ChatML: `<\|im_start\|>system\n...<\|im_end\|>\n<\|im_start\|>user\n...<\|im_end\|>\n<\|im_start\|>assistant\n`. Correctly leaves assistant turn open for generation. |
| 4 | Round-trip encoding works for all test cases? | **Yes.** Five categories tested: basic text, query, code with escapes, Unicode (日本語のテスト), and ChatML with special tokens. All round-trip `true`. |
| 5 | Library decision well-reasoned? | **Yes.** Three candidates evaluated: `daulet/tokenizers` (CGo/Rust dep — rejected), `pkoukk/tiktoken-go` (OpenAI format — rejected), `sugarme/tokenizer` (panics on Qwen regex lookahead — rejected). Custom narrow loader is the right call. |
| 6 | New `go.mod` dependencies minimal and appropriate? | **Yes.** Two new deps: `dlclark/regexp2` (pure Go, needed for Perl-style lookahead in pretokenizer regex) and `golang.org/x/text` (NFC normalization). Both are small, well-maintained, and serve clear purposes. |
| 7 | `go build ./...` passes? | **Yes.** Clean build, clean vet. |
| 8 | Spike 2 findings accurate and complete? | **Yes** with minor gaps (P3-2, P3-3). Core findings — library evaluation, chosen approach, special-token verification, round-trip results, registry decision, and extraction recommendation — are thorough and accurate. |

---

## Recommendation

Merge as-is. P2-1 is a performance note for the extraction phase, not a spike
blocker. The spike successfully proves the pure Go tokenizer path is viable and
unblocks Spike 3.
