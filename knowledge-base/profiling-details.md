# Cattery — Profiling & Platform Details

Benchmarked 2026-03-22 on a 6-core ARM64 VM (16GB RAM), post-#27 bounded-memory
streaming. All numbers are from cold-start runs (no engine pre-warming).

## Quick Reference

 | Metric                      | Value                          |
 | ---                         | ---                            |
 | Binary size                 | 11 MB                          |
 | First-run download          | ~150 MB (ORT + model + voices) |
 | Server idle RSS             | **7 MB**                       |
 | Cold start (first TTS)      | ~3.5s                          |
 | Short TTS (4s audio)        | 235 MB peak, 1.6x RTF          |
 | Long TTS (161s audio, file) | 675 MB peak, 0.75x RTF         |
 | Long TTS (--chunk-size 15s) | 678 MB peak, 0.93x RTF         |
 | Long TTS (pipe/stdout)      | 840 MB peak, 0.63x RTF         |
 | Short STT (4s audio)        | 180 MB peak, 0.20x RTF         |
 | Long STT (161s audio)       | 575 MB peak, 0.12x RTF         |
 | Long STT (--chunk-size 15s) | **228 MB peak**, 0.07x RTF     |

RTF = real-time factor. Below 1.0 means faster than real-time.

## Memory Profiles

### TTS (Kokoro-82M int8)

```
                Peak RSS (MB)
Short (4s)      ████████████ 235
Long (file)     ██████████████████████████████████ 675
Long (15s)      ██████████████████████████████████ 678
Long (pipe)     ██████████████████████████████████████████ 840
```

- File output uses seekable WAV writer (header patch on close) — bounded by
  one inference chunk plus write buffers.
- Pipe/stdout uses temp-file fallback — same disk I/O pattern but the grep/
  shell pipeline contributed extra RSS in this benchmark. The WAV data itself
  goes to disk, not RAM.
- `--chunk-size 15s` doesn't reduce TTS peak because Kokoro's chunk ceiling
  is token-based (480 tokens ≈ 12-15s), not duration-based. The flag is
  accepted but has no effect on TTS in this release.

### STT (Moonshine-tiny 27MB)

```
                Peak RSS (MB)
Short (4s)      █████████ 180
Long (default)  ████████████████████████████ 575
Long (15s)      ███████████ 228
```

- **`--chunk-size 15s` cuts STT peak by 60%** — from 575 MB to 228 MB.
- The sliding-window decoder + streaming resampler keep only the current
  chunk + overlap in memory. Duration is tracked from decoded source samples.
- Short audio (< 30s) takes the single-pass fast path with no overhead.

### Server Idle

```
Lazy (no engine loaded):     7 MB
Post-evict (malloc_trim):   ~50 MB
```

The server starts at 7 MB RSS with no engines loaded. Engines are created on
first request and evicted after the idle timeout (default 5 min). Post-eviction
RSS drops to ~50 MB after `malloc_trim` reclaims the C heap.

## Auto Chunk-Size Table

Available RAM is read from `/proc/meminfo` (Linux) or defaults to unknown on
other platforms. The auto-selected chunk size is:

 | Available RAM | Chunk size | Notes                              |
 | ---           | ---        | ---                                |
 | ≤ 512 MB      | 10s        | Warns on stderr, proceeds normally |
 | ≤ 1 GB        | 15s        | $6 VPS floor                       |
 | ≤ 2 GB        | 20s        |                                    |
 | ≤ 4 GB        | 30s        | Pi4 4GB default                    |
 | ≤ 8 GB        | 45s        |                                    |
 | > 8 GB        | 60s        | Fewer boundary artifacts           |
 | Unknown       | 30s        | macOS, Windows/WSL fallback        |

Override with `--chunk-size 15s` or `CATTERY_CHUNK_SIZE=15`.

## Device Estimates

### Raspberry Pi 4 (4GB, Cortex-A72 quad-core)

 | Metric             | TTS       | STT          |
 | ---                | ---       | ---          |
 | Estimated peak RSS | ~350 MB   | ~200 MB      |
 | Auto chunk size    | 30s       | 30s          |
 | Estimated RTF      | 2-4x      | 0.3-0.5x     |
 | 3-min clip         | ~6-12 min | ~1.5-2.5 min |
 | Comfortable?       | Yes (hot) | Yes          |

TTS is slower than real-time but completes without OOM. STT is still faster
than real-time even on Pi4. Both fit comfortably in 4GB with room for OS +
espeak-ng.

**Round-trip voice bot** (STT → LLM → TTS): 2-4s for short utterances with
hot engines. Viable for Telegram bots and local assistants.

### $6 VPS (1GB RAM, 1 vCPU)

 | Metric             | TTS             | STT        |
 | ---                | ---             | ---        |
 | Auto chunk size    | 15s             | 15s        |
 | Estimated peak RSS | ~300 MB         | ~230 MB    |
 | Estimated RTF      | 3-6x            | 0.5-1.0x   |
 | 1-min clip         | ~3-6 min        | ~30-60s    |
 | 3-min clip         | Tight, may swap | ~1.5-3 min |

STT works well. TTS is marginal — 1-min clips are fine, 3-min clips will
likely trigger swap. Use `--chunk-size 10s` to reduce peak RSS at the cost
of more boundary artifacts.

### 512 MB (extreme)

Both models load (~180-190 MB base) but inference headroom is very tight.

- **STT with `--chunk-size 10s`**: should complete short clips. Long clips
  may OOM — cattery will print a clean error, not a stack trace.
- **TTS**: unlikely to complete multi-chunk synthesis. Single-chunk (< 15s
  text) may work.
- This tier exists for "delightful to see it work" — not production use.

## Platform Support

### Fully Supported

 | Platform         | Memory detect | Notes            |
 | ---              | ---           | ---              |
 | **Linux x86_64** | /proc/meminfo | Primary platform |
 | **Linux arm64**  | /proc/meminfo | Pi4, ARM VPS     |
 | **WSL**          | /proc/meminfo | Behaves as Linux |

### Partial Support

 | Platform         | Memory detect   | Gap                 |
 | ---              | ---             | ---                 |
 | **macOS x86_64** | Defaults to 30s | No sysctl probe yet |
 | **macOS arm64**  | Defaults to 30s | No sysctl probe yet |

macOS compiles and runs. ORT downloads work for both architectures. The only
gap is memory auto-detection — chunk size defaults to 30s instead of adapting.
The `malloc_trim` call in `ort/ort.go` is a no-op on macOS (different
allocator) but harmless.

### Not Yet Supported

 | Platform           | Blocker                                                      |
 | ---                | ---                                                          |
 | **Windows native** | `ort/ort.go` imports `golang.org/x/sys/unix` unconditionally |
 | **Windows WSL**    | Works (treated as Linux)                                     |

Windows native needs build tags to split the Unix-specific ORT stderr redirect
and `malloc_trim` call. The download infrastructure already recognizes
`onnxruntime.dll` but `downloadORT()` rejects Windows. Issue #5 tracks this.

### System Dependencies

 | Dependency   | Required       | Bundled                                               |
 | ---          | ---            | ---                                                   |
 | espeak-ng    | Yes (TTS only) | No — `apt install espeak-ng` or `brew install espeak` |
 | ONNX Runtime | Yes            | Auto-downloaded to ~/.cattery/                        |
 | Model files  | Yes            | Auto-downloaded to ~/.cattery/                        |

Issue #22 tracks bundling espeak-ng to achieve zero system dependencies.

## Performance Tuning Tips

**Reduce memory**: `--chunk-size 10s` or `CATTERY_CHUNK_SIZE=10`. Smaller
chunks = more boundary artifacts but lower peak RSS. Mainly affects STT.

**Reduce latency**: `cattery serve --keep-alive` pre-warms engines and skips
cold start on first request. `--idle-timeout 300` keeps engines loaded between
requests.

**Multiple workers**: `cattery serve -w 2 --listen-workers 2` runs two TTS and
two STT engines in parallel. Each TTS engine adds ~300 MB peak; each STT
engine adds ~200 MB peak.

**Pipe vs file output**: File output is slightly faster (seekable WAV path
avoids temp file). Use `-o file.wav` when possible; `-o -` for piping is
correct but uses a temp-file intermediary.
