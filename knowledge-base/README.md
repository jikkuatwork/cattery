# Cattery

Text-to-speech and speech-to-text in Go. Single binary, no Python, no Docker.
Models and runtime auto-download on first run. Only system dependency is
`espeak-ng`.

```
go install github.com/jikkuatwork/cattery/cmd/cattery@latest
cattery "Hello, world."
```

## What It Does

- **TTS** (Kokoro-82M): 27 voices, 0.75x real-time on ARM64, streaming WAV output
- **STT** (Moonshine-tiny): 8x faster than real-time, bounded-memory sliding window
- **Server**: REST API with lazy engine pool, 7 MB idle RSS
- **Runs on Pi4**: 250 MB hot, 2-4s voice bot round-trip

## Quick Start

```bash
# Install (requires Go 1.25+ and espeak-ng)
sudo apt install espeak-ng   # or: brew install espeak
go install github.com/jikkuatwork/cattery/cmd/cattery@latest

# Speak
cattery "Hello, world."                      # random voice
cattery --voice bella "Hello"                # named voice
cattery --female --speed 1.2 -o out.wav "Hi" # female, faster, to file

# Listen
cattery listen recording.wav                 # transcribe audio

# Serve
cattery serve --port 7100                    # REST API
cattery serve --keep-alive -w 2              # 2 TTS workers, pre-warmed
```

## CLI

```
cattery "text"                    speak with random voice
cattery --voice 3 "text"         voice by number
cattery --voice bella "text"     voice by name
cattery --female "text"          random female voice
cattery --male "text"            random male voice
cattery --speed 1.5 -o out.wav   speed + output file
cattery --chunk-size 15s "text"  smaller memory footprint
cattery listen file.wav          transcribe audio
cattery listen --chunk-size 15s  transcribe with less memory
cattery list                     models + voices (numbered)
cattery status                   platform, deps, disk usage
cattery download                 pre-fetch model + all voices
cattery serve                    REST API on :7100
cattery serve --port 8080 -w 2   custom port, 2 TTS workers
cattery serve --listen-workers 2 custom STT workers
cattery serve --keep-alive       pre-warm engines, never evict
cattery serve --idle-timeout 60  evict engines after 60s idle
```

## Performance

Benchmarked on 6-core ARM64 VM (16GB). See
[profiling-details.md](profiling-details.md) for full numbers.

| Metric | Value |
|---|---|
| Binary | 11 MB |
| Server idle RSS | 7 MB |
| TTS short (4s audio) | 235 MB, 1.6x RTF |
| TTS long (161s audio) | 675 MB, 0.75x RTF |
| STT long (161s audio) | 575 MB, 0.12x RTF |
| STT long (--chunk-size 15s) | 228 MB, 0.07x RTF |
| First-run download | ~150 MB |

RTF = wall-clock / audio duration. Below 1.0 = faster than real-time.

## Memory Tuning

Cattery auto-detects available RAM and picks a chunk size:

| Available RAM | Chunk size | Target device |
|---|---|---|
| <= 512 MB | 10s | Extreme (warns, best-effort) |
| <= 1 GB | 15s | $6 VPS |
| <= 4 GB | 30s | Pi4 |
| > 8 GB | 60s | Desktop/server |

Override: `--chunk-size 15s` or `CATTERY_CHUNK_SIZE=15`

## Platform Support

| Platform | Status | Notes |
|---|---|---|
| Linux x86_64 | Full | Primary platform |
| Linux arm64 | Full | Pi4, ARM VPS |
| macOS (Intel + Apple Silicon) | Works | No RAM auto-detect (defaults 30s) |
| WSL | Full | Treated as Linux |
| Windows native | Not yet | Blocked on build tags (#5) |

## Architecture

```
cattery/
  cmd/cattery/       CLI
  speak/kokoro/      TTS engine (Kokoro-82M int8, ONNX)
  listen/moonshine/  STT engine (Moonshine-tiny, ONNX)
  audio/             Streaming WAV writer, PCM decoder, resampler
  server/            REST API with lazy engine pool
  ort/               Shared ONNX Runtime wrapper (dlopen)
  phonemize/         espeak-ng IPA phonemizer
  download/          Auto-download with progress + checksums
  preflight/         RAM detection, system checks
  registry/          Model/voice metadata
  paths/             ~/.cattery/ data directory
  knowledge-base/    Profiling, platform docs
```

## How It Works

**TTS pipeline**: text -> normalize -> phonemize (espeak-ng) -> chunk at 480
tokens -> ONNX inference per chunk -> stream PCM to WAV writer -> patch header
on close.

**STT pipeline**: audio -> streaming PCM decode -> streaming 16kHz resample ->
sliding-window chunk planner (30s target, silence-biased cuts, 0.5s overlap) ->
ONNX inference per chunk -> boundary word dedup -> concatenated transcript.

Both pipelines are bounded-memory: peak RSS is proportional to one chunk, not
the full clip.

## Dependencies

| Dependency | Bundled | Install |
|---|---|---|
| ONNX Runtime 1.24.1+ | Auto-downloaded | Automatic |
| Kokoro-82M model | Auto-downloaded | Automatic |
| Moonshine-tiny model | Auto-downloaded | Automatic |
| Voice files (27) | Auto-downloaded | Automatic |
| espeak-ng | No | `apt install espeak-ng` / `brew install espeak` |

Everything except espeak-ng downloads automatically to `~/.cattery/` on first
run. Total: ~150 MB.

## License

Apache-2.0. See [LICENSE](../LICENSE), [NOTICE](../NOTICE), and
[THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md).
