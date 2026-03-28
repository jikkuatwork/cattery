# Code Review — #32 OpenAI-Compatible Server API

**Reviewer:** cl-rev-32
**Commits:** 569d8c4, 713e156, b6d1b73, eaedf8c
**Plan:** koder/plans/32_openai_compat_api.md

## Verdict: PASS

The implementation faithfully follows the plan across all four phases. Routes,
request/response shapes, error format, and test coverage all match the spec.
No regressions to pool, auth, or engine behavior.

---

## Checklist

| Item | Status | Notes |
|------|--------|-------|
| `POST /v1/audio/speech` shape | OK | `input`, `voice`, `model`, `speed`, `response_format` — matches OpenAI |
| `POST /v1/audio/transcriptions` multipart | OK | `file` (required), `model`, `language`, `response_format` via `r.FormFile` |
| `GET /v1/models` envelope | OK | `{"object":"list","data":[{"id":...,"object":"model","created":0,"owned_by":"cattery"}]}` |
| Old routes removed | OK | `/v1/tts`, `/v1/stt`, `/v1/voices` all return 404, confirmed by tests |
| Nested OpenAI error format | OK | `{"error":{"message":...,"type":"invalid_request_error","code":null}}` on all new handlers |
| Custom headers dropped | OK | No `X-Model`, `X-Voice`, `X-Audio-Duration`, `X-Processing-Time`, `X-RTF`, `Content-Disposition` |
| `gender`/`lang` removed from TTS API | OK | Hardcoded `""` and `"en-us"` internally |
| `response_format` validation (TTS) | OK | Accepts `""` or `"wav"` only |
| `response_format` support (STT) | OK | `json` (default), `text`, rejects others |
| `synthMeta` value return | OK | Changed from `*synthMeta` to `synthMeta` per plan |
| `DisallowUnknownFields` kept | OK | Intentional forward-compat tradeoff, noted in plan |
| Pool/auth/engine unchanged | OK | `borrowTTS`, `borrowSTT`, `borrowLLM`, auth middleware untouched |
| `llm.go` untouched | OK | Plan says leave alone unless shared helper reduces duplication |
| Auth test route updated | OK | Rate-limit test hits `/v1/audio/speech`; new `TestAuthMiddlewareProtectsAudioSpeechRoute` |
| Test coverage adequate | OK | See breakdown below |

## Test coverage

| File | Cases |
|------|-------|
| `tts_test.go` | Happy path, missing input, unsupported format, old route 404 |
| `stt_test.go` | JSON response, text format, non-multipart rejection, missing file, old route 404 |
| `models_test.go` | Envelope shape, `object:"model"` assertion, legacy fields absent, old voices 404 |
| `auth_test.go` | Rate limit on new route, 401 on protected new route |
| `api_test.go` | Shared test server with TTS/STT/LLM stub engines |

## Observations (non-blocking)

1. **`_ = meta` (server.go:535)** — Minor style nit. Could use `wavBuf, _, err :=`
   in the call instead of assigning then discarding. No functional impact.

2. **`writeError` vs `writeOpenAIError` split** — `llm.go` and `auth.go` still use
   the flat `{"error":"..."}` format. This is correct per plan (LLM and auth are out
   of scope), but means OpenAI SDKs hitting `/v1/chat/completions` or auth failures
   will see the old error shape. Worth a follow-up issue if full SDK compat is
   desired on those paths.

3. **Test helper queue channel** — `newTestAPIServer` sets `QueueMax: 2` but never
   initializes `s.queue`, so queue-full paths can't fire in tests. Acceptable for
   unit-level handler tests; not a correctness issue.
