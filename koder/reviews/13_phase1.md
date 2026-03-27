# Review: Issue #13 — Phase 1 (llm/ package extraction)

**Verdict: NEEDS FIXES** (1 P1, 2 P2, 4 P3)

Reviewed files:
- `llm/engine.go` (24 lines)
- `llm/qwen/qwen.go` (398 lines)
- `llm/qwen/decode.go` (411 lines)
- `llm/qwen/tokenizer.go` (457 lines)

Compared against:
- `tts/kokoro/kokoro.go` (constructor / close pattern)
- `stt/moonshine/moonshine.go` (constructor / close pattern)

---

## Summary

The extraction is clean and well-structured. The interface is minimal (`Generate`,
`Close`), the constructor follows the kokoro/moonshine two-session pattern with
correct error-path cleanup, the tokenizer is a faithful byte-level BPE with NFC
normalization and regexp2 pretokenizer, the decode loop handles all three state
families, and graph inspection is fully data-driven from ONNX metadata. ChatML
formatting is correct. `go build ./...` and `go vet ./...` pass clean.

One P1 (dead code that silently discards copies), two P2s (unused field, missing
Temperature support), and four P3 cosmetic/minor items.

---

## Findings

### P1 — Must fix

**P1-1: `newDecodeState` copies into zero-length slices, then overwrites.**
`decode.go:42-47` — The `stateGroup` is initialized with `values: make([]ortgo.Value, len(...))` but `specs` is left as nil (zero-value). Lines 42–44 `copy()` into nil slices (no-op — copies zero elements), then lines 45–47 immediately overwrite with fresh `append([]stateSpec(nil), ...)` slices. The `copy` calls are dead code that silently does nothing.

This isn't a runtime bug today (the `append` lines save it), but it masks intent — a reader will assume the `copy` is doing the work. Either remove the dead `copy` calls or allocate the `specs` slices up front and drop the `append` lines.

```go
// Current (lines 37-48):
state := &decodeState{
    conv:      stateGroup{values: make([]ortgo.Value, len(spec.convStates))},
    recurrent: stateGroup{values: make([]ortgo.Value, len(spec.recurrentStates))},
    kv:        stateGroup{values: make([]ortgo.Value, len(spec.kvStates))},
}
copy(state.conv.specs, spec.convStates)       // no-op: specs is nil
copy(state.recurrent.specs, spec.recurrentStates) // no-op
copy(state.kv.specs, spec.kvStates)           // no-op
state.conv.specs = append([]stateSpec(nil), spec.convStates...)      // actual copy
state.recurrent.specs = append([]stateSpec(nil), spec.recurrentStates...) // actual copy
state.kv.specs = append([]stateSpec(nil), spec.kvStates...)          // actual copy

// Fix — just keep the append lines:
state := &decodeState{
    conv:      stateGroup{specs: append([]stateSpec(nil), spec.convStates...), values: make([]ortgo.Value, len(spec.convStates))},
    recurrent: stateGroup{specs: append([]stateSpec(nil), spec.recurrentStates...), values: make([]ortgo.Value, len(spec.recurrentStates))},
    kv:        stateGroup{specs: append([]stateSpec(nil), spec.kvStates...), values: make([]ortgo.Value, len(spec.kvStates))},
}
```

### P2 — Should fix

**P2-1: `Options.Temperature` is declared but never read.**
`engine.go:15` defines `Temperature float64` in `Options`, but `Generate` always
calls `greedyArgmax` — there's no sampling path. This is fine for Phase 1 greedy-only,
but the field is misleading: callers will set it expecting an effect. Either remove the
field now and add it when sampling lands, or add a brief doc comment stating it's
reserved / not yet implemented. Prefer removal — the plan can re-add it.

**P2-2: `config.MaxTokens` loaded but never used.**
`qwen.go:193` loads `cfg.MaxTokens` from model metadata, but `Generate` at line 141
only checks `opts.MaxTokens` and falls back to the hardcoded `defaultMaxTokens` (256).
The per-model max is silently ignored. Either use `cfg.MaxTokens` as the fallback
instead of the constant, or remove the field from `config`.

### P3 — Optional / cosmetic

**P3-1: Constructor signature diverges from moonshine pattern.**
`moonshine.New(modelDir, meta map[string]string)` takes a raw meta map.
`qwen.New(modelDir, model *registry.Model)` takes a full `*registry.Model`. Both work,
but the difference means the `llm` package has a hard dependency on `registry` while
`stt` doesn't. Not blocking — just noting the asymmetry for future engine-swap work
(issue #23/#30).

**P3-2: `sortStateSpecs` is O(n²) bubble sort.**
`qwen.go:373-381` — State spec count is small (~30–60 entries), so this is fine for
correctness. Could use `sort.Slice` for clarity and consistency with the tokenizer's
`sort.Slice` usage at `tokenizer.go:104`.

**P3-3: `logitsIdx` re-computed every decode step.**
`decode.go:216` calls `indexOfName(spec.decoderOutputs, spec.logitsName)` inside the
hot loop. The index is constant — could be cached in `modelSpec` during
`inspectGraphs`. Negligible cost vs. the ONNX run, but trivial to fix.

**P3-4: Tokenizer cache grows without bound.**
`tokenizer.go:322` caches every BPE result in `t.cache` with no eviction. For a
long-running server processing diverse input, this could grow indefinitely. Fine for
the current CLI use-case; worth a TODO if the engine is later used in a daemon.

---

## Checklist results

| # | Check | Result |
|---|-------|--------|
| 1 | Clean minimal interface | PASS — `Generate` + `Close`, matches plan |
| 2 | Follows kokoro/moonshine constructor pattern | PASS — two sessions, error-path `Destroy()` on second session failure (`qwen.go:107`) |
| 3 | Tokenizer: NFC, regexp2, byte-level BPE, added tokens | PASS — `norm.NFC` at `tokenizer.go:160`, `regexp2` at `:76`, byte tables at `:367`, added-token split at `:326` |
| 4 | Three-family state handling | PASS — conv/recurrent/kv detected from graph prefixes, zero-init, replace cycle all correct |
| 5 | Tensor destroy on every path | PASS with P1 caveat — `defer` on `initialEmbeds`/`state`, explicit `destroyValues`/`destroyMaybe` on error paths. `stepEmbeds` properly tracked vs `initialEmbeds` at `decode.go:243,250` |
| 6 | Context cancellation in decode loop | PASS — `ctx.Err()` checked at top of each step (`decode.go:184`) |
| 7 | Graph inspection from metadata | PASS — all state families discovered by prefix scan, no hardcoded layer counts |
| 8 | ChatML formatting | PASS — `<\|im_start\|>system\n...<\|im_end\|>\n` structure correct (`tokenizer.go:145-157`) |
| 9 | `go build ./...` | PASS |
| 10 | `go vet ./...` | PASS |
