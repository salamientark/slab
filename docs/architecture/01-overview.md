# 01 — Overview

## Purpose

Status-notch for AI coding agents on i3 + polybar + dunst. Clean-room Linux take on VibeIsland. Driven only by public Claude Code hook surfaces — no binary RE.

## Three user-visible features

1. **Badge** — top polybar bar shows nerd-font glyph + per-session chips (idle / running / awaiting / done-unseen).
2. **Approval** — `PermissionRequest` → `dunstify` Allow/Deny; decision fed back to Claude.
3. **Jump** — click chip → focus exact terminal (+ tmux pane).

## Process model

```
┌──────────────────────────────────────────────────────────────────────────┐
│ X11 / i3 session                                                         │
│                                                                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                    │
│  │ alacritty #1 │  │ alacritty #2 │  │ alacritty #N │                    │
│  │  └─ claude   │  │  └─ claude   │  │  └─ claude   │  fork hook.sh per  │
│  │      CLI     │  │      CLI     │  │      CLI     │  event             │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘                    │
│         │ hook stdout     │                 │                            │
│         ▼                 ▼                 ▼                            │
│              notchctl publish (unix socket client, transient)            │
│                            │                                             │
│                            ▼                                             │
│                  ┌────────────────────┐                                  │
│                  │      notchd        │  ← long-running, systemd --user  │
│                  │  - session map     │                                  │
│                  │  - subscribers     │                                  │
│                  │  - pending approv. │                                  │
│                  └─────────┬──────────┘                                  │
│        ┌───────────────────┼───────────────────┐                         │
│        ▼                   ▼                   ▼                         │
│ notchctl subscribe    dunstify              notchctl jump                │
│ (polybar tail=true)   (Allow/Deny)          (xdotool + i3-msg)           │
└──────────────────────────────────────────────────────────────────────────┘
```

## Why a daemon (not flat-files)

See `docs/adr/001-daemon-vs-flatfile.md`. Summary:

- Hooks fire bursts at >10 Hz; a tail-watcher would race.
- `PermissionRequest` is **blocking** — hook stdout is the decision. Needs request/response, not file mutation.
- Multi-session aggregate (worst-of) is cheap in-process, painful as file scan.
- One writer (notchd), many readers (polybar, jump, list) → unix socket is the natural fit.

## Trust boundaries

| Boundary | Crossing | Validation |
|----------|----------|------------|
| Claude → hook.sh | stdin JSON, env vars | `jq` parse, fields nullable |
| hook.sh → notchd | line-delim JSON over `unix:0600` socket | per-line `json.Unmarshal`, header role check |
| notchd → dunstify | argv (tool name, cwd) | HTML-escape minimum set in `approval.go:html()` |
| polybar → notchctl jump | `%{A1:cmd:}` clickable | argv only, no shell interpolation |

Socket is `0600` and lives in `$XDG_RUNTIME_DIR` → single-user trust domain.
