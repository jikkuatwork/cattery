# Plan 22 — Bundle `espeak-ng` via `go:embed`

## Status: DRAFT

## Goal

Land issue `#22` so TTS works after `go install` with no `apt`/`brew` step and no
network dependency for `espeak-ng`. The Go binary should contain:

- one platform-specific `espeak-ng` executable, selected at build time
- one shared `espeak-ng-data` archive
- extraction logic that materializes both into `~/.cattery/espeak-ng/`
- runtime status/preflight output that reports the bundled install instead of a
  PATH dependency

Archives are committed in-tree under `phonemize/bundle/`. They are not fetched
at runtime and are not stored via Git LFS.

## Scope

This plan covers:

1. building static `espeak-ng` bundles for 4 target platforms
2. `go:embed` layout and build-tagged wiring
3. runtime extraction, validation, and cache invalidation
4. `phonemize/` changes to always use the bundled binary
5. `preflight/` changes to stop treating `espeak-ng` as a system dependency
6. `THIRD_PARTY_NOTICES.md` updates for redistribution
7. a maintainer build script for regenerating the bundled archives

It also includes the necessary user-facing follow-through in `cmd/cattery` and
`server/`, because those paths currently still fail fast on `phonemize.Available()`
and `cattery status` currently reports `espeak-ng` as “installed / not found”.

## Current State

- `phonemize/espeak.go` shells out to `"espeak-ng"` on `PATH`.
- `phonemize.Available()` is just `exec.LookPath("espeak-ng")`.
- `preflight/check.go` marks missing PATH `espeak-ng` as a readiness error.
- `cmd/cattery/main.go` rejects TTS if `espeak-ng` is not installed and reports
  it that way in `cattery status`.
- `server/server.go` refuses to start without PATH `espeak-ng`.
- `THIRD_PARTY_NOTICES.md` explicitly says `espeak-ng` is not bundled.

Those assumptions all need to move to a bundled-runtime model.

## Decisions

### 1. Keep the embed surface small and opaque

Embed compressed tarballs, not loose files:

- `phonemize/bundle/<platform>.tar.gz` for the executable
- `phonemize/bundle/data.tar.gz` for `espeak-ng-data`

This keeps the legal and engineering story aligned with the issue:

- the Go source tree does not contain unpacked GPL/LGPL content
- the runtime still uses `os/exec`, not linking/cgo
- extraction is a one-time deployment step into `~/.cattery/espeak-ng/`

### 2. Use a bundle marker, not the app version, for cache invalidation

The issue sketch mentions a `.version` file tied to the cattery version. That is
too coarse: the app version can change without changing `espeak-ng`, and a
bundle could change between dev builds without a release tag bump.

Instead, write a marker file keyed to the bundled payload, for example:

```text
espeak-ng 1.51
bundle-format 1
linux/amd64
bin-sha256 <hex>
data-sha256 <hex>
```

The code should re-extract when the marker content does not exactly match the
compiled-in expectations.

### 3. Treat extraction as the availability check

After this change, “is `espeak-ng` available?” means:

- the embedded payload exists for this build target
- the extraction directory is present or can be created
- the extracted executable and data tree match the current bundle marker

The public helper should move from “is it on PATH?” to “can the bundled runtime
be resolved?”. That avoids duplicate logic across CLI, server, and preflight.

### 4. Prefer deterministic bundled behavior over host variation

Do not fall back to system `espeak-ng` when bundled extraction fails or when a
host binary exists on `PATH`. The bundled binary should be the single runtime
path used by `cattery`, otherwise IPA output varies by host package version and
the issue goal is only partially solved.

## Target Layout

```text
phonemize/
├── bundle/
│   ├── linux_amd64.tar.gz
│   ├── linux_arm64.tar.gz
│   ├── darwin_amd64.tar.gz
│   ├── darwin_arm64.tar.gz
│   └── data.tar.gz
├── embed_linux_amd64.go
├── embed_linux_arm64.go
├── embed_darwin_amd64.go
├── embed_darwin_arm64.go
├── bundle_meta.go
├── extract.go
└── espeak.go
```

Suggested extracted layout:

```text
~/.cattery/espeak-ng/
├── bin/
│   └── espeak-ng
├── data/
│   └── ...
└── .bundle-version
```

On macOS, the extracted binary can still be named `espeak-ng` without suffixes.

## File Plan

| File | Planned change |
| --- | --- |
| `phonemize/embed_linux_amd64.go` | Add `//go:build linux && amd64`, embed `bundle/linux_amd64.tar.gz` plus `bundle/data.tar.gz` |
| `phonemize/embed_linux_arm64.go` | Same for `linux && arm64` |
| `phonemize/embed_darwin_amd64.go` | Same for `darwin && amd64` |
| `phonemize/embed_darwin_arm64.go` | Same for `darwin && arm64` |
| `phonemize/bundle_meta.go` | Define bundle constants and expected marker content or hashes |
| `phonemize/extract.go` | Add extraction, marker validation, chmod, atomic staging, and cached path resolution |
| `phonemize/espeak.go` | Replace PATH lookup with bundled runtime resolution; inject `ESPEAK_DATA_PATH`; keep `Phonemize()` logic intact |
| `phonemize/espeak_test.go` | Replace PATH-dependent skips with bundle-resolution tests; add extraction/cache coverage where practical |
| `preflight/check.go` | Remove PATH `espeak-ng` check; optionally verify bundled runtime can resolve if preflight should remain strict |
| `cmd/cattery/main.go` | Remove install hint; update `status` output to bundled runtime/version wording |
| `server/server.go` | Remove PATH guard; fail only if bundled runtime cannot be prepared |
| `THIRD_PARTY_NOTICES.md` | Replace “external only” language with bundling-specific attribution and source pointer |
| `.github/workflows/build-espeak.yml` | Manual-trigger workflow to build espeak-ng on native runners and PR the archives |

## Implementation Details

### 1. Build `espeak-ng` archives via GitHub Actions (`workflow_dispatch`)

Targets:

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`

Expected archive contents:

- platform archive: `bin/espeak-ng`
- shared data archive: `data/...`

#### GitHub Actions workflow: `.github/workflows/build-espeak.yml`

Trigger: **`workflow_dispatch` only** — runs when a maintainer clicks
"Run workflow" in the GitHub UI. Never runs on push, PR, or schedule.

Inputs:

- `espeak_version` (string, default `1.51`) — upstream tag to build

Strategy: **matrix build on native runners**

```yaml
strategy:
  matrix:
    include:
      - os: ubuntu-latest
        goos: linux
        goarch: amd64
        artifact: linux_amd64.tar.gz
      - os: ubuntu-24.04-arm
        goos: linux
        goarch: arm64
        artifact: linux_arm64.tar.gz
      - os: macos-13          # Intel runner
        goos: darwin
        goarch: amd64
        artifact: darwin_amd64.tar.gz
      - os: macos-latest       # Apple Silicon runner
        goos: darwin
        goarch: arm64
        artifact: darwin_arm64.tar.gz
```

Each matrix job:

1. Checks out espeak-ng at the pinned tag
2. Installs build deps (`cmake`, `make`; Linux: `musl-tools` for static build)
3. Builds: `cmake -DBUILD_SHARED_LIBS=OFF` + `make`
4. Linux: link with musl for truly static binaries (`CC=musl-gcc`)
5. macOS: system toolchain, ordinary Mach-O
6. Packages `bin/espeak-ng` into `<platform>.tar.gz`
7. Uploads as workflow artifact

A final job (after matrix completes):

1. Downloads all 4 platform artifacts
2. Packages `espeak-ng-data/` once as `data.tar.gz`
3. Opens a PR to commit all 5 archives into `phonemize/bundle/`
4. PR includes SHA-256 sums in the description for verification

Requirements for the built executable:

- Linux builds must be static (musl) to avoid glibc version mismatch
- macOS builds use system toolchain, ordinary Mach-O per architecture
- The bundled binary must work with `ESPEAK_DATA_PATH` pointed at the extracted
  `data/` directory — must not require a system install path

The workflow is maintainer tooling, not part of the regular build. Normal
`go build` / `go install` never invokes it. The resulting archives are checked
into the repo directly (no Git LFS) — they’re small (~3MB total) and updates
are infrequent.

### 2. `go:embed` structure with build tags

Use four small build-tagged files so each build includes only one executable
archive:

- `embed_linux_amd64.go`
- `embed_linux_arm64.go`
- `embed_darwin_amd64.go`
- `embed_darwin_arm64.go`

Each file should:

- import `_ "embed"`
- define `var espeakBinArchive []byte`
- define `var espeakDataArchive []byte`
- define `const bundleTarget = "<goos>/<goarch>"` or equivalent

The data archive is duplicated per platform build, which is acceptable because
it is shared by all targets and only one platform build is produced at a time.

Keep metadata that is not platform-specific in a common file:

- bundled upstream version, for example `1.51`
- bundle format version, for example `1`
- expected destination names
- optional expected SHA-256 hashes if they are checked in by the maintainer script

### 3. Runtime extraction and caching

Add a single resolver in `phonemize/extract.go`, for example:

- `func ResolveBundled() (Runtime, error)`
- `type Runtime struct { Bin string; DataDir string; Version string }`

Responsibilities:

- compute `~/.cattery/espeak-ng/` from `paths.DataDir()`
- check the marker file before doing any work
- extract both archives if the marker is missing or stale
- ensure the binary is executable
- return stable paths for command execution and status output

Extraction algorithm:

1. Build destination paths:
   - root: `~/.cattery/espeak-ng`
   - staging: `~/.cattery/espeak-ng.tmp-<pid>-<rand>`
2. If `.bundle-version` exists and exactly matches the compiled marker, trust
   the cache and return immediately.
3. Create the staging dir under `~/.cattery/`.
4. Untar the executable archive into staging.
5. Untar the data archive into staging.
6. Validate the extracted layout:
   - `bin/espeak-ng` exists and is a regular file
   - `data/` exists
7. `chmod 0755` the binary.
8. Write `.bundle-version` into staging only after successful extraction.
9. Atomically swap staging into place:
   - remove old root if present
   - rename staging to final root
10. Best-effort cleanup of abandoned staging dirs.

Concurrency:

- guard extraction with a package-level mutex in-process
- tolerate a second process racing by re-checking the marker after lock
- prefer rename-based replacement over piecemeal writes to avoid partial caches

Failure mode:

- return explicit extraction errors upward
- never silently fall back to PATH `espeak-ng`

### 4. `phonemize/` changes

`phonemize/espeak.go` should stop calling `"espeak-ng"` directly. Instead:

- resolve the bundled runtime once per command invocation
- execute `runtime.Bin`
- append `ESPEAK_DATA_PATH=<runtime.DataDir>` to `cmd.Env`

Suggested API shape:

- replace `Available() bool` with `Available() bool` backed by `ResolveBundled()`
  or add a more explicit `ResolveBundled()`/`RuntimeStatus()` helper and keep
  `Available()` as a thin wrapper for compatibility
- add a helper on the phonemizer, for example `func (e *EspeakPhonemizer) runtime() (Runtime, error)`

The phonemization behavior itself should remain unchanged:

- same punctuation splitting
- same default voice fallback (`en-us`)
- same espeak flags (`-q --ipa=3 --sep= -v`)

Tests to add or update:

- resolve bundled runtime succeeds with embedded payload
- repeated resolve uses the cache and does not re-extract
- stale marker triggers re-extraction
- `Available()` reflects bundle readiness, not PATH

### 5. `preflight/` changes

`preflight/check.go` should no longer report “espeak-ng not found on PATH”.

Two acceptable designs:

- minimal: remove the espeak-specific check entirely and leave extraction to the
  first synthesis attempt
- stronger: replace it with `phonemize.Available()` or a non-extracting bundle
  sanity check, so `preflight.Check()` still catches obviously broken local state

The stronger design is preferable if it does not do unnecessary work on every
call. A pragmatic split is:

- `preflight.Check()` calls a cheap `phonemize.Status()` helper that validates
  embed metadata and existing extracted files, but does not force extraction
- the actual extraction happens when TTS first needs the runtime

Whichever variant is chosen, user-visible error text must stop instructing users
to install `espeak-ng` with a system package manager.

### 6. `cmd/cattery` and server follow-through

The issue acceptance criteria mention `cattery status` showing the bundled
version. That requires these updates:

- `cmd/cattery/main.go`
  - remove the early PATH error in the TTS command
  - change `status` output from `installed/not found` to bundled status, for example:
    - `espeak-ng:     ✓ bundled 1.51`
    - `espeak-ng:     ✓ bundled 1.51 (extracted)`
    - `espeak-ng:     ✗ bundled runtime unavailable`
- `server/server.go`
  - remove the PATH guard
  - optionally resolve the bundle at server startup so misconfigured caches fail early

This is part of the implementation, not optional cleanup, because the current
CLI and server both block the bundled path from ever being used.

### 7. `THIRD_PARTY_NOTICES.md` updates

Replace the current “keep it external” language with redistribution-specific
notices covering both pieces:

- `espeak-ng` executable
  - upstream project name
  - pinned version or release tag
  - LGPL-2.1-or-later attribution
  - statement that `cattery` redistributes the unmodified executable
  - source pointer to the upstream repository/release tag
- `espeak-ng-data`
  - GPL-3.0-or-later attribution
  - statement that it is redistributed as separate data consumed by an external
    subprocess after extraction
  - same source pointer

Also update the findings section so it no longer says bundling is blocked.

If the repository already carries top-level `LICENSE`/`NOTICE`, do not duplicate
full GPL/LGPL texts unless the project wants them in-tree; the minimum plan
requirement here is accurate attribution plus a source pointer, matching the
issue statement.

### 8. GitHub Actions workflow

Replace the local `scripts/build-espeak.sh` with `.github/workflows/build-espeak.yml`
(described in section 1 above).

The workflow:

1. Is triggered manually via `workflow_dispatch` in the GitHub UI
2. Builds espeak-ng natively on 4 runners (2 Linux, 2 macOS)
3. Packages the 4 platform binaries + 1 shared data archive
4. Opens a PR committing the archives to `phonemize/bundle/`
5. PR description includes SHA-256 sums for verification

Non-goals:

- does not run on push, PR, or schedule — manual trigger only
- does not run from `go generate` or `go build`
- does not download anything at cattery runtime

## Execution Order

1. Add the checked-in archive layout under `phonemize/bundle/`.
2. Add the four build-tagged embed files plus shared bundle metadata.
3. Implement extraction and runtime resolution in `phonemize/extract.go`.
4. Rewrite `phonemize/espeak.go` to use the resolved bundled runtime and set
   `ESPEAK_DATA_PATH`.
5. Update tests in `phonemize/espeak_test.go` around bundle resolution and cache behavior.
6. Remove PATH-specific `espeak-ng` gating from `preflight/check.go`.
7. Update CLI TTS flow and `cattery status` output in `cmd/cattery/main.go`.
8. Update `server/server.go` startup checks to use the bundled runtime model.
9. Rewrite `THIRD_PARTY_NOTICES.md` to match redistribution reality.
10. Add `.github/workflows/build-espeak.yml` with the pinned upstream version.
11. Run formatting and verification.

## Verification

Automated:

- `go test ./...`
- targeted `phonemize` tests covering extraction/cache invalidation

Manual smoke checks per supported host:

- remove `~/.cattery/espeak-ng/`
- confirm `cattery status` reports bundled `espeak-ng`
- run `cattery "Hello"` with no system `espeak-ng` installed
- verify first run extracts `bin/espeak-ng` and `data/`
- verify second run reuses the cache
- corrupt `.bundle-version` and confirm re-extraction

Manual packaging checks:

- inspect resulting `go build` binary size delta
- run extracted `bin/espeak-ng --version` through the bundled path
- confirm Linux binaries do not fail on missing shared libc dependencies

## Risks

- Static Linux builds can be the hardest part; the maintainer script should be
  considered release tooling, not part of ordinary developer flows.
- If extraction replaces the target directory non-atomically, interrupted first
  runs can leave broken caches. Use staging + rename.
- If status/preflight still mention PATH `espeak-ng`, users will get conflicting
  guidance even after the bundle lands.
- Tests must stop assuming a host-installed `espeak-ng`, or CI will remain flaky
  across environments.

## Acceptance Mapping

| Issue requirement | Plan coverage |
| --- | --- |
| Static binaries for 4 platforms | `.github/workflows/build-espeak.yml` (manual trigger) produces checked-in `phonemize/bundle/*.tar.gz` |
| `go:embed` with build tags | `phonemize/embed_<platform>.go` files |
| Runtime extraction + caching | `phonemize/extract.go` with marker validation and atomic staging |
| `phonemize/` changes | `phonemize/espeak.go` resolves bundled runtime and sets `ESPEAK_DATA_PATH` |
| `preflight/` changes | remove PATH dependency; optionally replace with bundle sanity check |
| Third-party notices | rewrite `THIRD_PARTY_NOTICES.md` for LGPL/GPL redistribution |
| Build script | `scripts/build-espeak.sh` |
| `cattery status` shows bundled version | `cmd/cattery/main.go` status output update |
