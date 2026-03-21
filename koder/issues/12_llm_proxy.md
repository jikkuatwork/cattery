# 12 — LLM Proxy Layer (Ollama / OpenRouter)

## Status: open
## Priority: P2

Add an optional LLM proxy so cattery becomes a unified AI backend:
- Connect to Ollama (local) or OpenRouter (cloud) for text generation
- Expose as `/v1/chat/completions` (OpenAI-compatible)
- Config: `--ollama http://localhost:11434` or `--openrouter KEY`
- Passthrough with minimal overhead — cattery is the gateway, not the brain

Combined with TTS (#06) and STT (#08), this gives:

```
Voice in → [STT] → text → [LLM] → text → [TTS] → Voice out
```

A full conversational system from a single binary. High latency (~10-20s round trip on cheap hardware) but functional and resource-conscious.

## Why

- Indie builders install one thing, get voice + text + generation
- No Docker, no Python, no GPU required
- `go install` and you're done
- Perfect for Telegram bots, home assistants, hobby projects
