# Cattery — Go TTS Library

## Overview

Text-to-speech in Go. No Python. Tiny binary (~8MB). ONNX Runtime, model, and voice files auto-download on first run; `espeak-ng` remains a system dependency.

```
go install github.com/jikkuatwork/cattery/cmd/cattery@latest
cattery "Hello, world."
```

## Stack

- **Language**: Go (small cgo shim for `malloc_trim` during ORT teardown)
- **Inference**: ONNX Runtime 1.24.1+ via `yalue/onnxruntime_go` v1.27.0 (dlopen)
- **Model**: Kokoro-82M int8 quantized (92MB ONNX)
- **Phonemization**: espeak-ng via `os/exec` (only system dep)
- **Audio**: WAV (pure Go writer)

## Architecture

```
cattery/
├── .codex/
│   └── skills/       # Repo-local Codex workflow skills (`open`, `close`)
├── cmd/
│   ├── cattery/       # CLI
│   └── spike/         # Original spike (reference, has timing)
├── engine/            # ONNX inference, tokenizer, voice loading
├── phonemize/         # espeak-ng IPA phonemizer
├── audio/             # Pure Go WAV writer
├── download/          # Auto-download with progress bars and checksum verification where hashes are recorded
├── server/            # REST API server with lazy engine pool
├── preflight/         # System readiness checks (RAM, deps)
├── registry/          # Model/voice metadata registry
├── paths/             # Data directory resolution (~/.cattery/)
├── scripts/           # Helper scripts, benchmarks
└── koder/             # Project tracking
```

## CLI Commands

```
cattery "Hello, world."          # speak with random voice
cattery --voice 3 "Hello"       # pick voice by number
cattery --voice bella "Hello"    # pick voice by name
cattery --female "Hello"         # random female voice
cattery --male "Hello"           # random male voice
cattery --speed 1.5 -o out.wav   # speed + output file
cattery list                     # show models + voices (numbered)
cattery status                   # platform, deps, disk usage
cattery download                 # pre-fetch model + all voices
cattery serve                    # REST API on :7100 (lazy, 1 worker)
cattery serve --port 8080 -w 2   # custom port + workers
cattery serve --keep-alive       # pre-warm engines, never evict
cattery serve --idle-timeout 60  # evict engines after 60s idle
```

## Downloads (no auth, stable URLs)

| Asset | Source | Size |
|---|---|---|
| Model + voices | `jikkuatwork/cattery-artefacts` (Git LFS) | 88MB + 13MB |
| ORT runtime | Microsoft GitHub Releases | 18MB |

Registry currently includes 27 voices. Downloaded artefacts are cached in `~/.cattery/`; `espeak-ng` is not bundled.

## Performance (aarch64 VM, 6-core)

| Metric | Value |
|---|---|
| RTF | 0.70x (faster than real-time) |
| Idle RSS (lazy) | 8 MB (no engine loaded) |
| Idle RSS (post-evict) | ~50 MB (after malloc_trim) |
| Peak RSS (short text) | ~180 MB |
| Peak RSS (long text) | ~360 MB |
| Cold start | ~1.4s (ORT reload + model) |
| Binary | 8.3MB |
| First-run download | ~120MB total |

## Issues

| # | Title | Status | Pri |
|---|---|---|---|
| [01](issues/01_onnx_inference_spike.md) | ONNX inference spike | **done** | P0 |
| [02](issues/02_phonemizer_strategy.md) | Phonemizer strategy | **done** (espeak os/exec) | P0 |
| [03](issues/03_wav_writer.md) | WAV writer | **done** | P1 |
| [04](issues/04_cli.md) | CLI + model download | **done** | P1 |
| [05](issues/05_cross_platform_build.md) | Cross-platform build | open | P2 |
| [06](issues/06_rest_server.md) | REST API server | **done** | P1 |
| [07](issues/07_license_audit.md) | License audit (model hosting, deps) | open (audit complete) | P1 |
| [08](issues/08_stt_module.md) | Speech-to-text module | open | P2 |
| [09](issues/09_pretty_help.md) | Pretty CLI help | open | P3 |
| [10](issues/10_server_api_audit.md) | Server API audit for apps | open | P2 |
| [11](issues/11_server_auth.md) | Optional server auth | open | P2 |
| [12](issues/12_llm_proxy.md) | LLM proxy (Ollama/OpenRouter) | open | P2 |
| [13](issues/13_local_4b_model.md) | Local 4B LLM via ONNX | open | P3 |
| [14](issues/14_web_ui.md) | Embedded web UI | open | P3 |

## What's Next

- License compliance follow-through (#07) — add project license + artefact notices; blocks distribution
  Audit baseline is now captured in `THIRD_PARTY_NOTICES.md`.
- Server API audit (#10) + auth (#11) — before apps consume it
- STT module (#08) — completes audio pipeline
- LLM proxy (#12) — unified AI backend
- Vision: single-binary conversational system (STT → LLM → TTS) for indie builders

## Key Decisions Made

- **No pure Go ONNX**: GoMLX/gonnx lack FFT/ISTFT ops. ORT via dlopen is the only path.
- **Download on first run**: binary stays ~8MB, while ORT/model/voice artefacts are fetched as needed. `espeak-ng` stays external.
- **Separate artefacts repo**: `cattery-artefacts` holds binaries via Git LFS. No auth.
- **ORT from Microsoft**: not mirrored — their GitHub Release URLs are permanent.
- **espeak-ng via os/exec**: simplest phonemizer. WASM embed deferred.
- **Random voice by default**: no voice flag = random pick; `--male`/`--female` to filter.
- **Full voice set by default**: voice files are small (~510KB each), so the UX is optimized around fetching the whole set rather than micromanaging individual voices.
- **~/.cattery/ for data**: simple default for current platforms.
- **ORT stderr suppressed**: redirect fd during init to hide C-level warnings.
- **WAV bytes, not base64**: REST API returns raw WAV with Content-Type: audio/wav. 33% smaller than base64.
- **Lazy engine pool**: engines created on first request, evicted after idle timeout. Full ORT dlclose + malloc_trim reclaims C heap.
- **Shared char budget**: total characters across all queued requests bounded (default 500). Caps peak RSS regardless of request distribution.
- **Preflight package**: checks RAM, espeak-ng, model files, and ORT presence; current request handling uses the memory gate and status/CLI expose the rest.
- **Module path**: `github.com/jikkuatwork/cattery` — matches repo URL for `go get`.
- **License audit documented**: current third-party licensing and distribution obligations are recorded in `THIRD_PARTY_NOTICES.md`.
- **Repo-local Codex workflow**: `.codex/skills/open` and `.codex/skills/close` are tracked in-repo, not globally. `open` should read `koder/STATE.md` first after restarts; `close` should sync `koder/STATE.md`, validate changed local skills, and commit a coherent session when explicitly invoked.

## Research

- [01_initial.md](research/01_initial.md) — KittenTTS benchmarks, size analysis, library survey, phonemizer options
