# i3-notch — Architecture Review

Full walk-through of how `i3-notch` works. Each doc one concern.

## Index

| File | Concern |
|------|---------|
| [01-overview.md](01-overview.md) | Big picture, goals, top-level dataflow |
| [02-components.md](02-components.md) | Per-component schema + responsibilities |
| [03-protocol.md](03-protocol.md) | Wire protocol on the unix socket |
| [04-state-machines.md](04-state-machines.md) | Session FSM, approval FSM, daemon lifecycle |
| [05-hooks.md](05-hooks.md) | Claude Code hook surface and dispatcher |
| [06-rendering.md](06-rendering.md) | Polybar tail-line renderer |
| [07-jump.md](07-jump.md) | PID → X11 window → i3 con_id resolution |
| [08-install.md](08-install.md) | Install layout, systemd unit, polybar wiring |
| [09-failure-modes.md](09-failure-modes.md) | Known traps, leaks, recovery paths |

## Quick map

```
Claude Code CLI (one per terminal)
   │ stdin JSON per hook event
   ▼
hooks/hook.sh ── enrich ──▶ notchctl publish ──▶ unix socket
                                                     │
                                            notchd (Go daemon)
                                                     │ pub/sub
                          ┌──────────────────────────┼──────────────────────────┐
                          ▼                          ▼                          ▼
              notchctl subscribe          dunstify (approval)         notchctl jump
                  --format polybar         (Allow / Deny)             xdotool + i3-msg
                  → polybar tail                                      → focus terminal
```

Socket: `$XDG_RUNTIME_DIR/i3-notch.sock`. State: `$XDG_CACHE_HOME/i3-notch/state.json`.
