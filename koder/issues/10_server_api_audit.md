# 10 — Server API Audit

## Status: open
## Priority: P2

Review server endpoints for real app consumption:
- Are the endpoints what a mobile/web app actually needs?
- Should POST /v1/tts support streaming (chunked transfer) for long text?
- Should we add a job ID + polling model for async clients?
- Is the error response format consistent and machine-parseable?
- Should /v1/status expose more (e.g., estimated wait time)?
- Content negotiation: should clients be able to request different audio formats?
- CORS headers for browser-based apps?
- Rate limit headers (X-RateLimit-Remaining etc)?
