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
- **Audio**: WAV (pure Go streaming writer)

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
cattery serve --port 8080 -w 2   # custom speak workers
cattery serve --listen-workers 2 # custom STT workers
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
| [07](issues/07_license_audit.md) | License audit (model hosting, deps) | open (audit + repo license done) | P1 |
| [08](issues/08_stt_module.md) | Speech-to-text module | **done** (superseded by #16-#21) | P1 |
| [09](issues/09_pretty_help.md) | Pretty CLI help | **done** (subsumed by #19) | P3 |
| [10](issues/10_server_api_audit.md) | Server API audit for apps | **done** (subsumed by #21) | P2 |
| [11](issues/11_server_auth.md) | Optional server auth | open | P2 |
| [12](issues/12_llm_proxy.md) | LLM proxy (Ollama/OpenRouter) | open | P2 |
| [13](issues/13_local_4b_model.md) | Local 4B LLM via ONNX | open | P3 |
| [14](issues/14_web_ui.md) | Embedded web UI | open | P3 |
| [15](issues/15_mirror_json.md) | Artefact mirror registry (mirror.json) | open | P2 |
| [16](issues/16_extract_ort_runtime.md) | Extract shared ORT runtime from engine/ | **done** | P0 |
| [17](issues/17_tts_engine_interface.md) | TTS engine interface + package restructure | **done** | P0 |
| [18](issues/18_registry_redesign.md) | Registry redesign for multi-modal artefacts | **done** | P1 |
| [19](issues/19_cli_redesign.md) | CLI redesign: subcommand-per-modality | **done** | P1 |
| [20](issues/20_stt_package.md) | STT package: Moonshine-tiny | **done** | P1 |
| [21](issues/21_server_api_redesign.md) | Server API redesign for multi-modality | **done** | P2 |
| [22](issues/22_bundle_espeak.md) | Bundle espeak-ng (zero system deps) | open | P1 |
| [23](issues/23_openai_remote_engines.md) | OpenAI-compatible remote engines | open | P2 |
| [24](issues/24_tts_sentence_chunking.md) | Transparent sentence chunking for long text TTS | **done** | P1 |
| [25](issues/25_text_normalizer.md) | Pure Go text normalizer for TTS preprocessing | **done** | P1 |
| [26](issues/26_stt_audio_chunking.md) | STT audio chunking to prevent hallucination | **done** | P1 |
| [27](issues/27_bounded_memory_streaming.md) | Bounded-memory streaming for TTS/STT | **done** (native memtest validated; 4 GB / 1 GB cgroup run requires manual host) | P1 |
| [28](issues/28_rss_validation.md) | RSS validation: TTS accumulation + STT baseline overage | **done** | P1 |
| [29](issues/29_fix_memtest.md) | Fix memtest suite: test artifacts causing false failures and OOM risk | **done** | P1 |

## What's Next

**Memory validation pipeline complete** (plans 31–33). #27, #28, #29 closed.
Remaining cgroup validation (4G/1G/512M) is manual — see `scripts/memtest-constrained.sh`.

### Open

- **#22 Bundle espeak-ng** — eliminate the only system dependency
- **#23 OpenAI remote engines** — `OPENAI_API_KEY` unlocks remote TTS/STT
- **#11 Server auth** — API redesign landed, auth can follow
- **#12 LLM proxy** — unified AI backend (`cattery think`), partly covered by #23
- **#07** License compliance follow-through
- Vision: single-binary conversational system (STT → LLM → TTS) for indie builders on Pi4

## Key Decisions Made

- **No pure Go ONNX**: GoMLX/gonnx lack FFT/ISTFT ops. ORT via dlopen is the only path.
- **Download on first run**: binary stays ~8MB, while ORT/model/voice artefacts are fetched as needed. `espeak-ng` stays external.
- **Separate artefacts repo**: `cattery-artefacts` holds binaries via Git LFS. No auth.
- **ORT from Microsoft**: not mirrored — their GitHub Release URLs are permanent. Currently on v1.24.4. Linux amd64 asset uses `x64` (not `x86_64`) in filename; macOS ships arm64-only since ORT 1.20+; arch mapping is per-OS in `download/download.go`.
- **`scripts/install.sh`**: builds `./cmd/cattery` and drops it into `$HOME/.local/bin` (overrideable via `INSTALL_DIR`). Run after every local build.
- **espeak-ng via os/exec**: simplest phonemizer. WASM embed deferred.
- **Random voice by default**: no voice flag = random pick; `--male`/`--female` to filter.
- **Full voice set by default**: voice files are small (~510KB each), so the UX is optimized around fetching the whole set rather than micromanaging individual voices.
- **~/.cattery/ for data**: simple default for current platforms.
- **ORT stderr suppressed**: redirect fd during init to hide C-level warnings.
- **WAV bytes, not base64**: REST API returns raw WAV with Content-Type: audio/wav. 33% smaller than base64.
- **Streaming WAV output**: TTS now writes PCM16 chunk-by-chunk. Seekable
  outputs patch the header on close; non-seekable outputs spool PCM to temp
  storage, then write the final WAV in one pass.
- **Lazy engine pool**: engines created on first request, evicted after idle timeout. Full ORT dlclose + malloc_trim reclaims C heap.
- **Shared char budget**: total characters across all queued requests bounded (default 500). Caps peak RSS regardless of request distribution.
- **Preflight package**: checks RAM, espeak-ng, model files, and ORT
  presence; chunk-size resolution and low-memory warnings now live there too.
- **Shared chunk-size policy**: `preflight` now owns RAM probing plus
  `CATTERY_CHUNK_SIZE` / `--chunk-size` resolution with precedence
  `explicit > env > auto`, a conservative `10s..60s` RAM lookup table, and a
  once-per-process low-memory warning at `<= 512 MB`.
- **Module path**: `github.com/jikkuatwork/cattery` — matches repo URL for `go get`.
- **Project license**: `cattery` code is Apache-2.0; packaged releases should
  carry `LICENSE`, `NOTICE`, and `THIRD_PARTY_NOTICES.md`.
- **License audit documented**: current third-party licensing and distribution obligations are recorded in `THIRD_PARTY_NOTICES.md`.
- **STT model: Moonshine-tiny**: 27MB quantized ONNX, raw 16kHz PCM input (no mel spectrograms), 28x real-time on CPU, shares ORT instance with TTS. Community ONNX export from onnx-community/moonshine-tiny-ONNX. Weight license unclear — needs clarification before distribution.
- **STT chunking lives in Moonshine**: `moonshine.Engine.Transcribe()` now
  chunks resampled PCM at ~30s, prefers nearby silence in a `27s..33s` search
  window, overlaps by 0.5s, skips pure silence, and dedups boundary words at
  stitch time. No API change for callers.
- **Streaming STT input**: Moonshine no longer reads or resamples full clips
  up front. `audio.PCMStreamReader` incrementally decodes WAV PCM16, WAV
  float32, or raw float32 input; `audio.StreamResampler` keeps 16 kHz output
  continuous across block boundaries; and the Moonshine loop retains only the
  overlap + unread tail between chunk inferences while duration is tracked
  from decoded source samples.
- **Chunk size is now runtime-configurable**: `cmdSpeak`, `cmdListen`,
  `cmdServe`, and `server.New` all resolve chunk size the same way; Moonshine
  consumes it for streaming STT windows, while Kokoro accepts the field but
  still chunks by token budget in this pass.
- **Multi-modal package naming**: CLI verbs = package names = API paths. `speak/` (TTS), `listen/` (STT), future `think/` (LLM), `see/` (vision). Each has an `Engine` interface + per-model subdirectories (e.g. `speak/kokoro/`, `listen/moonshine/`). The "cattery = place where cats live, verbs = cats" metaphor is for branding/website — code uses clean verbs.
- **Pi4 viability confirmed**: TTS+STT hot = ~250MB RAM, 2-4s round-trip for voice message bot. Comfortable on Pi4 4GB.
- **Zero system deps goal**: bundle espeak-ng binary + data in `~/.cattery/`, auto-download like ORT/models. No `apt install` needed.
- **Remote models are just models**: no `--remote` flag. OpenAI engines are registered models (`openai-tts-1`, `openai-whisper-1`) selected via `--model`. Remote models only appear in `cattery list` when `OPENAI_API_KEY` is set. Default is always local — no accidental API spend. `OPENAI_BASE_URL` supports OpenRouter, Ollama, Azure. No SDK — pure `net/http`.
- **Dual-mode operation**: local models (Pi4, offline, free) OR remote APIs (zero download, higher quality, paid). Same CLI, same server, same `speak.Engine`/`listen.Engine` interface.
- **Numeric indices everywhere**: models and voices addressed by stable per-kind numeric index in CLI (`--voice 4 --model 1`). Slugs exist but are never required input. API responses always include both index and slug. Indices are 1-based, stable, never reassigned.
- **Repo-local Codex workflow**: `.codex/skills/open` and `.codex/skills/close` are tracked in-repo, not globally. `open` should read `koder/STATE.md` first after restarts; `close` should sync `koder/STATE.md`, validate changed local skills, and commit a coherent session when explicitly invoked.

## Research

- [01_initial.md](research/01_initial.md) — KittenTTS benchmarks, size analysis, library survey, phonemizer options
