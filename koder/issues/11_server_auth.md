# 11 — Optional Server Auth

## Status: done (plan 34)
## Priority: P2

Add opt-in authentication for the REST server.

## Implemented

- `--auth` flag on `cattery serve` — progressive enhancement, off by default
- API keys stored as SHA-256 hashes in `~/.cattery/keys.json`
- `cattery keys create/list/revoke/delete` CLI for key management
- `Authorization: Bearer cat_...` header on protected endpoints
- `/v1/status` stays public; all other routes + pprof gated when auth enabled
- Per-key fixed-window rate limiting (default 60 req/min, configurable)
- Hot-reload keys.json on file change (stat per request)
- Server refuses to start with `--auth` if no keys exist

## Decisions

- Header-only auth (no query param) — cleaner, no keys in logs/URLs
- No per-endpoint scopes — one key = full access (simplicity)
- Rate limit resets on server restart (in-memory only, fine for local use)
- Atomic key file writes via temp+rename
