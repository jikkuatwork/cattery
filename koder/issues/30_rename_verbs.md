# 30 — Rename speak/listen to tts/stt

## Status: done (plan 35)
## Priority: P2
## Depends on: nothing (standalone refactor)

## Problem

The `speak`/`listen` verb naming across CLI commands, API paths, and package
names adds a mental translation step. `tts`/`stt`/`llm`/`lvm` are shorter,
unambiguous, and universally understood.

## Scope

- CLI subcommands: `cattery speak` → `cattery tts`, `cattery listen` → `cattery stt`
- API paths: `/v1/speak` → `/v1/tts` (alias already exists), `/v1/listen` → `/v1/stt`
- Go packages: `speak/` → `tts/`, `listen/` → `stt/`
- Engine interfaces: `speak.Engine` → `tts.Engine`, `listen.Engine` → `stt.Engine`
- Server pool names, log messages, status response fields
- Future modalities follow the pattern: `llm/`, `lvm/`

## Notes

- `/v1/tts` already exists as an alias for `/v1/speak` — half-migrated
- This is a breaking change; do it in one PR, not incrementally
- The "cattery = cats, verbs = cats" metaphor was for branding — code should use the cleaner terms
- Keep old aliases for one release cycle if we ever do versioned releases
