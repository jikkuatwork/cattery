# 19 — CLI redesign: subcommand-per-modality

## Status: open
## Priority: P1
## Depends on: #17 (TTS interface), #20 (STT package)
## Blocks: nothing

## Problem

The current CLI is TTS-only. Bare `cattery "text"` synthesizes speech. Subcommands are management-only (`serve`, `list`, `status`, `download`). With STT arriving, the CLI needs a coherent verb structure that scales to more modalities without becoming inconsistent.

## Goal

Design a CLI where each modality is a verb: `cattery speak`, `cattery listen`. Management commands stay top-level. Bare `cattery "text"` remains a shortcut for `cattery speak "text"` (backwards compat, PoLS). All models and voices are addressed by numeric index — fast to type, no slugs to memorize.

## Core design principle: numeric indices everywhere

Models and voices are identified by **stable numeric index** in the CLI. Slugs exist but are internal — they appear in JSON API responses and registry data, never as required CLI input.

```
cattery speak --model 1 --voice 4 "Hello"
cattery listen --model 1 audio.wav
```

Not:
```
cattery speak --model kokoro-82m-v1.0 --voice af_bella "Hello"  # too long
```

Indices are assigned per-kind: TTS models are numbered 1, 2, 3...; STT models separately 1, 2, 3...; voices within a model are numbered 1, 2, 3...

**Default model = 1** for each kind. So `--model` is rarely needed.

## Design

### Command structure

```
cattery speak "Hello world"              # TTS: text → audio (model 1, random voice)
cattery speak --voice 4 "Hello"          # pick voice by number
cattery speak --model 2 "Hello"          # pick model by number
cattery speak --female --speed 1.2 "Hi"  # gender filter + speed

cattery listen audio.wav                 # STT: audio → text (model 1)
cattery listen --model 2 audio.wav       # explicit model
cattery listen -                         # stdin (pipe audio in)

cattery serve                            # REST API (all modalities)
cattery list                             # list all models + voices
cattery list speak                       # TTS only
cattery list listen                      # STT only
cattery status                           # system status
cattery download                         # download all artefacts
cattery download speak                   # TTS only
cattery download listen                  # STT only

cattery "Hello world"                    # shortcut → cattery speak "Hello world"
```

### `cattery list` — the index reference

```
$ cattery list

TTS
  01  Kokoro 82M           88.0 MB  local   ✓
  02  OpenAI TTS-1                   remote  ☁

  Voices (01 Kokoro 82M)
  01  ♀  Heart        Warm, expressive       ✓
  02  ♀  Alloy        Balanced, versatile    ✓
  03  ♀  Aoede        Melodic, clear         ✓
  04  ♀  Bella        Bright, friendly       ✓
  ...
  27  ♂  Lewis        Composed, refined      ✓

  Voices (02 OpenAI TTS-1)
  01  ●  Alloy
  02  ●  Echo
  03  ●  Fable
  04  ●  Onyx
  05  ●  Nova
  06  ●  Shimmer

STT
  01  Moonshine Tiny       27.0 MB  local   ✓
  02  OpenAI Whisper-1               remote  ☁
```

Design notes:
- Remote models only shown when `OPENAI_API_KEY` is set
- ✓ = downloaded, ☁ = remote (no download needed), blank = not downloaded
- Voices listed under their parent model with the model index prefix
- Voice gender: ♀ / ♂ for local, ● for remote (gender not applicable)
- Indices are two-digit zero-padded for alignment

### Index resolution rules

1. `--voice 4` → voice index 4 of the selected model
2. `--voice bella` → name lookup (still works, for scripts/humans who know names)
3. `--model 1` → model index 1 of the relevant kind (TTS for speak, STT for listen)
4. `--model kokoro-82m-v1.0` → slug lookup (still works, for API scripts)
5. No flag → model 1, random voice

Name/slug lookup is a fallback, not the primary interface. Indices are the fast path.

### PoLS principles

1. **Bare text = speak.** `cattery "Hello"` still works.
2. **Verbs match modality.** `speak`, `listen`, future `think`, `see`.
3. **Flags are local to verbs.** `--voice` only for `speak`. `--model` for any verb.
4. **Numbers are fast.** `--voice 4` beats `--voice af_bella` every time.
5. **Management commands are top-level nouns.** `serve`, `list`, `status`, `download`.
6. **No ambiguity.** Known verb/command or text to speak.

### `cattery listen` specifics

```
cattery listen recording.wav              # transcribe file → stdout
cattery listen recording.opus             # auto-detect format
cattery listen -                          # stdin
cattery listen --lang en                  # language hint
cattery listen --model 2 audio.wav        # explicit model (e.g., remote Whisper)
cattery listen -o transcript.txt          # output to file
```

Pipeable:
```bash
cattery listen call.wav | cattery speak              # round-trip
echo "Hello" | cattery speak | cattery listen         # TTS → STT
```

### `cattery download` redesign

```
cattery download                # everything
cattery download speak          # TTS artefacts
cattery download listen         # STT artefacts
cattery download --model 2      # specific model by index
```

### `cattery status`

```
cattery status

  Platform:      linux/arm64
  Data dir:      ~/.cattery

  espeak-ng:     ✓ bundled
  ONNX Runtime:  ✓ 1.24.1

  TTS
    01  ✓  Kokoro 82M       88.0 MB   27/27 voices
  STT
    01  ✓  Moonshine Tiny   27.0 MB

  Disk: 138.2 MB
```

### Implementation

Manual switch on `args[0]` — no CLI framework:

```go
switch args[0] {
case "speak":
    return cmdSpeak(args[1:])
case "listen":
    return cmdListen(args[1:])
case "serve":
    return cmdServe(args[1:])
// ...
default:
    return cmdSpeak(args) // bare text → speak
}
```

### File changes

- **Edit**: `cmd/cattery/main.go` — add `speak`/`listen` verbs, update dispatch, help
- **Add**: `cmd/cattery/listen.go` — `cmdListen()` (or inline)
- **Edit**: `cmd/cattery/main.go` — update `cmdList()`, `cmdDownload()`, `cmdStatus()` for indices + multi-modality
- **Edit**: `registry/registry.go` — stable index assignment, index lookup helpers

## Acceptance criteria

- [ ] `cattery speak --voice 4 "Hello"` works (numeric voice)
- [ ] `cattery speak --model 2 "Hello"` works (numeric model)
- [ ] `cattery speak --voice bella "Hello"` works (name fallback)
- [ ] `cattery speak "Hello"` works (defaults: model 1, random voice)
- [ ] `cattery "Hello"` works (bare text shortcut)
- [ ] `cattery listen audio.wav` transcribes to stdout
- [ ] `cattery listen -` reads from stdin
- [ ] `cattery list` shows indexed models + voices, local/remote markers
- [ ] `cattery list speak` / `cattery list listen` filters by kind
- [ ] Remote models hidden when no `OPENAI_API_KEY`
- [ ] `cattery download` fetches all artefacts
- [ ] `cattery download --model 2` fetches specific model
- [ ] `cattery status` shows indexed model status
- [ ] Help text shows numeric index usage
- [ ] `looksLikeCommand()` updated for `speak`, `listen`
- [ ] Pipeable: `cattery listen x.wav | cattery speak` works

## Notes

- No CLI framework. Hand-rolled flag parsing stays.
- Indices are assigned at registry level, not at runtime. Adding a new model gets the next index. Indices never change once assigned (stable across versions).
- The `--voice` flag accepts both numbers and names. Numbers are tried first, then name lookup. `--voice 4` is unambiguous. `--voice nova` could be a name — so it's a name lookup.
- For JSON API responses (server endpoints), always include both index and slug. The API is for machines; the CLI is for humans.
