# Review: Plan 32 — OpenAI-Compatible Server API

**Reviewer:** claude-rev
**Date:** 2026-03-28
**Plan:** `koder/plans/32_openai_compat_api.md`
**Issue:** `koder/issues/32_openai_compat_api.md`
**Verdict:** NEEDS FIXES

---

## Summary

The plan is well-structured and covers the right scope. File references,
handler names, and struct inventories are accurate against the current code.
Phase ordering is logical. However, there are several issues that need
attention before implementation — one P1 that would cause a compile error,
several P2s that affect correctness or SDK compatibility, and minor P3s.

---

## P1 — Must fix

### 1. `synthesize` signature uses wrong voice type

The plan proposes:

```go
func (s *Server) synthesize(
    ctx context.Context,
    req audioSpeechRequest,
    model *registry.Model,
    voice registry.Voice,
) (*bytes.Buffer, synthMeta, error)
```

Current code (`server/server.go:732`) uses `tts.Voice`, and
`resolveTTSVoice` (`server/server.go:696`) returns `tts.Voice`.
`kokoro.ResolveVoice` also returns `tts.Voice`. The plan should use
`tts.Voice` here, not `registry.Voice` — they're separate types in separate
packages.

Also: return type changes from `*synthMeta` to `synthMeta` (value). This is
fine but should be a deliberate choice, not an accidental diff. Mention it
explicitly or keep the pointer.

---

## P2 — Should fix

### 2. Phase 1 is not independently testable

Phase 1 registers `/v1/audio/speech` and removes `/v1/tts`, but keeps
`handleTTS` internals unchanged — meaning the new endpoint would still
decode `{"text": ...}` not `{"input": ...}`. The new route exists but
doesn't work correctly until Phase 2.

**Fix:** Either (a) merge Phases 1+2 for TTS (route + handler together), or
(b) note explicitly that Phase 1 is a compile-gate only and is not
independently functional. The current wording ("establishes the new public
surface") implies it works.

### 3. `Gender` and `Lang` removal not traced through internals

The plan removes `Gender` and `Lang` from `audioSpeechRequest` but doesn't
specify what to pass into `tts.Options` inside `synthesize`
(`server/server.go:747`):

```go
eng.Speak(&buf, req.Text, tts.Options{
    Voice:     voice.ID,
    Gender:    req.Gender,   // removed from request — what goes here?
    Speed:     req.Speed,
    Lang:      req.Lang,     // removed from request — what goes here?
    ChunkSize: s.cfg.ChunkSize,
})
```

**Fix:** State that `Gender` passes `""` (or is dropped from `tts.Options`
if unused downstream) and `Lang` is hardcoded to `"en-us"` (the current
default at `server/server.go:478`).

### 4. `resolveTTSVoice` still takes `gender` parameter

`resolveTTSVoice(model, req.Voice.String(), req.Gender)` at line 487.
With `Gender` gone from the request, this call needs updating. The plan
should specify: pass `""` for gender, or refactor the signature.

### 5. STT model resolution changes from query param to form field

Currently `handleSTT` reads model from `r.URL.Query().Get("model")`
(`server/stt.go:29`). The plan changes this to a multipart form field.
This is correct per the OpenAI spec but is a silent behavioral change.
The plan should call this out explicitly so the implementer doesn't
accidentally keep the query-param path alongside the form field.

### 6. OpenAI error response format

The plan keeps cattery's flat error shape `{"error": "string"}`. The OpenAI
API returns a nested object:

```json
{"error": {"message": "...", "type": "invalid_request_error", "code": null}}
```

The OpenAI Python SDK's error parsing expects this nested structure — it
reads `error.message`. With the flat format, SDK error messages may show as
empty or the raw body. This matters for the issue's goal of "OpenAI SDK
works as a drop-in client."

**Fix:** Either (a) add an OpenAI-shaped error helper, or (b) explicitly
mark this as out-of-scope with a note that SDK error handling won't be
fully compatible. Either is fine, but it should be a deliberate decision.

---

## P3 — Nice to have

### 7. `Content-Disposition` header on TTS response

Plan says "keep `Content-Disposition`" — OpenAI's `/v1/audio/speech` does
not set this header. It won't break SDK clients, but it's not strictly
compatible. Consider dropping it alongside the `X-*` headers for a clean
surface.

### 8. `DisallowUnknownFields` on TTS JSON decoder

The current handler (`server/server.go:459`) rejects unknown JSON fields.
The OpenAI SDK for TTS sends only `model`, `input`, `voice`, `speed`,
`response_format` — which matches `audioSpeechRequest` — so this should be
fine today. But if OpenAI adds new fields to their SDK in the future,
cattery would reject them. Note this as a known forward-compat risk, or
consider dropping the strict check.

---

## Verified correct

- **Route inventory:** `/v1/tts`, `/v1/stt`, `/v1/voices` exist at
  `server/server.go:404-408` — removal targets are accurate.
- **Struct inventory:** `ttsRequest` (line 119), `modelResponse` (128),
  `voiceResponse` (139), `sttResponse` (`stt.go:18`) — all confirmed.
- **`server/llm.go` no-change:** Confirmed. Handler and types already match
  OpenAI shape. No modifications needed.
- **`auth_test.go` route references:** Rate-limit test hits `/v1/tts`
  (line 129) — plan correctly identifies this needs updating.
  Auth/hot-reload tests hit `/v1/models` — these stay unchanged. Correct.
- **`llm_test.go` impact:** Tests construct `Server` directly and call
  `handleChatCompletions` — no dependency on deleted types or renamed
  handlers. Will stay green. Correct.
- **OpenAI request/response shapes:** TTS, STT, and models shapes all match
  the OpenAI spec for the supported subset.
- **Phase ordering:** Sound. Route/type first → handler migration → test
  lock-in is the right incremental sequence.
- **Out-of-scope boundaries:** Correctly drawn. CLI, pool, auth, status all
  stay untouched.
