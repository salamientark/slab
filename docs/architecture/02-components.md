# 02 — Components

## Layout

```text
cmd/notchd/main.go         daemon entrypoint (signal, ctx, paths)
cmd/notchctl/main.go       client CLI dispatch + jump
cmd/notchctl/polybar.go    polybar tail-line renderer + workspace lookup
cmd/notchctl/title.go      transcript-derived session title cache
internal/proto/proto.go    wire types: Event / Decision / Session / Snapshot
internal/daemon/daemon.go  socket server, FSM, pub/sub, liveness janitor
internal/daemon/approval.go dunstify driver
internal/daemon/bootstrap.go /proc scan to seed sessions on cold-start
hooks/hook.sh              dispatcher, jq-driven, called by Claude
hooks/session-topic.sh     UserPromptSubmit → topic file (haiku refine)
hooks/set-title.sh         /title slash command target
hooks/statusline.sh        Claude statusline renderer
scripts/raise-notch.sh     keep polybar bar above all clients
dist/polybar/notch.conf    bar definition + custom/script module
dist/systemd/i3-notch.service  user unit
install.sh                 idempotent installer (marker blocks)
```

## Component schema

```text
┌──────────────────────────────────────────────────────────────────────┐
│ notchd                                                               │
│ ┌──────────────────────────────────────────────────────────────────┐ │
│ │ Daemon struct                                                    │ │
│ │   socketPath  string                                             │ │
│ │   statePath   string                                             │ │
│ │   mu          sync.Mutex      ── single big lock                 │ │
│ │   sessions    map[sid]*Session                                   │ │
│ │   subs        map[chan Snapshot]struct{}                         │ │
│ │   pending     map[reqID]chan Decision                            │ │
│ └──────────────────────────────────────────────────────────────────┘ │
│        ▲              ▲                ▲                ▲            │
│   handleConn     livenessProbe   BootstrapFromProc  loadState        │
│   (per accept)   (2s tick)       (once on start)    (once on new)    │
│        │                                                             │
│   ┌────┴────────────┬───────────────┬────────────┐                   │
│   ▼                 ▼               ▼            ▼                   │
│ servePublisher  serveSubscriber  serveDecider  serveCommand          │
│ (publish role)  (subscribe role) (decide role) (list/ack)            │
└──────────────────────────────────────────────────────────────────────┘
```

### Constants (daemon.go:21)

| Name | Value | Why |
|------|-------|-----|
| `livenessProbeInterval` | 2s | dead-pid sweep + stale-status demotion |
| `approvalTimeout` | 30s | absolute cap on `PermissionRequest` block |
| `staleRunningTTL` | 5m | running session w/o events → idle |
| `staleAwaitingTTL` | 10m | dunstify timed out, daemon held awaiting → idle |

### Lock discipline

Every mutation of `sessions` / `subs` / `pending` holds `d.mu`. Snapshots are deep-copied (`*s` struct copy in `snapshotLocked`) so subscribers never see post-publish mutation. Subscriber chan is buffered 8; on overflow snapshot is **dropped silently** — the bar will catch up on the next event.

## Process roles summary

| Process | Lifetime | Owner |
|---------|----------|-------|
| `notchd` | systemd user unit, restart=on-failure | the user |
| `claude` CLI | per terminal, lives until user quits | the user |
| `hook.sh` | per event, ~ms | child of claude |
| `setsid -f bash …` (haiku refine) | up to 20s, detached | reparented to init |
| `notchctl subscribe` | as long as polybar bar runs | child of polybar |
| `polybar notch` | as long as i3 session | i3 startup |
| `dunstify` | per approval, up to 25s | child of notchd |
| `scripts/raise-notch.sh` | i3 session | i3 startup (optional) |
