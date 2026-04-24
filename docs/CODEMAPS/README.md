# i3-notch CODEMAP

Clean-room Linux port of VibeIsland. Status-notch for Claude Code sessions on i3 + polybar + dunst. Driven by Claude Code hook surface — no binary RE.

## Component map

```
Claude CLI ──hook json stdin──▶ hooks/hook.sh ──unix socket──▶ notchd
                                                                  │
                                           ┌──────────────────────┼──────────────────────┐
                                           ▼                      ▼                      ▼
                                      polybar tail           dunstify prompt         jump cmd
                                  (notchctl subscribe)       (approval flow)     (xdotool+i3-msg+tmux)
```

Socket: `$XDG_RUNTIME_DIR/i3-notch.sock` (`/run/user/$UID/i3-notch.sock`). Mode 0600.
Protocol: line-delimited JSON. Roles: `publish`, `subscribe`, `decide`, `command`. Defined in `internal/proto/proto.go`.
State: keyed by Claude `session_id`. Liveness probe every 2s prunes dead PIDs.

## File map

| Path | Role |
|------|------|
| `cmd/notchd/main.go` | Daemon entry. Thin wrapper: resolve socket path, construct `daemon.Daemon`, `Run(ctx)`. |
| `cmd/notchctl/main.go` | Client CLI. Subcommands: `publish` (hook bridge), `subscribe` (polybar tail w/ `--format polybar`), `jump <sid>`, `decide <rid> allow|deny`, `list`. |
| `internal/proto/proto.go` | Wire types: `Event`, `Decision`, `Session`, `Snapshot`, `Status` enum (`idle|running|awaiting|ended`). |
| `internal/daemon/daemon.go` | Socket server, per-conn role dispatch, sub fan-out, liveness probe, aggregate worst-of status compute. |
| `internal/daemon/approval.go` | PermissionRequest blocking flow. Register pending by `request_id`, spawn `dunstify`, await `decide` or 30s timeout. |
| `hooks/hook.sh` | Bash+jq hook dispatcher. One invocation per hook event. Enriches with PPID/TTY/tmux, publishes to daemon, emits `hookSpecificOutput` on PermissionRequest. |
| `scripts/raise-notch.sh` | Background helper to raise polybar notch bar on top. |
| `dist/systemd/i3-notch.service` | User unit. Runs `notchd`. |
| `dist/arch/PKGBUILD` | Arch package. |
| `docs/adr/001-daemon-vs-flatfile.md` | Why daemon beats parsing `~/.claude/projects/**/*.jsonl`. |
| `docs/recon.md` | Hook surface reconnaissance notes. |
| `.claude/settings.json` | Project-scoped hooks for dogfooding. |

## Key types (proto)

- `Event{kind, session_id, pid, cwd, tty, tool_name, tool_use_id, tmux_*, timestamp, request_id}` — hook → daemon.
- `Decision{request_id, allow, reason}` — user → daemon → hook (stdout `hookSpecificOutput`).
- `Session{id, pid, cwd, tty, tmux_*, status, last_tool, last_event_at, started_at}` — daemon state.
- `Snapshot{trigger_session, sessions, agg_status, agg_count}` — daemon → subscribers.

## Build

```
go build -o bin/notchd   ./cmd/notchd
go build -o bin/notchctl ./cmd/notchctl
```

Module: `github.com/jiliac/i3-notch`.

## Hook events wired

`SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PermissionRequest`, `Stop`, `SessionEnd`. Only `PermissionRequest` is blocking (needs Decision back via stdout).

## Aggregate status rule

worst-of across live sessions: `awaiting > running > idle > ended`. Badge glyph + count derived from aggregate.

## Jump flow

`notchctl jump <sid>` → daemon looks up `Session.{tty, tmux_*}` → `xdotool search --name` terminal w/ that tty → `i3-msg [con_id=...] focus` → if tmux fields set, `tmux select-pane -t sess:win.pane`.

## Known touchpoints outside repo

- `~/.config/polybar/config.ini` — `[bar/notch]`, `[module/i3notch]`. Backup: `~/.config/polybar/config.ini.bak-20260423-172619`.
- `~/.config/polybar/launch.sh` — launches notch bar + `raise-notch.sh`.
- `~/.config/systemd/user/i3-notch.service` — enabled; `systemctl --user disable --now i3-notch` to stop.
- `~/.claude/settings.json` — hooks block installed. Backup: `~/.claude/settings.json.bak-*`.

Revert order: restore both backups → disable systemd unit → delete repo.

## Open design question

Make i3-notch opt-in (deactivated by default, `i3-notch` launches). Requires:
1. Disable systemd auto-start.
2. Add `i3-notch` launcher CLI (start/stop/toggle/status).
3. Harden `hooks/hook.sh` — early-exit 0 when socket missing (else PermissionRequest defaults to deny).
