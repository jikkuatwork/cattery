# 06 — REST API Server

## Status: done

## Goal

HTTP server for hosting cattery TTS as a service.

## Design

- `POST /v1/tts` — JSON request → WAV bytes (streamed, not base64)
- `GET /v1/voices` — list voices as JSON
- `GET /v1/status` — health, queue depth, uptime

Queue: bounded worker pool (channel semaphore). Pre-warmed engine pool.
Returns 503 + Retry-After when full.

## CLI

```
cattery serve [--port 7100] [--workers 2] [--model ID]
```

## Files

- `server/server.go` — full server package
- `cmd/cattery/main.go` — `serve` subcommand
