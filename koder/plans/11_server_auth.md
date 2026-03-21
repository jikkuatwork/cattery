# Plan 11 — Optional Server Auth

## Why

- The server is open by default.
- Once apps use it, it needs a simple gate.

## Current State

- There is no auth in `server/server.go`.
- Routes are registered directly on one mux.
- `pprof` is public.
- The CLI has no auth flags.

## Decisions For First Cut

- Auth stays opt-in.
- Use one shared bearer token.
- Header only: `Authorization: Bearer <token>`.
- Do not support query-param auth.
- Use constant-time token compare.
- If auth is enabled, protect all `/v1/*` routes.
- If auth is enabled, protect `/debug/pprof/*` too.
- If public health is needed later, add a separate health endpoint.

## Scope

- Config plumbing.
- Auth middleware.
- Clear `401` JSON errors.
- Tests.
- Help text and examples.

## Out Of Scope

- Per-user auth.
- Expiring tokens.
- API key storage.
- OAuth or session auth.

## Work Plan

1. Extend `server.Config` with `AuthToken`.
2. Add `--auth-token SECRET` to `cattery serve`.
3. Wrap handlers with auth middleware.
4. Parse `Authorization` strictly.
5. Return `401` with `WWW-Authenticate: Bearer`.
6. Reuse the structured error shape from `#10`.
7. Add tests for:
   - auth disabled allows requests
   - missing token returns `401`
   - wrong token returns `401`
   - correct token reaches the handler
   - malformed auth header returns `401`
8. Update help text and tracking notes.

## Files Likely Touched

- `server/server.go`
- `cmd/cattery/main.go`
- new `server/*_test.go`
- `koder/issues/11_server_auth.md`
- `koder/STATE.md`

## Risks

- Logging the token by mistake.
- Header parsing bugs.
- Making local `curl` usage annoying.

## Done When

- `cattery serve --auth-token SECRET` works.
- Protected routes reject missing or wrong bearer tokens.
- Auth-off mode behaves exactly like today.
- Tests cover the route matrix.
