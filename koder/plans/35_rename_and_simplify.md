# Plan 35 — Rename to `tts`/`stt` and Simplify Default UX

## Status: DRAFT

## Goal

Land issues `#30` and `#31` together as one cohesive refactor:

- rename user-facing verbs from `speak` / `listen` to `tts` / `stt`
- rename Go packages `speak` / `listen` to `tts` / `stt`
- keep old CLI subcommands, HTTP routes, and serve flags as hidden compatibility aliases
- reduce the default help surface to the core commands and move advanced knobs behind `cattery help --advanced`
- simplify TTS/STT stderr summaries so model names disappear from the happy path

This should ship as one pass, not as a staggered migration, because package
paths, command names, tests, and server JSON all need to stay consistent.

## Rename / Move Matrix

| From | To | Why |
|---|---|---|
| `speak/speak.go` | `tts/tts.go` | Rename package path and file to the new modality name |
| `speak/kokoro/chunk.go` | `tts/kokoro/chunk.go` | Keep TTS implementation under the renamed package tree |
| `speak/kokoro/chunk_test.go` | `tts/kokoro/chunk_test.go` | Keep Kokoro tests colocated after the move |
| `speak/kokoro/kokoro.go` | `tts/kokoro/kokoro.go` | Rename Kokoro import path from `speak/...` to `tts/...` |
| `listen/listen.go` | `stt/stt.go` | Rename package path and file to the new modality name |
| `listen/moonshine/chunk.go` | `stt/moonshine/chunk.go` | Keep STT implementation under the renamed package tree |
| `listen/moonshine/chunk_test.go` | `stt/moonshine/chunk_test.go` | Keep Moonshine chunk tests colocated after the move |
| `listen/moonshine/decoder.go` | `stt/moonshine/decoder.go` | Rename Moonshine import path from `listen/...` to `stt/...` |
| `listen/moonshine/moonshine.go` | `stt/moonshine/moonshine.go` | Rename Moonshine engine package imports and interfaces |
| `listen/moonshine/stream_test.go` | `stt/moonshine/stream_test.go` | Keep stream tests colocated after the move |
| `listen/moonshine/tokenizer.go` | `stt/moonshine/tokenizer.go` | Keep tokenizer logic under the renamed STT package tree |
| `cmd/cattery/listen.go` | `cmd/cattery/stt.go` | Match the primary CLI command name |
| `server/listen.go` | `server/stt.go` | Match the primary server handler / file responsibility |

## Files To Edit

| File | Planned change |
|---|---|
| `cmd/cattery/main.go` | Rename command dispatch to `tts` / `stt`; keep `speak` / `listen` as hidden aliases; add `help --advanced`; split default vs advanced help text; rename serve config fields and flags to `tts` / `stt`; keep old serve flags as hidden aliases; update `parseKindAlias`, `commandNames`, typo detection, examples, usage strings, and help prose |
| `cmd/cattery/io.go` | Rename TTS helper names if needed for clarity and update hard-coded usage text from `cattery speak` to `cattery tts` |
| `cmd/cattery/main_test.go` | Update tests for alias parsing, command detection, and default-primary command names; add coverage that old verbs still parse but new verbs are the names exposed by help/errors |
| `cmd/cattery/stt.go` | Rename package imports from `listen` to `stt`; update usage strings, model lookup hints, and stderr summary format; keep behavior identical apart from naming/output changes |
| `cmd/stt-spike/main.go` | Update imports from `speak` to `tts` so the spike still builds after the package move |
| `memtest/rss_test.go` | Update imports and type references from `speak` / `listen` to `tts` / `stt` |
| `tts/tts.go` | Change package name to `tts`; keep `Engine`, `Options`, and `Voice` type names stable under the new package |
| `tts/kokoro/kokoro.go` | Update imports and type references to `tts`; keep `Speak()`, `Voices()`, and `ResolveVoice()` behavior unchanged |
| `tts/kokoro/chunk.go` | Package rename only unless comments mention `speak` |
| `tts/kokoro/chunk_test.go` | Package/import rename only |
| `stt/stt.go` | Change package name to `stt`; keep `Engine`, `Options`, and `Result` type names stable under the new package |
| `stt/moonshine/moonshine.go` | Update imports and type references to `stt`; keep `Transcribe()` and chunking behavior unchanged |
| `stt/moonshine/chunk.go` | Package rename only unless comments mention `listen` |
| `stt/moonshine/chunk_test.go` | Package/import rename only |
| `stt/moonshine/decoder.go` | Package rename only |
| `stt/moonshine/stream_test.go` | Package/import rename only |
| `stt/moonshine/tokenizer.go` | Package rename only |
| `server/server.go` | Rename config fields (`SpeakWorkers` -> `TTSWorkers`, `ListenWorkers` -> `STTWorkers`, `SpeakModel` -> `TTSModel`, `ListenModel` -> `STTModel`); rename internal state/pool/model fields and request/status structs from speak/listen to tts/stt; make `/v1/tts` primary and add `/v1/stt`; keep `/v1/speak` and `/v1/listen` as aliases; rename status JSON keys to `tts` / `stt`; update log lines and error strings to the new terminology |
| `server/stt.go` | Rename imports and type references from `listen` to `stt`; add `/v1/stt`-first terminology in helpers and errors; keep `/v1/listen` route compatibility via `server.go` |
| `server/auth_test.go` | Update protected-route test path from `/v1/speak` to `/v1/tts`; optionally add a second assertion that `/v1/speak` remains accepted as an alias if a focused route test is added elsewhere |

## Execution Order

1. `git mv` the package trees and modality-specific entrypoint files first:
   `speak/` -> `tts/`, `listen/` -> `stt/`, `cmd/cattery/listen.go` -> `cmd/cattery/stt.go`, `server/listen.go` -> `server/stt.go`.
2. Update package declarations inside moved files:
   `package speak` -> `package tts`, `package listen` -> `package stt`.
3. Rewrite all import paths and type references repo-wide:
   `github.com/jikkuatwork/cattery/speak` -> `.../tts`,
   `github.com/jikkuatwork/cattery/listen` -> `.../stt`,
   plus `speak.Engine` / `listen.Engine` -> `tts.Engine` / `stt.Engine`.
4. Rename internal server identifiers next so compilation errors collapse quickly:
   pool names, config fields, request/status structs, helper names, and log messages in `server/server.go` and `server/stt.go`.
5. Update CLI dispatch and command parsing in `cmd/cattery/main.go`:
   primary verbs `tts` / `stt`, hidden aliases `speak` / `listen`, and renamed serve flags with compatibility aliases.
6. Update CLI usage strings and happy-path stderr formatting in `cmd/cattery/main.go`, `cmd/cattery/stt.go`, and `cmd/cattery/io.go`.
7. Implement the default/advanced help split:
   `printUsage()` becomes default help, add advanced help output, and wire `cattery help --advanced`.
8. Update tests after the rename is compiling:
   `cmd/cattery/main_test.go`, `server/auth_test.go`, moved package tests, and `memtest/rss_test.go`.
9. Run `gofmt -w` on all moved/edited Go files.
10. Run verification:
   `go test ./...`, targeted CLI/server smoke checks, then optional memtest/manual HTTP compatibility checks.

## CLI / Help Design

### Command behavior

- Primary user-facing commands become `cattery tts ...` and `cattery stt ...`.
- `cattery speak ...` and `cattery listen ...` remain functional, but are omitted from default and advanced command summaries except in an explicit compatibility-alias note.
- Bare text still routes to TTS:
  `cattery "Hello"` == `cattery tts "Hello"`.
- Serve flags rename to:
  - `--tts-workers`
  - `--stt-workers`
  - `--tts-model`
  - `--stt-model`
- Backward-compatible hidden aliases remain accepted:
  - `--workers`, `--speak-workers`, `-w` -> `--tts-workers`
  - `--listen-workers` -> `--stt-workers`
  - `--speak-model` -> `--tts-model`
  - `--listen-model` -> `--stt-model`

### Default help text

```text
cattery - local speech tools

Usage:
  cattery "Hello, world."          Shortcut for cattery tts
  cattery tts "Hello, world."      Text to speech
  cattery stt audio.wav            Speech to text
  cattery serve                    Start REST API server
  cattery status                   Show model/runtime status
  cattery download                 Pre-download local assets
  cattery help --advanced          Show advanced commands and flags

Commands:
  tts         Text to speech
  stt         Speech to text
  serve       Start REST API server
  status      Show system status and downloaded artefacts
  download    Pre-download local models and runtime
  help        Show this help

TTS:
  --voice REF      Voice number, name, or ID
  --female         Pick a random female voice
  --male           Pick a random male voice
  --speed FLOAT    Speech speed, 0.5-2.0 (default: 1.0)
  --output, -o     Output WAV file (default: output.wav or stdout if piped)

STT:
  --output, -o     Output text file (default: stdout)

Server:
  cattery serve --port 8080
  cattery serve --auth

Run 'cattery help --advanced' for list, keys, model selection, language hints,
chunk-size controls, and server tuning flags.
```

### Advanced help text

```text
cattery - local speech tools

Usage:
  cattery "Hello, world."          Shortcut for cattery tts
  cattery tts --voice 4 "Hi there"
  cattery tts --model 1 "Hi there"
  cattery stt call.wav
  cattery stt -

Commands:
  tts         Text to speech
  stt         Speech to text
  serve       Start REST API server
  status      Show system status and downloaded artefacts
  download    Pre-download local models and runtime
  list        List TTS/STT models and voices
  keys        Manage API keys for --auth server mode
  help        Show this help

TTS flags:
  --voice REF      Voice number, name, or ID
  --female         Pick a random female voice
  --male           Pick a random male voice
  --speed FLOAT    Speech speed, 0.5-2.0 (default: 1.0)
  --chunk-size DUR Chunk size override, 10s-60s (bare ints = seconds)
  --output, -o     Output WAV file (default: output.wav or stdout if piped)
  --lang LANG      Phonemizer language (default: en-us)
  --model REF      TTS model index or ID (default: 1)

STT flags:
  --chunk-size DUR Chunk size override, 10s-60s (bare ints = seconds)
  --output, -o     Output text file (default: stdout)
  --lang LANG      Language hint
  --model REF      STT model index or ID (default: 1)

Manage:
  cattery list
  cattery list tts
  cattery download stt
  cattery status tts --model 1

Pipes:
  cattery stt call.wav | cattery tts
  echo "Hello" | cattery tts | cattery stt

Server:
  cattery serve --port 8080
  cattery serve --tts-workers 2
  cattery serve --stt-workers 2
  cattery serve --chunk-size 20s
  cattery serve --tts-model 1
  cattery serve --stt-model 1
  cattery serve --max-chars 300
  cattery serve --queue-max 10
  cattery serve --idle-timeout 600
  cattery serve --keep-alive
  cattery serve --auth

Keys:
  cattery keys create --name my-app
  cattery keys list
  cattery keys revoke cat_a1b2c3d4
  cattery keys delete cat_a1b2c3d4

Compatibility aliases:
  cattery speak ...               Alias for cattery tts
  cattery listen ...              Alias for cattery stt
  --speak-workers, -w, --workers  Alias for --tts-workers
  --listen-workers                Alias for --stt-workers
  --speak-model                   Alias for --tts-model
  --listen-model                  Alias for --stt-model

Chunk size:
  CATTERY_CHUNK_SIZE   Shared override for tts, stt, and serve
  Auto default         10s <=512MB, 15s <=1GB, 20s <=2GB, 30s <=4GB,
                       45s <=8GB, 60s >8GB, 30s if RAM is unknown

On first run, cattery downloads the model (~92MB) and runtime (~18MB).
No accounts or API keys required.
```

## New Stderr Output

### TTS

Current CLI output includes the model name:

```text
output.wav | Used Bella in Kokoro 82M, took 1.2s (RTF: 0.70)
```

Planned output drops the model name and keeps only destination, voice, audio
duration, and RTF:

```text
output.wav | Bella, 1.2s (RTF: 0.70)
stdout | Adam, 3.8s (RTF: 0.64)
```

Implementation note:

- keep voice display name, not voice ID
- format duration with one decimal place
- keep RTF with two decimals

### STT

Current CLI output includes the model name and input source:

```text
stdout | Used Moonshine Tiny on 3.5s from call.wav, took 0.8s (RTF: 0.23)
```

Planned output drops the model name and input source from the default path:

```text
stdout | 3.5s audio, 0.8s (RTF: 0.23)
transcript.txt | 12.4s audio, 2.9s (RTF: 0.23)
```

Implementation note:

- destination remains first (`stdout` or output filename)
- audio duration and elapsed time both use one decimal place
- model names remain visible in `cattery status`, not in per-run stderr

## Server API / Status

### Routes

Before:

- primary TTS route: `/v1/speak`
- alias TTS route: `/v1/tts`
- STT route: `/v1/listen`

After:

- primary TTS route: `/v1/tts`
- compatibility TTS alias: `/v1/speak`
- primary STT route: `/v1/stt`
- compatibility STT alias: `/v1/listen`

### Status JSON before / after

Before:

```json
{
  "status": "ok",
  "speak": {
    "model": 1,
    "model_id": "kokoro-82m-v1.0",
    "model_name": "Kokoro 82M",
    "workers": 1,
    "engines_ready": 0,
    "max_chars": 500,
    "chars_used": 0
  },
  "listen": {
    "model": 1,
    "model_id": "moonshine-tiny-v1.0",
    "model_name": "Moonshine Tiny",
    "workers": 1,
    "engines_ready": 0
  },
  "queued": 0,
  "processed": 0,
  "failed": 0,
  "uptime": "12s"
}
```

After:

```json
{
  "status": "ok",
  "tts": {
    "model": 1,
    "model_id": "kokoro-82m-v1.0",
    "model_name": "Kokoro 82M",
    "workers": 1,
    "engines_ready": 0,
    "max_chars": 500,
    "chars_used": 0
  },
  "stt": {
    "model": 1,
    "model_id": "moonshine-tiny-v1.0",
    "model_name": "Moonshine Tiny",
    "workers": 1,
    "engines_ready": 0
  },
  "queued": 0,
  "processed": 0,
  "failed": 0,
  "uptime": "12s"
}
```

Internal Go struct and field names should match the new terminology as well,
so the server stops carrying `speak*` / `listen*` identifiers internally.

## Compatibility Rules

- Keep CLI aliases `speak` and `listen` working for one release cycle.
- Keep HTTP aliases `/v1/speak` and `/v1/listen` working for one release cycle.
- Keep old serve flags accepted but omitted from help.
- Keep `parseKindAlias()` accepting both old and new verbs so `cattery list speak`
  and `cattery list tts` both work during the transition.
- All help text, error messages, examples, and logs should prefer `tts` / `stt`
  unless they are explicitly documenting a compatibility alias.

## Verification Steps

1. Formatting and compile safety:
   - `gofmt -w ./cmd/cattery ./server ./tts ./stt ./memtest ./cmd/stt-spike`
   - `go test ./...`
2. CLI rename / alias behavior:
   - `go test ./cmd/cattery`
   - `go run ./cmd/cattery help`
   - `go run ./cmd/cattery help --advanced`
   - verify default help does not list `list`, `keys`, `--model`, `--chunk-size`, `--lang`, `--tts-workers`, `--stt-workers`
   - verify advanced help does list them
   - `go run ./cmd/cattery tts "hello"`
   - `go run ./cmd/cattery speak "hello"` still works
   - `go run ./cmd/cattery stt sample.wav`
   - `go run ./cmd/cattery listen sample.wav` still works
3. Serve flags and HTTP compatibility:
   - `go run ./cmd/cattery serve --tts-workers 2 --stt-workers 2`
   - `go run ./cmd/cattery serve --speak-workers 2 --listen-workers 2`
   - POST to `/v1/tts` and `/v1/speak`, confirm both synthesize
   - POST to `/v1/stt` and `/v1/listen`, confirm both transcribe
   - GET `/v1/status`, confirm top-level keys are `tts` and `stt`
4. Regression checks:
   - `go test ./server`
   - `go test ./tts/... ./stt/...`
   - `go build ./cmd/stt-spike`
5. Optional heavyweight validation when local assets are present:
   - `go test -tags memtest ./memtest -v`

## Risks To Watch

- The package-path move is repo-wide and will break imports everywhere until
  all references are updated in the same pass.
- `cmd/cattery/main.go` currently owns both CLI dispatch and all help text, so
  the UX simplification and rename work are tightly coupled there.
- Hidden alias support can easily leak into default help if `commandNames()` and
  help rendering are not separated into “accepts” vs “advertises”.
- Server JSON renames are externally visible; tests should assert both route
  compatibility and the new `tts` / `stt` status keys.
