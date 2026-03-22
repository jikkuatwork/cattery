# Harnex — Multi-Agent Orchestration

Harnex wraps terminal AI agents (Claude Code, Codex, or any CLI tool) and
creates a local control plane for inter-agent discovery, messaging, and
coordination. Agents find each other automatically and communicate through a
message queue backed by per-session HTTP APIs.

**Use case**: Multiple agents work in parallel — one implements, one reviews,
one tests — while you watch in tmux windows or let them run in the background.

---

## Architecture

```
┌─ CLI Layer (harnex binary)
│  Commands: run, send, wait, stop, status, logs, pane, recipes, guide, skills
│
├─ Session Runtime
│  ├─ Session        — PTY lifecycle, HTTP API, registry/log management
│  ├─ SessionState   — Thread-safe state machine (prompt/busy/blocked/unknown)
│  ├─ Inbox          — Message queue with auto-delivery on prompt detection
│  ├─ ApiServer      — Bare HTTP server (one thread per connection)
│  └─ FileChangeHook — inotify file watcher for external event triggers
│
├─ Adapters (agent-specific prompt detection)
│  ├─ Codex   — Detects "›" prompt, 2s send wait
│  ├─ Claude  — Workspace trust prompt, Vim mode, "›" prompt
│  └─ Generic — Fallback for unknown CLIs
│
└─ Storage
   ├─ Registry   ~/.local/state/harnex/sessions/<hash>--<id>.json
   ├─ Exits      ~/.local/state/harnex/exits/<hash>--<id>.json
   └─ Transcripts ~/.local/state/harnex/output/<hash>--<id>.log
```

### How a session works

1. `harnex run codex --id worker` spawns `codex` under a pseudoterminal (PTY)
2. Allocates a deterministic port: `hash(repo_root + id) % 4000 + 43000`
3. Starts HTTP API on `127.0.0.1:<port>` with bearer token auth
4. Writes registry JSON to `~/.local/state/harnex/sessions/`
5. Background threads: PTY reader, state poller, inbox delivery, HTTP server

### State machine

```
              ┌──────────┐
   ┌──────────│  unknown  │──────────┐
   │          └──────────┘           │
   ▼                                 ▼
┌──────────┐   screen change   ┌──────────┐
│  prompt   │ ←───────────────  │   busy   │
│           │ ────────────────→ │          │
└──────────┘   screen change   └──────────┘
   │                                 │
   ▼                                 ▼
┌──────────┐                   ┌──────────┐
│ blocked  │                   │ blocked  │
└──────────┘                   └──────────┘
```

- **prompt** — Agent is at input prompt, ready for messages
- **busy** — Agent is processing (forced after injection)
- **blocked** — Needs user confirmation (trust prompt, etc.)
- **unknown** — Initial state or unparseable screen

### Message lifecycle

**Fast path** (queue empty + agent at prompt):
```
harnex send → POST /send → Inbox.enqueue()
  → deliver_now() → HTTP 200 + "delivered"
```

**Slow path** (agent busy or queue non-empty):
```
harnex send → POST /send → Inbox.enqueue()
  → HTTP 202 + "queued" + message_id
  → Delivery loop waits for prompt → dequeue → inject → force_busy
```

**Message statuses**: `queued`, `delivered`, `failed`, `expired`, `dropped`

Default inbox TTL: 120 seconds (configurable via `--inbox-ttl`).

---

## Commands

### `harnex run <cli>` — Start a wrapped session

```bash
harnex run codex --id worker
harnex run claude --id reviewer
harnex run codex --id impl-1 \
  --context "Implement the plan. Commit when done." \
  -- --cd ~/repo/worktree
```

| Flag | What it does |
|------|-------------|
| `--id ID` | Session name (default: random two-word) |
| `--description TEXT` | Short description stored in registry |
| `--no-tmux` | Run in current terminal (foreground) |
| `--detach` | Headless background (implies --no-tmux) |
| `--tmux-name NAME` | Custom tmux window name |
| `--context TEXT` | Initial prompt (session ID auto-prepended) |
| `--watch PATH` | Inject `file-change-hook` on file change |
| `--host HOST` | Bind host (default: 127.0.0.1) |
| `--port PORT` | Force specific API port |
| `--timeout SECS` | Wait budget for detached registration (default: 5) |
| `--inbox-ttl SECS` | Expire queued messages after N seconds (default: 120) |
| `-- [args...]` | Pass remaining args to the wrapped CLI |

**Tmux is the default mode.** Switch between windows with `Ctrl-b n/p/w`.

### `harnex send --id ID` — Inject a message

```bash
# Basic send
harnex send --id worker --message "implement plan 23"

# Atomic send+wait (recommended for orchestration)
harnex send --id worker --message "implement plan 23" \
  --wait-for-idle --timeout 600

# Fire and forget
harnex send --id worker --message "implement plan 23" --no-wait

# Just press Enter (no text)
harnex send --id worker --submit-only

# Type without pressing Enter
harnex send --id worker --message "partial text" --no-submit

# Force send even if busy (use sparingly)
harnex send --id worker --message "urgent" --force
```

| Flag | What it does |
|------|-------------|
| `--id ID` | Target session |
| `--message TEXT` | Message text (or positional args, or STDIN) |
| `--wait-for-idle` | Block until agent returns to prompt after processing |
| `--no-wait` | Return immediately (HTTP 202) |
| `--no-submit` | Type text without pressing Enter |
| `--submit-only` | Press Enter without typing text |
| `--force` | Bypass readiness checks (can corrupt input) |
| `--relay` / `--no-relay` | Control relay header |
| `--timeout SECS` | Overall wait budget (default: 120) |
| `--repo PATH` | Resolve session from repo root |
| `--cli CLI` | Filter by CLI type |
| `--verbose` | Print lookup details to stderr |

**Why `--wait-for-idle`?** Eliminates the race condition where separate
`send` + `wait --until prompt` sees the stale prompt state before the agent
starts working. One flag handles the entire lifecycle.

### `harnex pane --id ID` — Capture tmux screen

```bash
harnex pane --id worker --lines 25     # last 25 lines
harnex pane --id worker --lines 80     # larger snapshot
harnex pane --id worker --follow       # stream live
harnex pane --id worker --json         # JSON output
```

This reads the tmux window directly — no cooperation from the agent needed.
Works exactly like a human glancing at the terminal.

### `harnex wait --id ID` — Block until exit or state

```bash
harnex wait --id worker                        # wait for exit
harnex wait --id worker --until prompt          # wait for prompt
harnex wait --id worker --timeout 300           # with timeout
```

Exit code 124 on timeout.

### `harnex stop --id ID` — Graceful shutdown

```bash
harnex stop --id worker
```

Sends the adapter's native exit sequence (`/exit` + Enter for Claude/Codex).

### `harnex status` — List live sessions

```bash
harnex status              # current repo
harnex status --all        # all repos
harnex status --id worker --json  # detailed JSON
```

### `harnex logs --id ID` — Read transcript

```bash
harnex logs --id worker --lines 50
harnex logs --id worker --follow
```

---

## HTTP API

Each session exposes an HTTP API on `127.0.0.1:<port>`. All endpoints
require `Authorization: Bearer <token>` (token is in the registry JSON).

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/status` | GET | Session state, inbox stats |
| `/health` | GET | Alias for `/status` |
| `/send` | POST | Inject text into agent |
| `/stop` | POST | Send exit sequence |
| `/inbox` | GET | List pending messages |
| `/inbox` | DELETE | Clear all queued messages |
| `/inbox/:id` | DELETE | Drop a specific message |
| `/messages/:id` | GET | Check delivery status |

**POST /send body:**
```json
{
  "text": "implement plan A",
  "submit": true,
  "enter_only": false,
  "force": false
}
```

**Response codes**: 200 (delivered), 202 (queued), 409 (blocked/not ready)

---

## Relay Headers

When sending from inside a harnex session to a different session, harnex
automatically prepends a relay header:

```
[harnex relay from=claude id=supervisor at=2026-03-14T12:00:00+04:00]
<your message>
```

The peer sees this and knows the message came from another agent.

**Auto-relay rules:**
1. Disabled for `--submit-only`
2. Disabled with `--no-relay`
3. Forced with `--relay`
4. Disabled if not inside a harnex session (`$HARNEX_SESSION_ID` unset)
5. Default: auto-relay if target ≠ current session

---

## Environment Variables

**Set by harnex on agent startup:**

| Variable | Value |
|----------|-------|
| `HARNEX_SESSION_ID` | Unique session identifier (random hex) |
| `HARNEX_SESSION_CLI` | CLI type (`claude`, `codex`) |
| `HARNEX_ID` | User-friendly session ID |
| `HARNEX_SESSION_REPO_ROOT` | Git repo root |
| `HARNEX_DESCRIPTION` | Session description (if provided) |

**User-configurable:**

| Variable | Default | Purpose |
|----------|---------|---------|
| `HARNEX_HOST` | `127.0.0.1` | Default bind host |
| `HARNEX_BASE_PORT` | `43000` | Start of port range |
| `HARNEX_PORT_SPAN` | `4000` | Range width (43000-46999) |
| `HARNEX_STATE_DIR` | `~/.local/state/harnex` | Registry/logs location |
| `HARNEX_TRACE` | — | Set to "1" for full exception traces |

---

## Adapter Details

Each adapter implements prompt detection, message injection, and exit
sequences specific to the wrapped CLI.

### Codex adapter
- Launches with `--no-alt-screen` for inline output
- Detects prompt: lines containing "›", ">", or "⁄" prefix
- Multi-step submit: type text → delay 75ms (+ extra per KB) → Enter
- 2-second wait before send when `wait_for_sendable_state?` is true

### Claude adapter
- Detects workspace trust prompt: `"Quick safety check"` + `"Yes, I trust"`
- Detects confirmation: `"Enter to confirm"` + `"Esc to cancel"`
- Detects prompt: `"--INSERT--"` (insert mode) or `"› "` prefix
- Detects Vim normal mode: `"NORMAL"` or `"--NORMAL--"`
- Can handle `--submit-only` on trust prompt (special action)

### Generic adapter
- Fallback for unknown CLIs
- Can't detect prompt state (returns `input_ready: nil`)
- Basic submit: text + newline

---

## Practical Tips

### Long messages: use file references

PTY buffers and shell quoting break with long inline messages. Write the
task to a file and point to it:

```bash
cat > /tmp/task.md <<'EOF'
<detailed instructions>
EOF
harnex send --id worker --message "Read and execute /tmp/task.md"
```

If the task is already written down (plan file, issue), reference directly:

```bash
harnex send --id worker --message "Implement koder/plans/plan_16.md"
```

### Prefer `--wait-for-idle` over send + sleep + wait

```bash
# Bad: race condition
harnex send --id cx-1 --message "implement the plan"
sleep 5
harnex wait --id cx-1 --until prompt --timeout 600

# Good: atomic
harnex send --id cx-1 --message "implement the plan" \
  --wait-for-idle --timeout 600
```

### Tmux is the default

```bash
# Default: tmux window
harnex run codex --id worker

# Foreground (blocks terminal)
harnex run codex --id worker --no-tmux

# Headless/automated
harnex run codex --id worker --detach
```

### Check status before sending

```bash
harnex status                          # is the session alive?
harnex status --id worker --json       # detailed state
```

### Naming conventions

Use descriptive, prefixed IDs that show agent type and purpose:

| Pattern | Use |
|---------|-----|
| `cx-impl-NN` | Codex implementing issue NN |
| `cl-rev-NN` | Claude reviewing issue NN |
| `cx-fix-NN` | Codex fixing review findings for issue NN |
| `cl-reviewer` | Long-running Claude review session |
| `cx-worker` | General-purpose Codex worker |

---

## Troubleshooting

**Session not found**: Check `harnex status --all`. The session might be
registered to a different repo root. Use `--repo PATH` to resolve.

**Message stuck as queued**: Agent is busy or blocked. Check
`harnex pane --id <ID> --lines 10` — it might be waiting for user
confirmation (trust prompt, permission).

**Port conflict**: Ports are deterministic based on `repo_root + id`.
If a stale registry file exists, the port might appear taken. Check
`ls ~/.local/state/harnex/sessions/` and remove stale files.

**Delivery timeout**: Default is 120s. For long tasks, use
`--timeout 600` or higher. The timeout covers the entire lifecycle
(lookup + delivery + optional idle wait).
