# 11 — Optional Server Auth

## Status: open
## Priority: P2

Add opt-in authentication for the REST server:
- `--auth-token SECRET` flag — simple bearer token check
- Skip auth if flag not set (default: open, for local/trusted use)
- Return 401 with clear error if token missing/wrong
- Consider: API key via header vs query param?
- Consider: per-endpoint auth (e.g., status is public, tts requires auth)?
