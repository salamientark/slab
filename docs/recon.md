# Phase 0 Recon — Public Surfaces

Compiled 2026-04-23 from 3 parallel research agents. Source surfaces:
- docs.claude.com/en/docs/claude-code/hooks
- polybar wiki
- i3wm.org + xdotool man + empirical JSONL samples on this machine

---

## 1. Claude Code Hooks

### Events (7)

| Event | Trigger | Key input fields | Key output fields | Notes for i3-notch |
|-------|---------|------------------|-------------------|---------------------|
| `SessionStart` | session begin (matchers: startup/resume/clear/compact) | `sessionId`, `cwd` | `hookSpecificOutput.additionalContext` | capture pid (PPID), tty, CLAUDE_TMUX_* env |
| `UserPromptSubmit` | user message sent | prompt text | `additionalContext` | state → running |
| `PreToolUse` | before tool exec, BEFORE approval | `tool_name`, `tool_input`, `tool_use_id` | `permissionDecision: allow\|deny\|ask\|defer` | state → running, tool=X |
| `PermissionRequest` | user-facing approval dialog | tool context | `decision.behavior`, `updatedInput` | **BLOCKS on user** → state=awaiting |
| `PostToolUse` | after tool success | tool context | `additionalContext` | state → running |
| `Stop` | turn end | turn metrics | — | state → idle |
| `SessionEnd` | session close | — | — | unreliable; fall back to pid liveness |

### Protocol

- stdin: single JSON payload.
- stdout: JSON with `hookSpecificOutput` (if modifying behavior).
- exit 0 = OK, exit 2 = block, other = non-blocking error.

### Verified from real transcript

- `progress` lines in JSONL carry `data.type=hook_progress`, `data.hookEvent=<name>`.
- PermissionRequest not observed in sampled transcripts (likely behind permission mode).

---

## 2. JSONL Transcript Format

**Path:** `~/.claude/projects/<cwd-slug>/<session-uuid>.jsonl`

**Common fields:** `uuid, parentUuid, sessionId, timestamp (ISO 8601 UTC), type, cwd`

**Message types:**
- `user` — user message
- `assistant` — model output. Key field: `message.stop_reason` ∈ {`end_turn`, `tool_use`}
- `system` — local commands, subtype e.g. `local_command`, `turn_duration`
- `progress` — hook progress events
- `file-history-snapshot` — file tracking

**Status derivation from last line:**

| Last line | Status |
|-----------|--------|
| `assistant` + `stop_reason: end_turn` | idle |
| `assistant` + `stop_reason: tool_use` | running |
| `progress` + `hookEvent: PermissionRequest` | awaiting |
| `system` + `subtype: turn_duration` | idle |

**Atomicity:** newline-terminated, line-atomic. Safe to tail with `tail -F`.

**Absent:** no reliable session-end marker → use pid liveness probe.

---

## 3. polybar custom module

### Tail mode

```ini
[module/i3-notch]
type = custom/script
exec = bash -c 'while ! notchctl subscribe --format=polybar; do sleep 2; done'
tail = true
format = <label>
label = %output%
click-left = notchctl jump
click-right = notchctl expand
```

`tail = true` → each newline on stdout = immediate redraw. `interval` ignored.

### Format tags

- `%{F#RRGGBB}` fg, `%{B#RRGGBB}` bg, `%{F-}` reset
- `%{T2}` switch to `font-1` (nerd font)
- `%{A1:cmd:}txt%{A}` left-click, `A2` middle, `A3` right

### Go daemon → polybar

Go `fmt.Println` is line-buffered on tty, unbuffered through pipe. Safe. No flush needed.

### Restart

polybar does NOT respawn dead script. Wrap in `while ! cmd; do sleep 2; done` or run daemon via `systemd --user` with `Restart=on-failure` and have `notchctl subscribe` reconnect with backoff.

---

## 4. i3 terminal jump

### PID → con_id

i3 has NO `pid=` criteria. Two-step resolution:

```bash
PID=$1
XWIN=$(xdotool search --all --pid "$PID" | head -1)
CON_ID=$(i3-msg -t get_tree | jq ".. | select(.window? == $XWIN) | .id" | head -1)
i3-msg "[con_id=\"$CON_ID\"] focus"
```

Reliable even when claude runs shell → tmux → claude nesting, because xdotool reads `_NET_WM_PID` atom across all windows (doesn't traverse process tree).

### Edge cases handled by i3 `focus`

- Another workspace → auto-switches
- Scratchpad/minimized → unhides to current workspace
- Multi-monitor → transparent

### tmux pane focus

Capture in SessionStart hook env:

```bash
CLAUDE_TMUX_SESSION=$(tmux display-message -p '#{session_name}' 2>/dev/null)
CLAUDE_TMUX_WINDOW=$(tmux display-message -p '#{window_index}' 2>/dev/null)
CLAUDE_TMUX_PANE=$(tmux display-message -p '#{pane_index}' 2>/dev/null)
```

Replay after window focus:

```bash
[ -n "$CLAUDE_TMUX_SESSION" ] && \
  tmux select-window -t "$CLAUDE_TMUX_SESSION:$CLAUDE_TMUX_WINDOW" && \
  tmux select-pane -t "$CLAUDE_TMUX_SESSION:$CLAUDE_TMUX_WINDOW.$CLAUDE_TMUX_PANE"
```

### sway (v0.1 parity)

`swaymsg -t get_tree` exposes `pid` directly in node JSON → skip xdotool step. Same `[con_id=X] focus` syntax.

---

## Derived decisions

1. Daemon tracks sessions via `sessionId`. Map: `sessionId → {pid, cwd, tty, tmux..., status, lastTool, lastEventAt}`.
2. Session liveness via `kill(pid, 0)` probe every 2s; expire on ESRCH.
3. PermissionRequest hook is the approval blocker. Hook opens dunst via daemon, waits on response pipe, echoes decision JSON, exits 0.
4. `notchctl subscribe` = daemon-side formatter; emits polybar-ready lines directly.
