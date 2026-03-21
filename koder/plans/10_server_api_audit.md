# Plan 10 — Server API Audit

## Why

- The server works now.
- The contract is still CLI-grade, not app-grade.

## Current API

- `POST /v1/tts`
- `GET /v1/voices`
- `GET /v1/status`
- `GET /debug/pprof/*`
- `POST /v1/tts` returns raw WAV plus `X-*` headers.
- Errors are only `{"error":"..."}` today.
- There is no CORS handling.
- There is no request id.
- There are no HTTP tests yet.

## Goals

- Make the API predictable for app clients.
- Keep the design small.
- Set up HTTP plumbing that `#11` can reuse.

## Decisions For This Pass

- Keep `POST /v1/tts` synchronous.
- Keep WAV as the only response format for now.
- Do not add streaming in this pass.
- Do not add async jobs in this pass.
- Add machine-readable errors.
- Add opt-in CORS.
- Add request ids to responses and logs.
- Revisit whether `pprof` should stay public.

## Scope

- Audit and document the current contract.
- Implement the minimum hardening needed for mobile and web clients.
- Add tests for HTTP semantics.

## Proposed API Deltas

- Error shape becomes structured.
  - Example:
    `{"error":{"code":"queue_full","message":"...","retryable":true}}`
  - Add `retry_after_seconds` when that matters.
- Every response gets `X-Request-ID`.
- `POST /v1/tts` stays raw WAV.
  - Keep timing and voice headers.
  - Document status codes and retry headers.
- `GET /v1/status` gets fields that help clients reason about capacity.
- CORS is off by default.
  - Use an allowlist, not `*`, by default.
- `pprof` should be disabled or protected once auth lands.

## Work Plan

1. Write the API contract we want to support.
2. Refactor response helpers into a small HTTP layer.
3. Add request id generation and structured errors.
4. Add CORS middleware and config plumbing.
5. Revisit the `status` payload.
6. Add `httptest` coverage for:
   - error shape
   - CORS preflight and allowed origins
   - retry headers
   - request id presence
7. Update help text and tracking notes.

## Files Likely Touched

- `server/server.go`
- `cmd/cattery/main.go`
- new `server/*_test.go`
- `koder/issues/10_server_api_audit.md`
- `koder/STATE.md`

## Risks

- Over-designing before real clients exist.
- Breaking simple `curl` usage.
- Coupling `#10` and `#11` too tightly.

## Done When

- The HTTP contract is documented.
- Errors are machine-readable.
- CORS is configurable.
- Request ids exist.
- Tests lock the behavior.
