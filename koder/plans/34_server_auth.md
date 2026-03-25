# Plan 34 — Server Auth (Issue #11)

## Status: DRAFT

## Goal

Add opt-in API key authentication to `cattery serve`. Auth is a progressive
enhancement: off by default, enabled only with `--auth`. Keys live in
`~/.cattery/keys.json` and are managed via `cattery keys` CLI.

## Design

### Key format

- Prefix: `cat_` followed by 32 hex chars → `cat_a1b2c3d4e5f6...` (36 chars total)
- Generated via `crypto/rand`
- Only the SHA-256 hash is stored; full key shown once at creation

### Storage: `~/.cattery/keys.json`

```json
[
  {
    "id": "cat_k1a2b3c4",
    "key_hash": "sha256hex...",
    "name": "my-app",
    "created": "2026-03-25T12:00:00Z",
    "rate_limit": 60,
    "disabled": false
  }
]
```

- `id` = first 12 chars of the full key (prefix + 8 hex) — used for display/revoke
- `key_hash` = hex(sha256(full_key))
- `name` = user-supplied label
- `rate_limit` = requests per minute (default 60, 0 = unlimited)
- `disabled` = soft-delete for revocation

### CLI: `cattery keys`

```
cattery keys create --name "my-app"           # prints full key once
cattery keys create --name "bot" --rate 120   # custom rate limit
cattery keys list                              # table: id, name, created, rate, disabled
cattery keys revoke <id>                       # sets disabled: true
cattery keys delete <id>                       # removes from file
```

### Server: `--auth` flag

- `cattery serve --auth` → loads keys.json, enforces auth
- No `--auth` → fully open (current behavior, zero overhead)
- `--auth` with no keys file or empty keys → refuse to start:
  `"no API keys found; run 'cattery keys create' first"`

### Middleware behavior

- Reads `Authorization: Bearer cat_...` header
- Hashes the token, looks up in keys list
- Missing header → 401 `{"error": "authorization required"}`
- Invalid/unknown key → 401 `{"error": "invalid API key"}`
- Disabled key → 403 `{"error": "API key revoked"}`
- Rate exceeded → 429 + `Retry-After` header

### Public endpoints (no auth even with --auth)

- `GET /v1/status` — health checks must always work

### Protected when --auth is set

- `POST /v1/speak`, `POST /v1/tts`
- `POST /v1/listen`
- `GET /v1/models`
- `GET /v1/voices`
- `GET /debug/pprof/*`

### Rate limiting

- Per-key, in-memory fixed-window counter (resets each minute)
- `sync.Map` keyed by key ID → `{count int64, windowStart time.Time}`
- No persistence — resets on server restart (fine for local use)
- Window entries evicted when key hasn't been seen for 10 minutes

### Hot reload

- On each authenticated request, `stat()` keys.json
- If mtime changed, re-read and re-parse
- Cached in `Server.authKeys` behind a `sync.RWMutex`
- Cost: one stat syscall per request (~microseconds)

## Files to create

| File | Purpose |
|------|---------|
| `server/auth.go` | Key store, auth middleware, rate limiter |
| `server/auth_test.go` | Unit tests for middleware, rate limiting, key loading |
| `cmd/cattery/keys.go` | `cattery keys` subcommand |

## Files to modify

| File | Change |
|------|--------|
| `server/server.go` | Add `Auth bool` to Config; wrap protected handlers with auth middleware; add `authStore` field to Server |
| `cmd/cattery/main.go` | Add `keys` subcommand dispatch; add `--auth` to `cmdServe`; add `keys` to help text and `commandNames()` |

## Implementation order

1. `server/auth.go` — KeyStore (load/save/lookup), AuthMiddleware, RateLimiter
2. `server/auth_test.go` — test key validation, rate limiting, hot reload, disabled keys
3. Wire into `server/server.go` — Config.Auth, middleware wrapping
4. `cmd/cattery/keys.go` — create/list/revoke/delete
5. Wire `keys` into `cmd/cattery/main.go`
6. Update help text
7. Test end-to-end: `cattery keys create`, `cattery serve --auth`, curl with/without key

## Edge cases

- keys.json doesn't exist when `--auth` is set → error at startup
- keys.json is empty array when `--auth` is set → error at startup
- keys.json has only disabled keys → server starts (valid file, just no active keys)
- Concurrent key file writes (two `cattery keys create` at once) → flock or atomic write
- Key ID collision (first 12 chars match) → regenerate (vanishingly unlikely)

## What's out of scope

- TLS (use a reverse proxy)
- JWT/OAuth
- Per-endpoint scopes
- Key expiry/rotation
- Persistent rate limit state
